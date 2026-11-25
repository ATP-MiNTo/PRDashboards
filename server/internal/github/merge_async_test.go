package github_test

import (
	"context"
	"errors"
	"fmt"
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

const (
	stackMessage = "Merging stacked PRs via this endpoint is not supported. Use the asynchronous merge endpoint instead."
	goodID       = "7d1b8c1e-52a4-4f0e-9a43-0c2f5a1e9b10"
)

// asyncServer plays GitHub: the sync endpoint, the async submit and the polls. submit and poll
// answer the nth call (the last answer repeats).
type asyncServer struct {
	mu       sync.Mutex
	sync     func(w http.ResponseWriter)
	submit   func(w http.ResponseWriter)
	polls    []string
	pollCode int
	calls    []string
	versions map[string]string
	bodies   []string
	paths    []string
}

func (a *asyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, _ := io.ReadAll(r.Body)
	a.calls = append(a.calls, r.Method+" "+r.URL.Path)
	a.bodies = append(a.bodies, string(b))
	if a.versions == nil {
		a.versions = map[string]string{}
	}
	a.versions[r.Method+" "+r.URL.Path] = r.Header.Get("X-GitHub-Api-Version")
	switch {
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/merge"):
		a.sync(w)
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/merge-async"):
		a.submit(w)
	default:
		n := 0
		for _, c := range a.calls {
			if strings.HasPrefix(c, "GET ") {
				n++
			}
		}
		body := a.polls[min(n, len(a.polls))-1]
		if a.pollCode != 0 {
			w.WriteHeader(a.pollCode)
		}
		_, _ = io.WriteString(w, body)
	}
}

func accepted(w http.ResponseWriter) {
	w.WriteHeader(http.StatusAccepted)
	_, _ = io.WriteString(w, `{"status":"pending","details":{"uuid":"`+goodID+`","merge_method":"squash"}}`)
}

func status(code int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

func (a *asyncServer) client(t *testing.T) (*github.Client, *[]time.Duration, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	var slept []time.Duration
	c := &github.Client{HTTP: srv.Client(), APIBase: srv.URL, Backoff: backoff.Policy{
		Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
	}}
	return c, &slept, srv
}

func TestIsStackRefusalOnlyForTheStackMessage(t *testing.T) {
	tests := []struct {
		name, message string
		async         bool
	}{
		{"exact message", stackMessage, true},
		{"case insensitive", strings.ToUpper(stackMessage), true},
		{"permission", "Resource not accessible by personal access token", false},
		{"sso", "Resource protected by organization SAML enforcement", false},
		{"only one half", "Merging stacked PRs is not supported", false},
		{"other half", "Use the asynchronous merge endpoint instead", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &asyncServer{
				sync: func(w http.ResponseWriter) {
					w.Header().Set("X-Accepted-GitHub-Permissions", "contents=write")
					status(403, fmt.Sprintf(`{"message":%q}`, tt.message))(w)
				},
				submit: accepted,
				polls:  []string{`{"status":"merged"}`},
			}
			c, _, _ := a.client(t)
			res, err := c.Merge(context.Background(), "tok", "acme/web", 7, "squash", "sha1")
			if tt.async {
				if err != nil {
					t.Fatalf("%v", err)
				}
				if len(a.calls) != 3 || a.calls[0] != "PUT /repos/acme/web/pulls/7/merge" || a.calls[1] != "PUT /repos/acme/web/pulls/7/merge-async" {
					t.Fatalf("%v", a.calls)
				}
				return
			}
			var perm *github.PermissionError
			var blocked *github.OrgBlocked
			if (!errors.As(err, &perm) && !errors.As(err, &blocked)) || len(a.calls) != 1 || res.SHA != "" {
				t.Fatalf("%v %v", err, a.calls)
			}
		})
	}
}

func TestStackedMergeKeepsTheHeadPinAndNeverBecomesAPermissionError(t *testing.T) {
	a := &asyncServer{
		sync: func(w http.ResponseWriter) {
			w.Header().Set("X-Accepted-GitHub-Permissions", "contents=write")
			status(403, `{"message":"`+stackMessage+`"}`)(w)
		},
		submit: accepted,
		polls:  []string{`{"status":"pending"}`, `{"status":"merged","details":{"merge_commit_sha":"abc1234"}}`},
	}
	c, slept, _ := a.client(t)
	res, err := c.Merge(context.Background(), "tok", "acme/web", 7, "squash", "sha1")
	var perm *github.PermissionError
	if errors.As(err, &perm) || err != nil || res.SHA != "abc1234" {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(a.bodies[1], `"sha":"sha1"`) || !strings.Contains(a.bodies[1], `"merge_method":"squash"`) {
		t.Fatalf("%s", a.bodies[1])
	}
	if len(*slept) != 2 || (*slept)[0] != time.Second {
		t.Fatalf("%v", *slept)
	}
}

func TestMergeAsyncSubmitAnswers(t *testing.T) {
	existing := `{"message":"A merge request already exists","details":{"uuid":"` + goodID + `"}}`
	tests := []struct {
		name   string
		submit func(http.ResponseWriter)
		check  func(github.MergeResult, error) bool
	}{
		{"202 then merged", accepted, func(r github.MergeResult, e error) bool { return e == nil && r.SHA == "" }},
		{"200 merged", status(200, `{"status":"merged","details":{"merge_commit_sha":"deadbeef"}}`), func(r github.MergeResult, e error) bool { return e == nil && r.SHA == "deadbeef" }},
		{"200 queued", status(200, `{"status":"enqueued"}`), func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrEnqueued) }},
		{"200 unknown", status(200, `{}`), func(_ github.MergeResult, e error) bool {
			var a *github.APIError
			return errors.As(e, &a)
		}},
		{"202 without id", status(202, `{"status":"pending"}`), func(_ github.MergeResult, e error) bool {
			var a *github.APIError
			return errors.As(e, &a)
		}},
		{"202 with a bad id", status(202, `{"status":"pending","details":{"uuid":"../x"}}`), func(_ github.MergeResult, e error) bool {
			var a *github.APIError
			return errors.As(e, &a)
		}},
		{"409 with the existing request", status(409, existing), func(_ github.MergeResult, e error) bool { return e == nil }},
		{"409 without an id", status(409, `{"message":"Head branch was modified"}`), func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrHeadMoved) }},
		{"400", status(400, `{"message":"Pull request is closed"}`), func(_ github.MergeResult, e error) bool {
			return errors.Is(e, github.ErrNotMergeable) && strings.Contains(e.Error(), "closed")
		}},
		{"422", status(422, `{"message":"Invalid merge method"}`), func(_ github.MergeResult, e error) bool {
			return errors.Is(e, github.ErrNotMergeable) && strings.Contains(e.Error(), "Invalid merge method")
		}},
		{"404", status(404, `{}`), func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrPRNotFound) }},
		{"403 permission", func(w http.ResponseWriter) {
			w.Header().Set("X-Accepted-GitHub-Permissions", "contents=write")
			status(403, `{"message":"Resource not accessible by personal access token"}`)(w)
		}, func(_ github.MergeResult, e error) bool {
			var p *github.PermissionError
			return errors.As(e, &p) && len(p.Accepted) == 1
		}},
		{"403 sso", func(w http.ResponseWriter) {
			w.Header().Set("X-GitHub-SSO", "required; url=https://github.com/orgs/acme/sso?authorization_request=x")
			status(403, `{"message":"Resource protected by organization SAML enforcement"}`)(w)
		}, func(_ github.MergeResult, e error) bool {
			var b *github.OrgBlocked
			return errors.As(e, &b) && b.Org == "acme"
		}},
		{"401", status(401, `{}`), func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrBadCredentials) }},
		{"500", status(500, `{"message":"boom"}`), func(_ github.MergeResult, e error) bool {
			var a *github.APIError
			return errors.As(e, &a) && a.Status == 500
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &asyncServer{submit: tt.submit, polls: []string{`{"status":"merged"}`}}
			c, _, _ := a.client(t)
			res, err := c.MergeAsync(context.Background(), "tok", "acme/web", 7, "squash", "sha1")
			if !tt.check(res, err) {
				t.Fatalf("%+v %v", res, err)
			}
			if a.calls[0] != "PUT /repos/acme/web/pulls/7/merge-async" || a.versions[a.calls[0]] != "2026-03-10" {
				t.Fatalf("%v %v", a.calls, a.versions)
			}
		})
	}
}

