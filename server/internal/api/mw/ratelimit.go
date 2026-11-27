package mw

import (
	"context"
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

func (b *buckets) limiter(key string) *rate.Limiter {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, ok := b.m[key]
	if !ok {
		l = rate.NewLimiter(b.limit, b.burst)
		b.m[key] = l
	}
	return l
}

// reserve returns zero when a token is available, otherwise how long to wait. The token is
// spent only when spend is true.
func (b *buckets) reserve(key string, now time.Time, spend bool) time.Duration {
	res := b.limiter(key).ReserveN(now, 1)
	d := res.DelayFrom(now)
	if d > 0 || !spend {
		res.CancelAt(now)
	}
	return d
}

func (b *buckets) take(key string, now time.Time) time.Duration { return b.reserve(key, now, true) }

func (b *buckets) peek(key string, now time.Time) time.Duration { return b.reserve(key, now, false) }

// RateLimit applies a token bucket of 30 writes per minute per session (one shared bucket for
// requests without a session) and a stricter one of 10 per minute for session creation. Only
// failed launches spend a session-creation token, through ChargeLaunchFailure; a request is
// refused while the bucket is empty.
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
				wait = creates.peek("", clock())
				if wait == 0 {
					r = r.WithContext(context.WithValue(r.Context(), chargeKey{}, func() { creates.take("", clock()) }))
				}
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

type chargeKey struct{}

// ChargeLaunchFailure spends one session-creation token. The handler calls it after a launch
// token is refused, so only failed attempts are limited.
func ChargeLaunchFailure(ctx context.Context) {
	if f, ok := ctx.Value(chargeKey{}).(func()); ok {
		f()
	}
}
