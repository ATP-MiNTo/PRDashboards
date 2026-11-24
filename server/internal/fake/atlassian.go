//go:build e2e

package fake

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type atlassianConfig struct {
	authorizeConfig
	// ExpiresIn is the lifetime in seconds of issued access tokens. The app refreshes tokens that
	// have less than a minute left, so 1 makes every use refresh.
	ExpiresIn int `json:"expiresIn"`
	// RefreshFail makes every refresh answer invalid_grant.
	RefreshFail bool `json:"refreshFail"`
	// Sites is how many Jira sites accessible-resources lists (1 or 2).
	Sites int `json:"sites"`
	// TransitionMode steers POST transition: "" applies it, "reject" answers 400 and "down" answers 503.
	TransitionMode string `json:"transitionMode"`
}

func defaultAtlassianConfig() atlassianConfig { return atlassianConfig{ExpiresIn: 3600, Sites: 1} }

type atlassianState struct {
	Config          atlassianConfig `json:"config"`
	Authorizations  int             `json:"authorizations"`
	Exchanges       int             `json:"exchanges"`
	Refreshes       int             `json:"refreshes"`
	RejectedRefresh int             `json:"rejectedRefreshes"`
	LastCallbackURL string          `json:"lastCallbackUrl"`
	CurrentRefresh  string          `json:"currentRefreshToken"`
	// Statuses holds the status of tickets moved so far, by key.
	Statuses map[string]string `json:"statuses"`
	// Moves is every accepted or refused transition request, as "KEY:transitionId:fields-json".
	Moves []string `json:"moves"`
}

type atlassian struct {
	mu     sync.Mutex
	st     atlassianState
	n      int
	calls  jiraCalls
	codes  safeMap[string]
	access safeMap[bool]
}

func freshAtlassianState() atlassianState {
	return atlassianState{Config: defaultAtlassianConfig(), Statuses: map[string]string{}, Moves: []string{}}
}

func newAtlassian() *atlassian { return &atlassian{st: freshAtlassianState()} }

func (a *atlassian) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authorize", a.authorize)
	mux.HandleFunc("POST /oauth/token", a.token)
	mux.HandleFunc("GET /oauth/token/accessible-resources", a.resources)
	a.jiraRoutes(mux, &a.calls)
	mux.HandleFunc("GET /__admin/state", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		writeJSON(w, http.StatusOK, struct {
			atlassianState
			Jira map[string]int64 `json:"jira"`
		}{a.st, a.calls.snapshot()})
	})
	mux.HandleFunc("POST /__admin/config", func(w http.ResponseWriter, r *http.Request) {
		c := defaultAtlassianConfig()
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&c); err != nil || c.Sites < 1 || c.Sites > 2 || c.ExpiresIn < 1 {
			http.Error(w, "bad config", http.StatusBadRequest)
			return
		}
		a.mu.Lock()
		a.st.Config = c
		a.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /__admin/reset", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.Lock()
		a.st = freshAtlassianState()
		a.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func (a *atlassian) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != AtlassianClient.ClientID || q.Get("audience") != "api.atlassian.com" || q.Get("response_type") != "code" ||
		q.Get("prompt") != "consent" || !strings.Contains(q.Get("scope"), "offline_access") || q.Get("state") == "" {
		http.Error(w, "invalid authorize request", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	cfg := a.st.Config.authorizeConfig
	a.st.Authorizations++
	a.mu.Unlock()
	authorizeRedirect(w, r, cfg, func() string {
		code := randomHex(10)
		a.codes.put(code, q.Get("redirect_uri"))
		return code
	}, func(target string) {
		a.mu.Lock()
		a.st.LastCallbackURL = target
		a.mu.Unlock()
	})
}

func (a *atlassian) token(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if !equal(body["client_id"], AtlassianClient.ClientID) || !equal(body["client_secret"], AtlassianClient.ClientSecret) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "access_denied"})
		return
	}
	switch body["grant_type"] {
	case "authorization_code":
		redirect, ok := a.codes.take(body["code"])
		if !ok || redirect != body["redirect_uri"] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		a.mu.Lock()
		a.st.Exchanges++
		a.mu.Unlock()
		a.issue(w)
	case "refresh_token":
		a.mu.Lock()
		reject := a.st.Config.RefreshFail || body["refresh_token"] != a.st.CurrentRefresh || a.st.CurrentRefresh == ""
		if reject {
			a.st.RejectedRefresh++
		} else {
			a.st.Refreshes++
		}
		a.mu.Unlock()
		if reject {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid_grant", "error_description": "Unknown or invalid refresh token."})
			return
		}
		a.issue(w)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}
}

// issue mints a new access token and a new refresh token, which replaces the previous one.
func (a *atlassian) issue(w http.ResponseWriter) {
	a.mu.Lock()
	a.n++
	access, refresh := "atl-access-"+strconv.Itoa(a.n)+"-"+randomHex(4), "atl-refresh-"+strconv.Itoa(a.n)+"-"+randomHex(4)
	a.st.CurrentRefresh = refresh
	expires := a.st.Config.ExpiresIn
	a.mu.Unlock()
	a.access.put(access, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "refresh_token": refresh, "expires_in": expires, "scope": "read:jira-work write:jira-work read:jira-user offline_access",
	})
}

func (a *atlassian) resources(w http.ResponseWriter, r *http.Request) {
	if !a.access.has(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Unauthorized"})
		return
	}
	a.mu.Lock()
	n := a.st.Config.Sites
	a.mu.Unlock()
	scopes := []string{"read:jira-work", "write:jira-work", "read:jira-user"}
	sites := []map[string]any{
		{"id": "cloud-1", "name": "Acme", "url": "https://acme.atlassian.net", "scopes": scopes},
		{"id": "cloud-2", "name": "Beta Corp", "url": "https://beta.atlassian.net", "scopes": scopes},
	}
	writeJSON(w, http.StatusOK, sites[:n])
}