func TestMergeAsyncSendsTheVersionHeaderOnEveryCall(t *testing.T) {
	a := &asyncServer{submit: accepted, polls: []string{`{"status":"pending"}`, `{"status":"merged"}`}}
	c, _, _ := a.client(t)
	if _, err := c.MergeAsync(context.Background(), "tok", "acme/web", 7, "merge", "sha1"); err != nil {
		t.Fatal(err)
	}
	want := "GET /repos/acme/web/pulls/7/merge-async/" + goodID
	if len(a.calls) != 3 || a.calls[2] != want {
		t.Fatalf("%v", a.calls)
	}
	for call, v := range a.versions {
		if v != "2026-03-10" {
			t.Errorf("%s sent version %q", call, v)
		}
	}
}

func TestMergeAsyncRejectsAnUnsafeRequestId(t *testing.T) {
	for _, id := range []string{"../x", "a/b", "a b", "a?b=1", "", strings.Repeat("a", 65)} {
		a := &asyncServer{submit: status(202, fmt.Sprintf(`{"status":"pending","details":{"uuid":%q}}`, id)), polls: []string{`{"status":"merged"}`}}
		c, _, _ := a.client(t)
		_, err := c.MergeAsync(context.Background(), "tok", "acme/web", 7, "squash", "sha1")
		var api *github.APIError
		if !errors.As(err, &api) || len(a.calls) != 1 {
			t.Errorf("%q: %v %v", id, err, a.calls)
		}
	}
}

