//go:build e2e

package fake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ghCode struct{ challenge, redirect string }

type githubConfig struct {
	authorizeConfig
	// SSO makes the GraphQL endpoint answer like an organization that has not authorized the token.
	SSO bool `json:"sso"`
	// MergeMode steers the REST merge: "" merges, "not_mergeable" answers 405 and "conflict" answers 409.
	MergeMode string `json:"mergeMode"`
	// HeadMoved changes the head commit of every pull request, in the search and in node lookups,
	// as if someone had pushed since the page loaded.
	HeadMoved bool `json:"headMoved"`
	// DeleteFails makes the branch delete answer 500.
	DeleteFails bool `json:"deleteFails"`
}

type githubState struct {
	Config            githubConfig `json:"config"`
	Authorizations    int          `json:"authorizations"`
	Exchanges         int          `json:"exchanges"`
	LastCallbackURL   string       `json:"lastCallbackUrl"`
	LastCodeVerifier  string       `json:"lastCodeVerifier"`
	LastRedirectURI   string       `json:"lastRedirectUri"`
	BadExchangeErrors int          `json:"badExchangeErrors"`
	NodeQueries       int          `json:"nodeQueries"`
	Searches          int          `json:"searches"`
	Merges            int          `json:"merges"`
	BranchDeletes     int          `json:"branchDeletes"`
	// Merged lists the node ids merged so far; they leave the open search.
	Merged []string `json:"merged"`
	// LastMerge is the last accepted or refused merge request body.
	LastMerge map[string]string `json:"lastMerge"`
	// Deleted lists the branches deleted, as "owner/repo:branch".
	Deleted []string `json:"deleted"`
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
	mux.HandleFunc("PUT /repos/{owner}/{repo}/pulls/{number}/merge", g.merge)
	mux.HandleFunc("DELETE /repos/{owner}/{repo}/git/refs/heads/{branch...}", g.deleteBranch)
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

// authed answers 401 for an unknown token and otherwise returns the scopes of the token.
func (g *github) authed(w http.ResponseWriter, r *http.Request) (scopes string, ok bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	scopes, isPAT := githubPATs[token]
	switch {
	case isPAT:
	case g.tokens.has(token):
		scopes = "repo, read:org"
	default:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return "", false
	}
	return scopes, true
}

func (g *github) graphql(w http.ResponseWriter, r *http.Request) {
	scopes, ok := g.authed(w, r)
	if !ok {
		return
	}
	g.mu.Lock()
	cfg, merged := g.st.Config, slices.Clone(g.st.Merged)
	g.mu.Unlock()
	sso := cfg.SSO
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	if q, ok := req.Variables["q"].(string); ok {
		g.mu.Lock()
		g.st.Searches++
		g.mu.Unlock()
		header, body := searchBody(q, sso, cfg.HeadMoved, merged, time.Now())
		for k, v := range header {
			w.Header().Set(k, v)
		}
		writeJSON(w, http.StatusOK, body)
		return
	}
	if id, ok := req.Variables["id"].(string); ok {
		g.mu.Lock()
		g.st.NodeQueries++
		g.mu.Unlock()
		writeJSON(w, http.StatusOK, nodeBody(id, cfg.HeadMoved, merged, time.Now()))
		return
	}
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

func (g *github) merge(w http.ResponseWriter, r *http.Request) {
	if _, ok := g.authed(w, r); !ok {
		return
	}
	var body map[string]string
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Problems parsing JSON"})
		return
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("repo")
	number, _ := strconv.Atoi(r.PathValue("number"))
	p, ok := findFixture(repo, number)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	cfg := g.st.Config
	g.st.Merges++
	g.st.LastMerge = map[string]string{"repo": repo, "number": r.PathValue("number"), "method": body["merge_method"], "sha": body["sha"]}
	switch {
	case cfg.MergeMode == "not_mergeable":
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "Pull Request is not mergeable"})
	case cfg.MergeMode == "conflict" || body["sha"] != p.headSHA(cfg.HeadMoved):
		writeJSON(w, http.StatusConflict, map[string]string{"message": "Head branch was modified. Review and try the merge again."})
	default:
		g.st.Merged = append(g.st.Merged, p.id())
		writeJSON(w, http.StatusOK, map[string]any{"sha": fmt.Sprintf("%040x", p.number*104729), "merged": true, "message": "Pull Request successfully merged"})
	}
}

func (g *github) deleteBranch(w http.ResponseWriter, r *http.Request) {
	if _, ok := g.authed(w, r); !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.st.BranchDeletes++
	if g.st.Config.DeleteFails {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Server Error"})
		return
	}
	g.st.Deleted = append(g.st.Deleted, r.PathValue("owner")+"/"+r.PathValue("repo")+":"+r.PathValue("branch"))
	w.WriteHeader(http.StatusNoContent)
}
