package secrets

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

var fastKDF = kdfParams{time: 1, memoryKiB: 64, threads: 1}

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func storeContract(t *testing.T, s Store) {
	t.Helper()
	if _, err := s.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	if err := s.Delete("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
	if err := s.Set("a", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("a", []byte("two\x00\xff")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("a")
	if err != nil || string(got) != "two\x00\xff" {
		t.Fatalf("get: %q %v", got, err)
	}
	got[0] = 'X'
	if again, _ := s.Get("a"); string(again) != "two\x00\xff" {
		t.Fatal("Get returned shared memory")
	}
	if err := s.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
}

func TestMemory(t *testing.T) { storeContract(t, NewMemory()) }

func TestKeychain(t *testing.T) {
	keyring.MockInit()
	storeContract(t, Keychain{})
	if err := (Keychain{}).Probe(); err != nil {
		t.Fatal(err)
	}
}

func TestKeychainErrors(t *testing.T) {
	keyring.MockInitWithError(errors.New("locked"))
	t.Cleanup(keyring.MockInit)
	k := Keychain{}
	if _, err := k.Get("a"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
	if err := k.Set("a", bytes.Repeat([]byte{1}, 4000)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large set: %v", err)
	}
	if k.Set("a", []byte("x")) == nil || k.Delete("a") == nil || k.Probe() == nil {
		t.Fatal("expected errors")
	}
}

func TestFileContract(t *testing.T) {
	f, err := openFile(filepath.Join(t.TempDir(), FileName), []byte("correct horse"), &testutil.Rand{}, fastKDF)
	if err != nil {
		t.Fatal(err)
	}
	storeContract(t, f)
}

func TestFileRoundTripAndPerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, err := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("github.token", []byte("ghp_secret")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte("ghp_secret")) || bytes.Contains(raw, []byte("github.token")) {
		t.Fatal("plaintext in the file")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode().Perm())
		}
	}
	g, err := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := g.Get("github.token"); err != nil || string(v) != "ghp_secret" {
		t.Fatalf("reopened: %q %v", v, err)
	}
}

func TestFreshSaltAndNonceEveryWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF)
	seen := map[string]bool{}
	for range 3 {
		if err := f.Set("a", []byte("same")); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(path)
		id := string(raw[14:headerLen])
		if seen[id] {
			t.Fatal("salt and nonce reused")
		}
		seen[id] = true
	}
}

func TestFileWrongPassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if _, err := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF); err != nil {
		t.Fatal(err)
	}
	if _, err := openFile(path, []byte("wrong"), &testutil.Rand{}, fastKDF); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("got %v", err)
	}
}

func TestFileTamper(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF)
	_ = f.Set("a", []byte("value"))
	orig, _ := os.ReadFile(path)
	for name, idx := range map[string]int{"header salt": 20, "header nonce": 32, "ciphertext": headerLen + 2, "tag": len(orig) - 1} {
		mod := append([]byte(nil), orig...)
		mod[idx] ^= 1
		if err := os.WriteFile(path, mod, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF); !errors.Is(err, ErrWrongPassphrase) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestFileRejectsForeignFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF)
	_ = f.Set("a", []byte("v"))
	orig, _ := os.ReadFile(path)
	badVersion := append([]byte(nil), orig...)
	badVersion[4] = 9
	badParams := append([]byte(nil), orig...)
	badParams[9] = 0xff // memory far over the limit
	cases := map[string][]byte{"short": []byte("PRDS"), "magic": bytes.Repeat([]byte{1}, 200), "params": badParams}
	for name, data := range cases {
		_ = os.WriteFile(path, data, 0o600)
		if _, err := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF); !errors.Is(err, ErrNotSecretsFile) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	_ = os.WriteFile(path, badVersion, 0o600)
	if _, err := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF); err == nil {
		t.Error("unknown version accepted")
	}
}

func TestFileWriteFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	f, _ := openFile(path, []byte("correct horse"), &testutil.Rand{}, fastKDF)
	_ = f.Set("a", []byte("old"))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("a", []byte("new")); err == nil {
		t.Fatal("expected write error")
	}
	if v, _ := f.Get("a"); string(v) != "old" {
		t.Fatalf("not rolled back: %q", v)
	}
	if err := f.Set("b", []byte("x")); err == nil {
		t.Fatal("expected write error")
	}
	if _, err := f.Get("b"); !errors.Is(err, ErrNotFound) {
		t.Fatal("new item kept after failed write")
	}
	if err := f.Delete("a"); err == nil {
		t.Fatal("expected write error")
	}
	if v, _ := f.Get("a"); string(v) != "old" {
		t.Fatal("delete not rolled back")
	}
}

func TestOpenFileUnreadable(t *testing.T) {
	if _, err := OpenFile(t.TempDir(), []byte("x"), &testutil.Rand{}); err == nil {
		t.Fatal("expected error for a directory")
	}
}

func TestDefaultKDFRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, err := OpenFile(path, []byte("correct horse"), &testutil.Rand{})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Set("a", []byte("v"))
	raw, _ := os.ReadFile(path)
	if raw[13] != 4 || raw[5+3] != 3 {
		t.Fatalf("header does not carry the default Argon2id parameters: %v", raw[:14])
	}
	if _, err := OpenFile(path, []byte("correct horse"), &testutil.Rand{}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenPicksKeychain(t *testing.T) {
	keyring.MockInit()
	s, kind, err := Open(t.TempDir(), &testutil.Rand{}, nil, discard)
	if err != nil || kind != "keychain" {
		t.Fatalf("%v %v", kind, err)
	}
	if _, ok := s.(*File); !ok {
		t.Fatalf("%T", s)
	}
}

func fallbackToFile(t *testing.T) {
	t.Helper()
	keyring.MockInitWithError(errors.New("no secret service"))
	t.Cleanup(keyring.MockInit)
}

func TestOpenFallbackCreateAndReopen(t *testing.T) {
	fallbackToFile(t)
	dir := t.TempDir()
	var confirms []bool
	prompt := func(confirm bool) ([]byte, error) {
		confirms = append(confirms, confirm)
		return []byte("long enough"), nil
	}
	s, kind, err := Open(dir, &testutil.Rand{}, prompt, discard)
	if err != nil || kind != "file" {
		t.Fatalf("%v %v", kind, err)
	}
	if err := s.Set("a", []byte("v")); err != nil {
		t.Fatal(err)
	}
	s2, _, err := Open(dir, &testutil.Rand{}, prompt, discard)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s2.Get("a"); string(v) != "v" {
		t.Fatal("not persisted")
	}
	if len(confirms) != 2 || !confirms[0] || confirms[1] {
		t.Fatalf("confirm flags %v: want create with confirmation, then plain", confirms)
	}
}

func TestOpenFallbackErrors(t *testing.T) {
	fallbackToFile(t)
	dir := t.TempDir()
	short := func(bool) ([]byte, error) { return []byte("short"), nil }
	if _, _, err := Open(dir, &testutil.Rand{}, short, discard); err == nil {
		t.Fatal("short passphrase accepted")
	}
	noTTY := func(bool) ([]byte, error) { return nil, ErrNoTerminal }
	if _, _, err := Open(dir, &testutil.Rand{}, noTTY, discard); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenFallbackRetriesWrongPassphrase(t *testing.T) {
	fallbackToFile(t)
	dir := t.TempDir()
	f, _ := openFile(filepath.Join(dir, FileName), []byte("right passphrase"), &testutil.Rand{}, fastKDF)
	_ = f.Set("a", []byte("v"))
	answers := []string{"nope nope", "right passphrase"}
	prompt := func(bool) ([]byte, error) { a := answers[0]; answers = answers[1:]; return []byte(a), nil }
	if _, _, err := Open(dir, &testutil.Rand{}, prompt, discard); err != nil {
		t.Fatal(err)
	}
	always := func(bool) ([]byte, error) { return []byte("nope nope"), nil }
	if _, _, err := Open(dir, &testutil.Rand{}, always, discard); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("got %v", err)
	}
}

func TestTerminalPrompterRefusesNonTerminal(t *testing.T) {
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	if _, err := TerminalPrompter(r, io.Discard)(false); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("got %v", err)
	}
}
