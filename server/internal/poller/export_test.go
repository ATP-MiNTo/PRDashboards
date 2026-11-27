package poller

import (
	"context"
	"time"
)

// Loop is a poller whose timer is replaced, so tests drive Run without waiting.
type Loop struct{ p *Poller }

// NewLoopForTest returns a Run loop for p that waits through after.
func NewLoopForTest(p *Poller, after func(time.Duration) <-chan time.Time) *Loop {
	p.cfg.After = after
	return &Loop{p: p}
}

// Run runs the poller loop.
func (l *Loop) Run(ctx context.Context) { l.p.Run(ctx) }

// RateLimitUsage exposes the mapping of the GitHub budget to the snapshot field.
var RateLimitUsage = rateLimitUsage
