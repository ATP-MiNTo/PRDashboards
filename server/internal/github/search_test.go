package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

type sent struct {
	Query     string
	Variables map[string]any
}

type recorder struct {
	mu   sync.Mutex
	reqs []sent
}

func (r *recorder) all() []sent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sent(nil), r.reqs...)
}

// serveSearch answers every search with the fixtures in names, in order, separately for each
// query: the assigned, authored and review requested searches all walk the same list (so the union
// is the same pull requests, found three times).
func serveSearch(t *testing.T, names ...string) (*github.Client, *recorder) {
	t.Helper()
	return serveRouted(t, func(string) []string { return names })
}

// serveRouted answers a search with the next fixture of the list route returns for its query.
// Each query has its own position in its list, so concurrent searches do not disturb each other.
func serveRouted(t *testing.T, route func(q string) []string) (*github.Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	pos := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var s sent
		if err := json.Unmarshal(body, &s); err != nil {
			t.Errorf("request body: %v", err)
		}
		q, _ := s.Variables["q"].(string)
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, s)
		n := pos[q]
		pos[q]++
		rec.mu.Unlock()
		names := route(q)
		load(t, names[min(n, len(names)-1)]).write(w)
	}))
	t.Cleanup(srv.Close)
	clock := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	return &github.Client{
		HTTP:    srv.Client(),
		APIBase: srv.URL,
		Clock:   func() time.Time { return clock },
		Backoff: backoff.Policy{Sleep: func(context.Context, time.Duration) error { return nil }, Attempts: 2},
	}, rec
}

// queries returns the q variable of every recorded request, sorted.
func (r *recorder) queries() []string {
	var out []string
	for _, s := range r.all() {
		out = append(out, s.Variables["q"].(string))
	}
	slices.Sort(out)
	return out
}

// page builds a one-page search answer with the given pull request ids.
func page(ids []string, issueCount, remaining, cost int, extra map[string]any) fixtureBody {
	nodes := []any{}
	for _, id := range ids {
		nodes = append(nodes, map[string]any{"__typename": "PullRequest", "id": id, "number": 1, "mergedAt": "2026-11-24T08:00:00Z",
			"repository": map[string]any{"nameWithOwner": "acme/web"}})
	}
	body := map[string]any{"data": map[string]any{
		"rateLimit": map[string]any{"limit": 5000, "cost": cost, "remaining": remaining, "resetAt": "2026-11-24T10:00:00Z"},
		"search":    map[string]any{"issueCount": issueCount, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil}, "nodes": nodes},
	}}
	maps.Copy(body, extra)
	return fixtureBody{status: 200, body: body}
}

type fixtureBody struct {
	status  int
	headers map[string]string
	body    map[string]any
}

// serveBodies answers each search with the body route returns for its query and counts requests.
func serveBodies(t *testing.T, route func(q string) fixtureBody) *github.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var s sent
		_ = json.NewDecoder(r.Body).Decode(&s)
		f := route(s.Variables["q"].(string))
		for k, v := range f.headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		_ = json.NewEncoder(w).Encode(f.body)
	}))
	t.Cleanup(srv.Close)
	return &github.Client{
		HTTP:    srv.Client(),
		APIBase: srv.URL,
		Clock:   func() time.Time { return time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC) },
		Backoff: backoff.Policy{Sleep: func(context.Context, time.Duration) error { return nil }, Attempts: 1},
	}
}

