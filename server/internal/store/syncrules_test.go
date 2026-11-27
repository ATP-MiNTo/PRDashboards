package store_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

func rule(id, trigger string) store.SyncRule {
	return store.SyncRule{ID: id, Name: id, Enabled: true, Trigger: trigger, Target: "Done", Mode: "all"}
}

func TestPutRulesReplacesTheListInOrder(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	stale := rule("b", "commit_stale")
	stale.Hours = 48
	stale.Filters = store.RuleFilters{Repo: "acme/*", Status: "In Review"}
	if err := s.PutRules(ctx, []store.SyncRule{rule("a", "merged"), stale, rule("c", "ci_failing")}, nil, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Rules(ctx)
	if err != nil || len(got) != 3 || got[1].Hours != 48 || got[1].Filters.Repo != "acme/*" || got[1].Filters.Status != "In Review" || got[2].Trigger != "ci_failing" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := s.PutRules(ctx, []store.SyncRule{rule("c", "ci_failing"), rule("a", "merged")}, nil, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Rules(ctx); len(got) != 2 || got[0].ID != "c" || got[1].ID != "a" {
		t.Fatalf("order or delete: %+v", got)
	}
	if n := count(t, path, `SELECT count(*) FROM sync_rules`); n != 2 {
		t.Fatalf("rules %d", n)
	}
}

func TestPutRulesResetsAndBaselinesFirings(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	both := []store.SyncRule{rule("a", "merged"), rule("b", "review_approved")}
	if err := s.PutRules(ctx, both, nil, []store.Firing{{Rule: "a", Ticket: "INP-1", Fingerprint: "x"}, {Rule: "b", Ticket: "INP-1", Fingerprint: "y"}}, clock.Now()); err != nil {
		t.Fatal(err)
	}
	// An edited rule starts fresh, and the baseline of the same save is written after the reset.
	if err := s.PutRules(ctx, both, []string{"a"}, []store.Firing{{Rule: "a", Ticket: "INP-2", Fingerprint: "z"}}, clock.Now()); err != nil {
		t.Fatal(err)
	}
	st, err := s.SyncState(ctx, clock.Now().Add(-time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range st.Fired {
		got = append(got, f.Rule+"|"+f.Ticket+"|"+f.Fingerprint)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"a|INP-2|z", "b|INP-1|y"}) {
		t.Fatalf("%v", got)
	}
	// Deleting a rule removes its firings.
	if err := s.PutRules(ctx, both[:1], nil, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM rule_firings WHERE rule_id = 'b'`); n != 0 {
		t.Fatalf("firings of a deleted rule: %d", n)
	}
}

func TestSyncStateMergedPRsAndReadySHA(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if err := s.PutRules(ctx, []store.SyncRule{rule("a", "merged")}, nil, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	r := baseRefresh(clock.Now())
	r.PRs = append(r.PRs,
		store.PRRow{ID: "P3", RepoID: "R1", Number: 3, Title: "Old", HeadBranch: "x", BaseBranch: "main", HeadSHA: "ccc", Draft: true},
		store.PRRow{ID: "P4", RepoID: "R1", Number: 4, Title: "Ancient", HeadBranch: "y", BaseBranch: "main", HeadSHA: "ddd"})
	r.Links = append(r.Links,
		store.LinkRow{PRID: "P3", Key: "INP-3", Source: "branch"}, store.LinkRow{PRID: "P4", Key: "INP-9", Source: "branch"},
		store.LinkRow{PRID: "P2", Key: "INP-7", Source: "branch"})
	if err := s.SaveRefresh(ctx, r); err != nil {
		t.Fatal(err)
	}
	st, err := s.SyncState(ctx, clock.Now().Add(-time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Merged) != 0 || st.ReadySHA["P1"] != "aaa" || st.ReadySHA["P3"] != "" || len(st.ReadySHA) != 3 {
		t.Fatalf("open PRs only, a draft has no ready sha: %+v", st)
	}
	if len(st.ApprovalsNeed) == 0 || st.ApprovalsNeed["R1"] != 1 {
		t.Fatalf("each repo carries the approvals it requires: %+v", st.ApprovalsNeed)
	}

	// P1 merged an hour ago, P4 a year ago, and P2 is the one the running refresh just resolved.
	old := clock.Now().Add(-365 * 24 * time.Hour).Unix()
	recent := clock.Now().Add(-time.Hour).Unix()
	r = baseRefresh(clock.Now())
	r.PRs = r.PRs[1:]
	r.Links = []store.LinkRow{{PRID: "P2", Key: "INP-7", Source: "branch"}}
	r.Resolved = []store.Resolution{{ID: "P1", State: "merged", MergedAt: &recent}, {ID: "P4", State: "merged", MergedAt: &old}, {ID: "P3", State: "closed"}}
	if err := s.SaveRefresh(ctx, r); err != nil {
		t.Fatal(err)
	}
	st, err = s.SyncState(ctx, clock.Now().Add(-24*time.Hour), []string{"P2"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range st.Merged {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{"P1", "P2"}) {
		t.Fatalf("recent merge and the one just resolved: %v", ids)
	}
	if m := st.Merged[0]; m.Repo != "acme/web" || m.Number != 1 || m.Base != "main" || !slices.Equal(m.Tickets, []string{"INP-1", "INP-2"}) {
		t.Fatalf("%+v", m)
	}
	// A merged pull request of any age counts while another PR of the ticket is still open.
	if _, err := rawDB(t, path).Exec(`INSERT INTO ticket_links (pr_id, ticket_key, source, removed, position) VALUES ('P2', 'INP-9', 'branch', 0, 0)`); err != nil {
		t.Fatal(err)
	}
	st, _ = s.SyncState(ctx, clock.Now().Add(-24*time.Hour), nil)
	ids = nil
	for _, m := range st.Merged {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{"P1", "P4"}) {
		t.Fatalf("%v", ids)
	}
}

func TestReadySHAOnlyChangesWhenTheDraftBecomesReady(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	save := func(sha string, draft bool) {
		t.Helper()
		r := baseRefresh(clock.Now())
		r.PRs = []store.PRRow{{ID: "P1", RepoID: "R1", Number: 1, Title: "t", HeadBranch: "h", BaseBranch: "main", HeadSHA: sha, Draft: draft}}
		r.Repos = r.Repos[:1]
		r.Links = nil
		if err := s.SaveRefresh(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	ready := func() string {
		st, err := s.SyncState(ctx, clock.Now(), nil)
		if err != nil {
			t.Fatal(err)
		}
		return st.ReadySHA["P1"]
	}
	save("a", true)
	if ready() != "" {
		t.Fatal("draft")
	}
	save("b", false)
	save("c", false)
	if ready() != "b" {
		t.Fatalf("a push after ready kept the original: %q", ready())
	}
	save("d", true)
	save("e", false)
	if ready() != "e" {
		t.Fatalf("ready again after a draft: %q", ready())
	}
}

func TestSaveSyncAndRefreshWriteTheEvaluationAtomically(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if err := s.PutRules(ctx, []store.SyncRule{rule("a", "merged")}, nil, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	w := store.SyncWrite{
		Activity: []store.ActivityRow{{Kind: "transition", TicketKey: "INP-1", TriggeredBy: "rule:a", Result: "ok", Summary: "moved"}},
		Retries:  []store.RetryRow{{TicketKey: "INP-2", TransitionID: "31", ToStatus: "Done", LastError: "boom"}},
		Firings:  []store.Firing{{Rule: "a", Ticket: "INP-1", Fingerprint: "f"}},
	}
	if w.Empty() || !(store.SyncWrite{}).Empty() {
		t.Fatal("Empty")
	}
	if err := s.SaveSync(ctx, 4, []byte("s"), clock.Now(), w); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"activity": 1, "pending_retries": 1, "rule_firings": 1, "snapshot": 1} {
		if n := count(t, path, `SELECT count(*) FROM `+table); n != want {
			t.Errorf("%s: %d", table, n)
		}
	}
	r := baseRefresh(clock.Now())
	r.Sync = store.SyncWrite{Activity: w.Activity, Firings: []store.Firing{{Rule: "a", Ticket: "INP-5", Fingerprint: "g"}}}
	if err := s.SaveRefresh(ctx, r); err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM activity`); n != 2 {
		t.Fatalf("activity %d", n)
	}
	if n := count(t, path, `SELECT count(*) FROM rule_firings`); n != 2 {
		t.Fatalf("firings %d", n)
	}
	// A failing write rolls the snapshot back with it.
	if err := s.SaveSync(ctx, 99, nil, clock.Now(), store.SyncWrite{Activity: []store.ActivityRow{{Kind: "x"}}}); err == nil {
		t.Fatal("a row without its required columns was accepted")
	}
	if snap, _ := s.LoadSnapshot(ctx); snap.Version == 99 {
		t.Fatal("the snapshot was written although the transaction failed")
	}
}

func TestRuleModeDefaultIsStored(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	follow, anyRule := rule("a", "merged"), rule("b", "merged")
	follow.Mode, anyRule.Mode = "default", "any"
	if err := s.PutRules(ctx, []store.SyncRule{follow, anyRule, rule("c", "merged")}, nil, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Rules(ctx)
	if err != nil || len(got) != 3 || got[0].Mode != "default" || got[1].Mode != "any" || got[2].Mode != "all" {
		t.Fatalf("%+v %v", got, err)
	}
	bad := rule("d", "merged")
	bad.Mode = "some"
	if err := s.PutRules(ctx, []store.SyncRule{bad}, nil, nil, clock.Now()); err == nil {
		t.Fatal("an unknown mode was stored")
	}
}

// The migration that allows mode 'default' rebuilds sync_rules, so it must keep the stored rules with
// their explicit mode, and their firings.
func TestMigrationKeepsRulesAndFiringsWhenModeDefaultIsAllowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db := rawDB(t, path)
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	for v, name := range []string{"0001_init.sql", "0002_merge.sql", "0003_sync.sql", "0004_approvals.sql", "0005_repo_prefs.sql"} {
		body, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, 0)`, v+1); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO sync_rules (id, position, name, enabled, trigger, filters, target_status, mode) VALUES ('a', 0, 'A', 1, '{"type":"merged"}', '{}', 'Done', 'any')`,
		`INSERT INTO sync_rules (id, position, name, enabled, trigger, filters, target_status, mode) VALUES ('b', 1, 'B', 0, '{"type":"merged"}', '{}', 'QA', 'all')`,
		`INSERT INTO rule_firings (rule_id, ticket_key, fingerprint, fired_at) VALUES ('a', 'INP-1', 'fp', 5)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	clock := testutil.NewClock()
	s, err := store.Open(path, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	got, err := s.Rules(ctx)
	if err != nil || len(got) != 2 || got[0].ID != "a" || got[0].Mode != "any" || got[1].ID != "b" || got[1].Mode != "all" || got[1].Enabled {
		t.Fatalf("%+v %v", got, err)
	}
	if n := count(t, path, `SELECT count(*) FROM rule_firings WHERE rule_id = 'a' AND ticket_key = 'INP-1' AND fingerprint = 'fp'`); n != 1 {
		t.Fatalf("firings kept: %d", n)
	}
	got[0].Mode = "default"
	if err := s.PutRules(ctx, got, nil, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	// The cascade still works on the rebuilt tables.
	if err := s.PutRules(ctx, got[1:], nil, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM rule_firings`); n != 0 {
		t.Fatalf("firings of a deleted rule: %d", n)
	}
}

func TestRecordFiringsStoresFingerprintsOfKnownRules(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if err := s.PutRules(ctx, []store.SyncRule{rule("a", "merged")}, nil, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	err := s.RecordFirings(ctx, []store.Firing{{Rule: "a", Ticket: "INP-1", Fingerprint: "x"}, {Rule: "gone", Ticket: "INP-1", Fingerprint: "y"}}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM rule_firings`); n != 1 {
		t.Fatalf("firings %d", n)
	}
	if err := s.RecordFirings(ctx, nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
}
