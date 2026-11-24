// Package backoff retries HTTP calls that a provider rejected for rate limiting, following the
// rules of docs/TECH_SPEC.md section 10.4. GitHub and Jira share it.
package backoff

import (
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Defaults of section 10.4.
const (
	DefaultBase     = 2 * time.Second
	DefaultMax      = 5 * time.Minute
	DefaultAttempts = 5
)

// Policy decides when a rate limited call is repeated. Zero fields take the defaults. Clock,
// Sleep and Jitter are injected so tests run without waiting.
type Policy struct {
	Base, Max time.Duration
	Attempts  int
	Clock     func() time.Time
	Sleep     func(ctx context.Context, d time.Duration) error
	// Jitter returns a value in [0, 1).
	Jitter func() float64
}

func (p Policy) base() time.Duration {
	if p.Base > 0 {
		return p.Base
	}
	return DefaultBase
}

func (p Policy) max() time.Duration {
	if p.Max > 0 {
		return p.Max
	}
	return DefaultMax
}

func (p Policy) attempts() int {
	if p.Attempts > 0 {
		return p.Attempts
	}
	return DefaultAttempts
}

func (p Policy) now() time.Time {
	if p.Clock != nil {
		return p.Clock()
	}
	return time.Now()
}

func (p Policy) jitter() float64 {
	if p.Jitter != nil {
		return p.Jitter()
	}
	return rand.Float64() //nolint:gosec // jitter does not need a secure source
}

func (p Policy) sleep(ctx context.Context, d time.Duration) error {
	if p.Sleep != nil {
		return p.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Hint returns how long the provider asked the client to wait, from Retry-After (seconds or an
// HTTP date) or x-ratelimit-reset (epoch seconds). The bool is false when there is no hint.
func Hint(h http.Header, now time.Time) (time.Duration, bool) {
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second, true
		}
		if t, err := http.ParseTime(v); err == nil {
			return max(0, t.Sub(now)), true
		}
	}
	if v := strings.TrimSpace(h.Get("X-RateLimit-Reset")); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			return max(0, time.Unix(epoch, 0).Sub(now)), true
		}
	}
	return 0, false
}

// Retryable reports whether resp is a rate limit answer: 429 and 503 always, and 403 only when
// it carries a wait hint or says the primary limit is used up, so a plain forbidden is final.
func Retryable(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	case http.StatusForbidden:
		if resp.Header.Get("Retry-After") != "" {
			return true
		}
		return resp.Header.Get("X-RateLimit-Remaining") == "0" && resp.Header.Get("X-RateLimit-Reset") != ""
	}
	return false
}

// Delay is the wait before attempt number attempt+1 (attempt starts at 0). A provider hint wins.
// Without one the wait is base*2^attempt with jitter, never above Max. The bool is false when the
// provider asked for a wait longer than Max, which the caller should report instead of sleeping.
func (p Policy) Delay(h http.Header, attempt int) (time.Duration, bool) {
	if d, ok := Hint(h, p.now()); ok {
		return d, d <= p.max()
	}
	d := p.base() << min(attempt, 30)
	if d <= 0 || d > p.max() {
		d = p.max()
	}
	// Half fixed, half random, so concurrent clients spread out without waiting too little.
	return d/2 + time.Duration(p.jitter()*float64(d/2)), true
}

// Do runs call until it returns something other than a rate limit answer, the attempts are used
// up, or the provider asks for more than Max. The last response is returned as is, with its body
// open. Bodies of responses that are retried are drained and closed.
func (p Policy) Do(ctx context.Context, call func(ctx context.Context) (*http.Response, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := call(ctx)
		if err != nil || !Retryable(resp) || attempt+1 >= p.attempts() {
			return resp, err
		}
		d, ok := p.Delay(resp.Header, attempt)
		if !ok {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
		if err := p.sleep(ctx, d); err != nil {
			return nil, err
		}
	}
}