func TestSearchOpenParsesPullRequestsAndPaginates(t *testing.T) {
	c, rec := serveSearch(t, "search_open_page1", "search_open_page2_partial")
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	reqs := rec.all()
	if len(reqs) != 6 || res.Requests != 6 {
		t.Fatalf("requests %d", len(reqs))
	}
	if want := []string{github.OpenAssignedQuery, github.OpenAssignedQuery, github.OpenAuthoredQuery, github.OpenAuthoredQuery, github.OpenReviewQuery, github.OpenReviewQuery}; !slices.Equal(rec.queries(), want) {
		t.Fatalf("queries %v", rec.queries())
	}
	cursors := 0
	for _, r := range reqs {
		if r.Variables["cursor"] == "CURSOR1" {
			cursors++
		}
	}
	if cursors != 3 {
		t.Fatalf("each search follows its cursor once, got %d", cursors)
	}
	for _, want := range []string{"first: 50", "statusCheckRollup", "contexts(first: 50)", "totalCount", "rateLimit", "squashMergeAllowed", "latestReviews", "reviewRequests", "... on StatusContext", "app {", "updatedAt", "additions", "deletions", "author {", "viewerPermission"} {
		if !strings.Contains(reqs[0].Query, want) {
			t.Errorf("query lacks %q", want)
		}
	}
	if len(res.PRs) != 3 {
		t.Fatalf("prs %d", len(res.PRs))
	}
	if res.RateLimit.Limit != 5000 || res.RateLimit.Remaining != 4989 || res.RateLimit.Cost != 6 ||
		!res.RateLimit.ResetAt.Equal(time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)) || res.RateLimit.Low() {
		t.Fatalf("rate limit %+v", res.RateLimit)
	}

	p := res.PRs[0]
	if p.ID != "PR_1" || p.Number != 101 || p.HeadRef != "feat/INP-101-login" || p.BaseRef != "main" || p.HeadSHA != "sha-101" ||
		p.Mergeable != "MERGEABLE" || p.MergeStateStatus != "CLEAN" || p.ReviewDecision != "APPROVED" || p.Draft {
		t.Fatalf("pr %+v", p)
	}
	if p.Repo.NameWithOwner != "acme/web" || !p.Repo.SquashAllowed || !p.Repo.MergeAllowed || !p.Repo.RebaseAllowed || p.Repo.ViewerPermission != "MAINTAIN" {
		t.Fatalf("repo %+v", p.Repo)
	}
	if p.CommittedAt == nil || !p.CommittedAt.Equal(time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("committed %v", p.CommittedAt)
	}
	if p.Author != "priya" || p.Additions != 214 || p.Deletions != 38 ||
		p.UpdatedAt == nil || !p.UpdatedAt.Equal(time.Date(2026, 11, 24, 8, 30, 0, 0, time.UTC)) {
		t.Fatalf("author, size and update time %q +%d -%d %v", p.Author, p.Additions, p.Deletions, p.UpdatedAt)
	}
	if len(p.Reviews) != 1 || p.Reviews[0].Author != "alice" || len(p.ReviewRequests) != 1 || p.ReviewRequests[0] != "U_bob" {
		t.Fatalf("reviews %+v requests %v", p.Reviews, p.ReviewRequests)
	}
	if !res.PRs[1].Draft || res.PRs[1].Repo.SquashAllowed || res.PRs[1].ReviewDecision != "" || res.PRs[1].Reviews[0].Author != "" {
		t.Fatalf("draft %+v", res.PRs[1])
	}
	// A deleted author comes as null, and a node without the new fields has no author, time or lines.
	if q := res.PRs[1]; q.Author != "" || q.Additions != 0 || q.Deletions != 5 || q.UpdatedAt == nil {
		t.Fatalf("deleted author %q +%d -%d %v", q.Author, q.Additions, q.Deletions, q.UpdatedAt)
	}
	if q := res.PRs[2]; q.Author != "" || q.UpdatedAt != nil || q.Additions != 0 || q.Deletions != 0 {
		t.Fatalf("missing fields %+v", q)
	}
	if res.PRs[2].CI.State != github.CINone || res.PRs[2].CommittedAt != nil {
		t.Fatalf("no commit data %+v", res.PRs[2].CI)
	}
}

