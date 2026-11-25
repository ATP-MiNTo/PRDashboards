package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

type fixture struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

func load(t *testing.T, name string) fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) write(w http.ResponseWriter) {
	for k, v := range f.Headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.Status)
	_, _ = w.Write(f.Body)
}

func serve(t *testing.T, names ...string) (*github.Client, *atomic.Int32, *http.Request) {
	t.Helper()
	var calls atomic.Int32
	var last http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		last = *r.Clone(context.Background())
		load(t, names[min(n, len(names))-1]).write(w)
	}))
	t.Cleanup(srv.Close)
	c := &github.Client{
		HTTP:    srv.Client(),
		APIBase: srv.URL,
		Backoff: backoff.Policy{Sleep: func(context.Context, time.Duration) error { return nil }},
	}
	return c, &calls, &last
}

func TestViewerClassicToken(t *testing.T) {
	c, _, req := serve(t, "viewer_ok")
	v, err := c.Viewer(context.Background(), "ghp_x")
	if err != nil {
		t.Fatal(err)
	}
	if v.Login != "octocat" || !v.Classic || !v.HasScope("repo") || !v.HasScope("read:org") || v.HasScope("admin:repo_hook") {
		t.Fatalf("%+v", v)
	}
	if req.URL.Path != "/graphql" || req.Method != http.MethodPost || req.Header.Get("Authorization") != "Bearer ghp_x" {
		t.Fatalf("request %s %s auth=%q", req.Method, req.URL.Path, req.Header.Get("Authorization"))
	}
}

