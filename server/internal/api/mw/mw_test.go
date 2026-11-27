package mw_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

func TestChainOrder(t *testing.T) {
	var order []string
	tag := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := mw.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }),
		tag("a"), tag("b"), tag("c"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got := strings.Join(order, ","); got != "a,b,c,handler" {
		t.Fatalf("order %s", got)
	}
}

func TestRecover(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	h := mw.Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret-detail") }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "secret-detail") {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("body %s", w.Body)
	}
	if !strings.Contains(logs.String(), "secret-detail") {
		t.Fatal("panic not logged")
	}
}

func TestRecoverRepanicsAbort(t *testing.T) {
	h := mw.Recover(slog.New(slog.NewJSONHandler(io.Discard, nil)))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if recover() != http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity
			t.Fatal("ErrAbortHandler was swallowed")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestAccessLog(t *testing.T) {
	var logs bytes.Buffer
	clock := testutil.NewClock()
	h := mw.AccessLog(slog.New(slog.NewJSONHandler(&logs, nil)), clock.Now)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		clock.Advance(40 * time.Millisecond)
		w.WriteHeader(http.StatusTeapot)
	}))
	r := httptest.NewRequest(http.MethodPost, "/api/x?token=abc", strings.NewReader("body-secret"))
	r.Header.Set("Cookie", "prd_session=cookie-secret")
	h.ServeHTTP(httptest.NewRecorder(), r)
	out := logs.String()
	for _, want := range []string{`"method":"POST"`, `"path":"/api/x"`, `"status":418`, `"duration":40000000`} {
		if !strings.Contains(out, want) {
			t.Errorf("%s missing in %s", want, out)
		}
	}
	for _, leak := range []string{"token=abc", "body-secret", "cookie-secret"} {
		if strings.Contains(out, leak) {
			t.Errorf("%s leaked in %s", leak, out)
		}
	}
}

func TestAccessLogDefaultsToOK(t *testing.T) {
	var logs bytes.Buffer
	h := mw.AccessLog(slog.New(slog.NewJSONHandler(&logs, nil)), time.Now)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(logs.String(), `"status":200`) {
		t.Fatal(logs.String())
	}
	logs.Reset()
	h = mw.AccessLog(slog.New(slog.NewJSONHandler(&logs, nil)), time.Now)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(logs.String(), `"status":200`) {
		t.Fatal(logs.String())
	}
}

func TestAccessLogKeepsFlusher(t *testing.T) {
	h := mw.AccessLog(slog.New(slog.NewJSONHandler(io.Discard, nil)), time.Now)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush through wrapper: %v", err)
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestCSP(t *testing.T) {
	want := "default-src 'none'; script-src 'self' 'sha256-aaa=' 'sha256-bbb='; style-src 'self'; " +
		"img-src 'self' data: https://avatars.githubusercontent.com; font-src 'self'; connect-src 'self'; " +
		"manifest-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	if got := mw.CSP([]string{"'sha256-aaa='", "'sha256-bbb='"}); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := mw.CSP(nil); !strings.HasPrefix(got, "default-src 'none'; script-src 'self'; style-src") {
		t.Fatalf("no-hash CSP %s", got)
	}
}