func TestSearchOpenMapsEveryCheck(t *testing.T) {
	c, _ := serveSearch(t, "search_open_page1", "search_open_page2_partial")
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	ci := res.PRs[0].CI
	if ci.State != github.CIFailing || ci.Total != 60 || len(ci.Checks) != 10 {
		t.Fatalf("ci %+v", ci)
	}
	type row struct {
		state github.CheckState
		app   string
		url   string
	}
	want := map[string]row{
		"unit":           {github.CheckFailing, "GitHub Actions", "https://github.com/acme/web/actions/runs/1/job/3"},
		"ci/legacy":      {github.CheckFailing, "", "http://insecure.example.com/1"},
		"e2e":            {github.CheckRunning, "GitHub Actions", "https://github.com/acme/web/actions/runs/1/job/4"},
		"deploy-preview": {github.CheckPending, "", ""},
		"ci/queue":       {github.CheckPending, "", ""},
		"nightly":        {github.CheckCancelled, "GitHub Actions", "https://github.com/acme/web/actions/runs/1/job/6"},
		"lint":           {github.CheckPassing, "GitHub Actions", "https://github.com/acme/web/actions/runs/1/job/2"},
		"ci/jenkins":     {github.CheckPassing, "", "https://ci.example.com/job/web/42"},
		"advisory":       {github.CheckNeutral, "Advisor", "https://github.com/acme/web/actions/runs/1/job/7"},
		"docs":           {github.CheckSkipped, "GitHub Actions", "https://github.com/acme/web/actions/runs/1/job/5"},
	}
	var order []github.CheckState
	for _, ch := range ci.Checks {
		w, ok := want[ch.Name]
		if !ok || ch.State != w.state || ch.App != w.app || ch.URL != w.url {
			t.Errorf("check %+v, want %+v", ch, w)
		}
		order = append(order, ch.State)
	}
	wantOrder := []github.CheckState{"failing", "failing", "running", "pending", "pending", "cancelled", "passing", "passing", "neutral", "skipped"}
	for i, s := range wantOrder {
		if order[i] != s {
			t.Fatalf("order %v, want %v", order, wantOrder)
		}
	}
	var unit github.Check
	for _, ch := range ci.Checks {
		if ch.Name == "unit" {
			unit = ch
		}
	}
	if unit.StartedAt == nil || unit.CompletedAt == nil || unit.CompletedAt.Sub(*unit.StartedAt) != 4*time.Minute {
		t.Fatalf("times %+v", unit)
	}
	if res.PRs[1].CI.State != github.CIPassing {
		t.Fatalf("rollup %+v", res.PRs[1].CI)
	}
}

func TestSearchOpenPartialResultWithBlockedOrg(t *testing.T) {
	c, _ := serveSearch(t, "search_open_page1", "search_open_page2_partial")
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Blocked) != 1 || res.Blocked[0].Org != "acme-corp" || !strings.HasPrefix(res.Blocked[0].AuthorizeURL, "https://github.com/orgs/acme-corp/sso") {
		t.Fatalf("blocked %+v", res.Blocked)
	}
	if len(res.PRs) != 3 || res.Incomplete || len(res.Errors) != 0 {
		t.Fatalf("prs %d incomplete %v errors %v", len(res.PRs), res.Incomplete, res.Errors)
	}
}

func TestSearchOpenKeepsPRsWhenOtherErrorsCome(t *testing.T) {
	c, _ := serveSearch(t, "search_open_other_error")
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil || len(res.PRs) != 1 || len(res.Errors) != 3 || !strings.Contains(res.Errors[0], "INTERNAL") || len(res.Blocked) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSearchOpenBlockedWithNoData(t *testing.T) {
	c, _ := serveSearch(t, "search_open_sso_403")
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PRs) != 0 || len(res.Blocked) != 1 || res.Blocked[0].Org != "acme-corp" {
		t.Fatalf("%+v", res)
	}
}

