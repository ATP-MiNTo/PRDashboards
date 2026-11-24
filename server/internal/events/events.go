// Package events is the Server-Sent Events hub behind GET /events. The poller publishes, every
// connected browser tab receives. A client that does not keep up is dropped instead of slowing
// the others down.
package events

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"
)

// Defaults of Options.
const (
	DefaultHeartbeat    = 25 * time.Second
	DefaultBuffer       = 16
	DefaultWriteTimeout = 10 * time.Second
)

// Event names.
const (
	Snapshot   = "snapshot"
	SyncStatus = "sync-status"
	Activity   = "activity"
)

// Options tune the hub. Zero fields take the defaults.
type Options struct {
	Heartbeat    time.Duration
	Buffer       int
	WriteTimeout time.Duration
}

// Hub fans events out to subscribers. The last event of each name is kept and replayed to new
// subscribers, so a tab that connects late still learns the current state.
type Hub struct {
	opts Options

	mu     sync.Mutex
	subs   map[*Subscription]struct{}
	last   map[string][]byte
	closed bool
}

// NewHub creates a hub.
func NewHub(o Options) *Hub {
	if o.Heartbeat <= 0 {
		o.Heartbeat = DefaultHeartbeat
	}
	if o.Buffer <= 0 {
		o.Buffer = DefaultBuffer
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = DefaultWriteTimeout
	}
	return &Hub{opts: o, subs: map[*Subscription]struct{}{}, last: map[string][]byte{}}
}

// Subscription is one connected client. Frames arrive on C until Done is closed.
type Subscription struct {
	C    chan []byte
	done chan struct{}
	once sync.Once
	hub  *Hub
}

// Done is closed when the subscription ends: the client left, fell behind, or the hub closed.
func (s *Subscription) Done() <-chan struct{} { return s.done }

// Close unsubscribes.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	delete(s.hub.subs, s)
	s.hub.mu.Unlock()
	s.once.Do(func() { close(s.done) })
}

func frame(event string, data []byte) []byte {
	out := make([]byte, 0, len(event)+len(data)+16)
	out = append(out, "event: "...)
	out = append(out, event...)
	out = append(out, "\ndata: "...)
	out = append(out, data...)
	return append(out, '\n', '\n')
}

// Publish sends an event with a JSON payload to every subscriber. Subscribers whose buffer is full
// are dropped.
func (h *Hub) Publish(event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	f := frame(event, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.last[event] = f
	for s := range h.subs {
		select {
		case s.C <- f:
		default:
			delete(h.subs, s)
			s.once.Do(func() { close(s.done) })
		}
	}
	return nil
}

// Subscribe registers a client and queues the last event of every name for it.
func (h *Hub) Subscribe() *Subscription {
	s := &Subscription{C: make(chan []byte, max(h.opts.Buffer, len(h.last)+1)), done: make(chan struct{}), hub: h}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		s.once.Do(func() { close(s.done) })
		return s
	}
	names := make([]string, 0, len(h.last))
	for n := range h.last {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		s.C <- h.last[n]
	}
	h.subs[s] = struct{}{}
	return s
}

// Clients is the number of connected subscribers.
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Close ends every stream and refuses new ones. It lets the HTTP server shut down, which waits for
// open responses.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for s := range h.subs {
		delete(h.subs, s)
		s.once.Do(func() { close(s.done) })
	}
}

// ServeHTTP streams events as text/event-stream. A comment line is sent every heartbeat interval to
// keep proxies and the browser from closing an idle stream. Each write gets its own deadline
// through http.ResponseController, because the server has no WriteTimeout.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	write := func(b []byte) bool {
		if err := rc.SetWriteDeadline(time.Now().Add(h.opts.WriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return false
		}
		if _, err := w.Write(b); err != nil {
			return false
		}
		err := rc.Flush()
		return err == nil || errors.Is(err, http.ErrNotSupported)
	}

	sub := h.Subscribe()
	defer sub.Close()
	if !write([]byte(": connected\n\n")) {
		return
	}
	tick := time.NewTicker(h.opts.Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.Done():
			return
		case f := <-sub.C:
			if !write(f) {
				return
			}
		case <-tick.C:
			if !write([]byte(": heartbeat\n\n")) {
				return
			}
		}
	}
}
