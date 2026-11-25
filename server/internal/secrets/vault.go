package secrets

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
)

// ErrVaultKeyMissing is returned when the vault file exists but its key is gone from the keychain.
var ErrVaultKeyMissing = errors.New("the vault key is missing from the OS keychain")

// legacyItems are the items earlier versions kept in the keychain itself.
var legacyItems = []string{GitHubClient, GitHubToken, AtlassianClient, AtlassianToken, SessionKey}

// openKeychainVault opens the vault in dir, sealed with the key from the keychain. It creates
// the key and the vault on first use, and moves items left in the keychain by earlier versions
// into the vault.
func openKeychainVault(dir string, kc Keychain, rnd io.Reader, log *slog.Logger) (*File, error) {
	path := filepath.Join(dir, VaultFileName)
	key, err := kc.Get(VaultKeyName)
	switch {
	case errors.Is(err, ErrNotFound):
		if Exists(path) {
			return nil, fmt.Errorf("%w: %s cannot be opened and was left untouched; restore the %q item of service %q, "+
				"or move the file away to start with empty secrets and sign in again",
				ErrVaultKeyMissing, path, VaultKeyName, Service)
		}
		key = make([]byte, keyLen)
		if _, err := io.ReadFull(rnd, key); err != nil {
			return nil, fmt.Errorf("create vault key: %w", err)
		}
		if err := kc.Set(VaultKeyName, key); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	}
	v, err := OpenVault(path, key, rnd)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	migrateLegacy(v, kc, log)
	return v, nil
}

// migrateLegacy copies legacy keychain items the vault lacks into it with one write, then
// deletes the keychain copies. Failures leave the keychain items in place and are logged, so
// the next start tries again.
func migrateLegacy(v *File, kc Keychain, log *slog.Logger) {
	var found []string
	moved := map[string][]byte{}
	for _, name := range legacyItems {
		val, err := kc.Get(name)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			log.Warn("keychain item not migrated", "item", name, "err", err.Error())
			continue
		}
		found = append(found, name)
		if _, err := v.Get(name); errors.Is(err, ErrNotFound) {
			moved[name] = val
		}
	}
	if len(found) == 0 {
		return
	}
	if len(moved) > 0 {
		if err := v.setMany(moved); err != nil {
			log.Warn("keychain items not migrated to the vault", "err", err.Error())
			return
		}
	}
	for _, name := range found {
		if err := kc.Delete(name); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warn("migrated keychain item not deleted", "item", name, "err", err.Error())
		}
	}
	log.Info("moved keychain items into the vault", "count", len(moved), "items", found)
}
