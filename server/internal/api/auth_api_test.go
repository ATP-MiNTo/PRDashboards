package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body, err)
	}
	return v
}

type settingsBody struct {
	Polling                  struct{ GithubSec, JiraSec int }
	Theme                    string
	DefaultMethodForNewRepos string
	DeleteBranchAfterMerge   bool
	JiraSiteID               *string
	ExcludedStatuses         []string
}

const validSettings = `{"polling":{"githubSec":120,"jiraSec":900},"theme":"dark","defaultMethodForNewRepos":"rebase","deleteBranchAfterMerge":true,"jiraSiteId":null,"excludedStatuses":["Done","Closed"]}`

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	w := g.do(http.MethodGet, "/api/settings", "", cookie(l.cookie))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	got := decode[settingsBody](t, w)
	if got.Polling.GithubSec != 60 || got.Polling.JiraSec != 300 || got.Theme != "auto" || got.DefaultMethodForNewRepos != "squash" ||
		got.DeleteBranchAfterMerge || got.JiraSiteID != nil || !slices.Equal(got.ExcludedStatuses, []string{"Done"}) {
		t.Fatalf("defaults %+v", got)
	}
	if !strings.Contains(w.Body.String(), `"jiraSiteId":null`) {
		t.Fatalf("jiraSiteId must be present as null: %s", w.Body)
	}

	w = g.do(http.MethodPut, "/api/settings", validSettings, l.write()...)
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	got = decode[settingsBody](t, g.do(http.MethodGet, "/api/settings", "", cookie(l.cookie)))
	if got.Polling.GithubSec != 120 || got.Polling.JiraSec != 900 || got.Theme != "dark" || got.DefaultMethodForNewRepos != "rebase" || !got.DeleteBranchAfterMerge ||
		!slices.Equal(got.ExcludedStatuses, []string{"Done", "Closed"}) {
		t.Fatalf("after put %+v", got)
	}
}

