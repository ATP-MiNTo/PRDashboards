package secrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

// limitedKeyring models the macOS keychain: a stored value over 3000 bytes is refused.
type limitedKeyring struct {
	mu   sync.Mutex
	m    map[string]string
	sets []string
	gets map[string]int
}

func (k *limitedKeyring) Get(_, user string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.gets[user]++
	v, ok := k.m[user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (k *limitedKeyring) Set(_, user, pass string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(pass) > 3000 {
		return keyring.ErrSetDataTooBig
	}
	k.sets = append(k.sets, user)
	k.m[user] = pass
	return nil
}

func (k *limitedKeyring) Delete(_, user string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.m[user]; !ok {
		return keyring.ErrNotFound
	}
	delete(k.m, user)
	return nil
}

func useLimitedKeyring(t *testing.T) *limitedKeyring {
	t.Helper()
	k := &limitedKeyring{m: map[string]string{}, gets: map[string]int{}}
	old := kr
	kr = k
	t.Cleanup(func() { kr = old })
	return k
}

// names returns the item names currently in the fake keychain.
func (k *limitedKeyring) names() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for n := range k.m {
		out = append(out, n)
	}
	return out
}

func TestVaultHoldsLargeTokenAndKeychainOnlyTheKey(t *testing.T) {
	k := useLimitedKeyring(t)
	dir := t.TempDir()
	big := bytes.Repeat([]byte("eyJhbGciOi."), 2000) // 22 KB
	s, kind, err := Open(dir, &testutil.Rand{}, nil, discard)
	if err != nil || kind != "keychain" {
		t.Fatalf("%v %v", kind, err)
	}
	if err := s.Set(AtlassianToken, big); err != nil {
		t.Fatal(err)
	}
	s2, _, err := Open(dir, &testutil.Rand{}, nil, discard)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(AtlassianToken)
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("reopened: %d bytes, %v", len(got), err)
	}
	for _, n := range k.sets {
		if n != VaultKeyName && n != "probe" {
			t.Fatalf("keychain received %q", n)
		}
	}
	if names := k.names(); len(names) != 1 || names[0] != VaultKeyName {
		t.Fatalf("keychain holds %v", names)
	}
	if k.gets[VaultKeyName] != 2 { // one Get per Open
		t.Fatalf("vault.key read %d times for 2 opens", k.gets[VaultKeyName])
	}
	key, err := Keychain{}.Get(VaultKeyName)
	if err != nil || len(key) != 32 {
		t.Fatalf("vault key: %d bytes, %v", len(key), err)
	}
	file, _ := os.ReadFile(filepath.Join(dir, VaultFileName))
	if bytes.Contains(file, []byte("eyJhbGciOi")) || bytes.Contains(file, []byte(AtlassianToken)) {
		t.Fatal("plaintext in the vault")
	}
}

func TestKeychainRefusesLargeValueItself(t *testing.T) {
	useLimitedKeyring(t)
	if err := (Keychain{}).Set("x", make([]byte, 5000)); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}

