package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
)

// fakeProviders stands in for GitHub and Atlassian.
type fakeProviders struct {
	srv *httptest.Server

	githubScopes  atomic.Value // string
	githubMode    atomic.Value // "", "sso", "down"
	exchangeFails atomic.Bool
	refreshStatus atomic.Int32
	sites         atomic.Value // string JSON
	lastCode      atomic.Value // string
}

const twoSites = `[
	{"id":"cloud-1","name":"Acme","url":"https://acme.atlassian.net","scopes":["read:jira-work"]},
	{"id":"cloud-2","name":"Beta","url":"https://beta.atlassian.net","scopes":["read:jira-work"]}
]`

const oneSite = `[{"id":"cloud-1","name":"Acme","url":"https://acme.atlassian.net","scopes":["read:jira-work"]}]`

func newFakeProviders(t *testing.T) *fakeProviders {
	f := &fakeProviders{}
	f.githubScopes.Store("repo, read:org")
	f.githubMode.Store("")
	f.sites.Store(oneSite)
	f.lastCode.Store("")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /gh/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.lastCode.Store(r.PostForm.Get("code"))
		if f.exchangeFails.Load() {
			_, _ = io.WriteString(w, `{"error":"bad_verification_code"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"gho_abc","token_type":"bearer"}`)
	})
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		switch f.githubMode.Load().(string) {
		case "down":
			w.WriteHeader(http.StatusBadGateway)
			return
		case "sso":
			_, _ = io.WriteString(w, `{"data":{"viewer":null},"errors":[{"type":"FORBIDDEN","message":"The `+"`acme`"+` organization forbids access via a personal access token (classic)."}]}`)
			return
		}
		switch strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") {
		case "gho_abc", "ghp_pat", "github_pat_x":
		default:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if s := f.githubScopes.Load().(string); s != "" {
			w.Header().Set("X-OAuth-Scopes", s)
		}
		_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"octocat"}}}`)
	})
	mux.HandleFunc("POST /atl/token", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["grant_type"] == "refresh_token" {
			if st := int(f.refreshStatus.Load()); st != 0 {
				w.WriteHeader(st)
				_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"atl-access-2","refresh_token":"atl-refresh-2","expires_in":3600}`)
			return
		}
		f.lastCode.Store(body["code"])
		if f.exchangeFails.Load() {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"atl-access-1","refresh_token":"atl-refresh-1","expires_in":3600}`)
	})
	mux.HandleFunc("GET /atl/resources", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, f.sites.Load().(string))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeProviders) endpoints() auth.Endpoints {
	return auth.Endpoints{
		GitHubAuthorize:    "https://github.example/login/oauth/authorize",
		GitHubToken:        f.srv.URL + "/gh/token",
		AtlassianAuthorize: "https://atlassian.example/authorize",
		AtlassianToken:     f.srv.URL + "/atl/token",
		AtlassianResources: f.srv.URL + "/atl/resources",
	}
}
