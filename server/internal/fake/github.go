//go:build e2e

package fake

import (
	"cmp"
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
	// RateLimit makes the search answer like a used up limit: "reset" is a 403 with x-ratelimit-reset
	// ten minutes ahead, "retry_after" is a 429 with Retry-After.
	RateLimit string `json:"rateLimit"`
	// MultiPR adds three pull requests of three repositories that all link to INP-105.
	MultiPR bool `json:"multiPr"`
	// Stacked adds a GitHub-native stack of two pull requests in acme/stack (#21 below #22). The
	// synchronous merge of #21 is refused like GitHub does for a stacked pull request, and only the
	// asynchronous merge works.
	Stacked bool `json:"stacked"`
	// AsyncResult is what polling the asynchronous merge ends with after one "pending" answer:
	// "" (merged), "failed" or "enqueued".
	AsyncResult string `json:"asyncResult"`
	// Review adds three pull requests of acme/review for the review actions: #31 has one approval
	// (set the repository to need two), #32 is a draft and #33 was opened by the viewer, so GitHub
	// refuses the viewer's review of it.
	Review bool `json:"review"`
	// ReadOnly adds one pull request (#41) in oss/lib, a repository where the viewer's role is
	// READ: the dashboard blocks Merge and Ready for it up front. Every other repository answers
	// WRITE.
	ReadOnly bool `json:"readOnly"`
	// Approved lists the numbers of pull requests that GitHub reports as approved.
	Approved []int `json:"approved"`
	// reviews and ready are what the review actions changed (see githubState); fixturePRs applies
	// them. Unexported, so the admin config never carries them.
	reviews []fakeReview
	ready   []string
	// ExpiresIn is the lifetime in seconds of issued user tokens (default 28800, like GitHub). The
	// app refreshes tokens that have less than a minute left, so 1 makes every use refresh.
	ExpiresIn int `json:"expiresIn"`
	// RefreshFail makes every refresh answer bad_refresh_token.
	RefreshFail bool `json:"refreshFail"`
}

// defaultGitHubExpiry is the lifetime of a GitHub App user token with expiry turned on.
const defaultGitHubExpiry = 8 * 3600

// fakeReview is a review the viewer submitted through the fake.
type fakeReview struct {
	PR    string `json:"pr"`
	Event string `json:"event"`
	SHA   string `json:"sha"`
	Body  string `json:"body"`
}

type githubState struct {
	Config            githubConfig `json:"config"`
	Authorizations    int          `json:"authorizations"`
	Exchanges         int          `json:"exchanges"`
	LastCallbackURL   string       `json:"lastCallbackUrl"`
	LastCodeVerifier  string       `json:"lastCodeVerifier"`
	Refreshes         int          `json:"refreshes"`
	RejectedRefreshes int          `json:"rejectedRefreshes"`
	CurrentRefresh    string       `json:"currentRefreshToken"`
	LastRedirectURI   string       `json:"lastRedirectUri"`
	BadExchangeErrors int          `json:"badExchangeErrors"`
	NodeQueries       int          `json:"nodeQueries"`
	Searches          int          `json:"searches"`
	Merges            int          `json:"merges"`
	BranchDeletes     int          `json:"branchDeletes"`
	// AsyncSubmits and AsyncPolls count the calls of the asynchronous merge, and AsyncVersion is the
	// X-GitHub-Api-Version of the last one.
	AsyncSubmits int    `json:"asyncSubmits"`
	AsyncPolls   int    `json:"asyncPolls"`
	AsyncVersion string `json:"asyncVersion"`
	// Merged lists the node ids merged so far; they leave the open search.
	Merged []string `json:"merged"`
	// LastMerge is the last accepted or refused merge request body.
	LastMerge map[string]string `json:"lastMerge"`
	// Deleted lists the branches deleted, as "owner/repo:branch".
	Deleted []string `json:"deleted"`
	// Calls lists every search, node lookup, merge, review, ready mark and branch delete with the
	// token that made it, so a test can tell which token was used for what. A search is "search",
	// with an empty repo.
	Calls []fakeCall `json:"calls"`
	// Reviews lists the reviews submitted, in order; the latest of the viewer on a pull request is
	// what its latestReviews shows. Ready lists the node ids marked ready for review.
	Reviews []fakeReview `json:"reviews"`
	Ready   []string     `json:"ready"`
	// ReviewCalls and ReadyCalls count every call, refused or not.
	ReviewCalls int `json:"reviewCalls"`
	ReadyCalls  int `json:"readyCalls"`
}

