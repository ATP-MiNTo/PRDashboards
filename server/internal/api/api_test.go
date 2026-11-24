package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

const (
	host   = "127.0.0.1:4317"
	origin = "http://" + host
)

type rig struct {
	t        *testing.T
	h        http.Handler
	clock    *testutil.Clock
	launcher *auth.Launcher
	logs     *bytes.Buffer
	store    *store.Store
	secrets  *secrets.Memory
	oauth    *auth.OAuth
	prov     *fakeProviders
	sessions *auth.Sessions
}

func webFS() fs.FS {
	return fstest.MapFS{
		"index.html":              {Data: []byte("<html>home</html>")},
		"auth/launch/index.html":  {Data: []byte("<html>launch</html>")},
		"oauth/finish/index.html": {Data: []byte("<html>finish</html>")},
		"404.html":                {Data: []byte("<html>missing</html>")},
		"csp.json":                {Data: []byte(`{"scriptHashes":["'sha256-abc='"]}`)},
		"_next/static/a.js":       {Data: []byte("x")},
	}
}

func newRig(t *testing.T, web fs.FS, mods ...func(*api.Config)) *rig {
	t.Helper()
	clock := testutil.NewClock()
	rnd := &testutil.Rand{}
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	dir := t.TempDir()
	launcher, err := auth.NewLauncher(filepath.Join(dir, "launch.token"), 4317, clock.Now, rnd, log)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "data.db"), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	sec := secrets.NewMemory()
	prov := newFakeProviders(t)
	oauth := auth.NewOAuth(auth.Config{
		Secrets:   sec,
		States:    db,
		HTTP:      prov.srv.Client(),
		GitHub:    &github.Client{HTTP: prov.srv.Client(), APIBase: prov.srv.URL},
		Endpoints: prov.endpoints(),
		Clock:     clock.Now,
		Rand:      rnd,
		Port:      4317,
	})
	sessions := auth.NewSessions([]byte("0123456789abcdef0123456789abcdef"), clock.Now, rnd, db, log)
	cfg := api.Config{
		Port:     4317,
		Version:  "9.9.9",
		Sessions: sessions,
		Launcher: launcher,
		Store:    db,
		OAuth:    oauth,
		Web:      web,
		Log:      log,
		Clock:    clock.Now,
	}
	for _, m := range mods {
		m(&cfg)
	}
	h, err := api.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, h: h, clock: clock, launcher: launcher, logs: logs, store: db, secrets: sec, oauth: oauth, prov: prov, sessions: sessions}
}

type reqOpt func(*http.Request)

func header(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func noHeader(k string) reqOpt  { return func(r *http.Request) { r.Header.Del(k) } }
func cookie(v string) reqOpt    { return header("Cookie", "prd_session="+v) }
func csrf(v string) reqOpt      { return header(mw.CSRFHeader, v) }
func withHost(h string) reqOpt  { return func(r *http.Request) { r.Host = h } }

// do sends a same-origin browser-like request.
func (g *rig) do(method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	g.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, "http://"+host+path, rd)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if method != http.MethodGet && method != http.MethodHead {
		r.Header.Set("Origin", origin)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
	}
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	return w
}

func tokenBody(tok string) string { return `{"launchToken":"` + tok + `"}` }

type login struct{ cookie, csrf string }

func (g *rig) login() login {
	g.t.Helper()
	w := g.do(http.MethodPost, "/api/session", tokenBody(g.launcher.Current()))
	if w.Code != http.StatusNoContent {
		g.t.Fatalf("login status %d: %s", w.Code, w.Body)
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 {
		g.t.Fatalf("want one cookie, got %v", cs)
	}
	s := g.do(http.MethodGet, "/api/session", "", cookie(cs[0].Value))
	if s.Code != http.StatusOK {
		g.t.Fatalf("session status %d", s.Code)
	}
	var body struct{ CsrfToken string }
	if err := json.Unmarshal(s.Body.Bytes(), &body); err != nil {
		g.t.Fatal(err)
	}
	return login{cookie: cs[0].Value, csrf: body.CsrfToken}
}

func (l login) write() []reqOpt { return []reqOpt{cookie(l.cookie), csrf(l.csrf)} }

func wantProblem(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d (body %s)", w.Code, status, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type %q", ct)
	}
	var p mw.Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != code || p.Status != status || p.Title == "" || p.Type == "" {
		t.Fatalf("problem %+v, want code %q status %d", p, code, status)
	}
}

