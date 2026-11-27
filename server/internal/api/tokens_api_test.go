package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
)

func addToken(g *rig, l login, token, name string) *httptest.ResponseRecorder {
	body := `{"token":"` + token + `"`
	if name != "" {
		body += `,"name":"` + name + `"`
	}
	return g.do(http.MethodPost, "/api/github/tokens", body+"}", l.write()...)
}

func TestAddGithubToken(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	w := addToken(g, l, "ghp_pat", "")
	if w.Code != http.StatusCreated {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	got := decode[githubTokenBody](t, w)
	if len(got.ID) != 8 || got.Name != "Token (octocat)" || got.Method != "pat" || got.Login != "octocat" || got.FineGrained || got.Warning != nil || got.NeedsReconnect {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(w.Body.String(), `"warning":null`) {
		t.Fatalf("warning must be present as null: %s", w.Body)
	}
	if strings.Contains(w.Body.String(), "ghp_pat") {
		t.Fatal("token echoed")
	}

	g.prov.githubScopes.Store("")
	w = addToken(g, l, "github_pat_x", " Fine ")
	fine := decode[githubTokenBody](t, w)
	if w.Code != http.StatusCreated || fine.Name != "Fine" || !fine.FineGrained || fine.Warning == nil {
		t.Fatalf("%d %+v", w.Code, fine)
	}

	a := accounts(t, g, l)
	if a.GithubTokenLimit != 10 || len(a.GithubTokens) != 2 || a.GithubTokens[0].ID != got.ID || a.GithubTokens[1].ID != fine.ID {
		t.Fatalf("%+v", a)
	}
	if !a.Github.Connected || a.Github.Login != "octocat" || a.Github.Method != "pat" || a.Github.Warning != nil {
		t.Fatalf("the summary is the primary token's: %+v", a.Github)
	}
	body := g.do(http.MethodGet, "/api/accounts", "", cookie(l.cookie)).Body.String()
	if strings.Contains(body, "ghp_pat") || strings.Contains(body, "github_pat_x") || strings.Contains(g.logs.String(), "ghp_pat") {
		t.Fatal("a token leaked")
	}
}

func TestAddGithubTokenProblems(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	addToken(g, l, "ghp_pat", "Work")
	wantProblem(t, addToken(g, l, "ghp_pat", "Other"), http.StatusConflict, "token_duplicate")
	w := addToken(g, l, "ghp_pat", "Other")
	if !strings.Contains(w.Body.String(), "Work") {
		t.Fatalf("the stored token is named: %s", w.Body)
	}
	wantProblem(t, addToken(g, l, "ghp_extra", "work"), http.StatusConflict, "token_name_taken")
	wantProblem(t, addToken(g, l, "ghp_unknown", ""), http.StatusUnprocessableEntity, "invalid_token")
	wantProblem(t, addToken(g, l, "ghp_extra", "   "), http.StatusUnprocessableEntity, "invalid_body")
	wantProblem(t, addToken(g, l, "ghp_extra", `a\nb`), http.StatusUnprocessableEntity, "invalid_body")
	wantProblem(t, addToken(g, l, "ghp_extra", strings.Repeat("a", 41)), http.StatusBadRequest, "invalid_body")
	wantProblem(t, g.do(http.MethodPost, "/api/github/tokens", `{"token":""}`, l.write()...), http.StatusBadRequest, "invalid_body")
	wantProblem(t, g.do(http.MethodPost, "/api/github/tokens", `{"token":"a","x":1}`, l.write()...), http.StatusBadRequest, "invalid_body")
	wantProblem(t, g.do(http.MethodPost, "/api/github/tokens", `{}`, l.write()...), http.StatusBadRequest, "invalid_body")

	g.prov.githubScopes.Store("repo")
	wantProblem(t, addToken(g, l, "ghp_extra", ""), http.StatusUnprocessableEntity, "insufficient_scopes")
	g.prov.githubMode.Store("sso")
	wantProblem(t, addToken(g, l, "ghp_extra", ""), http.StatusForbidden, "org_blocked")
	g.prov.githubMode.Store("down")
	wantProblem(t, addToken(g, l, "ghp_extra", ""), http.StatusBadGateway, "upstream_error")
	if n := len(accounts(t, g, l).GithubTokens); n != 1 {
		t.Fatalf("a refused token was stored: %d", n)
	}
}

func TestGithubTokenLimit(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	for i := 1; i <= 10; i++ {
		if w := addToken(g, l, fmt.Sprintf("ghp_extra%d", i), ""); w.Code != http.StatusCreated {
			t.Fatalf("token %d: %d %s", i, w.Code, w.Body)
		}
	}
	w := addToken(g, l, "ghp_extra11", "")
	wantProblem(t, w, http.StatusConflict, "token_limit")
	if !strings.Contains(w.Body.String(), "At most 10") {
		t.Fatalf("a clear detail: %s", w.Body)
	}
	// PUT /api/github/pat adds a token the same way, so the limit holds for it too.
	wantProblem(t, g.do(http.MethodPut, "/api/github/pat", `{"token":"ghp_extra12"}`, l.write()...), http.StatusConflict, "token_limit")
	if a := accounts(t, g, l); len(a.GithubTokens) != 10 {
		t.Fatalf("%d", len(a.GithubTokens))
	}
}

func TestPutPatNamesAndAddsNextToExistingTokens(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	if w := g.do(http.MethodPut, "/api/github/pat", `{"token":"ghp_pat","name":"First"}`, l.write()...); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := g.do(http.MethodPut, "/api/github/pat", `{"token":"ghp_extra1","name":"Second"}`, l.write()...); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	a := accounts(t, g, l)
	if len(a.GithubTokens) != 2 || a.GithubTokens[0].Name != "First" || a.GithubTokens[1].Name != "Second" {
		t.Fatalf("%+v", a.GithubTokens)
	}
	wantProblem(t, g.do(http.MethodPut, "/api/github/pat", `{"token":"ghp_pat"}`, l.write()...), http.StatusConflict, "token_duplicate")
	wantProblem(t, g.do(http.MethodPut, "/api/github/pat", `{"token":"ghp_extra2","name":"first"}`, l.write()...), http.StatusConflict, "token_name_taken")
}

func TestLegacyGithubTokenShowsUpAsTheOnlyEntry(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	_ = g.secrets.Set(secrets.GitHubToken, []byte(`{"token":"ghp_pat","method":"pat","login":"octocat"}`))
	a := accounts(t, g, l)
	if len(a.GithubTokens) != 1 || a.GithubTokens[0].Name != "Token (octocat)" || !a.Github.Connected {
		t.Fatalf("%+v", a)
	}
	if _, err := g.secrets.Get(secrets.GitHubToken); err == nil {
		t.Fatal("legacy item kept")
	}
}

func TestRenameGithubToken(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	a := decode[githubTokenBody](t, addToken(g, l, "ghp_pat", "One"))
	b := decode[githubTokenBody](t, addToken(g, l, "ghp_extra1", "Two"))
	put := func(id, body string) *httptest.ResponseRecorder {
		return g.do(http.MethodPut, "/api/github/tokens/"+id, body, l.write()...)
	}
	w := put(b.ID, `{"name":" Three "}`)
	if got := decode[githubTokenBody](t, w); w.Code != 200 || got.Name != "Three" || got.ID != b.ID {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	wantProblem(t, put(b.ID, `{"name":"one"}`), http.StatusConflict, "token_name_taken")
	wantProblem(t, put(a.ID, `{"name":"  "}`), http.StatusUnprocessableEntity, "invalid_body")
	wantProblem(t, put(a.ID, `{"name":""}`), http.StatusBadRequest, "invalid_body")
	wantProblem(t, put(a.ID, `{"name":"x","token":"y"}`), http.StatusBadRequest, "invalid_body")
	wantProblem(t, put("deadbeef", `{"name":"x"}`), http.StatusNotFound, "not_found")
	wantProblem(t, put("DEADBEEF", `{"name":"x"}`), http.StatusBadRequest, "invalid_body")
	if got := accounts(t, g, l).GithubTokens; got[1].Name != "Three" || got[0].Name != "One" {
		t.Fatalf("%+v", got)
	}
}

func TestDeleteGithubToken(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	a := decode[githubTokenBody](t, addToken(g, l, "ghp_pat", "One"))
	b := decode[githubTokenBody](t, addToken(g, l, "ghp_extra1", "Two"))
	del := func(id string) *httptest.ResponseRecorder {
		return g.do(http.MethodDelete, "/api/github/tokens/"+id, "", l.write()...)
	}
	wantProblem(t, del("deadbeef"), http.StatusNotFound, "not_found")
	if w := del(a.ID); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	acc := accounts(t, g, l)
	if !acc.Github.Connected || len(acc.GithubTokens) != 1 || acc.GithubTokens[0].ID != b.ID {
		t.Fatalf("%+v", acc)
	}
	wantProblem(t, del(a.ID), http.StatusNotFound, "not_found")
	if w := del(b.ID); w.Code != http.StatusNoContent {
		t.Fatalf("%d", w.Code)
	}
	acc = accounts(t, g, l)
	if acc.Github.Connected || len(acc.GithubTokens) != 0 || acc.GithubTokenLimit != 10 {
		t.Fatalf("removing the last token disconnects: %+v", acc)
	}
	if !strings.Contains(g.do(http.MethodGet, "/api/accounts", "", cookie(l.cookie)).Body.String(), `"githubTokens":[]`) {
		t.Fatal("an empty list must be [] and not null")
	}
}

func TestDisconnectGithubRemovesEveryToken(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	addToken(g, l, "ghp_pat", "")
	addToken(g, l, "ghp_extra1", "")
	if w := g.do(http.MethodDelete, "/api/accounts/github", "", l.write()...); w.Code != http.StatusNoContent {
		t.Fatalf("%d", w.Code)
	}
	if a := accounts(t, g, l); a.Github.Connected || len(a.GithubTokens) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestCheckGithubToken(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	a := decode[githubTokenBody](t, addToken(g, l, "ghp_pat", "One"))
	b := decode[githubTokenBody](t, addToken(g, l, "ghp_extra1", "Two"))
	check := func(id string) *httptest.ResponseRecorder {
		return g.do(http.MethodPost, "/api/github/tokens/"+id+"/check", "", l.write()...)
	}
	if w := check(a.ID); w.Code != 200 || len(decode[accountsBody](t, w).GithubTokens) != 2 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	wantProblem(t, check("deadbeef"), http.StatusNotFound, "not_found")
	_ = g.secrets.Set(secrets.GitHubTokens, []byte(`[`+
		`{"id":"`+a.ID+`","name":"One","token":"ghp_pat","method":"pat","login":"octocat"},`+
		`{"id":"`+b.ID+`","name":"Two","token":"revoked","method":"pat","login":"octocat"}]`))
	wantProblem(t, check(b.ID), http.StatusUnprocessableEntity, "invalid_token")
	acc := accounts(t, g, l)
	if acc.GithubTokens[0].NeedsReconnect || !acc.GithubTokens[1].NeedsReconnect {
		t.Fatalf("only the rejected token is flagged: %+v", acc.GithubTokens)
	}
	if acc.Github.NeedsReconnect == nil || !*acc.Github.NeedsReconnect {
		t.Fatalf("the summary reports it: %+v", acc.Github)
	}
	if w := check(a.ID); w.Code != 200 {
		t.Fatalf("a good token still checks: %d %s", w.Code, w.Body)
	}
	// An App token that needs a sign-in again answers with the accounts, like the provider check.
	_ = g.secrets.Set(secrets.GitHubTokens, []byte(`[{"id":"0a0b0c0d","name":"App","token":"ghu_x","method":"app","login":"octocat","refreshToken":"r","needsReconnect":true}]`))
	if w := check("0a0b0c0d"); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// The provider-wide check checks every token.
	_ = g.secrets.Set(secrets.GitHubTokens, []byte(`[`+
		`{"id":"`+a.ID+`","name":"One","token":"revoked","method":"pat","login":"octocat"},`+
		`{"id":"`+b.ID+`","name":"Two","token":"ghp_extra1","method":"pat","login":"x"}]`))
	wantProblem(t, g.do(http.MethodPost, "/api/accounts/github/check", "", l.write()...), http.StatusUnprocessableEntity, "invalid_token")
	if acc := accounts(t, g, l); acc.GithubTokens[1].Login != "octocat" || !acc.GithubTokens[0].NeedsReconnect {
		t.Fatalf("every token was checked: %+v", acc.GithubTokens)
	}
}

// tokenRoutes are the routes of the list of tokens with a body that passes the schema.
var tokenRoutes = []struct{ method, path, body string }{
	{http.MethodPost, "/api/github/tokens", `{"token":"ghp_pat"}`},
	{http.MethodPut, "/api/github/tokens/0a0b0c0d", `{"name":"x"}`},
	{http.MethodDelete, "/api/github/tokens/0a0b0c0d", ""},
	{http.MethodPost, "/api/github/tokens/0a0b0c0d/check", ""},
	{http.MethodPut, "/api/github/pat", `{"token":"ghp_pat","name":"x"}`},
}

// TestTokenRoutesPassTheWholeMiddlewareStack walks every new route through the same checks the
// older routes get: host, fetch metadata, origin, session, CSRF, content type, body size, schema.
func TestTokenRoutesPassTheWholeMiddlewareStack(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	for _, rt := range tokenRoutes {
		name := rt.method + " " + rt.path
		do := func(opts ...reqOpt) *httptest.ResponseRecorder { return g.do(rt.method, rt.path, rt.body, opts...) }
		wantProblem(t, do(withHost("evil.example")), http.StatusMisdirectedRequest, "misdirected_host")
		wantProblem(t, do(cookie(l.cookie), csrf(l.csrf), header("Sec-Fetch-Site", "cross-site")), http.StatusForbidden, "cross_site")
		wantProblem(t, do(), http.StatusUnauthorized, "unauthenticated")
		wantProblem(t, do(cookie("forged.cookie"), csrf(l.csrf)), http.StatusUnauthorized, "unauthenticated")
		wantProblem(t, do(cookie(l.cookie)), http.StatusForbidden, "bad_csrf")
		wantProblem(t, do(cookie(l.cookie), csrf("wrong")), http.StatusForbidden, "bad_csrf")
		wantProblem(t, do(cookie(l.cookie), csrf(l.csrf), header("Origin", "https://evil.example")), http.StatusForbidden, "bad_origin")
		wantProblem(t, do(cookie(l.cookie), csrf(l.csrf), noHeader("Origin")), http.StatusForbidden, "bad_origin")
		w := g.do(http.MethodOptions, rt.path, "", header("Access-Control-Request-Method", rt.method), header("Origin", "https://evil.example"))
		wantProblem(t, w, http.StatusMethodNotAllowed, "cors_not_allowed")
		if rt.body != "" {
			wantProblem(t, do(cookie(l.cookie), csrf(l.csrf), header("Content-Type", "text/plain")), http.StatusUnsupportedMediaType, "unsupported_media_type")
			big := `{"x":"` + strings.Repeat("a", mw.MaxBodyBytes) + `"}`
			wantProblem(t, g.do(rt.method, rt.path, big, l.write()...), http.StatusRequestEntityTooLarge, "body_too_large")
			wantProblem(t, g.do(rt.method, rt.path, `{"unknown":1}`, l.write()...), http.StatusBadRequest, "invalid_body")
			wantProblem(t, g.do(rt.method, rt.path, `[]`, l.write()...), http.StatusBadRequest, "invalid_body")
		}
		// A request that passed everything reaches the handler (which may refuse it for its own
		// reasons, but never with one of the middleware's codes).
		if w := do(l.write()...); w.Code == http.StatusUnauthorized || w.Code == http.StatusMisdirectedRequest || w.Code == http.StatusRequestEntityTooLarge {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	// A GET of a write-only route does not fall through to anything.
	wantProblem(t, g.do(http.MethodGet, "/api/github/tokens", "", cookie(l.cookie)), http.StatusNotFound, "not_found")
}
