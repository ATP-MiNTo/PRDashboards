package poller_test

import (
	"context"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func TestSnapshotCarriesTheResolvedMergeMethod(t *testing.T) {
	r := newRig(t)
	web := pr("PR_a", 1, "acme/web", "feat/a", "main", "On main")
	dev := pr("PR_b", 2, "acme/web", "feat/b", "develop", "On develop")
	locked := pr("PR_c", 3, "acme/api", "feat/c", "develop", "Squash only")
	locked.Repo.MergeAllowed, locked.Repo.RebaseAllowed = false, false
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = append(f.result.PRs, web, dev, locked) })
	r.sync()

	by := map[int]string{}
	for _, p := range r.snapshot().Prs {
		by[p.Number] = string(p.MergeMethod.Method) + "|" + p.MergeMethod.Source
	}
	if by[1] != "merge|rule: main" || by[2] != "squash|new repo default" || by[3] != "squash|new repo default" {
		t.Fatalf("%v", by)
	}

	ctx := context.Background()
	if err := r.store.PutRepo(ctx, "R-acme/web", "rebase", []store.BranchRule{{Pattern: "feat/*", Method: "merge"}}, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := r.p.Relink(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range r.snapshot().Prs {
		switch p.Number {
		case 1:
			if p.MergeMethod.Method != "rebase" || p.MergeMethod.Source != "repo default" || p.Repo.IsNew {
				t.Fatalf("%+v", p.MergeMethod)
			}
		case 3:
			if len(p.MergeMethod.Allowed) != 1 || p.MergeMethod.Allowed[0] != "squash" {
				t.Fatalf("%+v", p.MergeMethod)
			}
		}
	}
	if open, _ := r.gh.calls(); open != 1 {
		t.Fatalf("a settings change rebuilds from cache: %d GitHub searches", open)
	}
}
