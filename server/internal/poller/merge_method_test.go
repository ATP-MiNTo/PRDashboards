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
	if err := r.store.PutRepo(ctx, "R-acme/web", "rebase", []store.BranchRule{{Pattern: "feat/*", Method: "merge"}}, true, nil); err != nil {
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

func TestARepoThatFollowsTheDefaultTakesTheGlobalMethod(t *testing.T) {
	r := newRig(t)
	web := pr("PR_a", 1, "acme/web", "feat/a", "develop", "On develop")
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = append(f.result.PRs, web) })
	r.sync()
	method := func() string { return string(r.snapshot().Prs[0].MergeMethod.Method) }
	if method() != "squash" {
		t.Fatalf("a new repo follows the default: %s", method())
	}

	ctx := context.Background()
	st, err := r.store.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st.DefaultMethod = "rebase"
	if err := r.store.PutSettings(ctx, st); err != nil {
		t.Fatal(err)
	}
	if err := r.p.Relink(ctx); err != nil {
		t.Fatal(err)
	}
	if method() != "rebase" {
		t.Fatalf("the repo follows the new default: %s", method())
	}

	// Its own method, kept but unused while it follows the default.
	if err := r.store.PutRepos(ctx, []store.RepoUpdate{{ID: "R-acme/web", Method: "merge"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.p.Relink(ctx); err != nil {
		t.Fatal(err)
	}
	if method() != "merge" {
		t.Fatalf("an explicit method is used: %s", method())
	}
	if err := r.store.PutRepos(ctx, []store.RepoUpdate{{ID: "R-acme/web", Method: "merge", Inherit: true}}); err != nil {
		t.Fatal(err)
	}
	if err := r.p.Relink(ctx); err != nil {
		t.Fatal(err)
	}
	if method() != "rebase" {
		t.Fatalf("back to the default: %s", method())
	}
}

func TestSnapshotCarriesTheViewerRoleOnTheRepository(t *testing.T) {
	r := newRig(t)
	w := pr("PR_w", 1, "acme/web", "feat/a", "main", "Writable")
	ro := pr("PR_r", 2, "oss/lib", "feat/b", "main", "Read only")
	ro.Repo.ViewerPermission = "READ"
	w.Repo.ViewerPermission = "MAINTAIN"
	odd := pr("PR_o", 3, "acme/odd", "feat/c", "main", "A role the contract lacks")
	odd.Repo.ViewerPermission = "SUPERUSER"
	none := pr("PR_n", 4, "acme/none", "feat/d", "main", "No role stored")
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = append(f.result.PRs, w, ro, odd, none) })
	r.sync()

	by := map[int]string{}
	for _, p := range r.snapshot().Prs {
		role := "absent"
		if p.Repo.ViewerPermission != nil {
			role = string(*p.Repo.ViewerPermission)
		}
		by[p.Number] = role
	}
	if by[1] != "MAINTAIN" || by[2] != "READ" || by[3] != "absent" || by[4] != "absent" {
		t.Fatalf("%v", by)
	}
}
