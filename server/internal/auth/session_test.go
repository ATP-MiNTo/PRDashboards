package auth_test

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

func newSessions(t *testing.T) (*auth.Sessions, *testutil.Clock) {
	t.Helper()
	clock := testutil.NewClock()
	return auth.NewSessions([]byte("0123456789abcdef0123456789abcdef"), clock.Now, &testutil.Rand{}), clock
}

func TestSessionRoundTrip(t *testing.T) {
	s, _ := newSessions(t)
	created, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s.Authenticate(created.Value)
	if !ok {
		t.Fatal("fresh session rejected")
	}
	if got.Key != created.Key || got.CSRF != created.CSRF || got.Reissue {
		t.Fatalf("unexpected session %+v", got)
	}
	if !got.CheckCSRF(created.CSRF) || got.CheckCSRF("x") || got.CheckCSRF("") {
		t.Fatal("CSRF comparison wrong")
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	s, _ := newSessions(t)
	created, _ := s.Create()
	c := created.Cookie()
	if c.Name != "prd_session" || c.Path != "/" || !c.HttpOnly || c.Secure ||
		c.SameSite != http.SameSiteStrictMode || c.MaxAge != 30*24*3600 {
		t.Fatalf("cookie %+v", c)
	}
	line := c.String()
	for _, want := range []string{"prd_session=", "HttpOnly", "SameSite=Strict", "Path=/", "Max-Age=2592000"} {
		if !strings.Contains(line, want) {
			t.Fatalf("%q missing from %q", want, line)
		}
	}
	if strings.Contains(line, "Secure") {
		t.Fatalf("Secure set on loopback http cookie: %q", line)
	}
}

func TestSessionRejectsForgeries(t *testing.T) {
	s, _ := newSessions(t)
	other := auth.NewSessions([]byte("ffffffffffffffffffffffffffffffff"), testutil.NewClock().Now, &testutil.Rand{})
	foreign, _ := other.Create()
	created, _ := s.Create()
	id, sig, _ := strings.Cut(created.Value, ".")
	cases := map[string]string{
		"empty":        "",
		"no dot":       id,
		"bad base64":   "!!!.???",
		"empty id":     "." + sig,
		"wrong sig":    id + "." + strings.Repeat("A", len(sig)),
		"foreign key":  foreign.Value,
		"truncated":    created.Value[:len(created.Value)-2],
		"unknown id":   "AAAA." + sig,
		"extra suffix": created.Value + ".x",
	}
	for name, v := range cases {
		if _, ok := s.Authenticate(v); ok {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSessionExpiry(t *testing.T) {
	s, clock := newSessions(t)
	created, _ := s.Create()
	clock.Advance(auth.SessionTTL - time.Second)
	if _, ok := s.Authenticate(created.Value); !ok {
		t.Fatal("rejected before expiry")
	}
	clock.Advance(auth.SessionTTL)
	if _, ok := s.Authenticate(created.Value); ok {
		t.Fatal("accepted after expiry")
	}
}

func TestSessionSlidingReissue(t *testing.T) {
	s, clock := newSessions(t)
	created, _ := s.Create()
	clock.Advance(auth.ReissueAfter - time.Minute)
	got, _ := s.Authenticate(created.Value)
	if got.Reissue {
		t.Fatal("reissued too early")
	}
	clock.Advance(time.Minute)
	got, ok := s.Authenticate(created.Value)
	if !ok || !got.Reissue {
		t.Fatalf("expected reissue, got %+v ok=%v", got, ok)
	}
	if want := clock.Now().Add(auth.SessionTTL); !got.Expires.Equal(want) {
		t.Fatalf("expires %v, want %v", got.Expires, want)
	}
	if got.Value != created.Value {
		t.Fatal("cookie value changed on reissue")
	}
	again, _ := s.Authenticate(created.Value)
	if again.Reissue {
		t.Fatal("reissued twice in a row")
	}
	clock.Advance(auth.SessionTTL - time.Hour)
	if _, ok := s.Authenticate(created.Value); !ok {
		t.Fatal("session did not slide")
	}
}

func TestSessionPurgesExpired(t *testing.T) {
	s, clock := newSessions(t)
	old, _ := s.Create()
	clock.Advance(auth.SessionTTL + time.Hour)
	if _, err := s.Create(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate(old.Value); ok {
		t.Fatal("expired session still present")
	}
}

func TestSessionsAreDistinct(t *testing.T) {
	s, _ := newSessions(t)
	a, _ := s.Create()
	b, _ := s.Create()
	if a.Value == b.Value || a.CSRF == b.CSRF || a.Key == b.Key {
		t.Fatal("sessions share values")
	}
	if a.CheckCSRF(b.CSRF) {
		t.Fatal("CSRF token valid across sessions")
	}
}

func TestLoadOrCreateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.key")
	key, err := auth.LoadOrCreateKey(path, &testutil.Rand{})
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("key length %d", len(key))
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode().Perm())
		}
	}
	again, err := auth.LoadOrCreateKey(path, &testutil.Rand{})
	if err != nil || string(again) != string(key) {
		t.Fatalf("key not stable: %v", err)
	}
}

func TestLoadOrCreateKeyBadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.LoadOrCreateKey(path, &testutil.Rand{}); err == nil {
		t.Fatal("expected error for wrong key size")
	}
	if _, err := auth.LoadOrCreateKey(t.TempDir(), &testutil.Rand{}); err == nil {
		t.Fatal("expected error when the key path is a directory")
	}
}