func TestSearchOpenCapWarning(t *testing.T) {
	c, _ := serveSearch(t, "search_open_capped")
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil || !res.Capped || res.Total != 1500 || !res.RateLimit.Low() {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSearchOpenLaterPageFailureKeepsEarlierPRs(t *testing.T) {
	c, rec := serveSearch(t, "search_open_page1", "search_open_server_error")
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil || !res.Incomplete || len(res.PRs) != 2 || len(rec.all()) != 6 {
		t.Fatalf("%+v %v requests %d", res, err, len(rec.all()))
	}
}

func TestSearchOpenFirstPageErrors(t *testing.T) {
	cases := []struct {
		fixture string
		check   func(error) bool
	}{
		{"viewer_bad_credentials", func(err error) bool { return errors.Is(err, github.ErrBadCredentials) }},
		{"search_open_server_error", func(err error) bool { return err != nil && strings.Contains(err.Error(), "502") }},
		{"search_open_http_rate_limited", func(err error) bool {
			var rl *github.RateLimitedError
			return errors.As(err, &rl) && rl.RetryAfter == 900*time.Second &&
				rl.ResetAt.Equal(time.Date(2026, 11, 24, 9, 15, 0, 0, time.UTC))
		}},
		{"search_open_reset_403", func(err error) bool {
			var rl *github.RateLimitedError
			return errors.As(err, &rl) && rl.RetryAfter == 10*time.Minute &&
				rl.ResetAt.Equal(time.Date(2026, 11, 24, 9, 10, 0, 0, time.UTC))
		}},
		{"search_open_secondary_403", func(err error) bool {
			var rl *github.RateLimitedError
			return errors.As(err, &rl) && rl.RetryAfter == 0 && rl.ResetAt.IsZero()
		}},
		{"search_open_graphql_rate_limited", func(err error) bool {
			var rl *github.RateLimitedError
			return errors.As(err, &rl) && rl.RetryAfter == 0
		}},
	}
	for _, tc := range cases {
		c, _ := serveSearch(t, tc.fixture)
		if _, err := c.SearchOpen(context.Background(), "tok"); !tc.check(err) {
			t.Errorf("%s: %v", tc.fixture, err)
		}
	}
}

func TestSearchMerged(t *testing.T) {
	c, rec := serveSearch(t, "search_merged")
	since := time.Date(2026, 11, 24, 7, 30, 0, 0, time.FixedZone("x", 3600))
	res, err := c.SearchMerged(context.Background(), "tok", since)
	if err != nil {
		t.Fatal(err)
	}
	reqs := rec.all()
	want := []string{"is:pr assignee:@me is:merged updated:>=2026-11-24T06:30:00Z", "is:pr author:@me is:merged updated:>=2026-11-24T06:30:00Z", "is:pr review-requested:@me is:merged updated:>=2026-11-24T06:30:00Z"}
	if len(reqs) != 3 || !slices.Equal(rec.queries(), want) {
		t.Fatalf("%+v", reqs)
	}
	if len(res.Merged) != 1 || !res.Merged["PR_9"].Equal(time.Date(2026, 11, 24, 8, 45, 0, 0, time.UTC)) || res.Requests != 3 {
		t.Fatalf("%+v", res)
	}
}

func TestSearchMergedIncompleteIsAnError(t *testing.T) {
	c, _ := serveSearch(t, "search_open_page1", "search_open_server_error")
	if _, err := c.SearchMerged(context.Background(), "tok", time.Now()); err == nil {
		t.Fatal("an incomplete merged search must not resolve pull requests")
	}
}

func TestRateLimitLow(t *testing.T) {
	for _, tc := range []struct {
		r    github.RateLimit
		want bool
	}{
		{github.RateLimit{Limit: 5000, Remaining: 500}, false},
		{github.RateLimit{Limit: 5000, Remaining: 499}, true},
		{github.RateLimit{}, false},
	} {
		if got := tc.r.Low(); got != tc.want {
			t.Errorf("%+v: %v", tc.r, got)
		}
	}
}

func ids(prs []github.PR) []string {
	var out []string
	for _, p := range prs {
		out = append(out, p.ID)
	}
	return out
}

func TestSearchOpenSendsAllThreeQueriesAndMergesByID(t *testing.T) {
	var mu sync.Mutex
	var sentQueries []string
	c := serveBodies(t, func(q string) fixtureBody {
		mu.Lock()
		sentQueries = append(sentQueries, q)
		mu.Unlock()
		switch q {
		case github.OpenAssignedQuery:
			return page([]string{"A", "B", "C"}, 3, 4000, 1, nil)
		case github.OpenAuthoredQuery:
			return page([]string{"C", "D", "A", "E"}, 4, 4500, 1, nil)
		}
		return page([]string{"F", "B", "E"}, 3, 4700, 1, nil)
	})
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(sentQueries)
	if !slices.Equal(sentQueries, []string{"is:pr is:open assignee:@me archived:false", "is:pr is:open author:@me archived:false", "is:pr is:open review-requested:@me archived:false"}) {
		t.Fatalf("queries %v", sentQueries)
	}
	if got := ids(res.PRs); !slices.Equal(got, []string{"A", "B", "C", "D", "E", "F"}) {
		t.Fatalf("assigned first, then authored, then review requested ones not yet seen: %v", got)
	}
	roles := map[string][]string{}
	for _, p := range res.PRs {
		roles[p.ID] = p.Roles
	}
	const as, au, rv = github.RoleAssigned, github.RoleAuthored, github.RoleReviewer
	want := map[string][]string{"A": {as, au}, "B": {as, rv}, "C": {as, au}, "D": {au}, "E": {au, rv}, "F": {rv}}
	for id, w := range want {
		if !slices.Equal(roles[id], w) {
			t.Errorf("roles of %s: got %v, want %v", id, roles[id], w)
		}
	}
	if res.Total != 6 || res.Capped || res.Incomplete || res.Requests != 3 || len(res.Errors) != 0 || len(res.Blocked) != 0 {
		t.Fatalf("%+v", res)
	}
	if res.RateLimit.Cost != 3 || res.RateLimit.Remaining != 4000 || res.RateLimit.Limit != 5000 {
		t.Fatalf("rate limit takes the lowest remaining and sums cost: %+v", res.RateLimit)
	}
}

func TestSearchOpenMergesCapIncompleteBlockedAndErrors(t *testing.T) {
	sso := map[string]string{"X-GitHub-SSO": "required; url=https://github.com/orgs/acme-corp/sso?authorization_request=X"}
	forbidden := map[string]any{"errors": []map[string]string{{"type": "FORBIDDEN", "message": "Resource protected by organization SAML enforcement. You must grant your OAuth token access to the `acme-corp` organization."}}}
	other := map[string]any{"errors": []map[string]string{{"type": "INTERNAL", "message": "boom"}}}
	c := serveBodies(t, func(q string) fixtureBody {
		if q == github.OpenAssignedQuery {
			f := page([]string{"A"}, 1500, 4900, 1, forbidden)
			f.headers = sso
			return f
		}
		f := page([]string{"B"}, 1, 100, 3, other)
		return f
	})
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Capped || res.Total != 1500 || len(res.PRs) != 2 {
		t.Fatalf("capped by either search: %+v", res)
	}
	if len(res.Blocked) != 1 || res.Blocked[0].Org != "acme-corp" || len(res.Errors) != 2 || !strings.Contains(res.Errors[0], "INTERNAL") {
		t.Fatalf("blocked %+v errors %v", res.Blocked, res.Errors)
	}
	if res.RateLimit.Remaining != 100 || res.RateLimit.Cost != 7 || !res.RateLimit.Low() {
		t.Fatalf("rate limit %+v", res.RateLimit)
	}
}

func TestSearchOpenPermissionForbiddenIsAnErrorNotABanner(t *testing.T) {
	cases := []struct {
		name    string
		message string
		blocked int
		errors  int
	}{
		{"permission", "Resource not accessible by personal access token", 0, 1},
		{"integration", "Resource not accessible by integration", 0, 1},
		{"sso", "Resource protected by organization SAML enforcement. You must grant your OAuth token access to the `acme-corp` organization.", 1, 0},
		{"policy", "The `acme-corp` organization forbids access via a personal access token (classic).", 1, 0},
	}
	for _, tc := range cases {
		forbidden := map[string]any{"errors": []map[string]string{{"type": "FORBIDDEN", "message": tc.message}}}
		c := serveBodies(t, func(q string) fixtureBody {
			if q == github.OpenAssignedQuery {
				return page([]string{"A"}, 1, 4900, 1, forbidden)
			}
			return page([]string{"B"}, 1, 4900, 1, nil)
		})
		res, err := c.SearchOpen(context.Background(), "tok")
		if err != nil || len(res.PRs) != 2 || len(res.Blocked) != tc.blocked || len(res.Errors) != tc.errors {
			t.Fatalf("%s: %+v %v", tc.name, res, err)
		}
		if tc.errors == 1 && !strings.Contains(res.Errors[0], "Resource not accessible") {
			t.Errorf("%s: %v", tc.name, res.Errors)
		}
		if tc.blocked == 1 && res.Blocked[0].Org != "acme-corp" {
			t.Errorf("%s: %+v", tc.name, res.Blocked)
		}
	}
}

func TestSearchOpenBlockedOrgIsListedOnce(t *testing.T) {
	sso := map[string]string{"X-GitHub-SSO": "required; url=https://github.com/orgs/acme-corp/sso?authorization_request=X"}
	forbidden := map[string]any{"errors": []map[string]string{{"type": "FORBIDDEN", "message": "Resource protected by organization SAML enforcement. You must grant your OAuth token access to the `acme-corp` organization."}}}
	c := serveBodies(t, func(string) fixtureBody {
		f := page(nil, 0, 4000, 1, forbidden)
		f.headers = sso
		return f
	})
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil || len(res.Blocked) != 1 || len(res.PRs) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSearchOpenIncompleteWhenEitherSearchLosesAPage(t *testing.T) {
	c, _ := serveRouted(t, func(q string) []string {
		if q == github.OpenAuthoredQuery {
			return []string{"search_open_page1", "search_open_server_error"}
		}
		return []string{"search_open_capped"}
	})
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil || !res.Incomplete || !res.Capped {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSearchOpenOneSearchFailingReturnsTheError(t *testing.T) {
	cases := []struct {
		name     string
		assigned string
		authored string
		check    func(error) bool
	}{
		{"authored fails", "search_open_page2_partial", "search_open_server_error", func(err error) bool { return err != nil && strings.Contains(err.Error(), "502") }},
		{"assigned fails", "search_open_server_error", "search_open_page2_partial", func(err error) bool { return err != nil && strings.Contains(err.Error(), "502") }},
		{"bad credentials beats a rate limit", "search_open_http_rate_limited", "viewer_bad_credentials", func(err error) bool { return errors.Is(err, github.ErrBadCredentials) }},
		{"bad credentials beats a rate limit, other order", "viewer_bad_credentials", "search_open_http_rate_limited", func(err error) bool { return errors.Is(err, github.ErrBadCredentials) }},
		{"rate limit beats a plain error", "search_open_server_error", "search_open_http_rate_limited", func(err error) bool {
			var rl *github.RateLimitedError
			return errors.As(err, &rl)
		}},
		{"rate limit beats a plain error, other order", "search_open_http_rate_limited", "search_open_server_error", func(err error) bool {
			var rl *github.RateLimitedError
			return errors.As(err, &rl)
		}},
	}
	for _, tc := range cases {
		c, _ := serveRouted(t, func(q string) []string {
			if q == github.OpenAssignedQuery {
				return []string{tc.assigned}
			}
			return []string{tc.authored}
		})
		res, err := c.SearchOpen(context.Background(), "tok")
		if !tc.check(err) || len(res.PRs) != 0 {
			t.Errorf("%s: %v (%d prs)", tc.name, err, len(res.PRs))
		}
	}
}

func TestSearchOpenReviewSearchFailingReturnsTheError(t *testing.T) {
	c, _ := serveRouted(t, func(q string) []string {
		if q == github.OpenReviewQuery {
			return []string{"search_open_server_error"}
		}
		return []string{"search_open_page2_partial"}
	})
	res, err := c.SearchOpen(context.Background(), "tok")
	if err == nil || !strings.Contains(err.Error(), "502") || len(res.PRs) != 0 {
		t.Fatalf("%v (%d prs)", err, len(res.PRs))
	}
}

func TestSearchOpenStopsOnCancelledContext(t *testing.T) {
	c, _ := serveSearch(t, "search_open_page1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.SearchOpen(ctx, "tok"); err == nil {
		t.Fatal("a cancelled context must fail the search")
	}
}

func TestSearchMergedUnionsAllThreeSearches(t *testing.T) {
	c := serveBodies(t, func(q string) fixtureBody {
		if strings.HasPrefix(q, "is:pr assignee:@me") {
			return page([]string{"M1", "M2"}, 2, 4000, 1, nil)
		}
		if strings.HasPrefix(q, "is:pr author:@me") {
			return page([]string{"M2", "M3"}, 2, 3000, 1, nil)
		}
		return page([]string{"M4"}, 1, 3500, 1, nil) // review requested: only found there
	})
	res, err := c.SearchMerged(context.Background(), "tok", time.Date(2026, 11, 24, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Merged) != 4 || res.Requests != 3 || res.RateLimit.Remaining != 3000 || res.RateLimit.Cost != 3 {
		t.Fatalf("%+v", res)
	}
	for _, id := range []string{"M1", "M2", "M3", "M4"} {
		if _, ok := res.Merged[id]; !ok {
			t.Errorf("missing %s", id)
		}
	}
}

func TestSearchMergedAnySearchFailingIsAnError(t *testing.T) {
	for _, failing := range []string{"is:pr assignee:@me", "is:pr author:@me", "is:pr review-requested:@me"} {
		c := serveBodies(t, func(q string) fixtureBody {
			if strings.HasPrefix(q, failing) {
				return fixtureBody{status: 500, body: map[string]any{"message": "boom"}}
			}
			return page([]string{"M1"}, 1, 4000, 1, nil)
		})
		if res, err := c.SearchMerged(context.Background(), "tok", time.Now()); err == nil || len(res.Merged) != 0 {
			t.Errorf("%s: %v %+v", failing, err, res)
		}
	}
}