// cfgLocked is the config with what the review actions changed. The caller holds g.mu.
func (g *github) cfgLocked() githubConfig {
	c := g.st.Config
	c.reviews, c.ready = slices.Clone(g.st.Reviews), slices.Clone(g.st.Ready)
	return c
}

type github struct {
	mu     sync.Mutex
	st     githubState
	codes  safeMap[ghCode]
	tokens safeMap[bool]
	n      int
}

func newGitHub() *github { return &github{} }

var githubPATs = map[string]string{
	"ghp_classic_ok":         "repo, read:org",
	"ghp_classic_no_org":     "repo",
	"github_pat_finegrained": "",
	// contents_read is a fine-grained token with Contents read-only: its merge is refused.
	"github_pat_contents_read": "",
	// pulls_read is a fine-grained token with Pull requests read-only: reviews and ready marks are refused.
	"github_pat_pulls_read": "",
	// github_pat_acme and github_pat_oss are fine-grained tokens of two owners, see githubTokenAccess.
	"github_pat_acme": "",
	"github_pat_oss":  "",
}

// defaultLogin is the account of every fake token but those of githubTokenAccess that name another.
const defaultLogin = "octocat"

// tokenAccess is what one fake token sees. A fine-grained token belongs to one owner, so it sees
// the repositories of its owner and the ones it was granted by name, each with the role of its
// account there. A token that is not in githubTokenAccess sees every repository.
type tokenAccess struct {
	login string
	// roles maps "owner/name", or "owner/*" for every repository of the owner, to the role.
	roles map[string]string
}

// githubTokenAccess gives the fake two tokens of different owners next to the unrestricted ones.
// github_pat_acme (octocat) writes in acme/* and can only read oss/lib. github_pat_oss (hubot) writes
// in oss/lib and sees nothing of acme.
var githubTokenAccess = map[string]tokenAccess{
	"github_pat_acme": {login: "octocat", roles: map[string]string{"acme/*": "WRITE", "oss/lib": "READ"}},
	"github_pat_oss":  {login: "hubot", roles: map[string]string{"oss/lib": "WRITE"}},
}

// permissionOf is the viewer's role of the token on the repository of the pull request, and
// whether the token sees the repository at all. "" is the unrestricted default.
func permissionOf(token string, p fixturePR) (string, bool) {
	acc, restricted := githubTokenAccess[token]
	if !restricted {
		return cmp.Or(p.viewerPermission, "WRITE"), true
	}
	if role, ok := acc.roles[p.repo]; ok {
		return role, true
	}
	owner, _, _ := strings.Cut(p.repo, "/")
	role, ok := acc.roles[owner+"/*"]
	return role, ok
}

// loginOf is the login of the account behind the token.
func loginOf(token string) string {
	if acc, ok := githubTokenAccess[token]; ok {
		return acc.login
	}
	return defaultLogin
}

// bearer is the token of a request.
func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// allowed checks that the token of the request can do a REST write to the repository of the
// fixture and records the call. A repository the token does not see is a 404, like on GitHub. A
// restricted token whose role there is below WRITE is refused with the permission the call needs
// (accepted, such as "contents=write"). The unrestricted tokens are never refused here.
func (g *github) allowed(w http.ResponseWriter, r *http.Request, op string, p fixturePR, accepted string) bool {
	token := bearer(r)
	g.mu.Lock()
	g.st.Calls = append(g.st.Calls, fakeCall{Op: op, Token: token, Repo: p.repo})
	g.mu.Unlock()
	role, sees := permissionOf(token, p)
	if !sees {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return false
	}
	if _, restricted := githubTokenAccess[token]; restricted && (role == "READ" || role == "TRIAGE") {
		w.Header().Set("X-Accepted-GitHub-Permissions", accepted)
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by personal access token"})
		return false
	}
	return true
}

