package mw

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	writeBurst   = 30
	sessionBurst = 10
	previewBurst = 120
)

type bucket struct {
	l    *rate.Limiter
	last time.Time
}

type buckets struct {
	limit rate.Limit
	burst int
	// refill is the time an idle bucket needs to be full again; an idle bucket past it is
	// indistinguishable from a new one and is dropped.
	refill time.Duration

	mu        sync.Mutex
	m         map[string]*bucket
	lastSweep time.Time
}

func newBuckets(perMinute int) *buckets {
	return &buckets{
		limit:  rate.Every(time.Minute / time.Duration(perMinute)),
		burst:  perMinute,
		refill: time.Minute,
		m:      make(map[string]*bucket),
	}
}

// take returns zero when the request is allowed, otherwise how long to wait.
func (b *buckets) take(key string, now time.Time) time.Duration {
	b.mu.Lock()
	b.sweep(now)
	e, ok := b.m[key]
	if !ok {
		e = &bucket{l: rate.NewLimiter(b.limit, b.burst)}
		b.m[key] = e
	}
	e.last = now
	b.mu.Unlock()
	res := e.l.ReserveN(now, 1)
	if d := res.DelayFrom(now); d > 0 {
		res.CancelAt(now)
		return d
	}
	return 0
}

// sweep drops the buckets idle for a full refill. It scans at most once per refill period, so the
// cost per request is amortized constant. The caller holds mu.
func (b *buckets) sweep(now time.Time) {
	if now.Sub(b.lastSweep) < b.refill {
		return
	}
	b.lastSweep = now
	for k, e := range b.m {
		if now.Sub(e.last) >= b.refill {
			delete(b.m, k)
		}
	}
}

func (b *buckets) size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.m)
}

// isPreview reports the rule preview endpoints: read-only computations over cached data that make
// no outside call, so they have a bucket of their own and do not spend the write budget.
func isPreview(r *http.Request) bool {
	return r.URL.Path == "/api/rules/preview" || r.URL.Path == "/api/rules/previews"
}

// RateLimit applies a token bucket of 30 writes per minute per session (one shared bucket for
// requests without a session), a stricter one of 10 per minute for session creation and a separate
// one of 120 per minute per session for the rule previews. Idle buckets are dropped.
func RateLimit(clock func() time.Time) func(http.Handler) http.Handler {
	writes := newBuckets(writeBurst)
	creates := newBuckets(sessionBurst)
	previews := newBuckets(previewBurst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !Protected(r) || !isWrite(r) {
				next.ServeHTTP(w, r)
				return
			}
			var wait time.Duration
			if isSessionCreate(r) {
				wait = creates.take("", clock())
			} else {
				key := ""
				if s, ok := SessionFrom(r.Context()); ok {
					key = s.Key
				}
				if isPreview(r) {
					wait = previews.take(key, clock())
				} else {
					wait = writes.take(key, clock())
				}
			}
			if wait > 0 {
				secs := max(1, int(math.Ceil(wait.Seconds())))
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				WriteProblem(w, http.StatusTooManyRequests, CodeRateLimited, "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