func TestMigrationMovesLegacyItems(t *testing.T) {
	k := useLimitedKeyring(t)
	kc := Keychain{}
	legacy := map[string]string{
		SessionKey: "0123456789abcdef0123456789abcdef", GitHubToken: `{"token":"t"}`,
		GitHubClient: `{"clientId":"a"}`, AtlassianClient: `{"clientId":"b"}`,
	}
	for n, v := range legacy {
		if err := kc.Set(n, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	s, _, err := Open(dir, &testutil.Rand{}, nil, discard)
	if err != nil {
		t.Fatal(err)
	}
	for n, v := range legacy {
		if got, err := s.Get(n); err != nil || string(got) != v {
			t.Fatalf("%s: %q %v", n, got, err)
		}
	}
	if names := k.names(); len(names) != 1 || names[0] != VaultKeyName {
		t.Fatalf("legacy items left in the keychain: %v", names)
	}
	before, _ := os.ReadFile(filepath.Join(dir, VaultFileName))
	s2, _, err := Open(dir, &testutil.Rand{}, nil, discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s2.Get(SessionKey); string(got) != legacy[SessionKey] {
		t.Fatal("lost on second open")
	}
	after, _ := os.ReadFile(filepath.Join(dir, VaultFileName))
	if !bytes.Equal(before, after) {
		t.Fatal("second open rewrote the vault")
	}
}

func TestMigrationKeepsVaultValueAndDeletesKeychainCopy(t *testing.T) {
	k := useLimitedKeyring(t)
	dir := t.TempDir()
	s, _, _ := Open(dir, &testutil.Rand{}, nil, discard)
	_ = s.Set(GitHubToken, []byte("vault copy"))
	_ = Keychain{}.Set(GitHubToken, []byte("stale keychain copy"))
	s2, _, err := Open(dir, &testutil.Rand{}, nil, discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s2.Get(GitHubToken); string(got) != "vault copy" {
		t.Fatalf("got %q", got)
	}
	if len(k.names()) != 1 {
		t.Fatalf("keychain holds %v", k.names())
	}
}

type failingDelete struct{ *limitedKeyring }

func (f failingDelete) Delete(_, user string) error {
	if user == GitHubToken {
		return errors.New("denied")
	}
	return f.limitedKeyring.Delete("", user)
}

func TestMigrationDeleteFailureDoesNotFailStartup(t *testing.T) {
	k := useLimitedKeyring(t)
	_ = Keychain{}.Set(GitHubToken, []byte("t"))
	kr = failingDelete{k}
	s, _, err := Open(t.TempDir(), &testutil.Rand{}, nil, discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(GitHubToken); string(got) != "t" {
		t.Fatal("not migrated")
	}
}

func TestVaultWithoutKeyIsAnErrorAndUntouched(t *testing.T) {
	k := useLimitedKeyring(t)
	dir := t.TempDir()
	s, _, _ := Open(dir, &testutil.Rand{}, nil, discard)
	_ = s.Set("a", []byte("v"))
	path := filepath.Join(dir, VaultFileName)
	before, _ := os.ReadFile(path)
	_ = k.Delete("", VaultKeyName)
	if _, _, err := Open(dir, &testutil.Rand{}, nil, discard); !errors.Is(err, ErrVaultKeyMissing) {
		t.Fatalf("got %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("vault changed")
	}
	if len(k.names()) != 0 {
		t.Fatalf("a new key was created: %v", k.names())
	}
}

func TestVaultWrongKeyTamperAndForeignFiles(t *testing.T) {
	k := useLimitedKeyring(t)
	dir := t.TempDir()
	s, _, _ := Open(dir, &testutil.Rand{}, nil, discard)
	_ = s.Set("a", []byte("v"))
	path := filepath.Join(dir, VaultFileName)
	orig, _ := os.ReadFile(path)

	other := bytes.Repeat([]byte{7}, 32)
	if _, err := OpenVault(path, other, &testutil.Rand{}); !errors.Is(err, ErrWrongVaultKey) {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := OpenVault(path, other[:5], &testutil.Rand{}); err == nil {
		t.Fatal("short key accepted")
	}
	for name, idx := range map[string]int{"nonce": 8, "ciphertext": vaultHeaderLen + 1, "tag": len(orig) - 1} {
		mod := append([]byte(nil), orig...)
		mod[idx] ^= 1
		_ = os.WriteFile(path, mod, 0o600)
		if _, _, err := Open(dir, &testutil.Rand{}, nil, discard); !errors.Is(err, ErrWrongVaultKey) {
			t.Errorf("%s: %v", name, err)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, mod) {
			t.Errorf("%s: file was replaced", name)
		}
	}
	badVersion := append([]byte(nil), orig...)
	badVersion[4] = 9
	for name, data := range map[string][]byte{
		"garbage": bytes.Repeat([]byte{1}, 200), "short": []byte("PRDV"), "version": badVersion,
	} {
		_ = os.WriteFile(path, data, 0o600)
		if _, _, err := Open(dir, &testutil.Rand{}, nil, discard); err == nil {
			t.Errorf("%s accepted", name)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
			t.Errorf("%s: file was replaced", name)
		}
	}
	_ = k
}

func TestPassphraseFileAndVaultAreNotInterchangeable(t *testing.T) {
	dir := t.TempDir()
	pp := filepath.Join(dir, FileName)
	f, _ := openFile(pp, []byte("correct horse"), &testutil.Rand{}, fastKDF)
	_ = f.Set("a", []byte("v"))
	if _, err := OpenVault(pp, bytes.Repeat([]byte{1}, 32), &testutil.Rand{}); !errors.Is(err, ErrNotVaultFile) {
		t.Fatalf("vault open of passphrase file: %v", err)
	}
	vp := filepath.Join(dir, VaultFileName)
	if _, err := OpenVault(vp, bytes.Repeat([]byte{1}, 32), &testutil.Rand{}); err != nil {
		t.Fatal(err)
	}
	if _, err := openFile(vp, []byte("correct horse"), &testutil.Rand{}, fastKDF); !errors.Is(err, ErrNotSecretsFile) {
		t.Fatalf("passphrase open of vault: %v", err)
	}
}

func TestVaultPermsAndFreshNonce(t *testing.T) {
	path := filepath.Join(t.TempDir(), VaultFileName)
	v, err := OpenVault(path, bytes.Repeat([]byte{3}, 32), &testutil.Rand{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for range 3 {
		if err := v.Set("a", []byte("same")); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(path)
		id := string(raw[5:vaultHeaderLen])
		if seen[id] {
			t.Fatal("nonce reused")
		}
		seen[id] = true
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
	fresh, err := OpenVault(filepath.Join(t.TempDir(), VaultFileName), bytes.Repeat([]byte{3}, 32), &testutil.Rand{})
	if err != nil {
		t.Fatal(err)
	}
	storeContract(t, fresh)
}

func TestSetManyWriteFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, VaultFileName)
	v, _ := OpenVault(path, bytes.Repeat([]byte{3}, 32), &testutil.Rand{})
	_ = v.Set("a", []byte("old"))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := v.setMany(map[string][]byte{"a": []byte("new"), "b": []byte("x")}); err == nil {
		t.Fatal("expected write error")
	}
	if got, _ := v.Get("a"); string(got) != "old" {
		t.Fatalf("a: %q", got)
	}
	if _, err := v.Get("b"); !errors.Is(err, ErrNotFound) {
		t.Fatal("b kept after failed write")
	}
}

func TestMigrationWriteFailureKeepsKeychainItems(t *testing.T) {
	k := useLimitedKeyring(t)
	_ = Keychain{}.Set(GitHubToken, []byte("t"))
	dir := t.TempDir()
	v, _ := OpenVault(filepath.Join(dir, VaultFileName), bytes.Repeat([]byte{3}, 32), &testutil.Rand{})
	_ = os.RemoveAll(dir)
	migrateLegacy(v, Keychain{}, discard)
	if _, err := (Keychain{}).Get(GitHubToken); err != nil {
		t.Fatalf("keychain item deleted after a failed vault write: %v", err)
	}
	_ = k
}
