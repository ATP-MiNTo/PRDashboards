package poller_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

const n1PRs = 120

func n1Node(i int) map[string]any {
	return map[string]any{
		"__typename": "PullRequest", "id": fmt.Sprintf("PR_%d", i), "number": i,
		"title": fmt.Sprintf("Change %d", i), "url": fmt.Sprintf("https://github.com/acme/web/pull/%d", i),
		"isDraft": false, "mergeable": "MERGEABLE", "mergeStateStatus": "CLEAN", "reviewDecision": "APPROVED",
		"headRefName": fmt.Sprintf("feat/INP-%d-change", i), "headRefOid": fmt.Sprintf("sha%d", i), "baseRefName": "main",
		"repository": map[string]any{
			"id": "R_web", "nameWithOwner": "acme/web",
			"squashMergeAllowed": true, "mergeCommitAllowed": true, "rebaseMergeAllowed": true,
		},
	}
}

// TestRefreshOfManyPullRequestsMakesNoPerRowCalls runs the real GitHub and Jira clients against
// local servers and counts every request: 120 pull requests with 120 distinct tickets.
func TestRefreshOfManyPullRequestsMakesNoPerRowCalls(t *testing.T) {
	var graphql, merged, assigned, authored, search, projects, other atomic.Int32
	var searchedKeys atomic.Int32

	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		graphql.Add(1)
		switch q := body.Variables["q"].(string); {
		case strings.Contains(q, "is:merged"):
			merged.Add(1)
		case q == github.OpenAssignedQuery:
			assigned.Add(1)
		case q == github.OpenAuthoredQuery:
			authored.Add(1)
		}
		start := 0
		if c, ok := body.Variables["cursor"].(string); ok {
			_, _ = fmt.Sscanf(c, "C%d", &start)
		}
		end := min(start+50, n1PRs)
		nodes := make([]any, 0, 50)
		for i := start; i < end; i++ {
			nodes = append(nodes, n1Node(i+1))
		}
		cursor := any(nil)
		if end < n1PRs {
			cursor = fmt.Sprintf("C%d", end)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"rateLimit": map[string]any{"limit": 5000, "cost": 1, "remaining": 4990, "resetAt": "2099-01-01T00:00:00Z"},
			"search": map[string]any{
				"issueCount": n1PRs,
				"pageInfo":   map[string]any{"hasNextPage": end < n1PRs, "endCursor": cursor},
				"nodes":      nodes,
			},
		}})
	}))
	defer gh.Close()

	jr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/search/jql"):
			search.Add(1)
			var body struct {
				JQL string `json:"jql"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			inner := strings.TrimSuffix(strings.TrimPrefix(body.JQL, "key in ("), ")")
			var issues []any
			for k := range strings.SplitSeq(inner, ",") {
				searchedKeys.Add(1)
				issues = append(issues, map[string]any{"id": k, "key": k, "fields": map[string]any{
					"summary": "Summary " + k,
					"status":  map[string]any{"name": "In Review", "statusCategory": map[string]any{"key": "indeterminate"}},
					"project": map[string]any{"key": "INP"}, "issuetype": map[string]any{"name": "Story"},
				}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"issues": issues, "isLast": true})
		case strings.HasSuffix(r.URL.Path, "/project/search"):
			projects.Add(1)
			_, _ = io.WriteString(w, `{"values":[{"key":"INP"}],"isLast":true}`)
		default:
			other.Add(1)
			http.NotFound(w, r)
		}
	}))
	defer jr.Close()

	clock := testutil.NewClock()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	policy := backoff.Policy{Clock: clock.Now, Sleep: func(context.Context, time.Duration) error { return nil }}
	p := poller.New(poller.Config{
		Store:     db,
		GitHub:    &github.Client{HTTP: gh.Client(), APIBase: gh.URL, Backoff: policy, Clock: clock.Now},
		Jira:      &jira.Client{HTTP: jr.Client(), Base: jr.URL, Backoff: policy, Clock: clock.Now, Access: func(context.Context) (string, error) { return "at", nil }},
		Accounts:  &fakeAccounts{token: "tok", site: jira.Site{ID: "cloud-1", URL: "https://acme.atlassian.net"}},
		Publisher: &recorder{},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     clock.Now,
		Backoff:   policy,
	})
	if err := p.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	snap := p.Current().Snapshot
	if len(snap.Prs) != n1PRs || snap.Sources.Github.State != "ok" || snap.Sources.Jira.State != "ok" {
		t.Fatalf("prs %d github %s jira %s", len(snap.Prs), snap.Sources.Github.State, snap.Sources.Jira.State)
	}
	for _, row := range snap.Prs {
		if len(row.Tickets) != 1 || !row.Tickets[0].Found {
			t.Fatalf("row %d tickets %+v", row.Number, row.Tickets)
		}
	}
	wantSearches := (n1PRs + jira.ChunkSize - 1) / jira.ChunkSize
	t.Logf("outbound requests for %d pull requests: GitHub GraphQL %d (assigned pages %d, authored pages %d, merged searches %d), Jira search %d, Jira project %d",
		n1PRs, graphql.Load(), assigned.Load(), authored.Load(), merged.Load(), search.Load(), projects.Load())
	// Two open searches (assigned and authored), each 3 pages of 50, and no merged search. Both
	// return the same 120 pull requests here, so the union must still be 120 rows.
	if graphql.Load() != 6 || assigned.Load() != 3 || authored.Load() != 3 || merged.Load() != 0 {
		t.Fatalf("GitHub: %d GraphQL requests (assigned %d, authored %d, merged %d), want 2 searches of 3 pages and no merged search",
			graphql.Load(), assigned.Load(), authored.Load(), merged.Load())
	}
	if int(search.Load()) != wantSearches || projects.Load() != 1 || other.Load() != 0 || searchedKeys.Load() != n1PRs {
		t.Fatalf("Jira: search %d (want %d), projects %d, other %d, keys %d", search.Load(), wantSearches, projects.Load(), other.Load(), searchedKeys.Load())
	}
}
