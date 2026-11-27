package auth_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

var ctx = context.Background()

func newSessions(t *testing.T) (*auth.Sessions, *testutil.Clock) {
	t.Helper()
	clock := testutil.NewClock()
	return auth.NewSessions([]byte("0123456789abcdef0123456789abcdef"), clock.Now, &testutil.Rand{}, nil, nil), clock
}

func TestSessionRoundTrip(t *testing.T) {
	s, _ := newSessions(t)
	created, err := s.Create(ctx)
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
	created, _ := s.Create(ctx)
	c := created.Cookie()
	if c.Name != "prd_session" || c.Path != "/" || !c.HttpOnly || c.Secure ||
		c.SameSite != http.SameSiteStrictMode || c.MaxAge != 14*24*3600 {
		t.Fatalf("cookie %+v", c)
	}
	line := c.String()
	for _, want := range []string{"prd_session=", "HttpOnly", "SameSite=Strict", "Path=/", "Max-Age=1209600"} {
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
	other := auth.NewSessions([]byte("ffffffffffffffffffffffffffffffff"), testutil.NewClock().Now, &testutil.Rand{}, nil, nil)
	foreign, _ := other.Create(ctx)
	created, _ := s.Create(ctx)
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
	created, _ := s.Create(ctx)
	clock.Advance(auth.MaxSessionAge - time.Second)
	if _, ok := s.Authenticate(created.Value); !ok {
		t.Fatal("rejected before expiry")
	}
	clock.Advance(time.Second)
	if _, ok := s.Authenticate(created.Value); ok {
		t.Fatal("accepted after expiry")
	}
}

func TestSessionSlidingReissue(t *testing.T) {
	s, clock := newSessions(t)
	created, _ := s.Create(ctx)
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
	if want := created.Expires; !got.Expires.Equal(want) {
		t.Fatalf("expires %v, want the absolute limit %v", got.Expires, want)
	}
	if want := int((auth.MaxSessionAge - auth.ReissueAfter) / time.Second); got.Cookie().MaxAge != want {
		t.Fatalf("cookie max age %d, want the remaining %d", got.Cookie().MaxAge, want)
	}
	if got.Value != created.Value {
		t.Fatal("cookie value changed on reissue")
	}
	again, _ := s.Authenticate(created.Value)
	if again.Reissue {
		t.Fatal("reissued twice in a row")
	}
	clock.Advance(auth.MaxSessionAge - auth.ReissueAfter - time.Hour)
	if _, ok := s.Authenticate(created.Value); !ok {
		t.Fatal("session rejected before the absolute limit")
	}
}

func TestSessionAbsoluteLifetimeIgnoresActivity(t *testing.T) {
	s, clock := newSessions(t)
	created, _ := s.Create(ctx)
	for clock.Now().Before(created.Expires.Add(-2 * auth.ReissueAfter)) {
		clock.Advance(auth.ReissueAfter + time.Minute)
		if _, ok := s.Authenticate(created.Value); !ok {
			t.Fatalf("rejected at %v", clock.Now())
		}
	}
	clock.Advance(created.Expires.Sub(clock.Now()))
	if _, ok := s.Authenticate(created.Value); ok {
		t.Fatal("active session outlived the absolute limit")
	}
}

func TestSessionCookieMaxAgeNeverPassesTheAbsoluteLimit(t *testing.T) {
	s, clock := newSessions(t)
	created, _ := s.Create(ctx)
	clock.Advance(auth.MaxSessionAge - time.Hour - time.Minute)
	got, ok := s.Authenticate(created.Value)
	if !ok || !got.Reissue {
		t.Fatalf("expected a reissue, got %+v ok=%v", got, ok)
	}
	if got.Cookie().MaxAge != int(time.Hour/time.Second)+60 {
		t.Fatalf("max age %d", got.Cookie().MaxAge)
	}
}

func TestSessionPurgesExpired(t *testing.T) {
	s, clock := newSessions(t)
	old, _ := s.Create(ctx)
	clock.Advance(auth.MaxSessionAge + time.Hour)
	if _, err := s.Create(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate(old.Value); ok {
		t.Fatal("expired session still present")
	}
}

func TestSessionsAreDistinct(t *testing.T) {
	s, _ := newSessions(t)
	a, _ := s.Create(ctx)
	b, _ := s.Create(ctx)
	if a.Value == b.Value || a.CSRF == b.CSRF || a.Key == b.Key {
		t.Fatal("sessions share values")
	}
	if a.CheckCSRF(b.CSRF) {
		t.Fatal("CSRF token valid across sessions")
	}
}