func TestHostAllowlist(t *testing.T) {
	g := newRig(t, webFS())
	for _, h := range []string{host, "localhost:4317", "LOCALHOST:4317"} {
		if w := g.do(http.MethodGet, "/", "", withHost(h)); w.Code != http.StatusOK {
			t.Errorf("host %q: status %d", h, w.Code)
		}
	}
	for _, h := range []string{"evil.example:4317", "127.0.0.1:9999", "127.0.0.1", "localhost", "[::1]:4317", "0.0.0.0:4317", ""} {
		for _, p := range []string{"/", "/api/session"} {
			wantProblem(t, g.do(http.MethodGet, p, "", withHost(h)), http.StatusMisdirectedRequest, "misdirected_host")
		}
	}
}

func TestHostAllowlistFollowsPort(t *testing.T) {
	g := newRig(t, webFS())
	h, err := api.New(api.Config{
		Port: 5000, Version: "v", Sessions: auth.NewSessions(make([]byte, 32), g.clock.Now, &testutil.Rand{}, nil, nil),
		Launcher: g.launcher, Web: webFS(), Log: slog.New(slog.NewJSONHandler(io.Discard, nil)), Clock: g.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string]int{"127.0.0.1:5000": 200, "localhost:5000": 200, "127.0.0.1:4317": 421} {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: status %d, want %d", host, w.Code, want)
		}
	}
}

func TestOptionsRejected(t *testing.T) {
	g := newRig(t, webFS())
	for _, p := range []string{"/", "/api/session", "/api/anything"} {
		w := g.do(http.MethodOptions, p, "", header("Access-Control-Request-Method", "POST"), header("Origin", "https://evil.example"))
		wantProblem(t, w, http.StatusMethodNotAllowed, "cors_not_allowed")
	}
}

func TestNoAccessControlHeaders(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	for _, w := range []*httptest.ResponseRecorder{
		g.do(http.MethodGet, "/", ""),
		g.do(http.MethodGet, "/api/session", "", cookie(l.cookie)),
		g.do(http.MethodGet, "/api/session", ""),
		g.do(http.MethodOptions, "/api/session", ""),
		g.do(http.MethodGet, "/", "", header("Origin", "https://evil.example")),
	} {
		for k := range w.Header() {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Fatalf("header %s present", k)
			}
		}
	}
}

func TestSecFetchSite(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	for _, site := range []string{"cross-site", "same-site", "none", "bogus"} {
		wantProblem(t, g.do(http.MethodGet, "/api/session", "", cookie(l.cookie), header("Sec-Fetch-Site", site)),
			http.StatusForbidden, "cross_site")
	}
	for name, opt := range map[string]reqOpt{"same-origin": header("Sec-Fetch-Site", "same-origin"), "absent": noHeader("Sec-Fetch-Site")} {
		if w := g.do(http.MethodGet, "/api/session", "", cookie(l.cookie), opt); w.Code != http.StatusOK {
			t.Errorf("%s: status %d", name, w.Code)
		}
	}
	if w := g.do(http.MethodGet, "/", "", header("Sec-Fetch-Site", "none")); w.Code != http.StatusOK {
		t.Errorf("static page navigation blocked: %d", w.Code)
	}
}

func TestOrigin(t *testing.T) {
	g := newRig(t, webFS())
	for _, o := range []string{"https://evil.example", "http://127.0.0.1:9999", "http://127.0.0.1", "null", "https://127.0.0.1:4317", ""} {
		opt := header("Origin", o)
		if o == "" {
			opt = noHeader("Origin")
		}
		wantProblem(t, g.do(http.MethodPost, "/api/session", tokenBody("x"), opt), http.StatusForbidden, "bad_origin")
	}
	for _, o := range []string{origin, "http://localhost:4317"} {
		wantProblem(t, g.do(http.MethodPost, "/api/session", tokenBody("x"), header("Origin", o)),
			http.StatusUnauthorized, "invalid_launch_token")
	}
	if w := g.do(http.MethodGet, "/api/session", "", header("Origin", "https://evil.example")); w.Code != http.StatusUnauthorized {
		t.Errorf("GET must not need an allowlisted Origin, got %d", w.Code)
	}
}

