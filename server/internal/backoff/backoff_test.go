package backoff_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

func resp(status int, kv ...string) *http.Response {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader("x"))}
}

type rig struct {
	p      backoff.Policy
	clock  *testutil.Clock
	sleeps []time.Duration
}

func newRig(jitter float64) *rig {
	r := &rig{clock: testutil.NewClock()}
	r.p = backoff.Policy{
		Clock:  r.clock.Now,
		Jitter: func() float64 { return jitter },
		Sleep: func(_ context.Context, d time.Duration) error {
			r.sleeps = append(r.sleeps, d)
			r.clock.Advance(d)
			return nil
		},
	}
	return r
}

func TestHint(t *testing.T) {
	now := testutil.NewClock().Now()
	cases := []struct {
		name string
		h    map[string]string
		want time.Duration
		ok   bool
	}{
		{"none", nil, 0, false},
		{"seconds", map[string]string{"Retry-After": "17"}, 17 * time.Second, true},
		{"http date", map[string]string{"Retry-After": now.Add(90 * time.Second).UTC().Format(http.TimeFormat)}, 90 * time.Second, true},
		{"past date", map[string]string{"Retry-After": now.Add(-time.Minute).UTC().Format(http.TimeFormat)}, 0, true},
		{"reset epoch", map[string]string{"X-RateLimit-Reset": strconv.FormatInt(now.Add(45*time.Second).Unix(), 10)}, 45 * time.Second, true},
		{"past reset", map[string]string{"X-RateLimit-Reset": strconv.FormatInt(now.Add(-time.Hour).Unix(), 10)}, 0, true},
		{"retry-after beats reset", map[string]string{"Retry-After": "3", "X-RateLimit-Reset": strconv.FormatInt(now.Add(time.Hour).Unix(), 10)}, 3 * time.Second, true},
		{"junk", map[string]string{"Retry-After": "soon", "X-RateLimit-Reset": "later"}, 0, false},
		{"negative", map[string]string{"Retry-After": "-5"}, 0, false},
	}
	for _, c := range cases {
		h := http.Header{}
		for k, v := range c.h {
			h.Set(k, v)
		}
		got, ok := backoff.Hint(h, now)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %v %v, want %v %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestRetryable(t *testing.T) {
	cases := []struct {
		name string
		r    *http.Response
		want bool
	}{
		{"200", resp(200), false},
		{"429", resp(429), true},
		{"503", resp(503), true},
		{"500", resp(500), false},
		{"plain 403", resp(403), false},
		{"403 retry-after", resp(403, "Retry-After", "10"), true},
		{"403 primary limit", resp(403, "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "1"), true},
		{"403 remaining but no reset", resp(403, "X-RateLimit-Remaining", "0"), false},
		{"403 remaining left", resp(403, "X-RateLimit-Remaining", "5", "X-RateLimit-Reset", "1"), false},
	}
	for _, c := range cases {
		if got := backoff.Retryable(c.r); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestDelayExponentialWithJitterAndCap(t *testing.T) {
	r := newRig(0)
	var got []time.Duration
	for i := range 10 {
		d, ok := r.p.Delay(http.Header{}, i)
		if !ok {
			t.Fatal("no delay")
		}
		got = append(got, d)
	}
	want := []time.Duration{1, 2, 4, 8, 16, 32, 64, 128, 150, 150}
	for i := range want {
		wantD := want[i] * time.Second // from attempt 8 the 5 minute cap applies, then half of it
		if got[i] != wantD {
			t.Errorf("attempt %d: %v, want %v", i, got[i], wantD)
		}
	}
	full := newRig(0.999999)
	if d, _ := full.p.Delay(http.Header{}, 0); d < 1990*time.Millisecond || d > 2*time.Second {
		t.Errorf("full jitter delay %v", d)
	}
	if d, _ := full.p.Delay(http.Header{}, 100); d > 5*time.Minute {
		t.Errorf("not capped: %v", d)
	}
}

func TestDelayHintOverMaxIsNotWaited(t *testing.T) {
	r := newRig(0)
	if d, ok := r.p.Delay(http.Header{"Retry-After": {"60"}}, 3); !ok || d != time.Minute {
		t.Fatalf("%v %v", d, ok)
	}
	if _, ok := r.p.Delay(http.Header{"Retry-After": {"3600"}}, 0); ok {
		t.Fatal("an hour wait must be reported, not slept")
	}
}

func TestDoRetriesAndHonoursHint(t *testing.T) {
	r := newRig(0)
	calls := 0
	got, err := r.p.Do(context.Background(), func(context.Context) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return resp(429, "Retry-After", "7"), nil
		case 2:
			return resp(503), nil
		}
		return resp(200), nil
	})
	if err != nil || got.StatusCode != 200 || calls != 3 {
		t.Fatalf("%v %v calls=%d", got, err, calls)
	}
	if len(r.sleeps) != 2 || r.sleeps[0] != 7*time.Second || r.sleeps[1] != 2*time.Second {
		t.Fatalf("sleeps %v", r.sleeps)
	}
}

func TestDoResetHeader(t *testing.T) {
	r := newRig(0)
	reset := strconv.FormatInt(r.clock.Now().Add(30*time.Second).Unix(), 10)
	n := 0
	_, _ = r.p.Do(context.Background(), func(context.Context) (*http.Response, error) {
		n++
		if n == 1 {
			return resp(403, "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", reset), nil
		}
		return resp(200), nil
	})
	if len(r.sleeps) != 1 || r.sleeps[0] != 30*time.Second {
		t.Fatalf("sleeps %v", r.sleeps)
	}
}

func TestDoStopsAfterAttempts(t *testing.T) {
	r := newRig(0)
	r.p.Attempts = 3
	calls := 0
	got, err := r.p.Do(context.Background(), func(context.Context) (*http.Response, error) {
		calls++
		return resp(429), nil
	})
	if err != nil || got.StatusCode != 429 || calls != 3 || len(r.sleeps) != 2 {
		t.Fatalf("%v %v calls=%d sleeps=%v", got, err, calls, r.sleeps)
	}
	if b, _ := io.ReadAll(got.Body); string(b) != "x" {
		t.Fatal("last response body must stay readable")
	}
}

func TestDoDoesNotRetryFinalAnswers(t *testing.T) {
	for _, status := range []int{200, 401, 403, 404, 500} {
		r := newRig(0)
		calls := 0
		got, _ := r.p.Do(context.Background(), func(context.Context) (*http.Response, error) {
			calls++
			return resp(status), nil
		})
		if got.StatusCode != status || calls != 1 || len(r.sleeps) != 0 {
			t.Errorf("status %d: calls=%d sleeps=%v", status, calls, r.sleeps)
		}
	}
}

func TestDoReturnsLongHintWithoutSleeping(t *testing.T) {
	r := newRig(0)
	got, err := r.p.Do(context.Background(), func(context.Context) (*http.Response, error) {
		return resp(429, "Retry-After", "7200"), nil
	})
	if err != nil || got.StatusCode != 429 || len(r.sleeps) != 0 {
		t.Fatalf("%v %v %v", got, err, r.sleeps)
	}
}

func TestDoPropagatesErrors(t *testing.T) {
	r := newRig(0)
	boom := errors.New("boom")
	if _, err := r.p.Do(context.Background(), func(context.Context) (*http.Response, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	r.p.Sleep = func(context.Context, time.Duration) error { return context.Canceled }
	if _, err := r.p.Do(context.Background(), func(context.Context) (*http.Response, error) { return resp(429), nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDefaultSleepHonoursContext(t *testing.T) {
	p := backoff.Policy{Base: time.Hour, Jitter: func() float64 { return 0 }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Do(ctx, func(context.Context) (*http.Response, error) { return resp(503), nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestDefaultSleepAndClock(t *testing.T) {
	p := backoff.Policy{Base: time.Millisecond, Max: 2 * time.Millisecond, Attempts: 2}
	n := 0
	got, err := p.Do(context.Background(), func(context.Context) (*http.Response, error) {
		n++
		if n == 1 {
			return resp(503), nil
		}
		return resp(200), nil
	})
	if err != nil || got.StatusCode != 200 {
		t.Fatalf("%v %v", got, err)
	}
	if _, ok := backoff.Hint(http.Header{"Retry-After": {"0"}}, time.Now()); !ok {
		t.Fatal("zero is a valid hint")
	}
}
