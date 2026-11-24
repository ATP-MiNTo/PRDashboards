package events_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/events"
)

func read(t *testing.T, ch <-chan []byte) string {
	t.Helper()
	select {
	case b := <-ch:
		return string(b)
	case <-time.After(2 * time.Second):
		t.Fatal("no frame")
		return ""
	}
}

func TestPublishReachesEverySubscriberAndLastEventsAreReplayed(t *testing.T) {
	h := events.NewHub(events.Options{})
	if err := h.Publish(events.SyncStatus, map[string]any{"running": false}); err != nil {
		t.Fatal(err)
	}
	if err := h.Publish(events.Snapshot, map[string]int{"version": 1}); err != nil {
		t.Fatal(err)
	}
	if err := h.Publish(events.Snapshot, map[string]int{"version": 2}); err != nil {
		t.Fatal(err)
	}
	a := h.Subscribe()
	defer a.Close()
	if got := read(t, a.C); got != "event: snapshot\ndata: {\"version\":2}\n\n" {
		t.Fatalf("replay snapshot: %q", got)
	}
	if got := read(t, a.C); got != "event: sync-status\ndata: {\"running\":false}\n\n" {
		t.Fatalf("replay status: %q", got)
	}
	b := h.Subscribe()
	defer b.Close()
	_, _ = read(t, b.C), read(t, b.C)
	if err := h.Publish(events.Snapshot, map[string]int{"version": 3}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*events.Subscription{a, b} {
		if got := read(t, s.C); !strings.Contains(got, `{"version":3}`) {
			t.Fatalf("%q", got)
		}
	}
	if h.Clients() != 2 {
		t.Fatalf("clients %d", h.Clients())
	}
}

func TestPublishRejectsUnencodablePayload(t *testing.T) {
	if err := events.NewHub(events.Options{}).Publish("x", make(chan int)); err == nil {
		t.Fatal("expected an encoding error")
	}
}

func TestSlowSubscriberIsDroppedAndOthersKeepReceiving(t *testing.T) {
	h := events.NewHub(events.Options{Buffer: 2})
	slow, fast := h.Subscribe(), h.Subscribe()
	defer fast.Close()
	for i := range 3 {
		_ = h.Publish(events.Snapshot, map[string]int{"version": i})
		_ = read(t, fast.C)
	}
	select {
	case <-slow.Done():
	default:
		t.Fatal("the subscriber that never reads must be dropped")
	}
	select {
	case <-fast.Done():
		t.Fatal("a reading subscriber must stay")
	default:
	}
	if h.Clients() != 1 {
		t.Fatalf("clients %d", h.Clients())
	}
	slow.Close()
}

func TestCloseEndsStreamsAndRefusesNewOnes(t *testing.T) {
	h := events.NewHub(events.Options{})
	s := h.Subscribe()
	h.Close()
	select {
	case <-s.Done():
	default:
		t.Fatal("close must end subscriptions")
	}
	late := h.Subscribe()
	select {
	case <-late.Done():
	default:
		t.Fatal("a closed hub hands out finished subscriptions")
	}
}

func TestServeHTTPStreamsHeadersEventsAndHeartbeat(t *testing.T) {
	h := events.NewHub(events.Options{Heartbeat: 20 * time.Millisecond})
	_ = h.Publish(events.Snapshot, map[string]int{"version": 5})
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", resp.StatusCode, resp.Header)
	}
	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if strings.Contains(strings.Join(lines, "\n"), ": heartbeat") {
			break
		}
	}
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "event: snapshot\ndata: {\"version\":5}") || !strings.Contains(got, ": heartbeat") {
		t.Fatalf("stream %q", got)
	}

	waitClients(t, h, 1)
	_ = h.Publish(events.SyncStatus, map[string]bool{"running": true})
	for sc.Scan() {
		if sc.Text() == `data: {"running":true}` {
			break
		}
	}
	cancel()
	waitClients(t, h, 0)
}

func waitClients(t *testing.T, h *events.Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for h.Clients() != n {
		if time.Now().After(deadline) {
			t.Fatalf("clients %d, want %d", h.Clients(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestServeHTTPEndsWhenHubCloses(t *testing.T) {
	h := events.NewHub(events.Options{})
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	waitClients(t, h, 1)
	h.Close()
	done := make(chan struct{})
	go func() {
		_, _ = bufio.NewReader(resp.Body).ReadString(0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream still open after Close")
	}
}

type failingWriter struct {
	httptest.ResponseRecorder
}

func (f *failingWriter) Write([]byte) (int, error) { return 0, http.ErrHandlerTimeout }

func TestServeHTTPStopsOnWriteError(t *testing.T) {
	h := events.NewHub(events.Options{})
	w := &failingWriter{ResponseRecorder: *httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/events", nil))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler must return when the connection is gone")
	}
}