func TestSessionRequired(t *testing.T) {
	g := newRig(t, webFS())
	for _, p := range []string{"/api/session", "/api/nothing", "/events", "/api"} {
		wantProblem(t, g.do(http.MethodGet, p, ""), http.StatusUnauthorized, "unauthenticated")
	}
	wantProblem(t, g.do(http.MethodPost, "/api/nothing", "{}"), http.StatusUnauthorized, "unauthenticated")
	for _, c := range []string{"garbage", "a.b", ""} {
		wantProblem(t, g.do(http.MethodGet, "/api/session", "", cookie(c)), http.StatusUnauthorized, "unauthenticated")
	}
}

func TestCSRF(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	wantProblem(t, g.do(http.MethodPost, "/api/nothing", "{}", cookie(l.cookie)), http.StatusForbidden, "bad_csrf")
	wantProblem(t, g.do(http.MethodPost, "/api/nothing", "{}", cookie(l.cookie), csrf("wrong")), http.StatusForbidden, "bad_csrf")
	wantProblem(t, g.do(http.MethodPost, "/api/nothing", "{}", cookie(l.cookie), csrf(l.csrf+"x")), http.StatusForbidden, "bad_csrf")
	other := g.login()
	wantProblem(t, g.do(http.MethodPost, "/api/nothing", "{}", cookie(l.cookie), csrf(other.csrf)), http.StatusForbidden, "bad_csrf")
	wantProblem(t, g.do(http.MethodPost, "/api/nothing", "{}", l.write()...), http.StatusNotFound, "not_found")
	wantProblem(t, g.do(http.MethodDelete, "/api/nothing", "", l.write()...), http.StatusNotFound, "not_found")
	if w := g.do(http.MethodGet, "/api/session", "", cookie(l.cookie)); w.Code != http.StatusOK {
		t.Errorf("GET needs no CSRF header, got %d", w.Code)
	}
}

func TestContentType(t *testing.T) {
	g := newRig(t, webFS())
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "application/jsonx", "", "garbage;;"} {
		opt := header("Content-Type", ct)
		if ct == "" {
			opt = noHeader("Content-Type")
		}
		wantProblem(t, g.do(http.MethodPost, "/api/session", tokenBody(g.launcher.Current()), opt),
			http.StatusUnsupportedMediaType, "unsupported_media_type")
	}
	w := g.do(http.MethodPost, "/api/session", tokenBody(g.launcher.Current()), header("Content-Type", "application/json; charset=utf-8"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("charset parameter rejected: %d", w.Code)
	}
}

func TestBodyTooLarge(t *testing.T) {
	g := newRig(t, webFS())
	big := `{"launchToken":"` + strings.Repeat("a", mw.MaxBodyBytes) + `"}`
	wantProblem(t, g.do(http.MethodPost, "/api/session", big), http.StatusRequestEntityTooLarge, "body_too_large")
	wantProblem(t, g.do(http.MethodPost, "/api/session", big, func(r *http.Request) { r.ContentLength = -1 }),
		http.StatusRequestEntityTooLarge, "body_too_large")
	pad := strings.Repeat(" ", mw.MaxBodyBytes-len(tokenBody("x")))
	wantProblem(t, g.do(http.MethodPost, "/api/session", tokenBody("x")+pad), http.StatusUnauthorized, "invalid_launch_token")
}

func TestInvalidBody(t *testing.T) {
	g := newRig(t, webFS())
	tok := g.launcher.Current()
	cases := map[string]string{
		"not json":         `{`,
		"trailing value":   tokenBody(tok) + ` {}`,
		"trailing garbage": tokenBody(tok) + `x`,
		"array":            `[]`,
		"null":             `null`,
		"unknown field":    `{"launchToken":"` + tok + `","extra":1}`,
		"wrong type":       `{"launchToken":5}`,
		"missing field":    `{}`,
		"empty token":      `{"launchToken":""}`,
		"long token":       `{"launchToken":"` + strings.Repeat("a", 129) + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			g.clock.Advance(time.Minute)
			wantProblem(t, g.do(http.MethodPost, "/api/session", body), http.StatusBadRequest, "invalid_body")
		})
	}
	wantProblem(t, g.do(http.MethodPost, "/api/session", ""), http.StatusBadRequest, "invalid_body")
	if g.launcher.Current() != tok {
		t.Fatal("rejected bodies must not spend the launch token")
	}
}

func TestUnknownAPIPath(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	for _, p := range []string{"/api/nope", "/api/", "/api/session/extra"} {
		wantProblem(t, g.do(http.MethodGet, p, "", cookie(l.cookie)), http.StatusNotFound, "not_found")
	}
	wantProblem(t, g.do(http.MethodPut, "/api/session", "{}", l.write()...), http.StatusNotFound, "not_found")
}

