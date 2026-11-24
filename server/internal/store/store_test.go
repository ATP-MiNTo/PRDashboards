package store_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

func open(t *testing.T) (*store.Store, string, *testutil.Clock) {
	t.Helper()
	clock := testutil.NewClock()
	path := filepath.Join(t.TempDir(), "data.db")
	s, err := store.Open(path, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path, clock
}

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateFromEmptyCreatesEveryTable(t *testing.T) {
	_, path, _ := open(t)
	db := rawDB(t, path)
	want := []string{
		"activity", "idempotency", "oauth_states", "pending_retries", "prs", "repo_branch_rules", "repos", "rule_firings",
		"schema_migrations", "sessions", "settings", "snapshot", "sync_rules", "ticket_links",
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		got = append(got, n)
	}
	if len(got) != len(want) {
		t.Fatalf("tables %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tables %v, want %v", got, want)
		}
	}
	for _, idx := range []string{"prs_state_repo", "ticket_links_ticket", "activity_created"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, idx).Scan(&n); err != nil || n != 1 {
			t.Errorf("index %s missing", idx)
		}
	}
}

func TestMigrationsAreRecordedOnceAndReopenIsIdempotent(t *testing.T) {
	s, path, clock := open(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := store.Open(path, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	var n int
	if err := rawDB(t, path).QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("migrations recorded %d, %v", n, err)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	s, path, clock := open(t)
	if _, err := rawDB(t, path).Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (999, 0)`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := store.Open(path, clock.Now); err == nil {
		t.Fatal("newer schema accepted")
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := store.Open(filepath.Join(t.TempDir(), "missing", "data.db"), time.Now); err == nil {
		t.Fatal("missing directory accepted")
	}
	bad := filepath.Join(t.TempDir(), "data.db")
	if err := os.WriteFile(bad, []byte("this is not a sqlite database, not at all, no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(bad, time.Now); err == nil {
		t.Fatal("garbage file accepted")
	}
}

func TestFileModeAndPragmas(t *testing.T) {
	s, path, _ := open(t)
	if err := s.PutSettings(context.Background(), store.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for _, p := range []string{path, path + "-wal", path + "-shm"} {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			if info.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s has mode %v", filepath.Base(p), info.Mode().Perm())
			}
		}
	}
	db := rawDB(t, path)
	var mode string
	_ = db.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	if mode != "wal" {
		t.Fatalf("journal mode %s", mode)
	}
	if err := s.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO prs (id, repo_id, number, title, head_branch, base_branch, head_sha, state, last_seen_at)
			VALUES ('p', 'missing', 1, 't', 'h', 'b', 's', 'open', 0)`)
		return err
	}); err == nil {
		t.Fatal("foreign keys not enforced")
	}
}

