package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

type recorder struct {
	auth.StateStore
	mu   sync.Mutex
	rows []store.OAuthState
}

func (r *recorder) InsertOAuthState(ctx context.Context, v store.OAuthState, now time.Time) error {
	r.mu.Lock()
	r.rows = append(r.rows, v)
	r.mu.Unlock()
	return r.StateStore.InsertOAuthState(ctx, v, now)
}

type provider struct {
	srv *httptest.Server

	githubTokenForm   atomic.Value // url.Values
	atlassianBodies   chan map[string]string
	refreshCalls      atomic.Int32
	refreshStatus     atomic.Int32
	refreshError      atomic.Value // string
	refreshDelay      time.Duration
	exchangeStatus    atomic.Int32
	githubScopes      atomic.Value // string; empty means no header
	githubTokenAnswer atomic.Value // string
	ghRefreshCalls    atomic.Int32
	ghRefreshStatus   atomic.Int32
	ghRefreshAnswer   atomic.Value // string
	ghRefreshForms    chan url.Values
	sites             atomic.Value // string JSON
	viewerCalls       atomic.Int32
	longLogin         string
}

func newProvider(t *testing.T) *provider {
	p := &provider{atlassianBodies: make(chan map[string]string, 64), ghRefreshForms: make(chan url.Values, 64)}
	p.githubScopes.Store("repo, read:org")
	p.githubTokenAnswer.Store(`{"access_token":"ghu_1","token_type":"bearer","refresh_token":"ghr_1","expires_in":28800,"refresh_token_expires_in":15811200}`)
	p.ghRefreshAnswer.Store(`{"access_token":"ghu_2","token_type":"bearer","refresh_token":"ghr_2","expires_in":28800}`)
	p.refreshError.Store("")
	p.sites.Store(`[
		{"id":"cloud-1","name":"Acme","url":"https://acme.atlassian.net","scopes":["read:jira-work","write:jira-work"]},
		{"id":"cloud-2","name":"Wiki","url":"https://acme.atlassian.net/wiki","scopes":["read:confluence-content.all"]},
		{"id":"cloud-3","name":"Evil","url":"https://evil.example","scopes":["read:jira-work"]}
	]`)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /gh/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") == "refresh_token" {
			p.ghRefreshCalls.Add(1)
			p.ghRefreshForms <- r.PostForm
			time.Sleep(p.refreshDelay)
			if st := int(p.ghRefreshStatus.Load()); st != 0 {
				w.WriteHeader(st)
				return
			}
			_, _ = io.WriteString(w, p.ghRefreshAnswer.Load().(string))
			return
		}
		p.githubTokenForm.Store(r.PostForm)
		if st := int(p.exchangeStatus.Load()); st != 0 {
			w.WriteHeader(st)
			return
		}
		_, _ = io.WriteString(w, p.githubTokenAnswer.Load().(string))
	})
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		p.viewerCalls.Add(1)
		login := "octocat"
		if p.longLogin != "" {
			login = p.longLogin
		}
		switch tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); {
		case tok == "ghu_1", tok == "ghu_2", tok == "gho_legacy", tok == "ghp_pat", tok == "github_pat_x", strings.HasPrefix(tok, "ghp_extra"):
		case tok == "ghp_hubot":
			login = "hubot"
		default:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if s := p.githubScopes.Load().(string); s != "" {
			w.Header().Set("X-OAuth-Scopes", s)
		}
		_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"`+login+`"}}}`)
	})
	mux.HandleFunc("POST /atl/token", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.atlassianBodies <- body
		if body["grant_type"] == "refresh_token" {
			p.refreshCalls.Add(1)
			time.Sleep(p.refreshDelay)
			if st := int(p.refreshStatus.Load()); st != 0 {
				w.WriteHeader(st)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": p.refreshError.Load().(string)})
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"atl-access-2","refresh_token":"atl-refresh-2","expires_in":3600}`)
			return
		}
		if st := int(p.exchangeStatus.Load()); st != 0 {
			w.WriteHeader(st)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"atl-access-1","refresh_token":"atl-refresh-1","expires_in":3600,"scope":"x"}`)
	})
	mux.HandleFunc("GET /atl/resources", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer atl-access") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, p.sites.Load().(string))
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

type oauthRig struct {
	o     *auth.OAuth
	sec   *secrets.Memory
	st    *store.Store
	rec   *recorder
	prov  *provider
	clock *testutil.Clock
}

func newOAuthRig(t *testing.T) *oauthRig {
	t.Helper()
	clock := testutil.NewClock()
	st, err := store.Open(filepath.Join(t.TempDir(), "data.db"), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	prov := newProvider(t)
	sec := secrets.NewMemory()
	rec := &recorder{StateStore: st}
	o := auth.NewOAuth(auth.Config{
		Secrets: sec,
		States:  rec,
		HTTP:    prov.srv.Client(),
		GitHub:  &github.Client{HTTP: prov.srv.Client(), APIBase: prov.srv.URL},
		Endpoints: auth.Endpoints{
			GitHubAuthorize:    "https://github.example/authorize",
			GitHubToken:        prov.srv.URL + "/gh/token",
			AtlassianAuthorize: "https://atlassian.example/authorize",
			AtlassianToken:     prov.srv.URL + "/atl/token",
			AtlassianResources: prov.srv.URL + "/atl/resources",
		},
		Clock: clock.Now,
		Rand:  &testutil.Rand{},
		Port:  4317,
	})
	r := &oauthRig{o: o, sec: sec, st: st, rec: rec, prov: prov, clock: clock}
	for _, p := range []auth.Provider{auth.GitHub, auth.Atlassian} {
		if err := o.SetClient(p, auth.ClientCredentials{ClientID: "cid-" + string(p), ClientSecret: "csecret-" + string(p)}); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *oauthRig) start(t *testing.T, p auth.Provider, session string) (*url.URL, string) {
	t.Helper()
	raw, err := r.o.Start(ctx, p, session)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u, u.Query().Get("state")
}

func tokenJSON(t *testing.T, sec secrets.Store, name string, v any) {
	t.Helper()
	raw, err := sec.Get(name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

// firstGitHub is the primary entry of the stored list of GitHub tokens.
func firstGitHub(t *testing.T, sec secrets.Store) auth.GitHubToken {
	t.Helper()
	var list []auth.GitHubToken
	tokenJSON(t, sec, secrets.GitHubTokens, &list)
	if len(list) == 0 {
		t.Fatal("empty list of GitHub tokens")
	}
	return list[0]
}

// setGitHub stores one GitHub token, given in the JSON of the legacy item, as the only entry of
// the list.
func setGitHub(t *testing.T, sec secrets.Store, legacy string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(legacy), &m); err != nil {
		t.Fatal(err)
	}
	m["id"], m["name"] = "a1b2c3d4", "Token"
	raw, _ := json.Marshal([]any{m})
	if err := sec.Set(secrets.GitHubTokens, raw); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackURL(t *testing.T) {
	r := newOAuthRig(t)
	if got := r.o.CallbackURL(auth.GitHub); got != "http://127.0.0.1:4317/oauth/github/callback" {
		t.Fatal(got)
	}
	if got := r.o.CallbackURL(auth.Atlassian); got != "http://127.0.0.1:4317/oauth/atlassian/callback" {
		t.Fatal(got)
	}
}

func TestParseProvider(t *testing.T) {
	for in, want := range map[string]bool{"github": true, "atlassian": true, "gitlab": false, "": false, "GitHub": false} {
		if _, ok := auth.ParseProvider(in); ok != want {
			t.Errorf("%q: %v", in, ok)
		}
	}
}

func TestStartGitHubBuildsPKCEAuthorizeURL(t *testing.T) {
	r := newOAuthRig(t)
	u, state := r.start(t, auth.GitHub, "sess-1")
	if u.Scheme+"://"+u.Host+u.Path != "https://github.example/authorize" {
		t.Fatalf("url %s", u)
	}
	q := u.Query()
	if q.Get("client_id") != "cid-github" || q.Get("redirect_uri") != "http://127.0.0.1:4317/oauth/github/callback" ||
		q.Has("scope") || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("query %v", q)
	}
	if len(state) != 43 {
		t.Fatalf("state %q is not 256 bits", state)
	}
	if strings.Contains(u.RawQuery, "+") {
		t.Fatalf("spaces must be %%20: %s", u.RawQuery)
	}
	row := r.rec.rows[0]
	if row.Hash != sha256.Sum256([]byte(state)) || row.Provider != "github" || row.SessionKey != "sess-1" ||
		!row.ExpiresAt.Equal(r.clock.Now().Add(10*time.Minute)) {
		t.Fatalf("row %+v", row)
	}
	sum := sha256.Sum256([]byte(row.Verifier))
	if len(row.Verifier) != 43 || q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatalf("challenge %q does not match the stored verifier", q.Get("code_challenge"))
	}
}

func TestStartAtlassianURL(t *testing.T) {
	r := newOAuthRig(t)
	u, _ := r.start(t, auth.Atlassian, "sess-1")
	q := u.Query()
	want := map[string]string{
		"audience": "api.atlassian.com", "client_id": "cid-atlassian", "response_type": "code", "prompt": "consent",
		"scope":        "read:jira-work write:jira-work read:jira-user offline_access",
		"redirect_uri": "http://127.0.0.1:4317/oauth/atlassian/callback",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Get("code_challenge") != "" {
		t.Error("atlassian flow must not send a PKCE challenge")
	}
	if r.rec.rows[0].Verifier != "" {
		t.Error("atlassian state has a verifier")
	}
}

func TestStartNeedsClientCredentials(t *testing.T) {
	r := newOAuthRig(t)
	_ = r.sec.Delete(secrets.GitHubClient)
	if _, err := r.o.Start(ctx, auth.GitHub, "s"); !errors.Is(err, auth.ErrNotConfigured) {
		t.Fatalf("got %v", err)
	}
	if r.o.ClientConfigured(auth.GitHub) || !r.o.ClientConfigured(auth.Atlassian) {
		t.Fatal("ClientConfigured wrong")
	}
	_ = r.o.SetClient(auth.GitHub, auth.ClientCredentials{ClientID: "x"})
	if r.o.ClientConfigured(auth.GitHub) {
		t.Fatal("half-filled credentials count as configured")
	}
}

func TestStatesAreDistinct(t *testing.T) {
	r := newOAuthRig(t)
	_, a := r.start(t, auth.GitHub, "s")
	_, b := r.start(t, auth.GitHub, "s")
	if a == b {
		t.Fatal("state reused")
	}
}

func TestCompleteGitHubSendsVerifierAndStoresToken(t *testing.T) {
	r := newOAuthRig(t)
	_, state := r.start(t, auth.GitHub, "sess-1")
	if err := r.o.Complete(ctx, auth.GitHub, "the-code", state, "sess-1"); err != nil {
		t.Fatal(err)
	}
	form := r.prov.githubTokenForm.Load().(url.Values)
	if form.Get("code") != "the-code" || form.Get("client_id") != "cid-github" || form.Get("client_secret") != "csecret-github" ||
		form.Get("redirect_uri") != "http://127.0.0.1:4317/oauth/github/callback" || form.Get("code_verifier") != r.rec.rows[0].Verifier {
		t.Fatalf("form %v", form)
	}
	tok := firstGitHub(t, r.sec)
	if tok.Token != "ghu_1" || tok.Method != "app" || tok.Login != "octocat" || tok.RefreshToken != "ghr_1" ||
		!tok.ExpiresAt.Equal(r.clock.Now().Add(8*time.Hour)) || tok.NeedsReconnect {
		t.Fatalf("%+v", tok)
	}
	acc, ok, err := r.o.GitHubAccount()
	if err != nil || !ok || acc.Login != "octocat" {
		t.Fatalf("%+v %v %v", acc, ok, err)
	}
}

func TestStateIsSingleUse(t *testing.T) {
	r := newOAuthRig(t)
	_, state := r.start(t, auth.GitHub, "sess-1")
	if err := r.o.Complete(ctx, auth.GitHub, "c", state, "sess-1"); err != nil {
		t.Fatal(err)
	}
	if err := r.o.Complete(ctx, auth.GitHub, "c", state, "sess-1"); !errors.Is(err, auth.ErrInvalidState) {
		t.Fatalf("reused state: %v", err)
	}
}

func TestStateRejections(t *testing.T) {
	cases := []struct {
		name    string
		advance time.Duration
		provide func(state string) (auth.Provider, string, string)
	}{
		{"unknown", 0, func(string) (auth.Provider, string, string) { return auth.GitHub, "nope", "s" }},
		{"empty", 0, func(string) (auth.Provider, string, string) { return auth.GitHub, "", "s" }},
		{"wrong provider", 0, func(st string) (auth.Provider, string, string) { return auth.Atlassian, st, "s" }},
		{"wrong session", 0, func(st string) (auth.Provider, string, string) { return auth.GitHub, st, "other" }},
		{"expired", auth.StateTTL, func(st string) (auth.Provider, string, string) { return auth.GitHub, st, "s" }},
	}
	for _, tc := range cases {
		r := newOAuthRig(t)
		_, state := r.start(t, auth.GitHub, "s")
		r.clock.Advance(tc.advance)
		p, st, sk := tc.provide(state)
		if err := r.o.Complete(ctx, p, "code", st, sk); !errors.Is(err, auth.ErrInvalidState) {
			t.Errorf("%s: got %v", tc.name, err)
		}
		if r.prov.githubTokenForm.Load() != nil || len(r.prov.atlassianBodies) != 0 {
			t.Errorf("%s: the provider was called", tc.name)
		}
		if _, err := r.sec.Get(secrets.GitHubTokens); err == nil {
			t.Errorf("%s: token stored", tc.name)
		}
	}
}

func TestRejectedAttemptSpendsTheState(t *testing.T) {
	r := newOAuthRig(t)
	_, state := r.start(t, auth.GitHub, "s")
	if err := r.o.Complete(ctx, auth.GitHub, "c", state, "other-session"); !errors.Is(err, auth.ErrInvalidState) {
		t.Fatal(err)
	}
	if err := r.o.Complete(ctx, auth.GitHub, "c", state, "s"); !errors.Is(err, auth.ErrInvalidState) {
		t.Fatalf("the right session could use a state a wrong one touched: %v", err)
	}
}

func TestCompleteExchangeFailures(t *testing.T) {
	r := newOAuthRig(t)
	complete := func(p auth.Provider, code string) error {
		_, state := r.start(t, p, "s")
		return r.o.Complete(ctx, p, code, state, "s")
	}
	if err := complete(auth.GitHub, ""); !errors.Is(err, auth.ErrExchange) {
		t.Fatalf("empty code: %v", err)
	}
	r.prov.githubTokenAnswer.Store(`{"error":"bad_verification_code","error_description":"expired"}`)
	if err := complete(auth.GitHub, "c"); !errors.Is(err, auth.ErrExchange) {
		t.Fatalf("error body: %v", err)
	}
	r.prov.exchangeStatus.Store(500)
	if err := complete(auth.GitHub, "c"); !errors.Is(err, auth.ErrExchange) {
		t.Fatalf("500: %v", err)
	}
	if err := complete(auth.Atlassian, "c"); !errors.Is(err, auth.ErrExchange) {
		t.Fatalf("atlassian 500: %v", err)
	}
	r.prov.exchangeStatus.Store(0)
	r.prov.githubTokenAnswer.Store(`{"access_token":"gho_unknown"}`)
	if err := complete(auth.GitHub, "c"); !errors.Is(err, auth.ErrExchange) {
		t.Fatalf("token GitHub rejects: %v", err)
	}
	for _, name := range []string{secrets.GitHubTokens, secrets.AtlassianToken} {
		if _, err := r.sec.Get(name); err == nil {
			t.Errorf("%s stored after a failed exchange", name)
		}
	}
	r.prov.srv.Close()
	if err := complete(auth.GitHub, "c"); !errors.Is(err, auth.ErrExchange) {
		t.Fatalf("network down: %v", err)
	}
	_ = r.sec.Delete(secrets.AtlassianClient)
	_, state := r.start(t, auth.GitHub, "s")
	_ = r.sec.Delete(secrets.GitHubClient)
	if err := r.o.Complete(ctx, auth.GitHub, "c", state, "s"); !errors.Is(err, auth.ErrNotConfigured) {
		t.Fatalf("credentials removed mid-flow: %v", err)
	}
}

func TestCompleteAtlassianStoresTokensAndValidSites(t *testing.T) {
	r := newOAuthRig(t)
	_, state := r.start(t, auth.Atlassian, "s")
	if err := r.o.Complete(ctx, auth.Atlassian, "the-code", state, "s"); err != nil {
		t.Fatal(err)
	}
	body := <-r.prov.atlassianBodies
	if body["grant_type"] != "authorization_code" || body["code"] != "the-code" || body["client_id"] != "cid-atlassian" ||
		body["client_secret"] != "csecret-atlassian" || body["redirect_uri"] != "http://127.0.0.1:4317/oauth/atlassian/callback" {
		t.Fatalf("body %v", body)
	}
	var tok auth.AtlassianToken
	tokenJSON(t, r.sec, secrets.AtlassianToken, &tok)
	if tok.AccessToken != "atl-access-1" || tok.RefreshToken != "atl-refresh-1" || !tok.ExpiresAt.Equal(r.clock.Now().Add(time.Hour)) {
		t.Fatalf("%+v", tok)
	}
	if len(tok.Sites) != 1 || tok.Sites[0] != (auth.Site{ID: "cloud-1", Name: "Acme", URL: "https://acme.atlassian.net"}) {
		t.Fatalf("only the https Jira site belongs in the list: %+v", tok.Sites)
	}
}

func TestAtlassianExchangeWithoutRefreshTokenFails(t *testing.T) {
	r := newOAuthRig(t)
	r.prov.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"a","expires_in":3600}`)
	})
	_, state := r.start(t, auth.Atlassian, "s")
	if err := r.o.Complete(ctx, auth.Atlassian, "c", state, "s"); !errors.Is(err, auth.ErrExchange) {
		t.Fatalf("got %v", err)
	}
}

