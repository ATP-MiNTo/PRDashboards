package poller_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

// inRepo is a pull request of a repository where the token's account has the given role.
func inRepo(id string, n int, repo, perm string, roles ...string) github.PR {
	p := pr(id, n, repo, "feat/"+id, "main", id+" "+perm)
	p.Repo.ViewerPermission = perm
	p.Roles = roles
	return p
}

func search(rl github.RateLimit, prs ...github.PR) github.SearchResult {
	return github.SearchResult{PRs: prs, RateLimit: rl}
}

var budget = github.RateLimit{Limit: 5000, Remaining: 4900}

// twoTokens sets up the fake accounts and the fake GitHub with two credentials, A (the primary
// one) and B, each with its own answer.
func twoTokens(t *testing.T, a, b github.SearchResult) *rig {
	t.Helper()
	r := newRig(t)
	r.accounts.set(func(f *fakeAccounts) {
		f.creds = []fakeCred{
			{id: "aaaaaaaa", name: "Work", login: "alice", token: "tok-a"},
			{id: "bbbbbbbb", name: "OSS", login: "alice", token: "tok-b"},
		}
	})
	r.gh.set(func(f *fakeGitHub) { f.byToken = map[string]github.SearchResult{"tok-a": a, "tok-b": b} })
	return r
}

