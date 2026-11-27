//go:build e2e

package fake

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type reviewFake struct {
	t   *testing.T
	g   *github
	srv *httptest.Server
}

func newReviewFake(t *testing.T) *reviewFake {
	t.Helper()
	g := newGitHub()
	g.st.Config.Review = true
	srv := httptest.NewServer(g.routes())
	t.Cleanup(srv.Close)
	return &reviewFake{t: t, g: g, srv: srv}
}

func (f *reviewFake) post(path, token, body string) (int, string) {
	f.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func (f *reviewFake) node(id string) map[string]any {
	f.t.Helper()
	f.g.mu.Lock()
	defer f.g.mu.Unlock()
	n := nodeBody(id, f.g.cfgLocked(), nil, time.Now())["data"].(map[string]any)["node"]
	if n == nil {
		f.t.Fatalf("no node %s", id)
	}
	return n.(map[string]any)
}

const tok = "ghp_classic_ok"

func TestReviewFixturesAreOnlyThereWhenAsked(t *testing.T) {
	if _, ok := findFixture("acme/review", 31, githubConfig{}); ok {
		t.Fatal("the review pull requests are part of every run")
	}
	for _, n := range []int{31, 32, 33} {
		if _, ok := findFixture("acme/review", n, githubConfig{Review: true}); !ok {
			t.Fatalf("#%d missing", n)
		}
	}
}

func TestApproveAddsTheViewersReviewAndRefusesOwnAndMovedCommits(t *testing.T) {
	f := newReviewFake(t)
	p, _ := findFixture("acme/review", 31, f.g.config())
	sha := p.headSHA(false)
	if code, body := f.post("/repos/acme/review/pulls/31/reviews", tok, `{"event":"APPROVE","commit_id":"`+sha+`"}`); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	n := f.node(p.id())
	reviews := n["latestReviews"].(map[string]any)["nodes"].([]any)
	if len(reviews) != 2 || n["reviewDecision"] != "APPROVED" {
		t.Fatalf("the fixture's approval plus the viewer's: %v", n["latestReviews"])
	}

	if code, body := f.post("/repos/acme/review/pulls/31/reviews", tok, `{"event":"APPROVE","commit_id":"0000000"}`); code != 422 || !strings.Contains(body, "commit_id") {
		t.Fatalf("a commit that is not the head: %d %s", code, body)
	}
	own, _ := findFixture("acme/review", 33, f.g.config())
	if code, body := f.post("/repos/acme/review/pulls/33/reviews", tok, `{"event":"APPROVE","commit_id":"`+own.headSHA(false)+`"}`); code != 422 || !strings.Contains(body, "your own pull request") {
		t.Fatalf("%d %s", code, body)
	}
	if code, body := f.post("/repos/acme/review/pulls/33/reviews", tok, `{"event":"REQUEST_CHANGES","body":"x","commit_id":"`+own.headSHA(false)+`"}`); code != 422 || !strings.Contains(body, "your own pull request") {
		t.Fatalf("%d %s", code, body)
	}
	if len(f.g.st.Reviews) != 1 || f.g.st.ReviewCalls != 4 {
		t.Fatalf("%+v calls %d", f.g.st.Reviews, f.g.st.ReviewCalls)
	}
}

func TestRequestChangesNeedsABodyAndSetsTheDecision(t *testing.T) {
	f := newReviewFake(t)
	p, _ := findFixture("acme/review", 31, f.g.config())
	sha := p.headSHA(false)
	if code, _ := f.post("/repos/acme/review/pulls/31/reviews", tok, `{"event":"REQUEST_CHANGES","commit_id":"`+sha+`"}`); code != 422 {
		t.Fatalf("no body: %d", code)
	}
	if code, body := f.post("/repos/acme/review/pulls/31/reviews", tok, `{"event":"REQUEST_CHANGES","body":"rename it","commit_id":"`+sha+`"}`); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if n := f.node(p.id()); n["reviewDecision"] != "CHANGES_REQUESTED" || f.g.st.Reviews[0].Body != "rename it" {
		t.Fatalf("%v %+v", n["reviewDecision"], f.g.st.Reviews)
	}
	if code, _ := f.post("/repos/acme/review/pulls/31/reviews", tok, `{"event":"COMMENT","commit_id":"`+sha+`"}`); code != 422 {
		t.Fatalf("event: %d", code)
	}
	if code, _ := f.post("/repos/acme/review/pulls/99/reviews", tok, `{"event":"APPROVE","commit_id":"`+sha+`"}`); code != 404 {
		t.Fatalf("unknown pull request: %d", code)
	}
	if code, _ := f.post("/repos/acme/review/pulls/31/reviews", "nope", `{}`); code != 401 {
		t.Fatalf("token: %d", code)
	}
}

func TestPullRequestsReadTokenIsRefused(t *testing.T) {
	f := newReviewFake(t)
	p, _ := findFixture("acme/review", 31, f.g.config())
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/repos/acme/review/pulls/31/reviews", strings.NewReader(`{"event":"APPROVE","commit_id":"`+p.headSHA(false)+`"}`))
	req.Header.Set("Authorization", "Bearer github_pat_pulls_read")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 403 || res.Header.Get("X-Accepted-GitHub-Permissions") != "pull_requests=write" || len(f.g.st.Reviews) != 0 {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	code, body := f.post("/graphql", "github_pat_pulls_read", `{"query":"mutation{markPullRequestReadyForReview}","variables":{"id":"PR_acme_review_32"}}`)
	if code != 200 || !strings.Contains(body, "FORBIDDEN") || len(f.g.st.Ready) != 0 {
		t.Fatalf("%d %s", code, body)
	}
}

func TestMarkReadyTakesADraftOutOfDraftOnce(t *testing.T) {
	f := newReviewFake(t)
	draft, _ := findFixture("acme/review", 32, f.g.config())
	if !f.node(draft.id())["isDraft"].(bool) {
		t.Fatal("#32 starts as a draft")
	}
	q := func(id string) (int, string) {
		b, _ := json.Marshal(map[string]any{"query": "mutation($id: ID!){ markPullRequestReadyForReview(input:{pullRequestId:$id}){ pullRequest{ id isDraft } } }", "variables": map[string]any{"id": id}})
		return f.post("/graphql", tok, string(b))
	}
	if code, body := q(draft.id()); code != 200 || !strings.Contains(body, `"isDraft":false`) {
		t.Fatalf("%d %s", code, body)
	}
	n := f.node(draft.id())
	if n["isDraft"].(bool) || n["mergeStateStatus"] != "CLEAN" {
		t.Fatalf("%v %v", n["isDraft"], n["mergeStateStatus"])
	}
	if _, body := q(draft.id()); !strings.Contains(body, "not a draft") {
		t.Fatalf("a second mark is refused: %s", body)
	}
	if _, body := q("PR_missing"); !strings.Contains(body, "NOT_FOUND") {
		t.Fatalf("%s", body)
	}
	if f.g.st.ReadyCalls != 3 || len(f.g.st.Ready) != 1 {
		t.Fatalf("%+v", f.g.st)
	}
}
