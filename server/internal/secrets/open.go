package secrets

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/term"
)

// FileName is the encrypted fallback file in the data dir.
const FileName = "secrets.enc"

// VaultFileName is the vault in the data dir used with the keychain, and VaultKeyName the only
// keychain item: the 32-byte key that seals it.
const (
	VaultFileName = "vault.enc"
	VaultKeyName  = "vault.key"
)

const (
	minPassphraseLen = 8
	maxAttempts      = 3
)

// ErrNoTerminal is returned when the keychain is unavailable and stdin cannot prompt.
var ErrNoTerminal = errors.New("no keychain is available and stdin is not a terminal, so the passphrase for " +
	FileName + " cannot be asked; run prdash from a terminal")

// Prompter reads a passphrase. confirm asks for it twice, as when the file is created.
type Prompter func(confirm bool) ([]byte, error)

// TerminalPrompter reads passphrases from in without echo. It fails with ErrNoTerminal when in
// is not a terminal.
func TerminalPrompter(in *os.File, out io.Writer) Prompter {
	return func(confirm bool) ([]byte, error) {
		fd := int(in.Fd()) //nolint:gosec // file descriptors fit in int
		if !term.IsTerminal(fd) {
			return nil, ErrNoTerminal
		}
		read := func(msg string) ([]byte, error) {
			fmt.Fprint(out, msg)
			b, err := term.ReadPassword(fd)
			fmt.Fprintln(out)
			return b, err
		}
		if !confirm {
			return read("Passphrase for " + FileName + ": ")
		}
		fmt.Fprintf(out, "No OS keychain is available. Secrets will be kept in an encrypted file.\n")
		first, err := read("Choose a passphrase: ")
		if err != nil {
			return nil, err
		}
		again, err := read("Repeat the passphrase: ")
		if err != nil {
			return nil, err
		}
		if string(first) != string(again) {
			return nil, errors.New("the passphrases do not match")
		}
		return first, nil
	}
}

// Open picks the backend: the OS keychain when a probe item can be written, read and deleted,
// otherwise the encrypted file in dir, with the passphrase from prompt. It returns the store and
// the backend name, "keychain" or "file". With the keychain, the store is the vault file
// VaultFileName in dir, sealed with the key kept in the keychain (see openKeychainVault).
func Open(dir string, rnd io.Reader, prompt Prompter, log *slog.Logger) (Store, string, error) {
	kc := Keychain{}
	err := kc.Probe()
	if err == nil {
		v, err := openKeychainVault(dir, kc, rnd, log)
		if err != nil {
			return nil, "", err
		}
		return v, "keychain", nil
	}
	log.Info("keychain unavailable, using the encrypted file", "reason", err.Error())

	path := filepath.Join(dir, FileName)
	exists := Exists(path)
	for attempt := 1; ; attempt++ {
		pass, err := prompt(!exists)
		if err != nil {
			return nil, "", err
		}
		if !exists && len(pass) < minPassphraseLen {
			return nil, "", fmt.Errorf("the passphrase must have at least %d characters", minPassphraseLen)
		}
		f, err := OpenFile(path, pass, rnd)
		if errors.Is(err, ErrWrongPassphrase) && attempt < maxAttempts {
			fmt.Fprintln(os.Stderr, "Wrong passphrase, try again.")
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return f, "file", nil
	}
}
