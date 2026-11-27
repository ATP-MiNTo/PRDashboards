package tokenpick_test

import (
	"slices"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/tokenpick"
)

func TestRank(t *testing.T) {
	order := []string{"ADMIN", "MAINTAIN", "WRITE", "TRIAGE", "READ", "", "SOMETHING_NEW"}
	for i := 0; i+1 < len(order); i++ {
		if tokenpick.Rank(order[i]) <= tokenpick.Rank(order[i+1]) && order[i+1] != "SOMETHING_NEW" {
			t.Errorf("%s must rank above %s", order[i], order[i+1])
		}
	}
	if tokenpick.Rank("") != 0 || tokenpick.Rank("SOMETHING_NEW") != 0 {
		t.Error("unknown roles rank last")
	}
}

func TestChoose(t *testing.T) {
	for _, tc := range []struct {
		name  string
		perms []string
		want  int
	}{
		{"empty", nil, -1},
		{"one", []string{"READ"}, 0},
		{"highest wins", []string{"READ", "WRITE", "TRIAGE"}, 1},
		{"admin over maintain", []string{"MAINTAIN", "ADMIN"}, 1},
		{"tie goes to the first", []string{"WRITE", "WRITE"}, 0},
		{"tie after a lower one", []string{"READ", "WRITE", "WRITE"}, 1},
		{"unknown loses to read", []string{"", "READ"}, 1},
		{"all unknown, first", []string{"", "NEW"}, 0},
		{"unknown first, known wins", []string{"NEW", "TRIAGE"}, 1},
	} {
		if got := tokenpick.Choose(tc.perms); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

func prOf(id, repo, perm, title string, roles ...string) github.PR {
	return github.PR{ID: id, Title: title, Roles: roles, Repo: github.Repo{ID: "R-" + repo, NameWithOwner: repo, ViewerPermission: perm}}
}

func found(id string, rl github.RateLimit, prs ...github.PR) tokenpick.Found {
	return tokenpick.Found{Token: tokenpick.Token{ID: id, Name: "name-" + id, Login: "login-" + id}, Result: github.SearchResult{PRs: prs, RateLimit: rl}}
}

func ids(prs []github.PR) []string {
	out := make([]string, len(prs))
	for i, p := range prs {
		out[i] = p.ID
	}
	return out
}

func TestMergeChoosesTheTokenPerRepository(t *testing.T) {
	m := tokenpick.Merge([]tokenpick.Found{
		found("a", github.RateLimit{},
			prOf("1", "acme/web", "WRITE", "web from a"),
			prOf("2", "oss/lib", "READ", "lib from a"),
			prOf("3", "acme/tie", "WRITE", "tie from a"),
			prOf("6", "only/a", "", "only a"),
		),
		found("b", github.RateLimit{},
			prOf("2", "oss/lib", "WRITE", "lib from b"),
			prOf("4", "oss/two", "TRIAGE", "two from b"),
			prOf("3", "acme/tie", "WRITE", "tie from b"),
			prOf("5", "oss/lib", "WRITE", "lib 5 from b"),
		),
		found("c", github.RateLimit{},
			prOf("4", "oss/two", "ADMIN", "two from c"),
		),
	})
	wantRepo := map[string]string{"R-acme/web": "a", "R-oss/lib": "b", "R-acme/tie": "a", "R-oss/two": "c", "R-only/a": "a"}
	for repo, tok := range wantRepo {
		if m.Repos[repo].ID != tok {
			t.Errorf("%s: token %q, want %q", repo, m.Repos[repo].ID, tok)
		}
	}
	if len(m.Repos) != len(wantRepo) {
		t.Errorf("%v", m.Repos)
	}
	if got := ids(m.Result.PRs); !slices.Equal(got, []string{"1", "2", "3", "6", "4", "5"}) {
		t.Fatalf("each pull request once, in the order they were first listed: %v", got)
	}
	byID := map[string]github.PR{}
	for _, p := range m.Result.PRs {
		byID[p.ID] = p
	}
	for id, want := range map[string]string{"1": "web from a", "2": "lib from b", "3": "tie from a", "4": "two from c", "5": "lib 5 from b"} {
		if byID[id].Title != want {
			t.Errorf("PR %s: %q, want the chosen token's copy %q", id, byID[id].Title, want)
		}
	}
	for id, want := range map[string]string{"2": "WRITE", "4": "ADMIN", "6": ""} {
		if got := byID[id].Repo.ViewerPermission; got != want {
			t.Errorf("PR %s carries permission %q, want %q", id, got, want)
		}
	}
}

func TestMergeTakesAPullRequestTheChosenTokenDidNotReturnFromTheBestOneThatDid(t *testing.T) {
	m := tokenpick.Merge([]tokenpick.Found{
		found("a", github.RateLimit{}, prOf("1", "oss/lib", "TRIAGE", "1 from a"), prOf("2", "oss/lib", "TRIAGE", "2 from a")),
		found("b", github.RateLimit{}, prOf("9", "oss/lib", "ADMIN", "9 from b")),
		found("c", github.RateLimit{}, prOf("2", "oss/lib", "READ", "2 from c"), prOf("3", "oss/lib", "READ", "3 from c")),
	})
	if m.Repos["R-oss/lib"].ID != "b" {
		t.Fatalf("%+v", m.Repos)
	}
	byID := map[string]github.PR{}
	for _, p := range m.Result.PRs {
		byID[p.ID] = p
	}
	if byID["1"].Title != "1 from a" || byID["2"].Title != "2 from a" || byID["3"].Title != "3 from c" || byID["9"].Title != "9 from b" {
		t.Fatalf("%+v", byID)
	}
	for id, p := range byID {
		if p.Repo.ViewerPermission != "ADMIN" {
			t.Errorf("PR %s: permission %q, want the chosen token's", id, p.Repo.ViewerPermission)
		}
	}
}

func TestMergeUnionsRolesInTheOrderOfTheContract(t *testing.T) {
	r := github.RoleReviewer
	a, au := github.RoleAssigned, github.RoleAuthored
	m := tokenpick.Merge([]tokenpick.Found{
		found("a", github.RateLimit{}, prOf("1", "x/y", "WRITE", "t", r), prOf("2", "x/y", "WRITE", "t", a)),
		found("b", github.RateLimit{}, prOf("1", "x/y", "WRITE", "t", au, a), prOf("2", "x/y", "WRITE", "t", a)),
	})
	if got := m.Result.PRs[0].Roles; !slices.Equal(got, []string{a, au, r}) {
		t.Errorf("%v", got)
	}
	if got := m.Result.PRs[1].Roles; !slices.Equal(got, []string{a}) {
		t.Errorf("%v", got)
	}
	// A pull request without roles stays without (the caller decides what that means).
	m = tokenpick.Merge([]tokenpick.Found{found("a", github.RateLimit{}, prOf("1", "x/y", "WRITE", "t"))})
	if len(m.Result.PRs[0].Roles) != 0 {
		t.Errorf("%v", m.Result.PRs[0].Roles)
	}
}

func TestMergeCombinesTheRestOfTheResult(t *testing.T) {
	a := found("a", github.RateLimit{}, prOf("1", "acme/web", "WRITE", "t"))
	a.Result.Total, a.Result.Requests, a.Result.Errors = 40, 3, []string{"e1"}
	a.Result.Blocked = []github.OrgBlocked{{Org: "oss", Reason: github.ReasonSSO}, {Org: "gone", Reason: github.ReasonSSO}, {Reason: github.ReasonPolicy}}
	b := found("b", github.RateLimit{}, prOf("2", "oss/lib", "WRITE", "t"))
	b.Result.Total, b.Result.Requests, b.Result.Errors, b.Result.Capped = 7, 3, []string{"e1", "e2"}, true
	b.Result.Blocked = []github.OrgBlocked{{Org: "gone", Reason: github.ReasonSSO}, {Org: "GONE", Reason: github.ReasonPolicy}, {Reason: github.ReasonPolicy}}
	c := found("c", github.RateLimit{})
	c.Result.Incomplete = true

	got := tokenpick.Merge([]tokenpick.Found{a, b, c}).Result
	if got.Total != 40 || got.Requests != 6 || !got.Capped || !got.Incomplete {
		t.Errorf("%+v", got)
	}
	if !slices.Equal(got.Errors, []string{"e1", "e2"}) {
		t.Errorf("%v", got.Errors)
	}
	// oss came back through b, so it is not blocked. gone is blocked once per reason; one without
	// a name stays (it cannot be matched).
	want := []github.OrgBlocked{{Org: "gone", Reason: github.ReasonSSO}, {Reason: github.ReasonPolicy}, {Org: "GONE", Reason: github.ReasonPolicy}}
	if !slices.Equal(got.Blocked, want) {
		t.Errorf("%+v", got.Blocked)
	}
	// With few pull requests, the total is at least what is listed.
	small := tokenpick.Merge([]tokenpick.Found{found("a", github.RateLimit{}, prOf("1", "a/b", "READ", "t"), prOf("2", "a/b", "READ", "t"))})
	if small.Result.Total != 2 {
		t.Errorf("%d", small.Result.Total)
	}
}

func TestMergeOfNothing(t *testing.T) {
	m := tokenpick.Merge(nil)
	if len(m.Result.PRs) != 0 || len(m.Repos) != 0 || m.Lowest != (github.RateLimit{}) || m.Tightest != (github.RateLimit{}) {
		t.Fatalf("%+v", m)
	}
}

func TestRateLimits(t *testing.T) {
	rl := func(limit, remaining, cost int) github.RateLimit {
		return github.RateLimit{Limit: limit, Remaining: remaining, Cost: cost}
	}
	for _, tc := range []struct {
		name             string
		in               []github.RateLimit
		lowest, tightest github.RateLimit
	}{
		{"none", nil, github.RateLimit{}, github.RateLimit{}},
		{"no limit reported", []github.RateLimit{rl(0, 0, 2), rl(0, 0, 3)}, github.RateLimit{Cost: 5}, github.RateLimit{Cost: 5}},
		{"one", []github.RateLimit{rl(5000, 100, 3)}, rl(5000, 100, 3), rl(5000, 100, 3)},
		{"lowest remaining is not the highest share", []github.RateLimit{rl(5000, 100, 3), rl(60, 5, 3)}, rl(60, 5, 6), rl(5000, 100, 6)},
		{"same token twice picks the first on a tie", []github.RateLimit{rl(5000, 100, 1), rl(5000, 100, 1)}, rl(5000, 100, 2), rl(5000, 100, 2)},
		{"an entry without a limit is ignored", []github.RateLimit{rl(0, 0, 1), rl(100, 90, 1)}, rl(100, 90, 2), rl(100, 90, 2)},
		{"higher share wins", []github.RateLimit{rl(100, 50, 0), rl(1000, 400, 0)}, rl(100, 50, 0), rl(1000, 400, 0)},
	} {
		if got := tokenpick.Lowest(tc.in); got != tc.lowest {
			t.Errorf("%s: lowest %+v, want %+v", tc.name, got, tc.lowest)
		}
		if got := tokenpick.Tightest(tc.in); got != tc.tightest {
			t.Errorf("%s: tightest %+v, want %+v", tc.name, got, tc.tightest)
		}
	}
}

func TestMergeRateLimits(t *testing.T) {
	m := tokenpick.Merge([]tokenpick.Found{
		found("a", github.RateLimit{Limit: 5000, Remaining: 100}),
		found("b", github.RateLimit{Limit: 60, Remaining: 5}),
	})
	if m.Lowest.Limit != 60 || m.Tightest.Limit != 5000 || m.Result.RateLimit != m.Lowest {
		t.Fatalf("%+v", m)
	}
}
