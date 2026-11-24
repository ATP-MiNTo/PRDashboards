// Package netguard is the only way the app reaches the network. Its transport allows a fixed set
// of hosts over https, limits concurrency per provider and fails redirects to other hosts.
package netguard

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Provider groups hosts that share a concurrency limit.
type Provider string

// Providers the app talks to.
const (
	GitHub    Provider = "github"
	Atlassian Provider = "atlassian"
)

const (
	// MaxConcurrent is the number of requests in flight per provider.
	MaxConcurrent = 4
	// DialTimeout limits connection setup.
	DialTimeout = 10 * time.Second
	// ClientTimeout limits a whole request.
	ClientTimeout = 30 * time.Second

	maxRedirects = 5
)

// ErrHostNotAllowed is returned for a request to a host outside the allowlist.
var ErrHostNotAllowed = errors.New("outbound host not allowed")

var allowed = map[string]Provider{
	"api.github.com":     GitHub,
	"github.com":         GitHub,
	"api.atlassian.com":  Atlassian,
	"auth.atlassian.com": Atlassian,
}

// Guard is an http.RoundTripper that enforces the allowlist.
type Guard struct {
	base http.RoundTripper
	sems map[Provider]chan struct{}

	mu    sync.RWMutex
	extra map[string]Provider // exact host:port allowed over plain http, filled by the e2e build only
}

// New returns a guard over a transport with TLS 1.2 as the minimum version and a 10 s dial timeout.
func New() *Guard {
	t := http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert // the default is always a *Transport
	t.DialContext = (&net.Dialer{Timeout: DialTimeout, KeepAlive: 30 * time.Second}).DialContext
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return newGuard(t)
}

func newGuard(base http.RoundTripper) *Guard {
	g := &Guard{base: base, sems: map[Provider]chan struct{}{}, extra: map[string]Provider{}}
	for _, p := range []Provider{GitHub, Atlassian} {
		g.sems[p] = make(chan struct{}, MaxConcurrent)
	}
	return g
}

// Client returns an http.Client over the guard with a 30 s timeout. Redirects are limited to five
// and must stay inside the allowlist.
func (g *Guard) Client() *http.Client {
	return &http.Client{
		Transport: g,
		Timeout:   ClientTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("stopped after 5 redirects")
			}
			_, err := g.check(req.URL)
			return err
		},
	}
}

func (g *Guard) check(u *url.URL) (Provider, error) {
	host := strings.ToLower(u.Host)
	g.mu.RLock()
	p, ok := g.extra[host]
	g.mu.RUnlock()
	if ok && u.Scheme == "http" {
		return p, nil
	}
	name := strings.ToLower(u.Hostname())
	if port := u.Port(); u.Scheme == "https" && (port == "" || port == "443") {
		if p, ok := allowed[name]; ok {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: %s://%s", ErrHostNotAllowed, u.Scheme, u.Host)
}

// RoundTrip implements http.RoundTripper.
func (g *Guard) RoundTrip(req *http.Request) (*http.Response, error) {
	p, err := g.check(req.URL)
	if err != nil {
		return nil, err
	}
	sem := g.sems[p]
	select {
	case sem <- struct{}{}:
	case <-req.Context().Done():
		return nil, context.Cause(req.Context())
	}
	resp, err := g.base.RoundTrip(req)
	if err != nil {
		<-sem
		return nil, err
	}
	resp.Body = &releaseBody{ReadCloser: resp.Body, release: func() { <-sem }}
	return resp, nil
}

// releaseBody frees the provider slot when the body is closed.
type releaseBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *releaseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}
