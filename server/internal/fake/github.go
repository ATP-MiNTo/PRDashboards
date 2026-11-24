//go:build e2e

package fake

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

type ghCode struct{ challenge, redirect string }

type githubConfig struct {
	authorizeConfig
	// SSO makes the GraphQL endpoint answer like an organization that has not authorized the token.
	SSO bool `json:"sso"`
}

type githubState struct {
	Config            githubConfig `json:"config"`
	Authorizations    int          `json:"authorizations"`
	Exchanges         int          `json:"exchanges"`
	LastCallbackURL   string       `json:"lastCallbackUrl"`
	LastCodeVerifier  string       `json:"lastCodeVerifier"`
	LastRedirectURI   string       `json:"lastRedirectUri"`
	BadExchangeErrors int          `json:"badExchangeErrors"`
}

type github struct {
	mu     sync.Mutex
	st     githubState
	codes  safeMap[ghCode]
	tokens safeMap[bool]
}

func newGitHub() *github { return &github{} }

var githubPATs = map[string]string{
	"ghp_classic_ok":         "repo, read:org",
	"ghp_classic_no_org":     "repo",
	"github_pat_finegrained": "",
}

func (g *github) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login/oauth/authorize", g.authorize)
	mux.HandleFunc("POST /login/oauth/access_token", g.accessToken)
	mux.HandleFunc("POST /graphql", g.graphql)
	mux.HandleFunc("GET /__admin/state", func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		writeJSON(w, http.StatusOK, g.st)
	})
	mux.HandleFunc("POST /__admin/config", func(w http.ResponseWriter, r *http.Request) {
		var c githubConfig
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&c); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.mu.Lock()
		g.st.Config = c
		g.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /__admin/reset", func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()
		g.st = githubState{}
		g.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func (g *github) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != GitHubClient.ClientID || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		!strings.Contains(q.Get("scope"), "repo") || q.Get("state") == "" {
		http.Error(w, "invalid authorize request", http.StatusBadRequest)
		return
	}
	g.mu.Lock()
	cfg := g.st.Config.authorizeConfig
	g.st.Authorizations++
	g.mu.Unlock()
	authorizeRedirect(w, r, cfg, func() string {
		code := randomHex(10)
		g.codes.put(code, ghCode{challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")})
		return code
	}, func(target string) {
		g.mu.Lock()
		g.st.LastCallbackURL = target
		g.mu.Unlock()
	})
}

func (g *github) accessToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	fail := func(code string) {
		g.mu.Lock()
		g.st.BadExchangeErrors++
		g.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"error": code})
	}
	if !equal(r.PostForm.Get("client_id"), GitHubClient.ClientID) || !equal(r.PostForm.Get("client_secret"), GitHubClient.ClientSecret) {
		fail("incorrect_client_credentials")
		return
	}
	c, ok := g.codes.take(r.PostForm.Get("code"))
	if !ok || c.redirect != r.PostForm.Get("redirect_uri") {
		fail("bad_verification_code")
		return
	}
	if v := r.PostForm.Get("code_verifier"); v == "" || s256(v) != c.challenge {
		fail("bad_verification_code")
		return
	}
	token := "gho_fake_" + randomHex(8)
	g.tokens.put(token, true)
	g.mu.Lock()
	g.st.Exchanges++
	g.st.LastCodeVerifier = r.PostForm.Get("code_verifier")
	g.st.LastRedirectURI = r.PostForm.Get("redirect_uri")
	g.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"access_token": token, "token_type": "bearer", "scope": "repo,read:org"})
}

func (g *github) graphql(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	scopes, isPAT := githubPATs[token]
	switch {
	case isPAT:
	case g.tokens.has(token):
		scopes = "repo, read:org"
	default:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	g.mu.Lock()
	sso := g.st.Config.SSO
	g.mu.Unlock()
	if sso {
		w.Header().Set("X-GitHub-SSO", "required; url=https://github.com/orgs/acme-corp/sso?authorization_request=FAKE")
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"viewer": nil},
			"errors": []map[string]string{{
				"type":    "FORBIDDEN",
				"message": "Resource protected by organization SAML enforcement. You must grant your OAuth token access to this organization.",
			}},
		})
		return
	}
	if scopes != "" {
		w.Header().Set("X-OAuth-Scopes", scopes)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"viewer": map[string]string{"login": "octocat"}}})
}