func repoOf(t *testing.T, s gen.Snapshot, nameWithOwner string) gen.SnapshotRepo {
	t.Helper()
	for _, p := range s.Prs {
		if p.Repo.NameWithOwner == nameWithOwner {
			return p.Repo
		}
	}
	t.Fatalf("no pull request in %s", nameWithOwner)
	return gen.SnapshotRepo{}
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestTokensChooseTheHighestPermissionPerRepository(t *testing.T) {
	r := twoTokens(t,
		search(budget,
			inRepo("PR_w1", 1, "acme/web", "WRITE", github.RoleAssigned),
			inRepo("PR_o1", 41, "oss/lib", "READ", github.RoleReviewer),
			inRepo("PR_t1", 5, "acme/tie", "WRITE", github.RoleAssigned),
		),
		search(budget,
			inRepo("PR_o1", 41, "oss/lib", "WRITE", github.RoleAuthored),
			inRepo("PR_o2", 42, "oss/lib", "WRITE", github.RoleAuthored),
			inRepo("PR_t1", 5, "acme/tie", "WRITE", github.RoleAuthored),
			inRepo("PR_ad", 9, "oss/admin", "ADMIN", github.RoleAssigned),
		),
	)
	r.sync()
	s := r.snapshot()
	if s.Sources.Github.State != "ok" || len(s.Prs) != 5 {
		t.Fatalf("%+v %d", s.Sources.Github, len(s.Prs))
	}
	for repo, want := range map[string]struct{ id, name, perm string }{
		"acme/web":  {"aaaaaaaa", "Work", "WRITE"},
		"oss/lib":   {"bbbbbbbb", "OSS", "WRITE"},
		"acme/tie":  {"aaaaaaaa", "Work", "WRITE"},
		"oss/admin": {"bbbbbbbb", "OSS", "ADMIN"},
	} {
		got := repoOf(t, s, repo)
		if str(got.TokenId) != want.id || str(got.TokenName) != want.name || got.ViewerPermission == nil || string(*got.ViewerPermission) != want.perm {
			t.Errorf("%s: token %s (%s) permission %v, want %+v", repo, str(got.TokenId), str(got.TokenName), got.ViewerPermission, want)
		}
	}
	// The pull request both tokens listed appears once, with the roles of both and the fields of
	// the token that was chosen for its repository.
	var seen int
	for _, p := range s.Prs {
		if p.Id != "PR_o1" {
			continue
		}
		seen++
		if p.Title != "PR_o1 WRITE" || !slices.Equal(p.Roles, []gen.SnapshotPrRoles{gen.Authored, gen.Reviewer}) {
			t.Errorf("%q %v", p.Title, p.Roles)
		}
	}
	if seen != 1 {
		t.Fatalf("PR_o1 listed %d times", seen)
	}
	// Ties go to the first token in the list.
	if tie := repoOf(t, s, "acme/tie"); str(tie.TokenId) != "aaaaaaaa" {
		t.Fatal("tie")
	}
	// Each token did its own searches, in list order.
	if got := r.gh.openTokens; !slices.Equal(got, []string{"tok-a", "tok-b"}) {
		t.Fatalf("%v", got)
	}
}

func TestTokensSnapshotCarriesTheChosenTokenAfterAReload(t *testing.T) {
	r := twoTokens(t, search(budget, inRepo("PR_a", 1, "acme/web", "WRITE")), search(budget))
	r.sync()
	p2 := r.newPoller(nil, nil)
	if err := p2.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := repoOf(t, p2.Current().Snapshot, "acme/web")
	if str(got.TokenId) != "aaaaaaaa" || str(got.TokenName) != "Work" {
		t.Fatalf("%+v", got)
	}
	l, ok := p2.Linked("PR_a")
	if !ok || l.TokenID != "aaaaaaaa" {
		t.Fatalf("%+v %v", l, ok)
	}
}

func TestTokensRateLimitShowsTheTightestBudget(t *testing.T) {
	r := twoTokens(t,
		search(github.RateLimit{Limit: 5000, Remaining: 100}, inRepo("PR_a", 1, "acme/web", "WRITE")),
		search(github.RateLimit{Limit: 60, Remaining: 5}, inRepo("PR_b", 2, "oss/lib", "WRITE")),
	)
	r.sync()
	rl := r.snapshot().Sources.Github.RateLimit
	if rl == nil || rl.Used != 4900 || rl.Limit != 5000 {
		t.Fatalf("the highest share used is shown: %+v", rl)
	}
}

func TestTokensOneRejectedTokenFailsTheRefreshAndNamesIt(t *testing.T) {
	good := search(budget, inRepo("PR_a", 1, "acme/web", "WRITE"))
	r := twoTokens(t, good, search(budget, inRepo("PR_b", 2, "oss/lib", "WRITE")))
	r.sync()
	if len(r.snapshot().Prs) != 2 {
		t.Fatal("setup")
	}
	r.accounts.set(func(f *fakeAccounts) { f.creds[1].token = "revoked" })
	r.sync()
	s := r.snapshot().Sources.Github
	if s.State != "disconnected" || s.Error == nil || *s.Error != `GitHub token "OSS" was rejected` {
		t.Fatalf("%+v %s", s, str(s.Error))
	}
	if got := r.accounts.rejected; !slices.Equal(got, []string{"bbbbbbbb"}) {
		t.Fatalf("only the rejected token is marked: %v", got)
	}
}

func TestTokensOneTokenThatNeedsAReconnectFailsTheRefreshAndNamesIt(t *testing.T) {
	r := twoTokens(t, search(budget, inRepo("PR_a", 1, "acme/web", "WRITE")), search(budget))
	r.accounts.set(func(f *fakeAccounts) { f.creds[0].err = fmt.Errorf("refresh: %w", auth.ErrNeedsReconnect) })
	r.sync()
	s := r.snapshot().Sources.Github
	if s.State != "disconnected" || s.Error == nil || *s.Error != `Reconnect GitHub token "Work"` {
		t.Fatalf("%+v %s", s, str(s.Error))
	}
	if len(r.accounts.rejected) != 0 {
		t.Fatalf("the others are not marked: %v", r.accounts.rejected)
	}
	if got := r.gh.openTokens; len(got) != 0 {
		t.Fatalf("a token that cannot be used is not searched with, and the refresh stops: %v", got)
	}
}

func TestTokensAFailureOfOneKeepsTheLastListAndBacksOff(t *testing.T) {
	r := twoTokens(t,
		search(budget, inRepo("PR_a", 1, "acme/web", "WRITE")),
		search(budget, inRepo("PR_b", 2, "oss/lib", "WRITE")),
	)
	r.sync()
	r.clock.Advance(time.Minute)
	r.gh.set(func(f *fakeGitHub) { f.errByToken = map[string]error{"tok-b": errors.New("boom")} })
	r.sync()
	s := r.snapshot()
	if s.Sources.Github.State != "error" || len(s.Prs) != 2 || str(repoOf(t, s, "oss/lib").TokenId) != "bbbbbbbb" {
		t.Fatalf("the list stays: %+v %d", s.Sources.Github, len(s.Prs))
	}
	r.clock.Advance(time.Minute)
	r.gh.set(func(f *fakeGitHub) {
		f.errByToken = map[string]error{"tok-a": &github.RateLimitedError{RetryAfter: time.Minute}}
	})
	r.sync()
	if s := r.snapshot().Sources.Github; s.State != "rate_limited" || s.ResetAt == nil || len(r.snapshot().Prs) != 2 {
		t.Fatalf("a rate limit from any token applies as for one: %+v", s)
	}
}

func TestTokensMergedSearchRunsForEveryTokenAndIsUnioned(t *testing.T) {
	a := inRepo("PR_a", 1, "acme/web", "WRITE")
	b := inRepo("PR_b", 2, "oss/lib", "WRITE")
	c := inRepo("PR_c", 3, "oss/lib", "WRITE")
	r := twoTokens(t, search(budget, a), search(budget, b, c))
	r.sync()

	mergedAt := r.clock.Now().Add(-2 * time.Minute)
	r.clock.Advance(time.Minute)
	r.gh.set(func(f *fakeGitHub) {
		f.byToken = map[string]github.SearchResult{"tok-a": search(budget, a), "tok-b": search(budget)}
		f.mergedByToken = map[string]map[string]time.Time{"tok-b": {"PR_b": mergedAt}}
	})
	r.sync()
	if got := r.gh.mergedTokens; !slices.Equal(got, []string{"tok-a", "tok-b"}) {
		t.Fatalf("one merged search per token: %v", got)
	}
	open, err := r.store.OpenPRs(context.Background())
	if err != nil || len(open) != 1 {
		t.Fatalf("PR_b was merged, PR_c closed: %v %v", open, err)
	}
	// If one token's merged search fails, nothing is settled and the list says so.
	r2 := twoTokens(t, search(budget, a), search(budget, b))
	r2.sync()
	r2.clock.Advance(time.Minute)
	r2.gh.set(func(f *fakeGitHub) {
		f.byToken = map[string]github.SearchResult{"tok-a": search(budget, a), "tok-b": search(budget)}
		f.mergedByToken = map[string]map[string]time.Time{}
		f.mergedErr = errors.New("boom")
	})
	r2.sync()
	if w := r2.snapshot().Warnings; len(w) != 1 || w[0].Code != "merged_lookup_failed" {
		t.Fatalf("%+v", w)
	}
}

func TestTokensBlockedOrganizationGotThroughWithAnotherToken(t *testing.T) {
	a := search(budget, inRepo("PR_a", 1, "acme/web", "WRITE"))
	a.Blocked = []github.OrgBlocked{{Org: "oss", Reason: github.ReasonSSO}, {Org: "other", Reason: github.ReasonSSO}}
	r := twoTokens(t, a, search(budget, inRepo("PR_b", 2, "oss/lib", "WRITE")))
	r.sync()
	b := r.snapshot().Sources.BlockedOrgs
	if len(b) != 1 || b[0].Org != "other" {
		t.Fatalf("an organization another token reads is not blocked: %+v", b)
	}
}

func TestTokensNoCredentialIsDisconnected(t *testing.T) {
	r := newRig(t, inRepo("PR_a", 1, "acme/web", "WRITE"))
	r.accounts.set(func(f *fakeAccounts) { f.creds = []fakeCred{} })
	r.sync()
	if got := r.snapshot().Sources.Github.State; got != "ok" {
		t.Fatalf("empty creds falls back to the default token: %s", got)
	}
	r.accounts.set(func(f *fakeAccounts) { f.tokenEr = auth.ErrNotConnected })
	r.sync()
	if s := r.snapshot(); s.Sources.Github.State != "disconnected" || len(s.Prs) != 0 {
		t.Fatalf("%+v", s.Sources.Github)
	}
}
