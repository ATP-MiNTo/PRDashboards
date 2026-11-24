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
)

type buckets struct {
	limit rate.Limit
	burst int

	mu sync.Mutex
	m  map[string]*rate.Limiter
}

func newBuckets(perMinute int) *buckets {
	return &buckets{
		limit: rate.Every(time.Minute / time.Duration(perMinute)),
		burst: perMinute,
		m:     make(map[string]*rate.Limiter),
	}
}

// take returns zero when the request is allowed, otherwise how long to wait.
func (b *buckets) take(key string, now time.Time) time.Duration {
	b.mu.Lock()
	l, ok := b.m[key]
	if !ok {
		l = rate.NewLimiter(b.limit, b.burst)
		b.m[key] = l
	}
	b.mu.Unlock()
	res := l.ReserveN(now, 1)
	if d := res.DelayFrom(now); d > 0 {
		res.CancelAt(now)
		return d
	}
	return 0
}

// RateLimit applies a token bucket of 30 writes per minute per session (one shared bucket for
// requests without a session) and a stricter one of 10 per minute for session creation.
func RateLimit(clock func() time.Time) func(http.Handler) http.Handler {
	writes := newBuckets(writeBurst)
	creates := newBuckets(sessionBurst)
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
				wait = writes.take(key, clock())
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
