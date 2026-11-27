package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

// noWait retries without sleeping.
var noWait = backoff.Policy{Sleep: func(context.Context, time.Duration) error { return nil }}

const reviewSHA = "0123456789abcdef0123456789abcdef01234567"

func TestSubmitReviewPostsTheEventPinnedToTheHeadCommit(t *testing.T) {
	var method, path, auth, version string
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth, version = r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-GitHub-Api-Version")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"id":1,"state":"APPROVED"}`)
	}))
	defer srv.Close()
	if err := client(srv).SubmitReview(context.Background(), "tok", "acme/web", 7, github.ReviewApprove, reviewSHA, "LGTM"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/repos/acme/web/pulls/7/reviews" || auth != "Bearer tok" || version == "" {
		t.Fatalf("%s %s %s %s", method, path, auth, version)
	}
	if got["event"] != "APPROVE" || got["commit_id"] != reviewSHA || got["body"] != "LGTM" || len(got) != 3 {
		t.Fatalf("%v", got)
	}

	got = nil
	if err := client(srv).SubmitReview(context.Background(), "tok", "acme/web", 7, github.ReviewRequestChanges, reviewSHA, ""); err != nil {
		t.Fatal(err)
	}
	if got["event"] != "REQUEST_CHANGES" || got["body"] != "" || len(got) != 2 {
		t.Fatalf("an empty body is not sent: %v", got)
	}
}

