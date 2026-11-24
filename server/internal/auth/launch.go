package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

const (
	// LaunchTokenTTL is how long a launch token stays valid.
	LaunchTokenTTL = 2 * time.Minute
	// LaunchRotateEvery is how often the token file is refreshed.
	LaunchRotateEvery = time.Minute

	tokenBytes = 32
	maxLive    = 2
)

type launchToken struct {
	value   string
	expires time.Time
}

// Launcher issues one-time launch tokens and keeps the newest one in a 0600 file so a second
// start of the binary can open the browser on the running instance.
type Launcher struct {
	path  string
	clock func() time.Time
	rand  io.Reader
	log   *slog.Logger
	every time.Duration

	mu   sync.Mutex
	live []launchToken
}

// NewLauncher creates a launcher and writes the first token to path.
func NewLauncher(path string, clock func() time.Time, rnd io.Reader, log *slog.Logger) (*Launcher, error) {
	l := &Launcher{path: path, clock: clock, rand: rnd, log: log, every: LaunchRotateEvery}
	if err := l.Rotate(); err != nil {
		return nil, err
	}
	return l, nil
}

// Current returns the newest token.
func (l *Launcher) Current() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.live[len(l.live)-1].value
}

// Rotate issues a new token and writes it to the token file. The previous token stays valid until
// it expires.
func (l *Launcher) Rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rotateLocked()
}

func (l *Launcher) rotateLocked() error {
	buf := make([]byte, tokenBytes)
	if _, err := io.ReadFull(l.rand, buf); err != nil {
		return err
	}
	now := l.clock()
	l.prune(now)
	t := launchToken{value: base64.RawURLEncoding.EncodeToString(buf), expires: now.Add(LaunchTokenTTL)}
	l.live = append(l.live, t)
	if len(l.live) > maxLive {
		l.live = l.live[len(l.live)-maxLive:]
	}
	return WriteFileAtomic(l.path, []byte(t.value))
}

func (l *Launcher) prune(now time.Time) {
	kept := l.live[:0]
	for _, t := range l.live {
		if now.Before(t.expires) {
			kept = append(kept, t)
		}
	}
	l.live = kept
}

// Consume reports whether candidate is a live launch token. A matching token is spent and a new
// one is issued.
func (l *Launcher) Consume(candidate string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(l.clock())
	match := -1
	for i, t := range l.live {
		if subtle.ConstantTimeCompare([]byte(t.value), []byte(candidate)) == 1 {
			match = i
		}
	}
	if match < 0 {
		return false
	}
	l.live = append(l.live[:match], l.live[match+1:]...)
	if err := l.rotateLocked(); err != nil {
		l.log.Error("rotate launch token", "err", err)
	}
	return true
}

// Run rotates the token on a fixed interval until ctx is done.
func (l *Launcher) Run(ctx context.Context) {
	t := time.NewTicker(l.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := l.Rotate(); err != nil {
				l.log.Error("rotate launch token", "err", err)
			}
		}
	}
}

// Close removes the token file.
func (l *Launcher) Close() error {
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SetInterval changes the rotation interval used by Run.
func (l *Launcher) SetInterval(d time.Duration) { l.every = d }