// allowedRepo is allowed for a call that names a repository and not a pull request (the branch
// delete): the first fixture of the repository stands for it.
func (g *github) allowedRepo(w http.ResponseWriter, r *http.Request, op, repo, accepted string) bool {
	for _, p := range fixturePRs(time.Now(), g.config()) {
		if p.repo == repo {
			return g.allowed(w, r, op, p, accepted)
		}
	}
	// A repository without a fixture is not restricted by role; the call is still recorded.
	g.mu.Lock()
	g.st.Calls = append(g.st.Calls, fakeCall{Op: op, Token: bearer(r), Repo: repo})
	g.mu.Unlock()
	return true
}

// fakeCall is a call of a token to a repository, in the state of the fake.
type fakeCall struct {
	Op    string `json:"op"`
	Token string `json:"token"`
	Repo  string `json:"repo"`
}

func (g *github) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login/oauth/authorize", g.authorize)
	mux.HandleFunc("POST /login/oauth/access_token", g.accessToken)
	mux.HandleFunc("POST /graphql", g.graphql)
	mux.HandleFunc("PUT /repos/{owner}/{repo}/pulls/{number}/merge", g.merge)
	mux.HandleFunc("PUT /repos/{owner}/{repo}/pulls/{number}/merge-async", g.mergeAsync)
	mux.HandleFunc("GET /repos/{owner}/{repo}/pulls/{number}/merge-async/{uuid}", g.mergeAsyncPoll)
	mux.HandleFunc("POST /repos/{owner}/{repo}/pulls/{number}/reviews", g.review)
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
		q.Has("scope") || q.Get("state") == "" {
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
	switch r.PostForm.Get("grant_type") {
	case "refresh_token":
		g.mu.Lock()
		reject := g.st.Config.RefreshFail || g.st.CurrentRefresh == "" || r.PostForm.Get("refresh_token") != g.st.CurrentRefresh
		if reject {
			g.st.RejectedRefreshes++
		} else {
			g.st.Refreshes++
		}
		g.mu.Unlock()
		if reject {
			fail("bad_refresh_token")
			return
		}
	case "":
		c, ok := g.codes.take(r.PostForm.Get("code"))
		if !ok || c.redirect != r.PostForm.Get("redirect_uri") {
			fail("bad_verification_code")
			return
		}
		if v := r.PostForm.Get("code_verifier"); v == "" || s256(v) != c.challenge {
			fail("bad_verification_code")
			return
		}
		g.mu.Lock()
		g.st.Exchanges++
		g.st.LastCodeVerifier = r.PostForm.Get("code_verifier")
		g.st.LastRedirectURI = r.PostForm.Get("redirect_uri")
		g.mu.Unlock()
	default:
		fail("unsupported_grant_type")
		return
	}
	g.mu.Lock()
	g.n++
	token, refresh := "ghu_fake_"+strconv.Itoa(g.n)+"_"+randomHex(8), "ghr_fake_"+strconv.Itoa(g.n)+"_"+randomHex(8)
	g.st.CurrentRefresh = refresh
	expires := g.st.Config.ExpiresIn
	if expires <= 0 {
		expires = defaultGitHubExpiry
	}
	g.mu.Unlock()
	g.tokens.put(token, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token, "token_type": "bearer", "expires_in": expires,
		"refresh_token": refresh, "refresh_token_expires_in": 15811200,
	})
}

