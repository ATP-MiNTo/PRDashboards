package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func openStore(t *testing.T, path string, clock *testutil.Clock) *store.Store {
	t.Helper()
	st, err := store.Open(path, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSessionsSurviveRestart(t *testing.T) {
	clock := testutil.NewClock()
	path := filepath.Join(t.TempDir(), "data.db")
	st := openStore(t, path, clock)
	first := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st, discard)
	created, err := first.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st2 := openStore(t, path, clock)
	second := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st2, discard)
	if _, ok := second.Authenticate(created.Value); ok {
		t.Fatal("session known before Load")
	}
	if err := second.Load(ctx); err != nil {
		t.Fatal(err)
	}
	got, ok := second.Authenticate(created.Value)
	if !ok || got.CSRF != created.CSRF || got.Key != created.Key {
		t.Fatalf("session lost across restart: %+v ok=%v", got, ok)
	}
}

func TestStoredSessionHoldsOnlyTheHash(t *testing.T) {
	clock := testutil.NewClock()
	st := openStore(t, filepath.Join(t.TempDir(), "data.db"), clock)
	s := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st, discard)
	created, _ := s.Create(ctx)
	rows, err := st.LoadSessions(ctx, clock.Now())
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", rows, err)
	}
	if got := rows[0].IDHash; string(got[:]) == created.Value {
		t.Fatal("cookie value stored")
	}
}

func TestSlidePersistsAndExpiredAreNotLoaded(t *testing.T) {
	clock := testutil.NewClock()
	path := filepath.Join(t.TempDir(), "data.db")
	st := openStore(t, path, clock)
	s := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st, discard)
	idle, _ := s.Create(ctx)
	clock.Advance(12 * time.Hour)
	slid, _ := s.Create(ctx)
	clock.Advance(auth.ReissueAfter + time.Hour)
	if got, ok := s.Authenticate(slid.Value); !ok || !got.Reissue {
		t.Fatal("expected a reissue")
	}
	clock.Advance(auth.MaxSessionAge - auth.ReissueAfter - 12*time.Hour - 30*time.Minute)

	restarted := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st, discard)
	if err := restarted.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.Authenticate(slid.Value); !ok {
		t.Fatal("slid session lost")
	}
	if _, ok := restarted.Authenticate(idle.Value); ok {
		t.Fatal("expired session restored")
	}
}

func TestExpiredSessionIsDeletedFromStore(t *testing.T) {
	clock := testutil.NewClock()
	st := openStore(t, filepath.Join(t.TempDir(), "data.db"), clock)
	s := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st, discard)
	created, _ := s.Create(ctx)
	clock.Advance(auth.MaxSessionAge + time.Second)
	if _, ok := s.Authenticate(created.Value); ok {
		t.Fatal("expired session accepted")
	}
	clock.Advance(-auth.MaxSessionAge) // look at the row as if it were still in range
	if rows, _ := st.LoadSessions(ctx, clock.Now()); len(rows) != 0 {
		t.Fatalf("row kept: %v", rows)
	}
}

func TestLoadDropsAndClampsSessionsPastTheAbsoluteLimit(t *testing.T) {
	clock := testutil.NewClock()
	st := openStore(t, filepath.Join(t.TempDir(), "data.db"), clock)
	rnd := &testutil.Rand{}
	old := auth.NewSessions(testKey, clock.Now, rnd, st, discard)
	stale, _ := old.Create(ctx)
	clock.Advance(10 * 24 * time.Hour)
	fresh, _ := old.Create(ctx)
	// Rows written before the absolute limit existed carry a sliding expiry far past it.
	rows, err := st.LoadSessions(ctx, clock.Now())
	if err != nil || len(rows) != 2 {
		t.Fatalf("%v %v", rows, err)
	}
	for _, r := range rows {
		if err := st.SlideSession(ctx, r.IDHash, r.LastIssuedAt, r.CreatedAt.Add(auth.SessionTTL)); err != nil {
			t.Fatal(err)
		}
	}
	clock.Advance(5 * 24 * time.Hour)
	restarted := auth.NewSessions(testKey, clock.Now, rnd, st, discard)
	if err := restarted.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.Authenticate(stale.Value); ok {
		t.Fatal("session past 14 days restored")
	}
	got, ok := restarted.Authenticate(fresh.Value)
	if !ok || !got.Expires.Equal(fresh.Expires) {
		t.Fatalf("fresh session: %+v ok=%v", got, ok)
	}
}

func TestRevokeAll(t *testing.T) {
	clock := testutil.NewClock()
	st := openStore(t, filepath.Join(t.TempDir(), "data.db"), clock)
	s := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st, discard)
	a, _ := s.Create(ctx)
	b, _ := s.Create(ctx)
	if err := s.RevokeAll(ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{a.Value, b.Value} {
		if _, ok := s.Authenticate(v); ok {
			t.Fatal("session survived revoke-all")
		}
	}
	restarted := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st, discard)
	_ = restarted.Load(ctx)
	if _, ok := restarted.Authenticate(a.Value); ok {
		t.Fatal("revoked session came back after restart")
	}
	if c, err := s.Create(ctx); err != nil {
		t.Fatal(err)
	} else if _, ok := s.Authenticate(c.Value); !ok {
		t.Fatal("new session after revoke-all rejected")
	}
	exp := auth.ExpiredCookie()
	if exp.MaxAge >= 0 || exp.Name != "prd_session" || exp.Value != "" || !exp.HttpOnly {
		t.Fatalf("%+v", exp)
	}
}

