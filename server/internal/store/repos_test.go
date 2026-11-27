package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
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
	if err := s.PutRepo(ctx, "R1", "squash", rules, false, 0); err != nil {
		t.Fatal(err)
	}
	got, err := s.Repo(ctx, "R1")
	if err != nil || !got.IsNew || got.DefaultMethod != "squash" || len(got.Rules) != 3 || got.Rules[0].Pattern != "release/*" || got.Rules[2].Pattern != "x" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := s.PutRepo(ctx, "R1", "merge", nil, true, 0); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Repo(ctx, "R1")
	if got.IsNew || got.DefaultMethod != "merge" || len(got.Rules) != 0 {
		t.Fatalf("%+v", got)
	}
	if n := count(t, path, `SELECT count(*) FROM repo_branch_rules WHERE repo_id = 'R2'`); n != 2 {
		t.Fatalf("another repo's rules changed: %d", n)
	}
	if err := s.PutRepo(ctx, "R404", "merge", nil, false, 0); !errors.Is(err, store.ErrNotFound) {
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
	err := s.PutRepo(ctx, "R1", "squash", []store.BranchRule{{Pattern: "a", Method: "merge"}, {Pattern: "b", Method: "octopus"}}, true, 0)
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

func TestApprovalsRequiredDefaultsToOneAndIsKeptWhenLeftOut(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	repos, err := s.Repos(ctx)
	if err != nil || len(repos) != 2 || repos[0].ApprovalsRequired != 1 || repos[1].ApprovalsRequired != 1 {
		t.Fatalf("a repo never set needs one approval: %+v %v", repos, err)
	}
	if err := s.PutRepo(ctx, "R1", "squash", nil, false, 3); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Repo(ctx, "R1"); got.ApprovalsRequired != 3 {
		t.Fatalf("%+v", got)
	}
	// 0 means the update did not mention it: the stored value stays.
	if err := s.PutRepo(ctx, "R1", "merge", nil, true, 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Repo(ctx, "R1"); got.ApprovalsRequired != 3 || got.DefaultMethod != "merge" {
		t.Fatalf("%+v", got)
	}
	// A row written without the column (a database from before migration 4) reads as one.
	if n := count(t, path, `SELECT count(*) FROM repos WHERE id = 'R2' AND approvals_required = 1`); n != 1 {
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
		{ID: "R1", Method: "squash", Approvals: 2},
		{ID: "R2", Method: "merge"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := s.Repo(ctx, "R1")
	r2, _ := s.Repo(ctx, "R2")
	if r1.ApprovalsRequired != 2 || r2.ApprovalsRequired != 1 {
		t.Fatalf("%+v %+v", r1, r2)
	}
}

func TestApprovalsRequiredIsLimitedToOneThroughTen(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepo(ctx, "R1", "squash", nil, false, 11); err == nil {
		t.Fatal("11 approvals were stored")
	}
	if err := s.PutRepo(ctx, "R1", "squash", nil, false, 10); err != nil {
		t.Fatal(err)
	}
}