// authed answers 401 for an unknown token and otherwise returns the scopes of the token.
func (g *github) authed(w http.ResponseWriter, r *http.Request) (scopes string, ok bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	scopes, isPAT := githubPATs[token]
	switch {
	case isPAT:
	case g.tokens.has(token):
		// A GitHub App user token has no X-OAuth-Scopes header.
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
	cfg, merged := g.cfgLocked(), slices.Clone(g.st.Merged)
	g.mu.Unlock()
	sso := cfg.SSO
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	token := bearer(r)
	if q, ok := req.Variables["q"].(string); ok {
		g.mu.Lock()
		g.st.Searches++
		g.st.Calls = append(g.st.Calls, fakeCall{Op: "search", Token: token})
		g.mu.Unlock()
		if limited(w, cfg.RateLimit, time.Now()) {
			return
		}
		header, body := searchBodyFor(token, q, cfg, merged, time.Now())
		for k, v := range header {
			w.Header().Set(k, v)
		}
		writeJSON(w, http.StatusOK, body)
		return
	}
	if id, ok := req.Variables["id"].(string); ok && strings.Contains(req.Query, "markPullRequestReadyForReview") {
		g.markReady(w, r, id, cfg)
		return
	}
	if id, ok := req.Variables["id"].(string); ok {
		g.mu.Lock()
		g.st.NodeQueries++
		g.st.Calls = append(g.st.Calls, fakeCall{Op: "node", Token: token})
		g.mu.Unlock()
		writeJSON(w, http.StatusOK, nodeBodyFor(token, id, cfg, merged, time.Now()))
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
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"viewer": map[string]string{"login": loginOf(token)}}})
}

func (g *github) config() githubConfig {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cfgLocked()
}

// limited answers a search with a rate limit response when the config asks for one.
func limited(w http.ResponseWriter, mode string, now time.Time) bool {
	switch mode {
	case "reset":
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now.Add(10*time.Minute).Unix(), 10))
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "API rate limit exceeded"})
	case "retry_after":
		w.Header().Set("Retry-After", "900")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": "Too many requests"})
	default:
		return false
	}
	return true
}

func (g *github) merge(w http.ResponseWriter, r *http.Request) {
	if _, ok := g.authed(w, r); !ok {
		return
	}
	if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == "github_pat_contents_read" {
		w.Header().Set("X-Accepted-GitHub-Permissions", "contents=write")
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by personal access token"})
		return
	}
	var body map[string]string
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Problems parsing JSON"})
		return
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("repo")
	number, _ := strconv.Atoi(r.PathValue("number"))
	p, ok := findFixture(repo, number, g.config())
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	if !g.allowed(w, r, "merge", p, "contents=write") {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	cfg := g.cfgLocked()
	if p.stacked {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Merging stacked PRs via this endpoint is not supported. Use the asynchronous merge endpoint instead."})
		return
	}
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
	repo := r.PathValue("owner") + "/" + r.PathValue("repo")
	if !g.allowedRepo(w, r, "delete", repo, "contents=write") {
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

const asyncUUID = "7d1b8c1e-52a4-4f0e-9a43-0c2f5a1e9b10"

// mergeAsync accepts the asynchronous merge of a pull request: 202 with the id of the request.
func (g *github) mergeAsync(w http.ResponseWriter, r *http.Request) {
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
	p, ok := findFixture(repo, number, g.config())
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	if !g.allowed(w, r, "merge-async", p, "contents=write") {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.st.AsyncSubmits++
	g.st.AsyncPolls = 0
	g.st.AsyncVersion = r.Header.Get("X-GitHub-Api-Version")
	g.st.LastMerge = map[string]string{"repo": repo, "number": r.PathValue("number"), "method": body["merge_method"], "sha": body["sha"]}
	if body["sha"] != p.headSHA(g.st.Config.HeadMoved) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "Head branch was modified. Review and try the merge again."})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "pending", "details": map[string]any{
		"uuid": asyncUUID, "merge_method": body["merge_method"], "expected_head_sha": body["sha"],
	}})
}

// mergeAsyncPoll answers "pending" once and then the configured end of the request.
func (g *github) mergeAsyncPoll(w http.ResponseWriter, r *http.Request) {
	if _, ok := g.authed(w, r); !ok {
		return
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("repo")
	number, _ := strconv.Atoi(r.PathValue("number"))
	p, ok := findFixture(repo, number, g.config())
	if !ok || r.PathValue("uuid") != asyncUUID {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	if !g.allowed(w, r, "merge-async-poll", p, "contents=write") {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.st.AsyncPolls++
	if g.st.AsyncPolls < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"status": "pending"})
		return
	}
	switch g.st.Config.AsyncResult {
	case "failed":
		writeJSON(w, http.StatusOK, map[string]any{"status": "failed", "message": "Required status check \"build\" is expected."})
	case "enqueued":
		writeJSON(w, http.StatusOK, map[string]any{"status": "enqueued"})
	default:
		if !slices.Contains(g.st.Merged, p.id()) {
			g.st.Merged = append(g.st.Merged, p.id())
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "merged", "details": map[string]any{"merge_commit_sha": fmt.Sprintf("%040x", p.number*104729)}})
	}
}