func TestStoreFailuresSurface(t *testing.T) {
	clock := testutil.NewClock()
	st := openStore(t, filepath.Join(t.TempDir(), "data.db"), clock)
	s := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st, discard)
	created, _ := s.Create(ctx)
	_ = st.Close()
	if _, err := s.Create(ctx); err == nil {
		t.Fatal("create succeeded on a closed store")
	}
	if err := s.RevokeAll(ctx); err == nil {
		t.Fatal("revoke succeeded on a closed store")
	}
	if _, ok := s.Authenticate(created.Value); ok {
		t.Fatal("cache must be cleared even when the store failed")
	}
	if err := s.Load(ctx); err == nil {
		t.Fatal("load succeeded on a closed store")
	}
	// Sliding and expiry deletes are best effort and must not panic.
	s2 := auth.NewSessions(testKey, clock.Now, &testutil.Rand{}, st, nil)
	_ = s2.Load(ctx)
}

func TestMemoryOnlySessionsAcceptLoadAndRevoke(t *testing.T) {
	s, _ := newSessions(t)
	if err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Create(ctx)
	if err := s.RevokeAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate(c.Value); ok {
		t.Fatal("session survived")
	}
}

func TestLoadOrCreateKeyGeneratesIntoSecrets(t *testing.T) {
	sec := secrets.NewMemory()
	legacy := filepath.Join(t.TempDir(), "session.key")
	key, err := auth.LoadOrCreateKey(sec, legacy, &testutil.Rand{}, discard)
	if err != nil || len(key) != 32 {
		t.Fatalf("%d %v", len(key), err)
	}
	if stored, _ := sec.Get(secrets.SessionKey); string(stored) != string(key) {
		t.Fatal("key not stored in the secrets store")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("a key file was written")
	}
	again, err := auth.LoadOrCreateKey(sec, legacy, &testutil.Rand{}, discard)
	if err != nil || string(again) != string(key) {
		t.Fatalf("key not stable: %v", err)
	}
}

func TestLoadOrCreateKeyImportsLegacyFileOnce(t *testing.T) {
	sec := secrets.NewMemory()
	legacy := filepath.Join(t.TempDir(), "session.key")
	old := []byte("legacy-legacy-legacy-legacy-1234")
	if err := os.WriteFile(legacy, old, 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := auth.LoadOrCreateKey(sec, legacy, &testutil.Rand{}, discard)
	if err != nil || string(key) != string(old) {
		t.Fatalf("%q %v", key, err)
	}
	if stored, _ := sec.Get(secrets.SessionKey); string(stored) != string(old) {
		t.Fatal("legacy key not imported")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy file not deleted")
	}
}

func TestLoadOrCreateKeyStoredKeyWinsOverLeftoverFile(t *testing.T) {
	sec := secrets.NewMemory()
	_ = sec.Set(secrets.SessionKey, testKey)
	legacy := filepath.Join(t.TempDir(), "session.key")
	_ = os.WriteFile(legacy, []byte("another-key-another-key-another!"), 0o600)
	key, err := auth.LoadOrCreateKey(sec, legacy, &testutil.Rand{}, discard)
	if err != nil || string(key) != string(testKey) {
		t.Fatalf("%q %v", key, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("leftover file not deleted")
	}
}

type failingSecrets struct {
	secrets.Store
	get, set error
}

func (f failingSecrets) Get(n string) ([]byte, error) {
	if f.get != nil {
		return nil, f.get
	}
	return f.Store.Get(n)
}

func (f failingSecrets) Set(n string, v []byte) error {
	if f.set != nil {
		return f.set
	}
	return f.Store.Set(n, v)
}

func TestLoadOrCreateKeyErrors(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "session.key")
	boom := errors.New("boom")
	if _, err := auth.LoadOrCreateKey(failingSecrets{Store: secrets.NewMemory(), get: boom}, legacy, &testutil.Rand{}, discard); !errors.Is(err, boom) {
		t.Fatalf("get error: %v", err)
	}
	if _, err := auth.LoadOrCreateKey(failingSecrets{Store: secrets.NewMemory(), set: boom}, legacy, &testutil.Rand{}, discard); !errors.Is(err, boom) {
		t.Fatalf("set error: %v", err)
	}
	bad := secrets.NewMemory()
	_ = bad.Set(secrets.SessionKey, []byte("short"))
	if _, err := auth.LoadOrCreateKey(bad, legacy, &testutil.Rand{}, discard); err == nil {
		t.Fatal("short stored key accepted")
	}
	_ = os.WriteFile(legacy, []byte("short"), 0o600)
	if _, err := auth.LoadOrCreateKey(secrets.NewMemory(), legacy, &testutil.Rand{}, discard); err == nil {
		t.Fatal("short legacy key accepted")
	}
	if runtime.GOOS != "windows" {
		dir := t.TempDir()
		if _, err := auth.LoadOrCreateKey(secrets.NewMemory(), dir, &testutil.Rand{}, discard); err == nil {
			t.Fatal("a directory in place of the legacy file was accepted")
		}
	}
}
