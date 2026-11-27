package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
)

func configure(t *testing.T, g *rig) {
	t.Helper()
	for _, p := range []auth.Provider{auth.GitHub, auth.Atlassian} {
		if err := g.oauth.SetClient(p, auth.ClientCredentials{ClientID: "id-" + string(p), ClientSecret: "secret-" + string(p)}); err != nil {
			t.Fatal(err)
		}
	}
}

func oauthStart(t *testing.T, g *rig, l login, provider string) (*url.URL, string) {
	t.Helper()
	w := g.do(http.MethodGet, "/oauth/"+provider+"/start", "", cookie(l.cookie))
	if w.Code != http.StatusFound {
		t.Fatalf("start %s: %d %s", provider, w.Code, w.Body)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u, u.Query().Get("state")
}

func completeBody(provider, code, state string) string {
	return `{"provider":"` + provider + `","code":"` + code + `","state":"` + state + `"}`
}

// completeFlow runs start and complete for a provider as the browser would, and expects status.
func completeFlow(t *testing.T, g *rig, l login, provider string, status int) {
	t.Helper()
	configure(t, g)
	_, state := oauthStart(t, g, l, provider)
	w := g.do(http.MethodPost, "/api/oauth/complete", completeBody(provider, "the-code", state), l.write()...)
	if w.Code != status {
		t.Fatalf("complete %s: %d %s", provider, w.Code, w.Body)
	}
}

func TestOAuthStart(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	configure(t, g)

	u, state := oauthStart(t, g, l, "github")
	if u.Host != "github.example" || u.Query().Get("client_id") != "id-github" || len(state) != 43 ||
		u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("redirect_uri") != "http://127.0.0.1:4317/oauth/github/callback" {
		t.Fatalf("%s", u)
	}
	u, _ = oauthStart(t, g, l, "atlassian")
	if u.Host != "atlassian.example" || u.Query().Get("audience") != "api.atlassian.com" || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("%s", u)
	}
	w := g.do(http.MethodGet, "/oauth/github/start", "", cookie(l.cookie))
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Body.Len() != 0 {
		t.Fatalf("headers %v body %q", w.Header(), w.Body)
	}
}

func TestOAuthStartGuards(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	configure(t, g)
	wantProblem(t, g.do(http.MethodGet, "/oauth/github/start", ""), http.StatusUnauthorized, "unauthenticated")
	wantProblem(t, g.do(http.MethodGet, "/oauth/github/start", "", cookie("forged.value")), http.StatusUnauthorized, "unauthenticated")
	for _, site := range []string{"cross-site", "same-site", "none"} {
		wantProblem(t, g.do(http.MethodGet, "/oauth/github/start", "", cookie(l.cookie), header("Sec-Fetch-Site", site)),
			http.StatusForbidden, "cross_site")
	}
	wantProblem(t, g.do(http.MethodGet, "/oauth/github/start", "", cookie(l.cookie), withHost("evil.example:4317")),
		http.StatusMisdirectedRequest, "misdirected_host")
	wantProblem(t, g.do(http.MethodGet, "/oauth/gitlab/start", "", cookie(l.cookie)), http.StatusNotFound, "not_found")
	if w := g.do(http.MethodPost, "/oauth/github/start", "", l.write()...); w.Code < 400 {
		t.Fatalf("POST start: %d", w.Code)
	}
}