func TestLaunchFlow(t *testing.T) {
	g := newRig(t, webFS())
	tok := g.launcher.Current()

	wantProblem(t, g.do(http.MethodPost, "/api/session", tokenBody("wrong")), http.StatusUnauthorized, "invalid_launch_token")

	w := g.do(http.MethodPost, "/api/session", tokenBody(tok))
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("status %d body %q", w.Code, w.Body)
	}
	line := w.Header().Get("Set-Cookie")
	for _, want := range []string{"prd_session=", "HttpOnly", "SameSite=Strict", "Path=/", "Max-Age=2592000"} {
		if !strings.Contains(line, want) {
			t.Errorf("Set-Cookie %q lacks %q", line, want)
		}
	}
	if strings.Contains(line, "Secure") || strings.Contains(line, "Domain") {
		t.Errorf("Set-Cookie %q has Secure or Domain", line)
	}

	wantProblem(t, g.do(http.MethodPost, "/api/session", tokenBody(tok)), http.StatusUnauthorized, "invalid_launch_token")

	c := w.Result().Cookies()[0]
	s := g.do(http.MethodGet, "/api/session", "", cookie(c.Value))
	if s.Code != http.StatusOK || s.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d type %q", s.Code, s.Header().Get("Content-Type"))
	}
	var got struct {
		CsrfToken string    `json:"csrfToken"`
		ExpiresAt time.Time `json:"expiresAt"`
		Version   string    `json:"version"`
	}
	if err := json.Unmarshal(s.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CsrfToken == "" || got.Version != "9.9.9" || !got.ExpiresAt.Equal(g.clock.Now().Add(auth.SessionTTL)) {
		t.Fatalf("unexpected session body %+v", got)
	}
	if s.Header().Get("Set-Cookie") != "" {
		t.Error("cookie reissued on a fresh session")
	}
}

func TestSessionSlides(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	g.clock.Advance(time.Hour)
	if w := g.do(http.MethodGet, "/api/session", "", cookie(l.cookie)); w.Header().Get("Set-Cookie") != "" {
		t.Fatal("cookie reissued after one hour")
	}
	g.clock.Advance(24 * time.Hour)
	w := g.do(http.MethodGet, "/api/session", "", cookie(l.cookie))
	cs := w.Result().Cookies()
	if len(cs) != 1 || cs[0].Value != l.cookie || cs[0].MaxAge != 30*24*3600 || !cs[0].HttpOnly {
		t.Fatalf("expected reissued cookie, got %v", cs)
	}
}

func TestRateLimitWrites(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	for i := range 30 {
		if w := g.do(http.MethodPost, "/api/nothing", "{}", l.write()...); w.Code != http.StatusNotFound {
			t.Fatalf("request %d: status %d", i+1, w.Code)
		}
	}
	w := g.do(http.MethodPost, "/api/nothing", "{}", l.write()...)
	wantProblem(t, w, http.StatusTooManyRequests, "rate_limited")
	if got := w.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After %q, want 2", got)
	}
	if w := g.do(http.MethodGet, "/api/session", "", cookie(l.cookie)); w.Code != http.StatusOK {
		t.Fatalf("reads must not be limited, got %d", w.Code)
	}
	other := g.login()
	if w := g.do(http.MethodPost, "/api/nothing", "{}", other.write()...); w.Code != http.StatusNotFound {
		t.Fatalf("limit must be per session, got %d", w.Code)
	}
	g.clock.Advance(2 * time.Second)
	if w := g.do(http.MethodPost, "/api/nothing", "{}", l.write()...); w.Code != http.StatusNotFound {
		t.Fatalf("bucket did not refill, got %d", w.Code)
	}
}

func TestRateLimitUnauthenticatedSharesBucket(t *testing.T) {
	g := newRig(t, webFS())
	for range 30 {
		wantProblem(t, g.do(http.MethodPost, "/api/nothing", "{}"), http.StatusUnauthorized, "unauthenticated")
	}
	wantProblem(t, g.do(http.MethodPost, "/api/nothing", "{}"), http.StatusTooManyRequests, "rate_limited")
	wantProblem(t, g.do(http.MethodPost, "/api/nothing", "{}", cookie("forged.cookie")), http.StatusTooManyRequests, "rate_limited")
}

