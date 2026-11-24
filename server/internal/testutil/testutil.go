// Package testutil holds deterministic clock and random sources for tests.
package testutil

import (
	"sync"
	"time"
)

// Clock is a manually advanced clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock starts at a fixed instant.
func NewClock() *Clock {
	return &Clock{now: time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)}
}

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// Rand yields distinct bytes on every read: a counter spread over the output.
type Rand struct {
	mu sync.Mutex
	n  uint64
}

// Read implements io.Reader.
func (r *Rand) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	x := r.n*0x9e3779b97f4a7c15 + 1
	for i := range p {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		p[i] = byte(x)
	}
	return len(p), nil
}
