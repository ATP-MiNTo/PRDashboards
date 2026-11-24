package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

func serveSearch(t *testing.T, names ...string) (*github.Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var s sent
		if err := json.Unmarshal(body, &s); err != nil {
			t.Errorf("request body: %v", err)
		}
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, s)
		n := len(rec.reqs)
		rec.mu.Unlock()
		load(t, names[min(n, len(names))-1]).write(w)
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

func TestSearchOpenParsesPullRequestsAndPaginates(t *testing.T) {
	c, rec := serveSearch(t, "search_open_page1", "search_open_page2_partial")
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	reqs := rec.all()
	if len(reqs) != 2 || res.Requests != 2 {
		t.Fatalf("requests %d", len(reqs))
	}
	if reqs[0].Variables["q"] != github.OpenQuery || reqs[0].Variables["cursor"] != nil || reqs[1].Variables["cursor"] != "CURSOR1" {
		t.Fatalf("variables %v / %v", reqs[0].Variables, reqs[1].Variables)
	}
	for _, want := range []string{"first: 50", "statusCheckRollup", "contexts(first: 50)", "totalCount", "rateLimit", "squashMergeAllowed", "latestReviews", "reviewRequests", "... on StatusContext", "app {"} {
		if !strings.Contains(reqs[0].Query, want) {
			t.Errorf("query lacks %q", want)
		}
	}
	if len(res.PRs) != 3 {
		t.Fatalf("prs %d", len(res.PRs))
	}
	if res.RateLimit.Limit != 5000 || res.RateLimit.Remaining != 4989 || res.RateLimit.Cost != 2 ||
		!res.RateLimit.ResetAt.Equal(time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)) || res.RateLimit.Low() {
		t.Fatalf("rate limit %+v", res.RateLimit)
	}

	p := res.PRs[0]
	if p.ID != "PR_1" || p.Number != 101 || p.HeadRef != "feat/INP-101-login" || p.BaseRef != "main" || p.HeadSHA != "sha-101" ||
		p.Mergeable != "MERGEABLE" || p.MergeStateStatus != "CLEAN" || p.ReviewDecision != "APPROVED" || p.Draft {
		t.Fatalf("pr %+v", p)
	}
	if p.Repo.NameWithOwner != "acme/web" || !p.Repo.SquashAllowed || !p.Repo.MergeAllowed || !p.Repo.RebaseAllowed {
		t.Fatalf("repo %+v", p.Repo)
	}
	if p.CommittedAt == nil || !p.CommittedAt.Equal(time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("committed %v", p.CommittedAt)
	}
	if len(p.Reviews) != 1 || p.Reviews[0].Author != "alice" || len(p.ReviewRequests) != 1 || p.ReviewRequests[0] != "U_bob" {
		t.Fatalf("reviews %+v requests %v", p.Reviews, p.ReviewRequests)
	}
	if !res.PRs[1].Draft || res.PRs[1].Repo.SquashAllowed || res.PRs[1].ReviewDecision != "" || res.PRs[1].Reviews[0].Author != "" {
		t.Fatalf("draft %+v", res.PRs[1])
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
	if err != nil || len(res.PRs) != 1 || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "INTERNAL") || len(res.Blocked) != 0 {
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
	if err != nil || !res.Incomplete || len(res.PRs) != 2 || len(rec.all()) != 2 {
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
	if len(reqs) != 1 || reqs[0].Variables["q"] != "is:pr assignee:@me is:merged updated:>=2026-11-24T06:30:00Z" {
		t.Fatalf("%+v", reqs)
	}
	if len(res.Merged) != 1 || !res.Merged["PR_9"].Equal(time.Date(2026, 11, 24, 8, 45, 0, 0, time.UTC)) || res.Requests != 1 {
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
