package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

func TestReposListRulesInOrderWithOpenCounts(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	repos, err := s.Repos(ctx)
	if err != nil || len(repos) != 2 {
		t.Fatalf("%+v %v", repos, err)
	}
	api, web := repos[0], repos[1]
	if api.NameWithOwner != "acme/api" || web.NameWithOwner != "acme/web" || !web.IsNew || web.DefaultMethod != "rebase" || web.OpenPRs != 1 {
		t.Fatalf("%+v", repos)
	}
	if len(web.Rules) != 2 || web.Rules[0] != (store.BranchRule{Pattern: "main", Method: "merge"}) || web.Rules[1].Pattern != "production" {
		t.Fatalf("seed rules %+v", web.Rules)
	}
}

func TestPutRepoReplacesTheWholeRuleListAndClearsNew(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	rules := []store.BranchRule{{Pattern: "release/*", Method: "rebase"}, {Pattern: "main", Method: "squash"}, {Pattern: "x", Method: "merge"}}
	if err := s.PutRepo(ctx, "R1", "squash", rules, false, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.Repo(ctx, "R1")
	if err != nil || !got.IsNew || got.DefaultMethod != "squash" || len(got.Rules) != 3 || got.Rules[0].Pattern != "release/*" || got.Rules[2].Pattern != "x" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := s.PutRepo(ctx, "R1", "merge", nil, true, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Repo(ctx, "R1")
	if got.IsNew || got.DefaultMethod != "merge" || len(got.Rules) != 0 {
		t.Fatalf("%+v", got)
	}
	if n := count(t, path, `SELECT count(*) FROM repo_branch_rules WHERE repo_id = 'R2'`); n != 2 {
		t.Fatalf("another repo's rules changed: %d", n)
	}
	if err := s.PutRepo(ctx, "R404", "merge", nil, false, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err := s.Repo(ctx, "R404"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestPutRepoRollsBackWhenARuleIsInvalid(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	err := s.PutRepo(ctx, "R1", "squash", []store.BranchRule{{Pattern: "a", Method: "merge"}, {Pattern: "b", Method: "octopus"}}, true, nil)
	if err == nil {
		t.Fatal("an invalid method was stored")
	}
	got, _ := s.Repo(ctx, "R1")
	if !got.IsNew || got.DefaultMethod != "rebase" || len(got.Rules) != 2 {
		t.Fatalf("the failed replacement left changes: %+v", got)
	}
}

func TestPutReposUpdatesEveryRepoInOneTransaction(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	err := s.PutRepos(ctx, []store.RepoUpdate{
		{ID: "R1", Method: "squash", Rules: []store.BranchRule{{Pattern: "release/*", Method: "rebase"}, {Pattern: "main", Method: "merge"}}, ClearNew: true},
		{ID: "R2", Method: "merge"},
		{ID: "R2", Method: "rebase", Rules: nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := s.Repo(ctx, "R1")
	if r1.IsNew || r1.DefaultMethod != "squash" || len(r1.Rules) != 2 || r1.Rules[0].Pattern != "release/*" || r1.Rules[1].Pattern != "main" {
		t.Fatalf("%+v", r1)
	}
	r2, _ := s.Repo(ctx, "R2")
	if r2.DefaultMethod != "rebase" || len(r2.Rules) != 0 {
		t.Fatalf("a repeated id takes its last update: %+v", r2)
	}
}

func TestPutReposWithAnUnknownIDChangesNothing(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	err := s.PutRepos(ctx, []store.RepoUpdate{{ID: "R1", Method: "merge", ClearNew: true}, {ID: "R404", Method: "merge"}})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	r1, _ := s.Repo(ctx, "R1")
	if !r1.IsNew || r1.DefaultMethod != "rebase" || len(r1.Rules) != 2 {
		t.Fatalf("the known repo was changed: %+v", r1)
	}
}

func TestPutReposRollsBackWhenARuleIsInvalid(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	err := s.PutRepos(ctx, []store.RepoUpdate{
		{ID: "R2", Method: "merge", ClearNew: true},
		{ID: "R1", Method: "squash", Rules: []store.BranchRule{{Pattern: "a", Method: "octopus"}}},
	})
	if err == nil {
		t.Fatal("an invalid method was stored")
	}
	r2, _ := s.Repo(ctx, "R2")
	r1, _ := s.Repo(ctx, "R1")
	if r2.DefaultMethod != "rebase" || r1.DefaultMethod != "rebase" || len(r1.Rules) != 2 {
		t.Fatalf("the failed call left changes: %+v %+v", r1, r2)
	}
}

func intp(n int) *int { return &n }

func TestApprovalsRequiredDefaultsToZeroAndIsKeptWhenLeftOut(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	repos, err := s.Repos(ctx)
	if err != nil || len(repos) != 2 || repos[0].ApprovalsRequired != 0 || repos[1].ApprovalsRequired != 0 {
		t.Fatalf("a repo never set needs no approval: %+v %v", repos, err)
	}
	if err := s.PutRepo(ctx, "R1", "squash", nil, false, intp(3)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Repo(ctx, "R1"); got.ApprovalsRequired != 3 {
		t.Fatalf("%+v", got)
	}
	// nil means the update did not mention it: the stored value stays.
	if err := s.PutRepo(ctx, "R1", "merge", nil, true, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Repo(ctx, "R1"); got.ApprovalsRequired != 3 || got.DefaultMethod != "merge" {
		t.Fatalf("%+v", got)
	}
	// 0 is a value of its own: it is stored, not read as "left out".
	if err := s.PutRepo(ctx, "R1", "merge", nil, true, intp(0)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Repo(ctx, "R1"); got.ApprovalsRequired != 0 {
		t.Fatalf("%+v", got)
	}
	// A row written without the column reads as the default.
	if n := count(t, path, `SELECT count(*) FROM repos WHERE id = 'R2' AND approvals_required = 0`); n != 1 {
		t.Fatalf("%d", n)
	}
}

func TestPutReposSetsApprovalsPerRepoInTheSameTransaction(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	err := s.PutRepos(ctx, []store.RepoUpdate{
		{ID: "R1", Method: "squash", Approvals: intp(2)},
		{ID: "R2", Method: "merge"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := s.Repo(ctx, "R1")
	r2, _ := s.Repo(ctx, "R2")
	if r1.ApprovalsRequired != 2 || r2.ApprovalsRequired != 0 {
		t.Fatalf("%+v %+v", r1, r2)
	}
}

func TestApprovalsRequiredIsLimitedToZeroThroughTen(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{-1, 11} {
		if err := s.PutRepo(ctx, "R1", "squash", nil, false, intp(n)); err == nil {
			t.Fatalf("%d approvals were stored", n)
		}
	}
	for _, n := range []int{0, 10} {
		if err := s.PutRepo(ctx, "R1", "squash", nil, false, intp(n)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepoPrefsNewReposFollowTheDefaultAndUpdatesReplaceOrKeepThem(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Repo(ctx, "R1")
	if !got.InheritMethod || got.DeleteBranch != "default" {
		t.Fatalf("a repo seen for the first time follows the defaults: %+v", got)
	}
	yes := store.RepoUpdate{ID: "R1", Method: "merge", Inherit: false, DeleteBranch: "yes"}
	if err := s.PutRepos(ctx, []store.RepoUpdate{yes}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Repo(ctx, "R1")
	if got.InheritMethod || got.DeleteBranch != "yes" || got.DefaultMethod != "merge" {
		t.Fatalf("%+v", got)
	}
	// The delete-branch choice left out stays; the inheritance is replaced like the method.
	if err := s.PutRepos(ctx, []store.RepoUpdate{{ID: "R1", Method: "rebase", Inherit: true}}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Repo(ctx, "R1")
	if !got.InheritMethod || got.DeleteBranch != "yes" || got.DefaultMethod != "rebase" {
		t.Fatalf("%+v", got)
	}
	// PutRepo is the one-repo form: it keeps the delete-branch choice and uses the method itself.
	if err := s.PutRepo(ctx, "R1", "squash", nil, false, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Repo(ctx, "R1")
	if got.InheritMethod || got.DeleteBranch != "yes" {
		t.Fatalf("%+v", got)
	}
	if other, _ := s.Repo(ctx, "R2"); !other.InheritMethod || other.DeleteBranch != "default" {
		t.Fatalf("another repo changed: %+v", other)
	}
	if err := s.PutRepos(ctx, []store.RepoUpdate{{ID: "R1", Method: "squash", DeleteBranch: "maybe"}}); err == nil {
		t.Fatal("an unknown delete-branch choice was stored")
	}
}

func TestMigration0007RebuildsReposAndKeepsEverythingAroundIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db := rawDB(t, path)
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	for v, name := range []string{"0001_init", "0002_merge", "0003_sync", "0004_approvals", "0005_repo_prefs"} {
		body, err := os.ReadFile(filepath.Join("migrations", name+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, 0)`, v+1); err != nil {
			t.Fatal(err)
		}
	}
	// A database of version 5: one repo on the old default of 1, one that chose 4, one that chose 2 and
	// follows the default method, and pull requests and branch rules that reference them.
	for _, q := range []string{
		`INSERT INTO repos (id, name_with_owner, first_seen_at, default_method, is_new, approvals_required, method_inherit, delete_branch)
		 VALUES ('R1', 'acme/web', 5, 'rebase', 0, 1, 0, 'yes'), ('R2', 'acme/api', 6, 'merge', 1, 4, 1, 'no'), ('R3', 'acme/lib', 7, 'squash', 0, 2, 0, 'default')`,
		`INSERT INTO repo_branch_rules (repo_id, position, pattern, method) VALUES ('R1', 0, 'main', 'merge'), ('R1', 1, 'prod', 'rebase'), ('R2', 0, 'main', 'squash')`,
		`INSERT INTO prs (id, repo_id, number, title, head_branch, base_branch, head_sha, state, last_seen_at)
		 VALUES ('P1', 'R1', 1, 'One', 'feat/a', 'main', 'aaa', 'open', 9), ('P2', 'R2', 2, 'Two', 'feat/b', 'main', 'bbb', 'open', 9)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(path, testutil.NewClock().Now)
	if err != nil {
		t.Fatalf("the migration failed on a database with rows: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	repos, err := s.Repos(ctx)
	if err != nil || len(repos) != 3 {
		t.Fatalf("%+v %v", repos, err)
	}
	byID := map[string]store.RepoConfig{}
	for _, r := range repos {
		byID[r.ID] = r
	}
	if byID["R1"].ApprovalsRequired != 0 || byID["R2"].ApprovalsRequired != 4 || byID["R3"].ApprovalsRequired != 2 {
		t.Fatalf("1 was only the default and becomes 0, the choices stay: %+v", repos)
	}
	r1, r2 := byID["R1"], byID["R2"]
	if r1.NameWithOwner != "acme/web" || r1.DefaultMethod != "rebase" || r1.IsNew || r1.InheritMethod || r1.DeleteBranch != "yes" || r1.OpenPRs != 1 ||
		!r2.IsNew || !r2.InheritMethod || r2.DeleteBranch != "no" || r2.OpenPRs != 1 {
		t.Fatalf("the other columns and the open pull requests survive: %+v %+v", r1, r2)
	}
	if len(r1.Rules) != 2 || r1.Rules[0] != (store.BranchRule{Pattern: "main", Method: "merge"}) || r1.Rules[1].Pattern != "prod" || len(r2.Rules) != 1 {
		t.Fatalf("the branch rules the DROP would cascade away are put back: %+v %+v", r1.Rules, r2.Rules)
	}
	if n := count(t, path, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Fatalf("%d foreign key violations", n)
	}
	if n := count(t, path, `SELECT count(*) FROM sqlite_master WHERE name LIKE '%_keep'`); n != 0 {
		t.Fatalf("%d helper tables left", n)
	}
	// The new column rules: 0 to 10, default 0, and a deletion of a repo still cascades its rules.
	if err := s.PutRepo(ctx, "R1", "squash", nil, false, intp(11)); err == nil {
		t.Fatal("11 accepted after the rebuild")
	}
	if _, err := rawDB(t, path).Exec(`INSERT INTO repos (id, name_with_owner, first_seen_at, default_method) VALUES ('R9', 'acme/new', 1, 'squash')`); err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT approvals_required FROM repos WHERE id = 'R9'`); n != 0 {
		t.Fatalf("default %d", n)
	}
}