func TestLoadScriptHashes(t *testing.T) {
	cases := map[string]struct {
		fs   fstest.MapFS
		want []string
	}{
		"valid":      {fstest.MapFS{"csp.json": {Data: []byte(`{"scriptHashes":["'sha256-ab+/=' ","'sha256-Zm9v'"]}`)}}, []string{"'sha256-Zm9v'"}},
		"drops bad":  {fstest.MapFS{"csp.json": {Data: []byte(`{"scriptHashes":["'unsafe-inline'","'sha256-x'; script-src *","sha256-x","'sha256-ok='"]}`)}}, []string{"'sha256-ok='"}},
		"missing":    {fstest.MapFS{}, nil},
		"malformed":  {fstest.MapFS{"csp.json": {Data: []byte(`{`)}}, nil},
		"no field":   {fstest.MapFS{"csp.json": {Data: []byte(`{}`)}}, nil},
		"empty list": {fstest.MapFS{"csp.json": {Data: []byte(`{"scriptHashes":[]}`)}}, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := mw.LoadScriptHashes(c.fs)
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
	if mw.LoadScriptHashes(nil) != nil {
		t.Fatal("nil FS must give no hashes")
	}
}

func TestRateLimitRetryAfterRoundsUp(t *testing.T) {
	clock := testutil.NewClock()
	h := mw.RateLimit(clock.Now)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	send := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/anything", nil)
		h.ServeHTTP(w, r)
		return w
	}
	for range 30 {
		if w := send(); w.Code != http.StatusNoContent {
			t.Fatalf("status %d", w.Code)
		}
	}
	clock.Advance(500 * time.Millisecond)
	w := send()
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "2" {
		t.Fatalf("status %d Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	clock.Advance(1500 * time.Millisecond)
	if w := send(); w.Code != http.StatusNoContent {
		t.Fatalf("status %d after refill", w.Code)
	}
}

func TestRateLimitIgnoresNonAPIAndReads(t *testing.T) {
	clock := testutil.NewClock()
	h := mw.RateLimit(clock.Now)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, c := range []struct{ method, path string }{{http.MethodPost, "/other"}, {http.MethodGet, "/api/x"}, {http.MethodHead, "/api/x"}} {
		for range 100 {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
			if w.Code != http.StatusNoContent {
				t.Fatalf("%s %s limited", c.method, c.path)
			}
		}
	}
}

func TestValidateRoutesUnknownPathsThrough(t *testing.T) {
	spec, err := specForTest()
	if err != nil {
		t.Fatal(err)
	}
	validate, err := mw.Validate(spec)
	if err != nil {
		t.Fatal(err)
	}
	reached := false
	h := validate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	r := httptest.NewRequest(http.MethodPost, "/api/unknown", strings.NewReader(`{"a":1}`))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !reached {
		t.Fatal("unknown route was blocked")
	}
}

func TestValidateRestoresBody(t *testing.T) {
	spec, _ := specForTest()
	validate, _ := mw.Validate(spec)
	var got string
	h := validate(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))
	r := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(`{"launchToken":"abc"}`))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got != `{"launchToken":"abc"}` {
		t.Fatalf("handler saw %q", got)
	}
}

func TestProtectedSurface(t *testing.T) {
	cases := map[string]bool{
		"/api": true, "/api/": true, "/api/settings": true, "/events": true,
		"/oauth/github/start": true, "/oauth/atlassian/start": true, "/oauth/x/start": true,
		"/oauth/github/callback": false, "/oauth/finish/": false, "/oauth//start": false,
		"/oauth/github/start/": false, "/oauth/github/start/x": false, "/oauth/start": false,
		"/": false, "/setup/": false, "/auth/done/": false, "/apix": false,
	}
	for path, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4317"+path, nil)
		if got := mw.Protected(r); got != want {
			t.Errorf("%s: %v, want %v", path, got, want)
		}
	}
}

func TestRateLimitPreviewsHaveTheirOwnBucket(t *testing.T) {
	clock := testutil.NewClock()
	h := mw.RateLimit(clock.Now)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	send := func(path string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		return w.Code
	}
	for i := range 120 {
		path := "/api/rules/preview"
		if i%2 == 1 {
			path = "/api/rules/previews"
		}
		if c := send(path); c != http.StatusNoContent {
			t.Fatalf("preview %d: status %d", i, c)
		}
	}
	if c := send("/api/rules/previews"); c != http.StatusTooManyRequests {
		t.Fatalf("121st preview: status %d", c)
	}
	// The write budget is untouched by the previews, and they do not spend it.
	for i := range 30 {
		if c := send("/api/anything"); c != http.StatusNoContent {
			t.Fatalf("write %d: status %d", i, c)
		}
	}
	if c := send("/api/anything"); c != http.StatusTooManyRequests {
		t.Fatalf("31st write: status %d", c)
	}
	if c := send("/api/rules/preview"); c != http.StatusTooManyRequests {
		t.Fatalf("preview while the preview bucket is empty: status %d", c)
	}
}