func TestSettingsValidation(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	bad := map[string]string{
		"interval not allowed":  strings.Replace(validSettings, `"githubSec":120`, `"githubSec":61`, 1),
		"interval as string":    strings.Replace(validSettings, `"githubSec":120`, `"githubSec":"120"`, 1),
		"theme":                 strings.Replace(validSettings, `"dark"`, `"neon"`, 1),
		"method":                strings.Replace(validSettings, `"rebase"`, `"fast-forward"`, 1),
		"unknown field":         strings.Replace(validSettings, `"theme"`, `"extra":1,"theme"`, 1),
		"unknown polling key":   strings.Replace(validSettings, `"jiraSec":900`, `"jiraSec":900,"x":1`, 1),
		"missing field":         `{"polling":{"githubSec":60,"jiraSec":300},"theme":"auto"}`,
		"flag as string":        strings.Replace(validSettings, `true`, `"yes"`, 1),
		"empty excluded status": strings.Replace(validSettings, `"Closed"`, `""`, 1),
		"excluded not a list":   strings.Replace(validSettings, `["Done","Closed"]`, `"Done"`, 1),
		"too many excluded":     strings.Replace(validSettings, `["Done","Closed"]`, `[`+strings.TrimSuffix(strings.Repeat(`"a",`, 51), ",")+`]`, 1),
		"empty site":            strings.Replace(validSettings, `"jiraSiteId":null`, `"jiraSiteId":""`, 1),
		"not an object":         `[]`,
	}
	for name, body := range bad {
		wantProblem(t, g.do(http.MethodPut, "/api/settings", body, l.write()...), http.StatusBadRequest, "invalid_body")
		_ = name
	}
	got := decode[settingsBody](t, g.do(http.MethodGet, "/api/settings", "", cookie(l.cookie)))
	if got.Theme != "auto" {
		t.Fatal("a rejected PUT changed the settings")
	}
	for _, v := range []int{60, 120, 300, 900, 3600} {
		body := strings.Replace(validSettings, `"githubSec":120`, `"githubSec":`+itoa(v), 1)
		if w := g.do(http.MethodPut, "/api/settings", body, l.write()...); w.Code != http.StatusOK {
			t.Errorf("interval %d rejected: %s", v, w.Body)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestSettingsNeedSessionAndCSRF(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	wantProblem(t, g.do(http.MethodGet, "/api/settings", ""), http.StatusUnauthorized, "unauthenticated")
	wantProblem(t, g.do(http.MethodPut, "/api/settings", validSettings, cookie(l.cookie)), http.StatusForbidden, "bad_csrf")
	wantProblem(t, g.do(http.MethodPut, "/api/settings", validSettings, cookie(l.cookie), csrf(l.csrf), header("Origin", "https://evil.example")),
		http.StatusForbidden, "bad_origin")
}

func connectJira(t *testing.T, g *rig, l login, sites string) {
	t.Helper()
	g.prov.sites.Store(sites)
	completeFlow(t, g, l, "atlassian", http.StatusNoContent)
}

func TestSettingsJiraSiteMustBeReachable(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	_ = g.oauth.SetClient(auth.Atlassian, auth.ClientCredentials{ClientID: "id", ClientSecret: "s"})
	withSite := strings.Replace(validSettings, `"jiraSiteId":null`, `"jiraSiteId":"cloud-2"`, 1)
	wantProblem(t, g.do(http.MethodPut, "/api/settings", withSite, l.write()...), http.StatusUnprocessableEntity, "unknown_site")

	connectJira(t, g, l, twoSites)
	if w := g.do(http.MethodPut, "/api/settings", withSite, l.write()...); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	wantProblem(t, g.do(http.MethodPut, "/api/settings", strings.Replace(withSite, "cloud-2", "cloud-9", 1), l.write()...),
		http.StatusUnprocessableEntity, "unknown_site")
	if got := decode[settingsBody](t, g.do(http.MethodGet, "/api/settings", "", cookie(l.cookie))); got.JiraSiteID == nil || *got.JiraSiteID != "cloud-2" {
		t.Fatalf("%+v", got)
	}
	if w := g.do(http.MethodPut, "/api/settings", validSettings, l.write()...); w.Code != http.StatusOK {
		t.Fatalf("clearing the site: %d", w.Code)
	}
}

type setupBody struct {
	Github, Atlassian struct {
		Configured  bool
		CallbackURL string
		InstallURL  *string
	}
}

func TestSetupNeverReturnsSecrets(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	w := g.do(http.MethodGet, "/api/setup", "", cookie(l.cookie))
	got := decode[setupBody](t, w)
	if got.Github.Configured || got.Atlassian.Configured ||
		got.Github.CallbackURL != "http://127.0.0.1:4317/oauth/github/callback" ||
		got.Atlassian.CallbackURL != "http://127.0.0.1:4317/oauth/atlassian/callback" {
		t.Fatalf("%+v", got)
	}
	for _, p := range []string{"github", "atlassian"} {
		w := g.do(http.MethodPut, "/api/setup/"+p, `{"clientId":" my-id ","clientSecret":"very-secret-value"}`, l.write()...)
		if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
			t.Fatalf("%s: %d %s", p, w.Code, w.Body)
		}
	}
	w = g.do(http.MethodGet, "/api/setup", "", cookie(l.cookie))
	got = decode[setupBody](t, w)
	if !got.Github.Configured || !got.Atlassian.Configured {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(w.Body.String(), "very-secret-value") || strings.Contains(w.Body.String(), "my-id") {
		t.Fatalf("credentials echoed: %s", w.Body)
	}
	raw, err := g.secrets.Get(secrets.GitHubClient)
	if err != nil || !strings.Contains(string(raw), `"clientId":"my-id"`) || !strings.Contains(string(raw), "very-secret-value") {
		t.Fatalf("not stored in the secrets store: %s %v", raw, err)
	}
	if strings.Contains(g.logs.String(), "very-secret-value") {
		t.Fatal("secret in the log")
	}
}

func TestSetupAppSlugBuildsTheInstallLink(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	put := func(path, body string) *httptest.ResponseRecorder {
		return g.do(http.MethodPut, path, body, l.write()...)
	}
	setup := func() setupBody {
		return decode[setupBody](t, g.do(http.MethodGet, "/api/setup", "", cookie(l.cookie)))
	}

	if w := put("/api/setup/github", `{"clientId":"a","clientSecret":"b"}`); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got := setup(); got.Github.InstallURL != nil {
		t.Fatalf("no slug, no link: %+v", got.Github)
	}
	if w := put("/api/setup/github", `{"clientId":"a","clientSecret":"b","appSlug":"my-dash"}`); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	got := setup()
	if got.Github.InstallURL == nil || *got.Github.InstallURL != "https://github.com/apps/my-dash/installations/new" || got.Atlassian.InstallURL != nil {
		t.Fatalf("%+v", got)
	}
	// Saving again without a slug clears it.
	put("/api/setup/github", `{"clientId":"a","clientSecret":"b"}`)
	if got := setup(); got.Github.InstallURL != nil {
		t.Fatalf("%+v", got.Github)
	}
	// Atlassian ignores the slug.
	if w := put("/api/setup/atlassian", `{"clientId":"a","clientSecret":"b","appSlug":"x"}`); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got := setup(); got.Atlassian.InstallURL != nil {
		t.Fatalf("%+v", got.Atlassian)
	}
	for name, slug := range map[string]string{"upper": "Acme", "url": "evil.example/x", "dash first": "-a", "space": "a b", "long": strings.Repeat("a", 101), "scheme": "https://x"} {
		w := put("/api/setup/github", `{"clientId":"keep","clientSecret":"b","appSlug":"`+slug+`"}`)
		if w.Code != http.StatusBadRequest && w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d", name, w.Code)
		}
	}
	if got := setup(); got.Github.InstallURL != nil {
		t.Fatalf("an invalid slug was stored: %+v", got.Github)
	}
	raw, _ := g.secrets.Get(secrets.GitHubClient)
	if strings.Contains(string(raw), `"keep"`) {
		t.Fatalf("an invalid request replaced the credentials: %s", raw)
	}
}

func TestAccountsReportsGitHubNeedsReconnect(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	if a := accounts(t, g, l); a.Github.NeedsReconnect != nil {
		t.Fatalf("%+v", a.Github)
	}
	_ = g.secrets.Set(secrets.GitHubToken, []byte(`{"token":"ghu_x","method":"app","login":"octocat","refreshToken":"ghr_x","needsReconnect":true}`))
	a := accounts(t, g, l)
	if !a.Github.Connected || a.Github.Method != "app" || a.Github.NeedsReconnect == nil || !*a.Github.NeedsReconnect {
		t.Fatalf("%+v", a.Github)
	}
	body := g.do(http.MethodGet, "/api/accounts", "", cookie(l.cookie)).Body.String()
	if strings.Contains(body, "ghu_x") || strings.Contains(body, "ghr_x") {
		t.Fatalf("token in the response: %s", body)
	}
	// A check of a flagged token answers with the account instead of an error.
	w := g.do(http.MethodPost, "/api/accounts/github/check", "", l.write()...)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestSetupValidation(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	for name, tc := range map[string]struct {
		path, body string
		status     int
		code       string
	}{
		"unknown provider": {"/api/setup/gitlab", `{"clientId":"a","clientSecret":"b"}`, 400, "invalid_body"},
		"empty id":         {"/api/setup/github", `{"clientId":"","clientSecret":"b"}`, 400, "invalid_body"},
		"blank secret":     {"/api/setup/github", `{"clientId":"a","clientSecret":"   "}`, 422, "invalid_body"},
		"missing secret":   {"/api/setup/github", `{"clientId":"a"}`, 400, "invalid_body"},
		"unknown field":    {"/api/setup/github", `{"clientId":"a","clientSecret":"b","x":1}`, 400, "invalid_body"},
		"too long":         {"/api/setup/github", `{"clientId":"` + strings.Repeat("a", 257) + `","clientSecret":"b"}`, 400, "invalid_body"},
	} {
		w := g.do(http.MethodPut, tc.path, tc.body, l.write()...)
		if w.Code != tc.status {
			t.Errorf("%s: status %d: %s", name, w.Code, w.Body)
		}
	}
	if g.oauth.ClientConfigured(auth.GitHub) {
		t.Fatal("invalid input was stored")
	}
}

func TestPAT(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	put := func(token string) *httptest.ResponseRecorder {
		return g.do(http.MethodPut, "/api/github/pat", `{"token":"`+token+`"}`, l.write()...)
	}

	w := put("ghp_pat")
	acc := decode[map[string]any](t, w)
	if w.Code != 200 || acc["connected"] != true || acc["login"] != "octocat" || acc["method"] != "pat" || acc["warning"] != nil {
		t.Fatalf("classic: %d %v", w.Code, acc)
	}
	if strings.Contains(w.Body.String(), "ghp_pat") {
		t.Fatal("token echoed")
	}
	if raw, _ := g.secrets.Get(secrets.GitHubToken); !strings.Contains(string(raw), "ghp_pat") {
		t.Fatal("token not in the secrets store")
	}

	g.prov.githubScopes.Store("")
	acc = decode[map[string]any](t, put("github_pat_x"))
	if acc["warning"] == nil || acc["warning"] == "" {
		t.Fatalf("fine-grained token needs a warning: %v", acc)
	}

	g.prov.githubScopes.Store("repo")
	w = put("ghp_pat")
	wantProblem(t, w, http.StatusUnprocessableEntity, "insufficient_scopes")
	if !strings.Contains(w.Body.String(), "read:org") {
		t.Fatalf("missing scope not named: %s", w.Body)
	}
	wantProblem(t, put("ghp_unknown"), http.StatusUnprocessableEntity, "invalid_token")

	g.prov.githubMode.Store("sso")
	w = put("ghp_pat")
	wantProblem(t, w, http.StatusForbidden, "org_blocked")
	if !strings.Contains(w.Body.String(), "acme") {
		t.Fatalf("org not named: %s", w.Body)
	}
	g.prov.githubMode.Store("down")
	wantProblem(t, put("ghp_pat"), http.StatusBadGateway, "upstream_error")

	wantProblem(t, g.do(http.MethodPut, "/api/github/pat", `{"token":""}`, l.write()...), http.StatusBadRequest, "invalid_body")
	wantProblem(t, g.do(http.MethodPut, "/api/github/pat", `{"token":"a","x":1}`, l.write()...), http.StatusBadRequest, "invalid_body")
	if strings.Contains(g.logs.String(), "ghp_pat") {
		t.Fatal("token in the log")
	}
}

type accountsBody struct {
	Github struct {
		Connected bool
		Login     string
		Method    string
		Warning   *string
		// NeedsReconnect is a pointer so the test sees that the field is absent when false.
		NeedsReconnect *bool
	}
	Jira struct {
		Connected      bool
		Sites          []struct{ ID, Name, URL string }
		SelectedSiteID *string
		NeedsReconnect bool
	}
}

func accounts(t *testing.T, g *rig, l login) accountsBody {
	t.Helper()
	w := g.do(http.MethodGet, "/api/accounts", "", cookie(l.cookie))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	return decode[accountsBody](t, w)
}

func TestAccountsDisconnectedAndConnected(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	w := g.do(http.MethodGet, "/api/accounts", "", cookie(l.cookie))
	if !strings.Contains(w.Body.String(), `"sites":[]`) || !strings.Contains(w.Body.String(), `"selectedSiteId":null`) {
		t.Fatalf("empty jira account must list no sites and a null selection: %s", w.Body)
	}
	a := decode[accountsBody](t, w)
	if a.Github.Connected || a.Jira.Connected || a.Jira.NeedsReconnect {
		t.Fatalf("%+v", a)
	}

	g.do(http.MethodPut, "/api/github/pat", `{"token":"ghp_pat"}`, l.write()...)
	_ = g.oauth.SetClient(auth.Atlassian, auth.ClientCredentials{ClientID: "id", ClientSecret: "s"})
	connectJira(t, g, l, oneSite)
	a = accounts(t, g, l)
	if !a.Github.Connected || a.Github.Login != "octocat" || a.Github.Method != "pat" {
		t.Fatalf("%+v", a.Github)
	}
	if !a.Jira.Connected || len(a.Jira.Sites) != 1 || a.Jira.Sites[0].URL != "https://acme.atlassian.net" ||
		a.Jira.SelectedSiteID == nil || *a.Jira.SelectedSiteID != "cloud-1" {
		t.Fatalf("one site is selected automatically: %+v", a.Jira)
	}
	if strings.Contains(g.do(http.MethodGet, "/api/accounts", "", cookie(l.cookie)).Body.String(), "atl-access") {
		t.Fatal("jira token in the response")
	}
}

func TestAccountsSiteSelection(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	_ = g.oauth.SetClient(auth.Atlassian, auth.ClientCredentials{ClientID: "id", ClientSecret: "s"})
	connectJira(t, g, l, twoSites)
	if a := accounts(t, g, l); len(a.Jira.Sites) != 2 || a.Jira.SelectedSiteID != nil {
		t.Fatalf("several sites need a pick: %+v", a.Jira)
	}
	body := strings.Replace(validSettings, `"jiraSiteId":null`, `"jiraSiteId":"cloud-2"`, 1)
	g.do(http.MethodPut, "/api/settings", body, l.write()...)
	if a := accounts(t, g, l); a.Jira.SelectedSiteID == nil || *a.Jira.SelectedSiteID != "cloud-2" {
		t.Fatalf("%+v", a.Jira)
	}
	g.prov.sites.Store(oneSite)
	g.do(http.MethodPost, "/api/accounts/atlassian/check", "", l.write()...)
	if a := accounts(t, g, l); a.Jira.SelectedSiteID == nil || *a.Jira.SelectedSiteID != "cloud-1" {
		t.Fatalf("a selection of a vanished site must fall back: %+v", a.Jira)
	}
}

func TestDisconnect(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	_ = g.oauth.SetClient(auth.Atlassian, auth.ClientCredentials{ClientID: "id", ClientSecret: "s"})
	connectJira(t, g, l, twoSites)
	g.do(http.MethodPut, "/api/settings", strings.Replace(validSettings, `"jiraSiteId":null`, `"jiraSiteId":"cloud-2"`, 1), l.write()...)
	g.do(http.MethodPut, "/api/github/pat", `{"token":"ghp_pat"}`, l.write()...)

	if w := g.do(http.MethodDelete, "/api/accounts/github", "", l.write()...); w.Code != http.StatusNoContent {
		t.Fatalf("%d", w.Code)
	}
	if w := g.do(http.MethodDelete, "/api/accounts/atlassian", "", l.write()...); w.Code != http.StatusNoContent {
		t.Fatalf("%d", w.Code)
	}
	if w := g.do(http.MethodDelete, "/api/accounts/atlassian", "", l.write()...); w.Code != http.StatusNoContent {
		t.Fatalf("disconnecting twice: %d", w.Code)
	}
	a := accounts(t, g, l)
	if a.Github.Connected || a.Jira.Connected || len(a.Jira.Sites) != 0 {
		t.Fatalf("%+v", a)
	}
	if got := decode[settingsBody](t, g.do(http.MethodGet, "/api/settings", "", cookie(l.cookie))); got.JiraSiteID != nil {
		t.Fatal("jiraSiteId survived the disconnect")
	}
	if !g.oauth.ClientConfigured(auth.Atlassian) {
		t.Fatal("client credentials removed")
	}
	wantProblem(t, g.do(http.MethodDelete, "/api/accounts/gitlab", "", l.write()...), http.StatusBadRequest, "invalid_body")
	wantProblem(t, g.do(http.MethodDelete, "/api/accounts/github", "", cookie(l.cookie)), http.StatusForbidden, "bad_csrf")
}

func TestCheckAccount(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	wantProblem(t, g.do(http.MethodPost, "/api/accounts/github/check", "", l.write()...), http.StatusConflict, "not_connected")
	wantProblem(t, g.do(http.MethodPost, "/api/accounts/atlassian/check", "", l.write()...), http.StatusConflict, "not_connected")

	g.do(http.MethodPut, "/api/github/pat", `{"token":"ghp_pat"}`, l.write()...)
	if w := g.do(http.MethodPost, "/api/accounts/github/check", "", l.write()...); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	_ = g.secrets.Set(secrets.GitHubToken, []byte(`{"token":"revoked","method":"pat","login":"x"}`))
	wantProblem(t, g.do(http.MethodPost, "/api/accounts/github/check", "", l.write()...), http.StatusUnprocessableEntity, "invalid_token")

	_ = g.oauth.SetClient(auth.Atlassian, auth.ClientCredentials{ClientID: "id", ClientSecret: "s"})
	connectJira(t, g, l, oneSite)
	if w := g.do(http.MethodPost, "/api/accounts/atlassian/check", "", l.write()...); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	g.clock.Advance(2 * time.Hour)
	g.prov.refreshStatus.Store(400)
	w := g.do(http.MethodPost, "/api/accounts/atlassian/check", "", l.write()...)
	if w.Code != 200 {
		t.Fatalf("refresh failure is a state, not an error: %d %s", w.Code, w.Body)
	}
	if a := decode[accountsBody](t, w); !a.Jira.NeedsReconnect || a.Jira.Connected {
		t.Fatalf("%+v", a.Jira)
	}
	if a := accounts(t, g, l); !a.Jira.NeedsReconnect || len(a.Jira.Sites) != 1 {
		t.Fatalf("flag not persisted: %+v", a.Jira)
	}
	g.prov.refreshStatus.Store(500)
	_ = g.secrets.Delete(secrets.AtlassianToken)
	connectJira(t, g, l, oneSite)
	g.clock.Advance(2 * time.Hour)
	wantProblem(t, g.do(http.MethodPost, "/api/accounts/atlassian/check", "", l.write()...), http.StatusBadGateway, "upstream_error")
}

func TestRevokeAllSessions(t *testing.T) {
	g := newRig(t, webFS())
	a, b := g.login(), g.login()
	wantProblem(t, g.do(http.MethodPost, "/api/sessions/revoke-all", "", cookie(a.cookie)), http.StatusForbidden, "bad_csrf")
	w := g.do(http.MethodPost, "/api/sessions/revoke-all", "", a.write()...)
	if w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var cleared *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "prd_session" {
			cleared = c
		}
	}
	if cleared == nil || cleared.Value != "" || cleared.MaxAge >= 0 || !cleared.HttpOnly || cleared.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie %+v", cleared)
	}
	for _, l := range []login{a, b} {
		wantProblem(t, g.do(http.MethodGet, "/api/session", "", cookie(l.cookie)), http.StatusUnauthorized, "unauthenticated")
	}
	c := g.login()
	if w := g.do(http.MethodGet, "/api/session", "", cookie(c.cookie)); w.Code != 200 {
		t.Fatalf("a new session after revoke-all: %d", w.Code)
	}
}

func TestSessionsSurviveRestart(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	restarted := auth.NewSessions([]byte("0123456789abcdef0123456789abcdef"), g.clock.Now, &testutil.Rand{}, g.store, nil)
	if err := restarted.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.Authenticate(l.cookie); !ok {
		t.Fatal("session lost across restart")
	}
}