// review accepts POST .../pulls/{n}/reviews like GitHub: the review is pinned to commit_id, a
// request for changes needs a body, and the viewer cannot review a pull request they opened.
func (g *github) review(w http.ResponseWriter, r *http.Request) {
	if _, ok := g.authed(w, r); !ok {
		return
	}
	g.mu.Lock()
	g.st.ReviewCalls++
	g.mu.Unlock()
	if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == "github_pat_pulls_read" {
		w.Header().Set("X-Accepted-GitHub-Permissions", "pull_requests=write")
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by personal access token"})
		return
	}
	var body map[string]string
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<17)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Problems parsing JSON"})
		return
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("repo")
	number, _ := strconv.Atoi(r.PathValue("number"))
	p, ok := findFixture(repo, number, g.config())
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	if !g.allowed(w, r, "review", p, "pull_requests=write") {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	invalid := func(msg string) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Unprocessable Entity", "errors": []string{msg}})
	}
	event := body["event"]
	switch {
	case event != "APPROVE" && event != "REQUEST_CHANGES":
		invalid("Review event is invalid")
	case p.openedBy(loginOf(bearer(r))):
		if event == "APPROVE" {
			invalid("Can not approve your own pull request")
		} else {
			invalid("Can not request changes on your own pull request")
		}
	case body["commit_id"] != p.headSHA(g.st.Config.HeadMoved):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed", "errors": []map[string]string{
			{"resource": "PullRequestReview", "code": "custom", "field": "commit_id", "message": "commit_id is not part of the pull request"}}})
	case event == "REQUEST_CHANGES" && strings.TrimSpace(body["body"]) == "":
		invalid("Review body can't be blank")
	default:
		g.st.Reviews = append(g.st.Reviews, fakeReview{PR: p.id(), Event: event, SHA: body["commit_id"], Body: body["body"]})
		writeJSON(w, http.StatusOK, map[string]any{"id": len(g.st.Reviews), "state": map[string]string{"APPROVE": "APPROVED", "REQUEST_CHANGES": "CHANGES_REQUESTED"}[event]})
	}
}

// markReady answers the markPullRequestReadyForReview mutation.
func (g *github) markReady(w http.ResponseWriter, r *http.Request, id string, cfg githubConfig) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.st.ReadyCalls++
	fail := func(kind, msg string) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data":   map[string]any{"markPullRequestReadyForReview": nil},
			"errors": []map[string]any{{"type": kind, "message": msg, "path": []string{"markPullRequestReadyForReview"}}},
		})
	}
	token := bearer(r)
	if token == "github_pat_pulls_read" {
		fail("FORBIDDEN", "Resource not accessible by personal access token")
		return
	}
	for _, p := range fixturePRs(time.Now(), cfg) {
		if p.id() != id {
			continue
		}
		role, sees := permissionOf(token, p)
		g.st.Calls = append(g.st.Calls, fakeCall{Op: "ready", Token: token, Repo: p.repo})
		if !sees {
			break // not found, like any node the token cannot read
		}
		if _, restricted := githubTokenAccess[token]; restricted && (role == "READ" || role == "TRIAGE") {
			fail("FORBIDDEN", "Resource not accessible by personal access token")
			return
		}
		if !p.draft {
			fail("UNPROCESSABLE", "Pull request is not a draft")
			return
		}
		g.st.Ready = append(g.st.Ready, id)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"markPullRequestReadyForReview": map[string]any{
			"pullRequest": map[string]any{"id": id, "isDraft": false}}}})
		return
	}
	fail("NOT_FOUND", "Could not resolve to a node with the global id of '"+id+"'.")
}