func TestRateLimitSessionCreation(t *testing.T) {
	g := newRig(t, webFS())
	for range 10 {
		wantProblem(t, g.do(http.MethodPost, "/api/session", tokenBody("bad")), http.StatusUnauthorized, "invalid_launch_token")
	}
	w := g.do(http.MethodPost, "/api/session", tokenBody(g.launcher.Current()))
	wantProblem(t, w, http.StatusTooManyRequests, "rate_limited")
	if got := w.Header().Get("Retry-After"); got != "6" {
		t.Fatalf("Retry-After %q, want 6", got)
	}
	g.clock.Advance(6 * time.Second)
	if w := g.do(http.MethodPost, "/api/session", tokenBody(g.launcher.Current())); w.Code != http.StatusNoContent {
		t.Fatalf("status %d after refill", w.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	g := newRig(t, webFS())
	l := g.login()
	wantCSP := mw.CSP([]string{"'sha256-abc='"})
	if !strings.Contains(wantCSP, "script-src 'self' 'sha256-abc='; style-src 'self'") {
		t.Fatalf("CSP %q", wantCSP)
	}
	responses := map[string]*httptest.ResponseRecorder{
		"static":       g.do(http.MethodGet, "/", ""),
		"static 404":   g.do(http.MethodGet, "/missing", ""),
		"api ok":       g.do(http.MethodGet, "/api/session", "", cookie(l.cookie)),
		"api 401":      g.do(http.MethodGet, "/api/session", ""),
		"api 404":      g.do(http.MethodGet, "/api/nope", "", cookie(l.cookie)),
		"421":          g.do(http.MethodGet, "/", "", withHost("evil.example")),
		"options 405":  g.do(http.MethodOptions, "/", ""),
		"cross site":   g.do(http.MethodGet, "/api/session", "", header("Sec-Fetch-Site", "cross-site")),
		"static asset": g.do(http.MethodGet, "/_next/static/a.js", ""),
	}
	want := map[string]string{
		"Content-Security-Policy":      wantCSP,
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Permissions-Policy":           "camera=(), microphone=(), geolocation=()",
	}
	for name, w := range responses {
		for k, v := range want {
			if got := w.Header().Get(k); got != v {
				t.Errorf("%s: %s = %q, want %q", name, k, got, v)
			}
		}
	}
	for _, name := range []string{"api ok", "api 401", "api 404", "cross site"} {
		if got := responses[name].Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control %q", name, got)
		}
	}
	if got := responses["static"].Header().Get("Cache-Control"); got == "no-store" {
		t.Error("static HTML must not be no-store")
	}
}

func TestCSPWithoutHashFile(t *testing.T) {
	g := newRig(t, fstest.MapFS{"index.html": {Data: []byte("x")}})
	csp := g.do(http.MethodGet, "/", "").Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'; style-src 'self'") {
		t.Fatalf("CSP %q", csp)
	}
}

func TestRequestLogHasNoSecrets(t *testing.T) {
	g := newRig(t, webFS())
	tok := g.launcher.Current()
	g.do(http.MethodPost, "/api/session?code=abc&state=xyz", tokenBody(tok))
	g.do(http.MethodGet, "/auth/launch/?x=secret#t="+tok, "")
	out := g.logs.String()
	for _, leak := range []string{tok, "code=abc", "state=xyz", "secret"} {
		if strings.Contains(out, leak) {
			t.Fatalf("%q leaked in logs: %s", leak, out)
		}
	}
	if !strings.Contains(out, `"path":"/api/session"`) || !strings.Contains(out, `"status":204`) {
		t.Fatalf("request log missing fields: %s", out)
	}
}

func TestSpecRequestSchemasRejectUnknownFields(t *testing.T) {
	spec, err := apiSpec()
	if err != nil {
		t.Fatal(err)
	}
	var walk func(where string, s *openapi3.Schema)
	walk = func(where string, s *openapi3.Schema) {
		if s.Type != nil && s.Type.Is("object") && (s.AdditionalProperties.Has == nil || *s.AdditionalProperties.Has) {
			t.Errorf("%s: object schema must set additionalProperties: false", where)
		}
		for name, p := range s.Properties {
			walk(where+"."+name, p.Value)
		}
		if s.Items != nil {
			walk(where+"[]", s.Items.Value)
		}
	}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if op.RequestBody == nil {
				continue
			}
			for ct, media := range op.RequestBody.Value.Content {
				walk(method+" "+path+" ("+ct+")", media.Schema.Value)
			}
		}
	}
}