func TestOAuthStartWithoutClientCredentials(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	w := g.do(http.MethodGet, "/oauth/github/start", "", cookie(l.cookie))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/auth/done/?provider=github&result=not_configured" {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestOAuthCallbackIsCookielessAndRedirectsToFinish(t *testing.T) {
	g := newRig(t, webFS())
	// A cross-site navigation: no cookie, Sec-Fetch-Site cross-site.
	w := g.do(http.MethodGet, "/oauth/github/callback?code=abc123&state=S_t-a", "", header("Sec-Fetch-Site", "cross-site"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil || loc.Host != "" || loc.Path != "/oauth/finish/" {
		t.Fatalf("location %q", w.Header().Get("Location"))
	}
	if q := loc.Query(); q.Get("provider") != "github" || q.Get("code") != "abc123" || q.Get("state") != "S_t-a" {
		t.Fatalf("query %v", q)
	}
	h := w.Header()
	if h.Get("Cache-Control") != "no-store" || h.Get("Referrer-Policy") != "no-referrer" || h.Get("Set-Cookie") != "" || w.Body.Len() != 0 {
		t.Fatalf("headers %v", h)
	}
	if h.Get("Content-Security-Policy") == "" {
		t.Fatal("security headers missing on the callback")
	}
	if w := g.do(http.MethodGet, "/oauth/atlassian/callback?code=c&state=s", "", header("Sec-Fetch-Site", "same-site")); w.Code != http.StatusSeeOther ||
		!strings.Contains(w.Header().Get("Location"), "provider=atlassian") {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestOAuthCallbackParameterShapes(t *testing.T) {
	g := newRig(t, webFS())
	invalid := "/oauth/finish/?error=invalid_callback&provider=github"
	cases := map[string]string{
		"/oauth/github/callback":                                                invalid,
		"/oauth/github/callback?code=a":                                         invalid,
		"/oauth/github/callback?state=a":                                        invalid,
		"/oauth/github/callback?code=a&state=b%20c":                             invalid,
		"/oauth/github/callback?code=a%3Cscript%3E&state=b":                     invalid,
		"/oauth/github/callback?code=" + strings.Repeat("a", 2049) + "&state=b": invalid,
		"/oauth/github/callback?code=a&state=" + strings.Repeat("b", 257):       invalid,
		"/oauth/github/callback?error=access_denied&state=b":                    "/oauth/finish/?error=access_denied&provider=github",
		"/oauth/github/callback?error=%3Cscript%3E":                             invalid,
		"/oauth/github/callback?error=access_denied&code=a&state=b":             "/oauth/finish/?error=access_denied&provider=github",
	}
	for path, want := range cases {
		w := g.do(http.MethodGet, path, "")
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
			t.Errorf("%.60s: %d %s, want %s", path, w.Code, w.Header().Get("Location"), want)
		}
	}
}

func TestOAuthCallbackExemptionIsExact(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	crossSite := header("Sec-Fetch-Site", "cross-site")
	good := "?code=a&state=b"
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if w := g.do(m, "/oauth/github/callback"+good, "", crossSite, header("Origin", origin)); w.Code < 400 || w.Code == http.StatusSeeOther {
			t.Errorf("%s on the callback: %d", m, w.Code)
		}
	}
	wantProblem(t, g.do(http.MethodGet, "/oauth/gitlab/callback"+good, ""), http.StatusNotFound, "not_found")
	for _, path := range []string{"/oauth/github/callback/", "/oauth/github/callbackx", "/oauth/github/callback/x", "/oauth//callback"} {
		if w := g.do(http.MethodGet, path+good, "", crossSite); w.Code == http.StatusSeeOther {
			t.Errorf("%s: redirected", path)
		}
	}
	// Under /api the cross-site rule still applies, even for a path that looks like the callback.
	wantProblem(t, g.do(http.MethodGet, "/api/oauth/github/callback"+good, "", crossSite), http.StatusForbidden, "cross_site")
	wantProblem(t, g.do(http.MethodGet, "/oauth/github/start", "", cookie(l.cookie), crossSite), http.StatusForbidden, "cross_site")
	// Host is still enforced on the callback.
	wantProblem(t, g.do(http.MethodGet, "/oauth/github/callback"+good, "", withHost("evil.example:4317")), http.StatusMisdirectedRequest, "misdirected_host")
	// The callback never reads or sets a session.
	w := g.do(http.MethodGet, "/oauth/github/callback"+good, "", cookie(l.cookie))
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("callback set a cookie")
	}
}

func TestFinishPageIsStaticAndNeedsNoSession(t *testing.T) {
	g := newRig(t, webFS())
	w := g.do(http.MethodGet, "/oauth/finish/?provider=github&code=a&state=b", "", header("Sec-Fetch-Site", "cross-site"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "finish") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestOAuthCompleteGuards(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	configure(t, g)
	body := completeBody("github", "c", "s")
	wantProblem(t, g.do(http.MethodPost, "/api/oauth/complete", body), http.StatusUnauthorized, "unauthenticated")
	wantProblem(t, g.do(http.MethodPost, "/api/oauth/complete", body, cookie(l.cookie)), http.StatusForbidden, "bad_csrf")
	wantProblem(t, g.do(http.MethodPost, "/api/oauth/complete", body, append(l.write(), header("Sec-Fetch-Site", "cross-site"))...), http.StatusForbidden, "cross_site")
	wantProblem(t, g.do(http.MethodPost, "/api/oauth/complete", body, append(l.write(), header("Origin", "https://evil.example"))...), http.StatusForbidden, "bad_origin")
	wantProblem(t, g.do(http.MethodGet, "/api/oauth/complete", "", cookie(l.cookie)), http.StatusNotFound, "not_found")
	for name, b := range map[string]string{
		"unknown provider": completeBody("gitlab", "c", "s"),
		"empty code":       completeBody("github", "", "s"),
		"empty state":      completeBody("github", "c", ""),
		"unknown field":    `{"provider":"github","code":"c","state":"s","x":1}`,
		"missing state":    `{"provider":"github","code":"c"}`,
		"long code":        completeBody("github", strings.Repeat("a", 2049), "s"),
	} {
		if w := g.do(http.MethodPost, "/api/oauth/complete", b, l.write()...); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
}

func TestOAuthCompleteStateRules(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	other := g.login()
	configure(t, g)
	complete := func(who login, provider, state string) *httptest.ResponseRecorder {
		return g.do(http.MethodPost, "/api/oauth/complete", completeBody(provider, "code", state), who.write()...)
	}

	_, state := oauthStart(t, g, l, "github")
	wantProblem(t, complete(l, "github", "bogus-state"), http.StatusBadRequest, "invalid_oauth_state")
	wantProblem(t, complete(other, "github", state), http.StatusBadRequest, "invalid_oauth_state")
	wantProblem(t, complete(l, "github", state), http.StatusBadRequest, "invalid_oauth_state") // spent by the wrong-session attempt

	_, state = oauthStart(t, g, l, "github")
	wantProblem(t, complete(l, "atlassian", state), http.StatusBadRequest, "invalid_oauth_state")

	_, state = oauthStart(t, g, l, "github")
	g.clock.Advance(auth.StateTTL)
	wantProblem(t, complete(l, "github", state), http.StatusBadRequest, "invalid_oauth_state")

	_, state = oauthStart(t, g, l, "github")
	if w := complete(l, "github", state); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	wantProblem(t, complete(l, "github", state), http.StatusBadRequest, "invalid_oauth_state")
	if a := accounts(t, g, l); !a.Github.Connected || a.Github.Method != "app" || a.Github.Login != "octocat" {
		t.Fatalf("%+v", a.Github)
	}
}

func TestOAuthCompleteErrors(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	configure(t, g)
	g.prov.exchangeFails.Store(true)
	_, state := oauthStart(t, g, l, "github")
	w := g.do(http.MethodPost, "/api/oauth/complete", completeBody("github", "secret-code-value", state), l.write()...)
	wantProblem(t, w, http.StatusBadGateway, "oauth_exchange_failed")
	if strings.Contains(w.Body.String(), "secret-code-value") || strings.Contains(g.logs.String(), "secret-code-value") {
		t.Fatal("authorization code leaked")
	}
	_, state = oauthStart(t, g, l, "atlassian")
	wantProblem(t, g.do(http.MethodPost, "/api/oauth/complete", completeBody("atlassian", "c", state), l.write()...), http.StatusBadGateway, "oauth_exchange_failed")

	_, state = oauthStart(t, g, l, "github")
	_ = g.secrets.Delete("github.oauth.client")
	wantProblem(t, g.do(http.MethodPost, "/api/oauth/complete", completeBody("github", "c", state), l.write()...), http.StatusConflict, "provider_not_configured")
}

func TestAtlassianFlowRefreshAndReconnect(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	completeFlow(t, g, l, "atlassian", http.StatusNoContent)
	if a := accounts(t, g, l); !a.Jira.Connected || a.Jira.NeedsReconnect {
		t.Fatalf("%+v", a.Jira)
	}
	g.clock.Advance(2 * time.Hour)
	if w := g.do(http.MethodPost, "/api/accounts/atlassian/check", "", l.write()...); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	g.clock.Advance(2 * time.Hour)
	g.prov.refreshStatus.Store(400)
	g.do(http.MethodPost, "/api/accounts/atlassian/check", "", l.write()...)
	if a := accounts(t, g, l); !a.Jira.NeedsReconnect {
		t.Fatalf("%+v", a.Jira)
	}
	g.prov.refreshStatus.Store(0)
	completeFlow(t, g, l, "atlassian", http.StatusNoContent)
	if a := accounts(t, g, l); a.Jira.NeedsReconnect || !a.Jira.Connected {
		t.Fatalf("reconnect did not clear the flag: %+v", a.Jira)
	}
}

func TestOAuthSecretsStayOutOfLogsAndResponses(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	completeFlow(t, g, l, "github", http.StatusNoContent)
	completeFlow(t, g, l, "atlassian", http.StatusNoContent)
	w := g.do(http.MethodGet, "/oauth/github/callback?code=visible-code-0123&state=visible-state-abc", "")
	if w.Code != http.StatusSeeOther {
		t.Fatal(w.Code)
	}
	logs := g.logs.String()
	for _, secret := range []string{"gho_abc", "atl-access", "atl-refresh", "secret-github", "secret-atlassian", "visible-code-0123", "visible-state-abc", "the-code"} {
		if strings.Contains(logs, secret) {
			t.Errorf("%q in the log", secret)
		}
	}
	for _, p := range []string{"/api/accounts", "/api/setup", "/api/settings"} {
		body := g.do(http.MethodGet, p, "", cookie(l.cookie)).Body.String()
		for _, secret := range []string{"gho_abc", "atl-access", "atl-refresh", "secret-github", "secret-atlassian"} {
			if strings.Contains(body, secret) {
				t.Errorf("%q in %s", secret, p)
			}
		}
	}
}
