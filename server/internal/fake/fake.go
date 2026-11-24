//go:build e2e

// Package fake holds in-process fake GitHub and Atlassian servers for the e2e build. They listen
// on fixed loopback ports, answer like the real services for the flows under test, and can be
// steered through an admin endpoint under /__admin/ on the fake itself. The app has no such
// endpoint, and this package is not compiled into the release binary.
package fake

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/netguard"
)

// Fixed addresses of the fakes. The browser is sent to the "localhost" spelling of the authorize
// endpoints so that the redirect back to 127.0.0.1:4317 is cross-site, as with the real providers.
const (
	GitHubAddr    = "127.0.0.1:4411"
	AtlassianAddr = "127.0.0.1:4412"
)

// Client credentials the fakes accept. Start returns them so the app can be pre-configured.
var (
	GitHubClient    = auth.ClientCredentials{ClientID: "fake-github-client", ClientSecret: "fake-github-secret"}
	AtlassianClient = auth.ClientCredentials{ClientID: "fake-atlassian-client", ClientSecret: "fake-atlassian-secret"}
)

// Servers is a running pair of fakes.
type Servers struct {
	Endpoints auth.Endpoints
	GitHubAPI string
	Close     func()
}

// Start listens on both addresses, allows them in guard and serves until Close.
func Start(guard *netguard.Guard, log *slog.Logger) (*Servers, error) {
	gh, at := newGitHub(), newAtlassian()
	var closers []func()
	closeAll := func() {
		for _, c := range closers {
			c()
		}
	}
	for _, s := range []struct {
		addr     string
		handler  http.Handler
		provider netguard.Provider
	}{
		{GitHubAddr, gh.routes(), netguard.GitHub},
		{AtlassianAddr, at.routes(), netguard.Atlassian},
	} {
		ln, err := net.Listen("tcp4", s.addr)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("fake server on %s: %w", s.addr, err)
		}
		srv := &http.Server{Handler: s.handler, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("fake server", "err", err)
			}
		}()
		closers = append(closers, func() { _ = srv.Shutdown(context.Background()) })
		guard.AllowLoopback(s.addr, s.provider)
	}
	log.Warn("running with fake GitHub and Atlassian servers", "github", GitHubAddr, "atlassian", AtlassianAddr)
	return &Servers{
		Endpoints: auth.Endpoints{
			GitHubAuthorize:    "http://localhost:4411/login/oauth/authorize",
			GitHubToken:        "http://" + GitHubAddr + "/login/oauth/access_token",
			AtlassianAuthorize: "http://localhost:4412/authorize",
			AtlassianToken:     "http://" + AtlassianAddr + "/oauth/token",
			AtlassianResources: "http://" + AtlassianAddr + "/oauth/token/accessible-resources",
		},
		GitHubAPI: "http://" + GitHubAddr,
		Close:     closeAll,
	}, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// authorizeRedirect answers an authorize request the way a user who clicks "Authorize" would: a
// 302 to redirect_uri with a code and the state. Admin modes make it deny or tamper with the state.
func authorizeRedirect(w http.ResponseWriter, r *http.Request, cfg authorizeConfig, issue func() string, setLast func(string)) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	if !strings.HasPrefix(redirect, "http://127.0.0.1:") {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	out := url.Values{}
	switch {
	case cfg.Deny:
		out.Set("error", "access_denied")
		out.Set("state", q.Get("state"))
	case cfg.TamperState:
		out.Set("code", issue())
		out.Set("state", "tampered-"+randomHex(8))
	default:
		out.Set("code", issue())
		out.Set("state", q.Get("state"))
	}
	target := redirect + "?" + out.Encode()
	setLast(target)
	http.Redirect(w, r, target, http.StatusFound)
}

type authorizeConfig struct {
	Deny        bool `json:"deny"`
	TamperState bool `json:"tamperState"`
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type safeMap[V any] struct {
	mu sync.Mutex
	m  map[string]V
}

func (s *safeMap[V]) put(k string, v V) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]V{}
	}
	s.m[k] = v
}

func (s *safeMap[V]) take(k string) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	delete(s.m, k)
	return v, ok
}

func (s *safeMap[V]) has(k string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[k]
	return ok
}