func TestReaderPoolIsReadOnly(t *testing.T) {
	s, _, _ := open(t)
	if err := s.PutSettings(context.Background(), store.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSettings(context.Background())
	if err != nil || got.GitHubSec != 60 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, _, _ := open(t)
	got, err := s.GetSettings(ctx)
	if err != nil || got != store.DefaultSettings() {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	site := "cloud-1"
	want := store.Settings{GitHubSec: 120, JiraSec: 900, Theme: "dark", DefaultMethod: "rebase", DeleteBranchAfterMerge: true, JiraSiteID: &site}
	if err := s.PutSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetSettings(ctx)
	if err != nil || got.JiraSiteID == nil || *got.JiraSiteID != site {
		t.Fatalf("%+v %v", got, err)
	}
	got.JiraSiteID = nil
	want.JiraSiteID = nil
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	want.JiraSiteID = nil
	if err := s.PutSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetSettings(ctx); got.JiraSiteID != nil {
		t.Fatal("null site not stored")
	}
}

func TestSettingsStoredAsTypedJSON(t *testing.T) {
	ctx := context.Background()
	s, path, _ := open(t)
	if err := s.PutSettings(ctx, store.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	vals := map[string]string{}
	rows, _ := rawDB(t, path).Query(`SELECT key, value FROM settings`)
	defer rows.Close()
	for rows.Next() {
		var k, v string
		_ = rows.Scan(&k, &v)
		vals[k] = v
	}
	for k, want := range map[string]string{
		"polling.githubSec": "60", "polling.jiraSec": "300", "theme": `"auto"`,
		"defaultMethodForNewRepos": `"squash"`, "deleteBranchAfterMerge": "false", "jiraSiteId": "null",
	} {
		if vals[k] != want {
			t.Errorf("%s = %q, want %q", k, vals[k], want)
		}
	}
}

func TestSettingsCorruptValue(t *testing.T) {
	s, path, _ := open(t)
	if _, err := rawDB(t, path).Exec(`INSERT INTO settings (key, value) VALUES ('theme', '{')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSettings(context.Background()); err == nil {
		t.Fatal("corrupt value accepted")
	}
}

func TestSettingsIgnoreUnknownKeys(t *testing.T) {
	s, path, _ := open(t)
	if _, err := rawDB(t, path).Exec(`INSERT INTO settings (key, value) VALUES ('future', '1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsLifecycle(t *testing.T) {
	ctx := context.Background()
	s, _, clock := open(t)
	now := clock.Now()
	a := store.Session{IDHash: sha256.Sum256([]byte("a")), CreatedAt: now, LastIssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	b := store.Session{IDHash: sha256.Sum256([]byte("b")), CreatedAt: now, LastIssuedAt: now, ExpiresAt: now.Add(2 * time.Hour)}
	for _, v := range []store.Session{a, b} {
		if err := s.InsertSession(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.InsertSession(ctx, a); err == nil {
		t.Fatal("duplicate session accepted")
	}
	if err := s.SlideSession(ctx, a.IDHash, now.Add(time.Minute), now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadSessions(ctx, now)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	for _, v := range got {
		if v.IDHash == a.IDHash && !v.ExpiresAt.Equal(now.Add(3*time.Hour)) {
			t.Fatalf("slide not stored: %v", v.ExpiresAt)
		}
	}
	if got, _ = s.LoadSessions(ctx, now.Add(150*time.Minute)); len(got) != 1 || got[0].IDHash != a.IDHash {
		t.Fatalf("expiry filter: %v", got)
	}
	if err := s.DeleteSession(ctx, a.IDHash); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.LoadSessions(ctx, now); len(got) != 1 {
		t.Fatalf("after delete: %v", got)
	}
	if err := s.DeleteAllSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.LoadSessions(ctx, now); len(got) != 0 {
		t.Fatalf("after delete all: %v", got)
	}
}

func TestInsertSessionPurgesExpired(t *testing.T) {
	ctx := context.Background()
	s, path, clock := open(t)
	now := clock.Now()
	old := store.Session{IDHash: sha256.Sum256([]byte("old")), CreatedAt: now, LastIssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	_ = s.InsertSession(ctx, old)
	later := now.Add(time.Hour)
	fresh := store.Session{IDHash: sha256.Sum256([]byte("new")), CreatedAt: later, LastIssuedAt: later, ExpiresAt: later.Add(time.Hour)}
	_ = s.InsertSession(ctx, fresh)
	var n int
	_ = rawDB(t, path).QueryRow(`SELECT count(*) FROM sessions`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d rows, want the expired one purged", n)
	}
}

func TestOAuthStateSingleUse(t *testing.T) {
	ctx := context.Background()
	s, _, clock := open(t)
	now := clock.Now()
	st := store.OAuthState{Hash: sha256.Sum256([]byte("s")), Provider: "github", SessionKey: "k", Verifier: "v", ExpiresAt: now.Add(10 * time.Minute)}
	if err := s.InsertOAuthState(ctx, st, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.ConsumeOAuthState(ctx, st.Hash)
	if err != nil || got.Provider != "github" || got.SessionKey != "k" || got.Verifier != "v" || !got.ExpiresAt.Equal(st.ExpiresAt) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.ConsumeOAuthState(ctx, st.Hash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second consume: %v", err)
	}
	if _, err := s.ConsumeOAuthState(ctx, sha256.Sum256([]byte("other"))); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestOAuthStateInsertPurgesExpired(t *testing.T) {
	ctx := context.Background()
	s, _, clock := open(t)
	now := clock.Now()
	old := store.OAuthState{Hash: sha256.Sum256([]byte("old")), Provider: "github", SessionKey: "k", Verifier: "v", ExpiresAt: now.Add(time.Minute)}
	_ = s.InsertOAuthState(ctx, old, now)
	later := now.Add(time.Hour)
	fresh := store.OAuthState{Hash: sha256.Sum256([]byte("new")), Provider: "atlassian", SessionKey: "k", ExpiresAt: later.Add(time.Minute)}
	if err := s.InsertOAuthState(ctx, fresh, later); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeOAuthState(ctx, old.Hash); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("expired state not purged")
	}
	bad := fresh
	bad.Hash = sha256.Sum256([]byte("bad"))
	bad.Provider = "gitlab"
	if err := s.InsertOAuthState(ctx, bad, later); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestConcurrentWritersAreSerialized(t *testing.T) {
	ctx := context.Background()
	s, _, clock := open(t)
	now := clock.Now()
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.InsertSession(ctx, store.Session{
				IDHash: sha256.Sum256([]byte{byte(i)}), CreatedAt: now, LastIssuedAt: now, ExpiresAt: now.Add(time.Hour),
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.LoadSessions(ctx, now); len(got) != 40 {
		t.Fatalf("%d sessions", len(got))
	}
}

func TestWriteRollsBack(t *testing.T) {
	ctx := context.Background()
	s, _, _ := open(t)
	boom := errors.New("boom")
	err := s.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES ('x', '1')`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	var n int
	_ = s.Write(ctx, func(tx *sql.Tx) error { return tx.QueryRow(`SELECT count(*) FROM settings`).Scan(&n) })
	if n != 0 {
		t.Fatal("rolled back write persisted")
	}
}
