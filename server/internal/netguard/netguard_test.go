package netguard

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTransport struct {
	mu      sync.Mutex
	seen    []string
	current atomic.Int32
	peak    atomic.Int32
	hold    chan struct{}
	handler func(*http.Request) (*http.Response, error)
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.seen = append(f.seen, r.URL.String())
	f.mu.Unlock()
	n := f.current.Add(1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	defer f.current.Add(-1)
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	if f.handler != nil {
		return f.handler(r)
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}}, nil
}

func get(t *testing.T, c *http.Client, raw string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.Do(req)
}

func TestAllowedHosts(t *testing.T) {
	ft := &fakeTransport{}
	c := newGuard(ft).Client()
	for _, u := range []string{
		"https://api.github.com/graphql", "https://github.com/login/oauth/access_token",
		"https://api.atlassian.com/oauth/token/accessible-resources", "https://auth.atlassian.com/oauth/token",
		"https://API.GitHub.com:443/x",
	} {
		resp, err := get(t, c, u)
		if err != nil {
			t.Errorf("%s: %v", u, err)
			continue
		}
		_ = resp.Body.Close()
	}
}

func TestDeniedHosts(t *testing.T) {
	ft := &fakeTransport{}
	c := newGuard(ft).Client()
	for _, u := range []string{
		"https://evil.example/x", "https://api.github.com.evil.example/x", "https://notgithub.com/x",
		"http://api.github.com/graphql", "https://api.github.com:8443/x", "https://127.0.0.1/x",
		"https://example.atlassian.net/rest/api/3/search", "ftp://github.com/x", "https://github.com@evil.example/x",
	} {
		if _, err := get(t, c, u); !errors.Is(err, ErrHostNotAllowed) {
			t.Errorf("%s: got %v", u, err)
		}
	}
	if len(ft.seen) != 0 {
		t.Fatalf("denied requests reached the transport: %v", ft.seen)
	}
}

func TestRedirectToOtherHostFails(t *testing.T) {
	ft := &fakeTransport{handler: func(r *http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set("Location", "https://evil.example/steal")
		return &http.Response{StatusCode: 302, Header: h, Body: http.NoBody, Request: r}, nil
	}}
	_, err := get(t, newGuard(ft).Client(), "https://api.github.com/x")
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("got %v", err)
	}
	if len(ft.seen) != 1 {
		t.Fatalf("redirect target was requested: %v", ft.seen)
	}
}

func TestRedirectWithinAllowlistAndLimit(t *testing.T) {
	n := 0
	ft := &fakeTransport{handler: func(r *http.Request) (*http.Response, error) {
		n++
		h := http.Header{}
		if n == 1 {
			h.Set("Location", "https://github.com/next")
			return &http.Response{StatusCode: 302, Header: h, Body: http.NoBody, Request: r}, nil
		}
		return &http.Response{StatusCode: 200, Header: h, Body: http.NoBody, Request: r}, nil
	}}
	resp, err := get(t, newGuard(ft).Client(), "https://api.github.com/x")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
	_ = resp.Body.Close()

	loop := &fakeTransport{handler: func(r *http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set("Location", "https://github.com/again")
		return &http.Response{StatusCode: 302, Header: h, Body: http.NoBody, Request: r}, nil
	}}
	if _, err := get(t, newGuard(loop).Client(), "https://github.com/start"); err == nil || !strings.Contains(err.Error(), "5 redirects") {
		t.Fatalf("got %v", err)
	}
}

func TestClientLimitsAndTLS(t *testing.T) {
	c := New().Client()
	if c.Timeout != 30*time.Second {
		t.Fatalf("timeout %v", c.Timeout)
	}
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	tr := New().base.(*http.Transport)
	if tr.Proxy != nil {
		t.Fatal("the transport must not use environment proxies")
	}
	if tr.TLSClientConfig.MinVersion != 0x0303 {
		t.Fatalf("min TLS version %x", tr.TLSClientConfig.MinVersion)
	}
}

func TestConcurrencyPerProvider(t *testing.T) {
	ft := &fakeTransport{hold: make(chan struct{})}
	g := newGuard(ft)
	c := g.Client()
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := get(t, c, "https://api.github.com/x")
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for ft.current.Load() < MaxConcurrent && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if got := ft.current.Load(); got != MaxConcurrent {
		t.Fatalf("%d in flight, want %d", got, MaxConcurrent)
	}
	// The other provider is not blocked by GitHub traffic.
	other := make(chan error, 1)
	go func() {
		resp, err := get(t, c, "https://auth.atlassian.com/x")
		if err == nil {
			_ = resp.Body.Close()
		}
		other <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if ft.current.Load() != MaxConcurrent+1 {
		t.Fatalf("atlassian request waited behind github: %d in flight", ft.current.Load())
	}
	close(ft.hold)
	wg.Wait()
	if err := <-other; err != nil {
		t.Fatal(err)
	}
	if ft.peak.Load() > MaxConcurrent+1 {
		t.Fatalf("peak %d", ft.peak.Load())
	}
}

func TestSlotHeldUntilBodyClosed(t *testing.T) {
	g := newGuard(&fakeTransport{})
	c := g.Client()
	var bodies []*http.Response
	for range MaxConcurrent {
		resp, err := get(t, c, "https://github.com/x")
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, resp)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://github.com/y", nil)
	if _, err := c.Do(req); err == nil {
		t.Fatal("fifth request ran while four bodies were open")
	}
	_ = bodies[0].Body.Close()
	_ = bodies[0].Body.Close() // closing twice frees one slot only
	resp, err := get(t, c, "https://github.com/z")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestSlotFreedOnTransportError(t *testing.T) {
	boom := errors.New("boom")
	g := newGuard(&fakeTransport{handler: func(*http.Request) (*http.Response, error) { return nil, boom }})
	for range 3 * MaxConcurrent {
		if _, err := get(t, g.Client(), "https://github.com/x"); !errors.Is(err, boom) {
			t.Fatalf("got %v", err)
		}
	}
}

func TestLoopbackIsDeniedByDefault(t *testing.T) {
	g := newGuard(&fakeTransport{})
	for _, raw := range []string{"http://127.0.0.1:4411/x", "https://127.0.0.1:4411/x"} {
		u, _ := url.Parse(raw)
		if _, err := g.check(u); !errors.Is(err, ErrHostNotAllowed) {
			t.Errorf("%s: %v", raw, err)
		}
	}
}