func connectAtlassian(t *testing.T, r *oauthRig) {
	t.Helper()
	_, state := r.start(t, auth.Atlassian, "s")
	if err := r.o.Complete(ctx, auth.Atlassian, "c", state, "s"); err != nil {
		t.Fatal(err)
	}
	<-r.prov.atlassianBodies
}

func TestAccessReturnsTokenWithoutRefreshWhileValid(t *testing.T) {
	r := newOAuthRig(t)
	connectAtlassian(t, r)
	r.clock.Advance(58 * time.Minute)
	if got, err := r.o.AtlassianAccess(ctx); err != nil || got != "atl-access-1" {
		t.Fatalf("%q %v", got, err)
	}
	if r.prov.refreshCalls.Load() != 0 {
		t.Fatal("refreshed while the token was valid")
	}
	r.clock.Advance(90 * time.Second) // inside the one minute skew window
	if got, err := r.o.AtlassianAccess(ctx); err != nil || got != "atl-access-2" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestRefreshRotatesWithExactlyOneCallForConcurrentCallers(t *testing.T) {
	r := newOAuthRig(t)
	r.prov.refreshDelay = 50 * time.Millisecond
	connectAtlassian(t, r)
	r.clock.Advance(2 * time.Hour)
	var wg sync.WaitGroup
	results := make(chan string, 25)
	errs := make(chan error, 25)
	for range 25 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := r.o.AtlassianAccess(ctx)
			results <- tok
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for tok := range results {
		if tok != "atl-access-2" {
			t.Fatalf("caller got %q", tok)
		}
	}
	if got := r.prov.refreshCalls.Load(); got != 1 {
		t.Fatalf("%d refresh calls, want exactly 1", got)
	}
	body := <-r.prov.atlassianBodies
	if body["grant_type"] != "refresh_token" || body["refresh_token"] != "atl-refresh-1" {
		t.Fatalf("refresh sent %v", body)
	}
	var tok auth.AtlassianToken
	tokenJSON(t, r.sec, secrets.AtlassianToken, &tok)
	if tok.RefreshToken != "atl-refresh-2" || tok.AccessToken != "atl-access-2" || len(tok.Sites) != 1 || tok.NeedsReconnect {
		t.Fatalf("rotated token not stored: %+v", tok)
	}
}

func TestRefreshKeepsOldRefreshTokenWhenNoneIsReturned(t *testing.T) {
	r := newOAuthRig(t)
	connectAtlassian(t, r)
	r.prov.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"atl-access-9","expires_in":60}`)
	})
	r.clock.Advance(2 * time.Hour)
	if _, err := r.o.AtlassianAccess(ctx); err != nil {
		t.Fatal(err)
	}
	var tok auth.AtlassianToken
	tokenJSON(t, r.sec, secrets.AtlassianToken, &tok)
	if tok.RefreshToken != "atl-refresh-1" {
		t.Fatalf("refresh token lost: %+v", tok)
	}
}

func TestRefreshRejectionMarksReconnect(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{400, "invalid_grant"}, {401, ""}, {403, "invalid_grant"}} {
		r := newOAuthRig(t)
		connectAtlassian(t, r)
		r.prov.refreshStatus.Store(int32(tc.status))
		r.prov.refreshError.Store(tc.code)
		r.clock.Advance(2 * time.Hour)
		if _, err := r.o.AtlassianAccess(ctx); !errors.Is(err, auth.ErrNeedsReconnect) {
			t.Fatalf("%d: got %v", tc.status, err)
		}
		var tok auth.AtlassianToken
		tokenJSON(t, r.sec, secrets.AtlassianToken, &tok)
		if !tok.NeedsReconnect || len(tok.Sites) != 1 {
			t.Fatalf("%d: %+v", tc.status, tok)
		}
		before := r.prov.refreshCalls.Load()
		if _, err := r.o.AtlassianAccess(ctx); !errors.Is(err, auth.ErrNeedsReconnect) || r.prov.refreshCalls.Load() != before {
			t.Fatalf("%d: a flagged token must not call Atlassian again", tc.status)
		}
	}
}

func TestTransientRefreshFailuresDoNotMarkReconnect(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{500, ""}, {503, ""}, {403, "forbidden"}} {
		r := newOAuthRig(t)
		connectAtlassian(t, r)
		r.prov.refreshStatus.Store(int32(tc.status))
		r.prov.refreshError.Store(tc.code)
		r.clock.Advance(2 * time.Hour)
		_, err := r.o.AtlassianAccess(ctx)
		if err == nil || errors.Is(err, auth.ErrNeedsReconnect) {
			t.Fatalf("%d: got %v", tc.status, err)
		}
		var tok auth.AtlassianToken
		tokenJSON(t, r.sec, secrets.AtlassianToken, &tok)
		if tok.NeedsReconnect || tok.RefreshToken != "atl-refresh-1" {
			t.Fatalf("%d: %+v", tc.status, tok)
		}
	}
}

func TestReconnectClearsTheFlag(t *testing.T) {
	r := newOAuthRig(t)
	connectAtlassian(t, r)
	r.prov.refreshStatus.Store(400)
	r.clock.Advance(2 * time.Hour)
	_, _ = r.o.AtlassianAccess(ctx)
	connectAtlassian(t, r)
	tok, ok, err := r.o.AtlassianAccount()
	if err != nil || !ok || tok.NeedsReconnect {
		t.Fatalf("%+v %v %v", tok, ok, err)
	}
}

func TestAccessWithoutConnection(t *testing.T) {
	r := newOAuthRig(t)
	if _, err := r.o.AtlassianAccess(ctx); !errors.Is(err, auth.ErrNotConnected) {
		t.Fatalf("got %v", err)
	}
	if _, ok, err := r.o.AtlassianAccount(); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if _, ok, err := r.o.GitHubAccount(); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
}

func TestRefreshNeedsClientCredentials(t *testing.T) {
	r := newOAuthRig(t)
	connectAtlassian(t, r)
	_ = r.sec.Delete(secrets.AtlassianClient)
	r.clock.Advance(2 * time.Hour)
	if _, err := r.o.AtlassianAccess(ctx); !errors.Is(err, auth.ErrNotConfigured) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckAtlassianRefreshesAndReloadsSites(t *testing.T) {
	r := newOAuthRig(t)
	connectAtlassian(t, r)
	r.prov.sites.Store(`[{"id":"a","name":"A","url":"https://a.atlassian.net"},{"id":"b","name":"B","url":"https://b.atlassian.net"}]`)
	r.clock.Advance(2 * time.Hour)
	tok, err := r.o.CheckAtlassian(ctx)
	if err != nil || len(tok.Sites) != 2 || tok.AccessToken != "atl-access-2" {
		t.Fatalf("%+v %v", tok, err)
	}
	stored, _, _ := r.o.AtlassianAccount()
	if len(stored.Sites) != 2 || stored.RefreshToken != "atl-refresh-2" {
		t.Fatalf("%+v", stored)
	}
	r.prov.refreshStatus.Store(401)
	r.clock.Advance(2 * time.Hour)
	tok, err = r.o.CheckAtlassian(ctx)
	if !errors.Is(err, auth.ErrNeedsReconnect) || !tok.NeedsReconnect {
		t.Fatalf("%+v %v", tok, err)
	}
	r2 := newOAuthRig(t)
	if _, err := r2.o.CheckAtlassian(ctx); !errors.Is(err, auth.ErrNotConnected) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckAtlassianSurfacesResourceErrors(t *testing.T) {
	r := newOAuthRig(t)
	connectAtlassian(t, r)
	r.prov.sites.Store(`not json`)
	if _, err := r.o.CheckAtlassian(ctx); err == nil {
		t.Fatal("bad resources accepted")
	}
	tok, _, _ := r.o.AtlassianAccount()
	if len(tok.Sites) != 1 {
		t.Fatal("sites lost after a failed reload")
	}
}

func connectGitHub(t *testing.T, r *oauthRig) {
	t.Helper()
	_, state := r.start(t, auth.GitHub, "s")
	if err := r.o.Complete(ctx, auth.GitHub, "c", state, "s"); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteGitHubWithoutExpiryStoresANonExpiringToken(t *testing.T) {
	r := newOAuthRig(t)
	r.prov.githubTokenAnswer.Store(`{"access_token":"ghu_1","token_type":"bearer"}`)
	connectGitHub(t, r)
	tok := firstGitHub(t, r.sec)
	if !tok.ExpiresAt.IsZero() || tok.RefreshToken != "" {
		t.Fatalf("%+v", tok)
	}
	r.clock.Advance(1000 * time.Hour)
	if got, err := r.o.GitHubAccess(ctx, ""); err != nil || got.Token != "ghu_1" || r.prov.ghRefreshCalls.Load() != 0 {
		t.Fatalf("%q %v", got, err)
	}
}

func TestGitHubAccessDoesNotRefreshWhileValid(t *testing.T) {
	r := newOAuthRig(t)
	connectGitHub(t, r)
	r.clock.Advance(8*time.Hour - 2*time.Minute)
	if got, err := r.o.GitHubAccess(ctx, ""); err != nil || got.Token != "ghu_1" {
		t.Fatalf("%q %v", got, err)
	}
	if r.prov.ghRefreshCalls.Load() != 0 {
		t.Fatal("refreshed while the token was valid")
	}
	r.clock.Advance(90 * time.Second) // inside the skew window
	if got, err := r.o.GitHubAccess(ctx, ""); err != nil || got.Token != "ghu_2" {
		t.Fatalf("%q %v", got, err)
	}
	form := <-r.prov.ghRefreshForms
	if form.Get("client_id") != "cid-github" || form.Get("client_secret") != "csecret-github" ||
		form.Get("refresh_token") != "ghr_1" || form.Get("grant_type") != "refresh_token" {
		t.Fatalf("form %v", form)
	}
	tok := firstGitHub(t, r.sec)
	if tok.Token != "ghu_2" || tok.RefreshToken != "ghr_2" || tok.Login != "octocat" || tok.Method != "app" ||
		!tok.ExpiresAt.Equal(r.clock.Now().Add(8*time.Hour)) {
		t.Fatalf("rotated token not stored: %+v", tok)
	}
}

func TestGitHubRefreshOnceForConcurrentCallers(t *testing.T) {
	r := newOAuthRig(t)
	r.prov.refreshDelay = 50 * time.Millisecond
	connectGitHub(t, r)
	r.clock.Advance(9 * time.Hour)
	var wg sync.WaitGroup
	got := make(chan string, 25)
	errs := make(chan error, 25)
	for range 25 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := r.o.GitHubAccess(ctx, "")
			got <- tok.Token
			errs <- err
		}()
	}
	wg.Wait()
	close(got)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for tok := range got {
		if tok != "ghu_2" {
			t.Fatalf("caller got %q", tok)
		}
	}
	if n := r.prov.ghRefreshCalls.Load(); n != 1 {
		t.Fatalf("%d refresh calls, want exactly 1", n)
	}
}

func TestGitHubRefreshRejection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		answer string
	}{
		{"bad_refresh_token in a 200 body", 0, `{"error":"bad_refresh_token","error_description":"The refresh token passed is incorrect or expired."}`},
		{"400", 400, ""},
		{"401", 401, ""},
	} {
		r := newOAuthRig(t)
		connectGitHub(t, r)
		r.prov.ghRefreshStatus.Store(int32(tc.status))
		r.prov.ghRefreshAnswer.Store(tc.answer)
		r.clock.Advance(9 * time.Hour)
		if _, err := r.o.GitHubAccess(ctx, ""); !errors.Is(err, auth.ErrNeedsReconnect) {
			t.Fatalf("%s: got %v", tc.name, err)
		}
		acc, ok, _ := r.o.GitHubAccount()
		if !ok || !acc.NeedsReconnect {
			t.Fatalf("%s: %+v", tc.name, acc)
		}
		if _, err := r.o.GitHubAccess(ctx, ""); !errors.Is(err, auth.ErrNeedsReconnect) || r.prov.ghRefreshCalls.Load() != 1 {
			t.Fatalf("%s: a flagged token must not call GitHub again", tc.name)
		}
		if _, err := r.o.CheckGitHub(ctx); !errors.Is(err, auth.ErrNeedsReconnect) {
			t.Fatalf("%s: check: %v", tc.name, err)
		}
		connectGitHub(t, r) // signing in again clears the flag
		if acc, _, _ := r.o.GitHubAccount(); acc.NeedsReconnect {
			t.Fatalf("%s: flag kept after reconnect", tc.name)
		}
	}
}

func TestGitHubTransientRefreshFailureKeepsTheToken(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		answer string
	}{
		{"500", 500, ""},
		{"503", 503, ""},
		{"error body", 0, `{"error":"incorrect_client_credentials"}`},
		{"empty token", 0, `{}`},
	} {
		r := newOAuthRig(t)
		connectGitHub(t, r)
		r.prov.ghRefreshStatus.Store(int32(tc.status))
		r.prov.ghRefreshAnswer.Store(tc.answer)
		r.clock.Advance(9 * time.Hour)
		_, err := r.o.GitHubAccess(ctx, "")
		if err == nil || errors.Is(err, auth.ErrNeedsReconnect) {
			t.Fatalf("%s: got %v", tc.name, err)
		}
		tok := firstGitHub(t, r.sec)
		if tok.NeedsReconnect || tok.RefreshToken != "ghr_1" || tok.Token != "ghu_1" {
			t.Fatalf("%s: %+v", tc.name, tok)
		}
	}
	r := newOAuthRig(t)
	connectGitHub(t, r)
	r.clock.Advance(9 * time.Hour)
	r.prov.srv.Close()
	if _, err := r.o.GitHubAccess(ctx, ""); err == nil || errors.Is(err, auth.ErrNeedsReconnect) {
		t.Fatalf("network down: %v", err)
	}
	if acc, _, _ := r.o.GitHubAccount(); acc.NeedsReconnect || acc.RefreshToken != "ghr_1" {
		t.Fatalf("%+v", acc)
	}
}

func TestGitHubRefreshKeepsOldRefreshTokenWhenNoneIsReturned(t *testing.T) {
	r := newOAuthRig(t)
	connectGitHub(t, r)
	r.prov.ghRefreshAnswer.Store(`{"access_token":"ghu_2","expires_in":60}`)
	r.clock.Advance(9 * time.Hour)
	if _, err := r.o.GitHubAccess(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if acc, _, _ := r.o.GitHubAccount(); acc.RefreshToken != "ghr_1" {
		t.Fatalf("%+v", acc)
	}
}

func TestGitHubAccessReturnsPATAndLegacyOAuthTokensAsStored(t *testing.T) {
	r := newOAuthRig(t)
	if _, err := r.o.GitHubAccess(ctx, ""); !errors.Is(err, auth.ErrNotConnected) {
		t.Fatalf("got %v", err)
	}
	for _, raw := range []string{
		`{"token":"gho_legacy","method":"oauth","login":"octocat"}`,
		`{"token":"ghp_pat","method":"pat","login":"octocat"}`,
	} {
		setGitHub(t, r.sec, raw)
		r.clock.Advance(10000 * time.Hour)
		got, err := r.o.GitHubAccess(ctx, "")
		if err != nil || (got.Token != "gho_legacy" && got.Token != "ghp_pat") || r.prov.ghRefreshCalls.Load() != 0 {
			t.Fatalf("%s: %q %v", raw, got, err)
		}
	}
	if tok, err := r.o.CheckGitHub(ctx); err != nil || tok.Method != "pat" {
		t.Fatalf("%+v %v", tok, err)
	}
}

func TestGitHubRefreshNeedsClientCredentials(t *testing.T) {
	r := newOAuthRig(t)
	connectGitHub(t, r)
	_ = r.sec.Delete(secrets.GitHubClient)
	r.clock.Advance(9 * time.Hour)
	if _, err := r.o.GitHubAccess(ctx, ""); !errors.Is(err, auth.ErrNotConfigured) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckGitHubRefreshesWhenDue(t *testing.T) {
	r := newOAuthRig(t)
	connectGitHub(t, r)
	r.clock.Advance(9 * time.Hour)
	tok, err := r.o.CheckGitHub(ctx)
	if err != nil || tok.Token != "ghu_2" || tok.RefreshToken != "ghr_2" || r.prov.ghRefreshCalls.Load() != 1 {
		t.Fatalf("%+v %v", tok, err)
	}
}

func TestClientSlugAndInstallURL(t *testing.T) {
	r := newOAuthRig(t)
	if got := r.o.InstallURL(); got != "" {
		t.Fatalf("no slug yet: %q", got)
	}
	for _, slug := range []string{"Acme", "-x", "a b", "a/b", "a..b", "a_b", "https://evil.example", strings.Repeat("a", 101), "é"} {
		err := r.o.SetClient(auth.GitHub, auth.ClientCredentials{ClientID: "i", ClientSecret: "s", AppSlug: slug})
		if !errors.Is(err, auth.ErrInvalidSlug) {
			t.Errorf("%q accepted: %v", slug, err)
		}
	}
	for _, slug := range []string{"a", "my-pr-dashboard", "app2", strings.Repeat("a", 100)} {
		if err := r.o.SetClient(auth.GitHub, auth.ClientCredentials{ClientID: "i", ClientSecret: "s", AppSlug: slug}); err != nil {
			t.Fatalf("%q: %v", slug, err)
		}
		if got := r.o.InstallURL(); got != "https://github.com/apps/"+slug+"/installations/new" {
			t.Fatalf("%q: %q", slug, got)
		}
	}
	// Atlassian has no slug, and a slug is dropped there.
	if err := r.o.SetClient(auth.Atlassian, auth.ClientCredentials{ClientID: "i", ClientSecret: "s", AppSlug: "x"}); err != nil {
		t.Fatal(err)
	}
	var c auth.ClientCredentials
	tokenJSON(t, r.sec, secrets.AtlassianClient, &c)
	if c.AppSlug != "" {
		t.Fatalf("%+v", c)
	}
	// A slug stored by hand that is not valid never becomes a link.
	_ = r.sec.Set(secrets.GitHubClient, []byte(`{"clientId":"i","clientSecret":"s","appSlug":"../evil"}`))
	if got := r.o.InstallURL(); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestSetPAT(t *testing.T) {
	r := newOAuthRig(t)

	rec, err := r.o.AddPAT(ctx, "ghp_pat", "")
	if err != nil || rec.Method != "pat" || rec.Login != "octocat" || rec.Warning != "" {
		t.Fatalf("classic: %+v %v", rec, err)
	}

	r.prov.githubScopes.Store("")
	rec, err = r.o.AddPAT(ctx, "github_pat_x", "")
	if err != nil || rec.Warning != auth.FineGrainedWarning {
		t.Fatalf("fine-grained: %+v %v", rec, err)
	}

	r.prov.githubScopes.Store("repo")
	_ = r.sec.Delete(secrets.GitHubTokens)
	_, err = r.o.AddPAT(ctx, "ghp_pat", "")
	var se *auth.ScopeError
	if !errors.As(err, &se) || !errors.Is(err, auth.ErrInsufficientScope) || len(se.Missing) != 1 || se.Missing[0] != "read:org" || se.Error() == "" {
		t.Fatalf("missing scope: %v", err)
	}
	r.prov.githubScopes.Store("admin:org, repo")
	if _, err := r.o.AddPAT(ctx, "ghp_pat", ""); err != nil {
		t.Fatalf("admin:org implies read:org: %v", err)
	}

	r.prov.githubScopes.Store("read:org")
	_ = r.sec.Delete(secrets.GitHubTokens)
	if _, err := r.o.AddPAT(ctx, "ghp_pat", ""); err == nil {
		t.Fatal("token without repo accepted")
	}
	if _, err := r.sec.Get(secrets.GitHubTokens); err == nil {
		t.Fatal("rejected token was stored")
	}
	if _, err := r.o.AddPAT(ctx, "ghp_unknown", ""); !errors.Is(err, github.ErrBadCredentials) {
		t.Fatalf("bad token: %v", err)
	}
}

func TestCheckGitHubAndDisconnect(t *testing.T) {
	r := newOAuthRig(t)
	if _, err := r.o.CheckGitHub(ctx); !errors.Is(err, auth.ErrNotConnected) {
		t.Fatalf("got %v", err)
	}
	_, _ = r.o.AddPAT(ctx, "ghp_pat", "")
	setGitHub(t, r.sec, `{"token":"ghp_pat","method":"pat","login":"old"}`)
	tok, err := r.o.CheckGitHub(ctx)
	if err != nil || tok.Login != "octocat" {
		t.Fatalf("%+v %v", tok, err)
	}
	stored, _, _ := r.o.GitHubAccount()
	if stored.Login != "octocat" {
		t.Fatalf("login not updated: %+v", stored)
	}
	setGitHub(t, r.sec, `{"token":"revoked","method":"pat","login":"x"}`)
	if _, err := r.o.CheckGitHub(ctx); !errors.Is(err, github.ErrBadCredentials) {
		t.Fatalf("got %v", err)
	}

	if err := r.o.Disconnect(auth.GitHub); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := r.o.GitHubAccount(); ok {
		t.Fatal("still connected")
	}
	if err := r.o.Disconnect(auth.GitHub); err != nil {
		t.Fatalf("disconnect when not connected: %v", err)
	}
	if !r.o.ClientConfigured(auth.GitHub) {
		t.Fatal("disconnect removed the client credentials")
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
