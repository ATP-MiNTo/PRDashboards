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
	if err := s.PutRepo(ctx, "R1", "squash", rules, false); err != nil {
		t.Fatal(err)
	}
	got, err := s.Repo(ctx, "R1")
	if err != nil || !got.IsNew || got.DefaultMethod != "squash" || len(got.Rules) != 3 || got.Rules[0].Pattern != "release/*" || got.Rules[2].Pattern != "x" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := s.PutRepo(ctx, "R1", "merge", nil, true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Repo(ctx, "R1")
	if got.IsNew || got.DefaultMethod != "merge" || len(got.Rules) != 0 {
		t.Fatalf("%+v", got)
	}
	if n := count(t, path, `SELECT count(*) FROM repo_branch_rules WHERE repo_id = 'R2'`); n != 2 {
		t.Fatalf("another repo's rules changed: %d", n)
	}
	if err := s.PutRepo(ctx, "R404", "merge", nil, false); !errors.Is(err, store.ErrNotFound) {
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
	err := s.PutRepo(ctx, "R1", "squash", []store.BranchRule{{Pattern: "a", Method: "merge"}, {Pattern: "b", Method: "octopus"}}, true)
	if err == nil {
		t.Fatal("an invalid method was stored")
	}
	got, _ := s.Repo(ctx, "R1")
	if !got.IsNew || got.DefaultMethod != "rebase" || len(got.Rules) != 2 {
		t.Fatalf("the failed replacement left changes: %+v", got)
	}
}