func TestMergeAsyncRejectsBadRepositoryNames(t *testing.T) {
	c := &github.Client{HTTP: http.DefaultClient, APIBase: "http://127.0.0.1:1"}
	for _, repo := range []string{"acme", "acme/..", "ac me/web", "acme/web/x"} {
		if _, err := c.MergeAsync(context.Background(), "t", repo, 1, "squash", "s"); err == nil {
			t.Errorf("accepted %q", repo)
		}
	}
}

func TestMergeAsyncPolling(t *testing.T) {
	long := strings.Repeat("x", 300)
	tests := []struct {
		name  string
		polls []string
		code  int
		check func(github.MergeResult, error) bool
		slept int
	}{
		{"merged", []string{`{"status":"pending"}`, `{"status":"pending"}`, `{"status":"merged","merge_commit_sha":"abcdef12"}`},
			0, func(r github.MergeResult, e error) bool { return e == nil && r.SHA == "abcdef12" }, 3},
		{"merged with a sha that is not one", []string{`{"status":"merged","sha":"<script>"}`},
			0, func(r github.MergeResult, e error) bool { return e == nil && r.SHA == "" }, 1},
		{"enqueued", []string{`{"status":"pending"}`, `{"status":"enqueued"}`},
			0, func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrEnqueued) }, 2},
		{"failed with the reason in message", []string{`{"status":"failed","message":"Required checks are missing"}`},
			0, func(_ github.MergeResult, e error) bool {
				var f *github.MergeFailedError
				return errors.As(e, &f) && f.Reason == "Required checks are missing" && strings.Contains(e.Error(), "Required checks")
			}, 1},
		{"failed with the reason in details", []string{`{"status":"failed","details":{"reason":"rule failed"}}`},
			0, func(_ github.MergeResult, e error) bool {
				var f *github.MergeFailedError
				return errors.As(e, &f) && f.Reason == "rule failed"
			}, 1},
		{"failed reason is cut", []string{`{"status":"failed","reason":"` + long + `"}`},
			0, func(_ github.MergeResult, e error) bool {
				var f *github.MergeFailedError
				return errors.As(e, &f) && len(f.Reason) <= 203
			}, 1},
		{"failed without a reason", []string{`{"status":"FAILED"}`},
			0, func(_ github.MergeResult, e error) bool {
				var f *github.MergeFailedError
				return errors.As(e, &f) && f.Reason != ""
			}, 1},
		{"pending forever stops at the cap", []string{`{"status":"pending"}`},
			0, func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrMergePending) }, -1},
		{"a busy GitHub is waited out", []string{`oops`},
			500, func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrMergePending) }, -1},
		{"poll 404", []string{`{}`}, 404, func(_ github.MergeResult, e error) bool { return errors.Is(e, github.ErrPRNotFound) }, 1},
		{"poll 403", []string{`{"message":"Resource not accessible by personal access token"}`}, 403, func(_ github.MergeResult, e error) bool {
			var p *github.PermissionError
			return errors.As(e, &p)
		}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &asyncServer{submit: accepted, polls: tt.polls, pollCode: tt.code}
			c, slept, _ := a.client(t)
			res, err := c.MergeAsync(context.Background(), "tok", "acme/web", 7, "squash", "sha1")
			if !tt.check(res, err) {
				t.Fatalf("%+v %v", res, err)
			}
			var total time.Duration
			for _, d := range *slept {
				total += d
				if d > 5*time.Second {
					t.Errorf("wait %v above 5s", d)
				}
			}
			if tt.slept >= 0 && len(*slept) != tt.slept {
				t.Errorf("slept %v", *slept)
			}
			if tt.slept < 0 && (total < 90*time.Second || total > 95*time.Second) {
				t.Errorf("waited %v in total, want the 90 s cap", total)
			}
		})
	}
}

func TestMergeAsyncWaitGrowsFromOneToFiveSeconds(t *testing.T) {
	a := &asyncServer{submit: accepted, polls: []string{`{"status":"pending"}`}}
	c, slept, _ := a.client(t)
	_, _ = c.MergeAsync(context.Background(), "tok", "acme/web", 7, "squash", "sha1")
	if len(*slept) < 5 || (*slept)[0] != time.Second || (*slept)[1] <= (*slept)[0] || (*slept)[len(*slept)-1] != 5*time.Second {
		t.Fatalf("%v", *slept)
	}
}

func TestMergeAsyncStopsWhenTheContextEnds(t *testing.T) {
	a := &asyncServer{submit: accepted, polls: []string{`{"status":"pending"}`}}
	srv := httptest.NewServer(a)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	c := &github.Client{HTTP: srv.Client(), APIBase: srv.URL, Backoff: backoff.Policy{
		Sleep: func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() },
	}}
	_, err := c.MergeAsync(ctx, "tok", "acme/web", 7, "squash", "sha1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	if len(a.calls) != 1 {
		t.Fatalf("polled after cancel: %v", a.calls)
	}
}