func TestViewerFineGrainedTokenHasNoScopes(t *testing.T) {
	c, _, _ := serve(t, "viewer_fine_grained")
	v, err := c.Viewer(context.Background(), "github_pat_x")
	if err != nil || v.Classic || len(v.Scopes) != 0 || v.Login != "octocat" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestHasScopeImplied(t *testing.T) {
	for _, scopes := range [][]string{{"admin:org"}, {"write:org"}, {"read:org"}} {
		if !(github.Viewer{Scopes: scopes}).HasScope("read:org") {
			t.Errorf("%v does not grant read:org", scopes)
		}
	}
	if (github.Viewer{Scopes: []string{"repo"}}).HasScope("read:org") {
		t.Error("repo granted read:org")
	}
}

func TestViewerBadCredentials(t *testing.T) {
	c, _, _ := serve(t, "viewer_bad_credentials")
	if _, err := c.Viewer(context.Background(), "x"); !errors.Is(err, github.ErrBadCredentials) {
		t.Fatalf("got %v", err)
	}
}

func TestViewerOrgBlocked(t *testing.T) {
	cases := []struct {
		fixture string
		org     string
		url     string
		reason  string
	}{
		{"viewer_saml_forbidden", "", "", "sso"},
		{"viewer_token_policy_forbidden", "acme-corp", "", "policy"},
		{"viewer_sso_header", "acme-corp", "https://github.com/orgs/acme-corp/sso?authorization_request=ABCDEF123", "sso"},
	}
	for _, tc := range cases {
		c, _, _ := serve(t, tc.fixture)
		_, err := c.Viewer(context.Background(), "x")
		var blocked *github.OrgBlocked
		if !errors.As(err, &blocked) {
			t.Fatalf("%s: got %v", tc.fixture, err)
		}
		if blocked.Org != tc.org || blocked.AuthorizeURL != tc.url || blocked.Reason != tc.reason {
			t.Errorf("%s: %+v", tc.fixture, blocked)
		}
		if blocked.Error() == "" {
			t.Error("empty message")
		}
	}
}

func TestViewerPartialResultsStillSucceeds(t *testing.T) {
	c, _, _ := serve(t, "viewer_partial_results")
	if v, err := c.Viewer(context.Background(), "x"); err != nil || v.Login != "octocat" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestViewerRetriesSecondaryRateLimit(t *testing.T) {
	c, calls, _ := serve(t, "rate_limited", "viewer_ok")
	v, err := c.Viewer(context.Background(), "x")
	if err != nil || v.Login != "octocat" || calls.Load() != 2 {
		t.Fatalf("%+v %v calls=%d", v, err, calls.Load())
	}
}

func TestViewerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("case") {
		case "garbage":
			_, _ = w.Write([]byte("not json"))
		case "nologin":
			_, _ = w.Write([]byte(`{"data":{"viewer":{}}}`))
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	for _, c := range []string{"garbage", "nologin", "status"} {
		client := &github.Client{
			HTTP:    &http.Client{Transport: rewrite{rt: srv.Client().Transport, query: "case=" + c}},
			APIBase: srv.URL,
		}
		if _, err := client.Viewer(context.Background(), "x"); err == nil {
			t.Errorf("%s: expected an error", c)
		}
	}
	dead := &github.Client{HTTP: srv.Client(), APIBase: "http://127.0.0.1:1"}
	if _, err := dead.Viewer(context.Background(), "x"); err == nil {
		t.Error("expected a connection error")
	}
}

type rewrite struct {
	rt    http.RoundTripper
	query string
}

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.RawQuery = r.query
	return r.rt.RoundTrip(req)
}

func TestClassify(t *testing.T) {
	hdr := func(kv ...string) http.Header {
		h := http.Header{}
		for i := 0; i < len(kv); i += 2 {
			h.Set(kv[i], kv[i+1])
		}
		return h
	}
	cases := []struct {
		name   string
		status int
		h      http.Header
		body   string
		want   *github.OrgBlocked
	}{
		{"clean", 200, nil, `{"data":{}}`, nil},
		{"not forbidden", 200, nil, `{"errors":[{"type":"NOT_FOUND","message":"x"}]}`, nil},
		{"server error ignores body", 500, nil, `{"errors":[{"type":"FORBIDDEN"}]}`, nil},
		{"garbage body", 200, nil, `<html>`, nil},
		{"partial results header", 200, hdr("X-GitHub-SSO", "partial-results; organizations=1,2"), `{}`, nil},
		{"required with url", 403, hdr("X-GitHub-SSO", "required; url=https://github.com/orgs/acme/sso?authorization_request=A"), `{}`,
			&github.OrgBlocked{Org: "acme", AuthorizeURL: "https://github.com/orgs/acme/sso?authorization_request=A", Reason: "sso"}},
		{"required without url", 403, hdr("X-GitHub-SSO", "required"), `{}`, &github.OrgBlocked{Reason: "sso"}},
		{"url on another host", 403, hdr("X-GitHub-SSO", "required; url=https://evil.example/orgs/acme/sso"), `{}`, &github.OrgBlocked{Reason: "sso"}},
		{"http url", 403, hdr("X-GitHub-SSO", "required; url=http://github.com/orgs/acme/sso"), `{}`, &github.OrgBlocked{Reason: "sso"}},
		{"credentials in url", 403, hdr("X-GitHub-SSO", "required; url=https://x@github.com/orgs/acme/sso"), `{}`, &github.OrgBlocked{Reason: "sso"}},
		{"wrong path", 403, hdr("X-GitHub-SSO", "required; url=https://github.com/login"), `{}`, &github.OrgBlocked{Reason: "sso"}},
		{"bad org name", 403, hdr("X-GitHub-SSO", "required; url=https://github.com/orgs/a%20b/sso"), `{}`, &github.OrgBlocked{Reason: "sso"}},
		{"graphql saml quoted org", 200, nil, "{\"errors\":[{\"type\":\"FORBIDDEN\",\"message\":\"`acme` requires SAML\"}]}",
			&github.OrgBlocked{Org: "acme", AuthorizeURL: "https://github.com/orgs/acme/sso", Reason: "sso"}},
		{"permission message is not a block", 200, nil, `{"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by personal access token"}]}`, nil},
		{"graphql forbidden without org", 200, nil, `{"errors":[{"type":"FORBIDDEN","message":"SAML"}]}`, &github.OrgBlocked{Reason: "sso"}},
		{"forbidden status with errors", 403, nil, `{"errors":[{"type":"FORBIDDEN","message":"SAML"}]}`, &github.OrgBlocked{Reason: "sso"}},
	}
	for _, tc := range cases {
		got := github.Classify(tc.status, tc.h, []byte(tc.body))
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("%s: got %+v, want nil", tc.name, got)
		case tc.want != nil && (got == nil || *got != *tc.want):
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if (&github.OrgBlocked{}).Error() == (&github.OrgBlocked{Org: "x"}).Error() {
		t.Error("messages should differ with and without an org")
	}
}
