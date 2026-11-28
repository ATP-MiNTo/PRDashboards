package github_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

func client(srv *httptest.Server) *github.Client {
	return &github.Client{HTTP: srv.Client(), APIBase: srv.URL}
}

const nodeOK = `{"data":{"rateLimit":{"limit":5000,"remaining":4999},"node":{"__typename":"PullRequest","id":"PR_1","number":7,"state":"OPEN",
"isCrossRepository":true,"isDraft":false,"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN","reviewDecision":"APPROVED",
"headRefName":"feat/x","headRefOid":"sha1","baseRefName":"main",
"repository":{"id":"R1","nameWithOwner":"acme/web","squashMergeAllowed":true,"mergeCommitAllowed":false,"rebaseMergeAllowed":true,"viewerPermission":"READ"}}}}`

func TestPullRequestFetchesOneNodeWithTheSearchFields(t *testing.T) {
	var query, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		query, auth = string(b), r.Header.Get("Authorization")
		_, _ = io.WriteString(w, nodeOK)
	}))
	defer srv.Close()
	pr, err := client(srv).PullRequest(context.Background(), "tok", "PR_1")
	if err != nil {
		t.Fatal(err)
	}
	if pr.ID != "PR_1" || pr.State != "OPEN" || !pr.CrossRepo || pr.HeadSHA != "sha1" || pr.Repo.MergeAllowed || !pr.Repo.SquashAllowed || pr.Repo.ViewerPermission != "READ" || pr.Mergeable != "MERGEABLE" {
		t.Fatalf("%+v", pr)
	}
	if auth != "Bearer tok" || !strings.Contains(query, "node(id: $id)") || !strings.Contains(query, "fragment PullRequestFields on PullRequest") ||
		!strings.Contains(query, "statusCheckRollup") {
		t.Fatalf("%s %s", auth, query)
	}
}

func TestPullRequestErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		header map[string]string
		check  func(error) bool
	}{
		{"null node", 200, `{"data":{"node":null}}`, nil, func(e error) bool { return errors.Is(e, github.ErrPRNotFound) }},
		{"not a pull request", 200, `{"data":{"node":{"__typename":"Issue","id":"I"}}}`, nil, func(e error) bool { return errors.Is(e, github.ErrPRNotFound) }},
		{"not found error", 200, `{"data":{"node":null},"errors":[{"type":"NOT_FOUND","message":"x"}]}`, nil, func(e error) bool { return errors.Is(e, github.ErrPRNotFound) }},
		{"other graphql error", 200, `{"data":{"node":null},"errors":[{"type":"INTERNAL","message":"boom"}]}`, nil, func(e error) bool { return e != nil && !errors.Is(e, github.ErrPRNotFound) }},
		{"graphql rate limit", 200, `{"data":{"node":null},"errors":[{"type":"RATE_LIMITED","message":"x"}]}`, nil, func(e error) bool {
			var rl *github.RateLimitedError
			return errors.As(e, &rl)
		}},
		{"bad token", 401, `{"message":"Bad credentials"}`, nil, func(e error) bool { return errors.Is(e, github.ErrBadCredentials) }},
		{"sso", 200, `{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":"SAML enforcement for the ` + "`acme`" + ` organization"}]}`, nil, func(e error) bool {
			var b *github.OrgBlocked
			return errors.As(e, &b) && b.Org == "acme"
		}},
		{"server error", 500, `oops`, nil, func(e error) bool { return e != nil }},
		{"bad json", 200, `{`, nil, func(e error) bool { return e != nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			_, err := client(srv).PullRequest(context.Background(), "tok", "PR_1")
			if !tt.check(err) {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestMergeMapsGitHubAnswers(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		check  func(github.MergeResult, error) bool
	}{
		{"merged", 200, `{"sha":"abc","merged":true,"message":"ok"}`, func(r github.MergeResult, e error) bool { return e == nil && r.SHA == "abc" }},
		{"200 without merged", 200, `{"merged":false}`, func(_ github.MergeResult, e error) bool {
			var a *github.APIError
			return errors.As(e, &a)
		}},
		{"405", 405, `{"message":"Pull Request is not mergeable"}`, func(_ github.MergeResult, e error) bool {
			return errors.Is(e, github.ErrNotMergeable) && strings.Contains(e.Error(), "not mergeable")
		}},
		{"409", 409, `{"message":"Head branch was modified"}`, func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrHeadMoved) }},
		{"404", 404, `{}`, func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrPRNotFound) }},
		{"422", 422, `{"message":"Invalid merge method"}`, func(_ github.MergeResult, e error) bool {
			var a *github.APIError
			return errors.As(e, &a) && a.Status == 422 && a.Message == "Invalid merge method"
		}},
		{"401", 401, `{}`, func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrBadCredentials) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var method, path, body string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				method, path, body = r.Method, r.URL.Path, string(b)
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			res, err := client(srv).Merge(context.Background(), "tok", "acme/web", 7, "squash", "sha1")
			if !tt.check(res, err) {
				t.Fatalf("%+v %v", res, err)
			}
			if method != http.MethodPut || path != "/repos/acme/web/pulls/7/merge" || !strings.Contains(body, `"merge_method":"squash"`) || !strings.Contains(body, `"sha":"sha1"`) {
				t.Fatalf("%s %s %s", method, path, body)
			}
		})
	}
}

func TestMergeAndDeleteRejectBadNames(t *testing.T) {
	c := &github.Client{HTTP: http.DefaultClient, APIBase: "http://127.0.0.1:1"}
	for _, repo := range []string{"acme", "acme/web/x", "ac me/web", "acme/..", "-acme/web", "acme/we b"} {
		if _, err := c.Merge(context.Background(), "t", repo, 1, "squash", "s"); err == nil {
			t.Errorf("merge accepted %q", repo)
		}
		if err := c.DeleteBranch(context.Background(), "t", repo, "feat"); err == nil {
			t.Errorf("delete accepted %q", repo)
		}
	}
	for _, branch := range []string{"", "a/../b"} {
		if err := c.DeleteBranch(context.Background(), "t", "acme/web", branch); err == nil {
			t.Errorf("delete accepted branch %q", branch)
		}
	}
}

func TestDeleteBranch(t *testing.T) {
	tests := []struct {
		status int
		body   string
		ok     bool
	}{
		{204, ``, true},
		{404, `{}`, true},
		{422, `{"message":"Reference does not exist"}`, true},
		{422, `{"message":"Cannot delete the default branch"}`, false},
		{403, `{"message":"Resource not accessible"}`, false},
	}
	for _, tt := range tests {
		var method, path string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.EscapedPath()
			w.WriteHeader(tt.status)
			_, _ = io.WriteString(w, tt.body)
		}))
		err := client(srv).DeleteBranch(context.Background(), "tok", "acme/web", "feat/INP 1#x")
		srv.Close()
		if (err == nil) != tt.ok {
			t.Errorf("%d: %v", tt.status, err)
		}
		if method != http.MethodDelete || path != "/repos/acme/web/git/refs/heads/feat/INP%201%23x" {
			t.Errorf("%s %s", method, path)
		}
	}
}