func TestSubmitReviewRefusesBadInputBeforeAnyCall(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	c := client(srv)
	long := strings.Repeat("x", github.MaxReviewBody+1)
	for name, err := range map[string]error{
		"repository": c.SubmitReview(context.Background(), "t", "../x", 1, github.ReviewApprove, reviewSHA, ""),
		"event":      c.SubmitReview(context.Background(), "t", "acme/web", 1, "COMMENT", reviewSHA, ""),
		"sha":        c.SubmitReview(context.Background(), "t", "acme/web", 1, github.ReviewApprove, "not a sha", ""),
		"too long":   c.SubmitReview(context.Background(), "t", "acme/web", 1, github.ReviewApprove, reviewSHA, long),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if calls != 0 {
		t.Fatalf("%d calls", calls)
	}
	if err := c.SubmitReview(context.Background(), "t", "acme/web", 1, github.ReviewApprove, reviewSHA, strings.Repeat("é", github.MaxReviewBody)); err != nil && strings.Contains(err.Error(), "characters") {
		t.Fatalf("the limit counts characters, not bytes: %v", err)
	}
}

func TestSubmitReviewErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		check   func(t *testing.T, err error)
	}{
		{"own pull request", 422, nil, `{"message":"Unprocessable Entity","errors":["Can not approve your own pull request"]}`, func(t *testing.T, err error) {
			if !errors.Is(err, github.ErrOwnPullRequest) {
				t.Fatalf("%v", err)
			}
		}},
		{"own pull request, request changes", 422, nil, `{"message":"Unprocessable Entity","errors":["Can not request changes on your own pull request"]}`, func(t *testing.T, err error) {
			if !errors.Is(err, github.ErrOwnPullRequest) {
				t.Fatalf("%v", err)
			}
		}},
		{"commit moved", 422, nil, `{"message":"Validation Failed","errors":[{"resource":"PullRequestReview","code":"custom","field":"commit_id","message":"commit_id is not part of the pull request"}]}`, func(t *testing.T, err error) {
			if !errors.Is(err, github.ErrHeadMoved) {
				t.Fatalf("%v", err)
			}
		}},
		{"other 422", 422, nil, `{"message":"Validation Failed","errors":["Review body can't be blank"]}`, func(t *testing.T, err error) {
			var rej *github.ReviewRejectedError
			if !errors.As(err, &rej) || !strings.Contains(rej.Message, "Review body can't be blank") {
				t.Fatalf("%v", err)
			}
		}},
		{"not found", 404, nil, `{"message":"Not Found"}`, func(t *testing.T, err error) {
			if !errors.Is(err, github.ErrPRNotFound) {
				t.Fatalf("%v", err)
			}
		}},
		{"permission with the header", 403, map[string]string{"X-Accepted-GitHub-Permissions": "pull_requests=write"}, `{"message":"Resource not accessible by personal access token"}`, func(t *testing.T, err error) {
			var perm *github.PermissionError
			if !errors.As(err, &perm) || perm.Detail() != "The token needs the GitHub permission pull_requests: write" {
				t.Fatalf("%v", err)
			}
		}},
		{"permission without the header names pull_requests", 403, nil, `{"message":"Resource not accessible by integration"}`, func(t *testing.T, err error) {
			var perm *github.PermissionError
			if !errors.As(err, &perm) || perm.Detail() != "The token needs the GitHub permission pull_requests: write" {
				t.Fatalf("%v", err)
			}
		}},
		{"sso", 403, map[string]string{"X-GitHub-SSO": ssoHeader}, `{"message":"Resource protected by organization SAML enforcement."}`, func(t *testing.T, err error) {
			var b *github.OrgBlocked
			if !errors.As(err, &b) || b.Reason != github.ReasonSSO {
				t.Fatalf("%v", err)
			}
		}},
		{"rate limit", 429, map[string]string{"Retry-After": "60"}, `{"message":"slow down"}`, func(t *testing.T, err error) {
			var rl *github.RateLimitedError
			if !errors.As(err, &rl) {
				t.Fatalf("%v", err)
			}
		}},
		{"bad token", 401, nil, `{"message":"Bad credentials"}`, func(t *testing.T, err error) {
			if !errors.Is(err, github.ErrBadCredentials) {
				t.Fatalf("%v", err)
			}
		}},
		{"server error", 500, nil, `{"message":"boom"}`, func(t *testing.T, err error) {
			var api *github.APIError
			if !errors.As(err, &api) || api.Status != 500 {
				t.Fatalf("%v", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := refusal(t, tt.status, tt.headers, tt.body)
			c.Backoff = noWait
			tt.check(t, c.SubmitReview(context.Background(), "tok", "acme/web", 7, github.ReviewApprove, reviewSHA, ""))
		})
	}
}

func TestMarkReadySendsTheMutationAndReadsTheNewState(t *testing.T) {
	var got struct {
		Query     string
		Variables map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"id":"PR_1","isDraft":false}}}}`)
	}))
	defer srv.Close()
	if err := client(srv).MarkReady(context.Background(), "tok", "PR_1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Query, "markPullRequestReadyForReview") || got.Variables["id"] != "PR_1" {
		t.Fatalf("%+v", got)
	}
}

func TestMarkReadyErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		check   func(t *testing.T, err error)
	}{
		{"permission", 200, nil, `{"data":{"markPullRequestReadyForReview":null},"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by personal access token","path":["markPullRequestReadyForReview"]}]}`, func(t *testing.T, err error) {
			var perm *github.PermissionError
			if !errors.As(err, &perm) || perm.Detail() != "The token needs the GitHub permission pull_requests: write" {
				t.Fatalf("%v", err)
			}
		}},
		{"sso", 200, nil, `{"data":null,"errors":[{"type":"FORBIDDEN","message":"Resource protected by organization SAML enforcement. The ` + "`acme`" + ` organization requires SSO."}]}`, func(t *testing.T, err error) {
			var b *github.OrgBlocked
			if !errors.As(err, &b) || b.Org != "acme" {
				t.Fatalf("%v", err)
			}
		}},
		{"not found", 200, nil, `{"data":{"markPullRequestReadyForReview":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a node"}]}`, func(t *testing.T, err error) {
			if !errors.Is(err, github.ErrPRNotFound) {
				t.Fatalf("%v", err)
			}
		}},
		{"not a draft", 200, nil, `{"data":{"markPullRequestReadyForReview":null},"errors":[{"type":"UNPROCESSABLE","message":"Pull request is not a draft"}]}`, func(t *testing.T, err error) {
			if !errors.Is(err, github.ErrNotDraft) {
				t.Fatalf("%v", err)
			}
		}},
		{"other refusal", 200, nil, `{"data":{"markPullRequestReadyForReview":null},"errors":[{"type":"UNPROCESSABLE","message":"Cannot mark this ready"}]}`, func(t *testing.T, err error) {
			var rej *github.ReviewRejectedError
			if !errors.As(err, &rej) || rej.Message != "Cannot mark this ready" {
				t.Fatalf("%v", err)
			}
		}},
		{"still a draft", 200, nil, `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"id":"PR_1","isDraft":true}}}}`, func(t *testing.T, err error) {
			var rej *github.ReviewRejectedError
			if !errors.As(err, &rej) {
				t.Fatalf("%v", err)
			}
		}},
		{"empty answer", 200, nil, `{"data":{}}`, func(t *testing.T, err error) {
			var rej *github.ReviewRejectedError
			if !errors.As(err, &rej) {
				t.Fatalf("%v", err)
			}
		}},
		{"graphql rate limit", 200, nil, `{"data":null,"errors":[{"type":"RATE_LIMITED","message":"x"}]}`, func(t *testing.T, err error) {
			var rl *github.RateLimitedError
			if !errors.As(err, &rl) {
				t.Fatalf("%v", err)
			}
		}},
		{"rest shaped 403", 403, nil, `{"message":"Resource not accessible by integration"}`, func(t *testing.T, err error) {
			var perm *github.PermissionError
			if !errors.As(err, &perm) {
				t.Fatalf("%v", err)
			}
		}},
		{"bad token", 401, nil, `{"message":"Bad credentials"}`, func(t *testing.T, err error) {
			if !errors.Is(err, github.ErrBadCredentials) {
				t.Fatalf("%v", err)
			}
		}},
		{"server error", 502, nil, `bad gateway`, func(t *testing.T, err error) {
			var api *github.APIError
			if !errors.As(err, &api) {
				t.Fatalf("%v", err)
			}
		}},
		{"bad json", 200, nil, `{`, func(t *testing.T, err error) {
			if err == nil {
				t.Fatal("accepted")
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := refusal(t, tt.status, tt.headers, tt.body)
			c.Backoff = noWait
			tt.check(t, c.MarkReady(context.Background(), "tok", "PR_1"))
		})
	}
}
