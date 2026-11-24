package auth_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func newLauncher(t *testing.T) (*auth.Launcher, *testutil.Clock, string) {
	t.Helper()
	clock := testutil.NewClock()
	path := filepath.Join(t.TempDir(), "launch.token")
	l, err := auth.NewLauncher(path, clock.Now, &testutil.Rand{}, discard)
	if err != nil {
		t.Fatal(err)
	}
	return l, clock, path
}

func TestLauncherWritesTokenFile(t *testing.T) {
	l, _, path := newLauncher(t)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != l.Current() {
		t.Fatalf("file %q, current %q", got, l.Current())
	}
	if len(l.Current()) != 43 {
		t.Fatalf("token length %d, want 43 (32 bytes base64url)", len(l.Current()))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestLauncherSingleUse(t *testing.T) {
	l, _, _ := newLauncher(t)
	tok := l.Current()
	if !l.Consume(tok) {
		t.Fatal("first use rejected")
	}
	if l.Consume(tok) {
		t.Fatal("second use accepted")
	}
	if l.Current() == tok {
		t.Fatal("token not rotated after use")
	}
}

func TestLauncherRejects(t *testing.T) {
	l, _, _ := newLauncher(t)
	for _, c := range []string{"", "nope", l.Current() + "x", l.Current()[1:]} {
		if l.Consume(c) {
			t.Fatalf("accepted %q", c)
		}
	}
	if !l.Consume(l.Current()) {
		t.Fatal("valid token rejected after failed attempts")
	}
}

func TestLauncherExpiry(t *testing.T) {
	l, clock, _ := newLauncher(t)
	tok := l.Current()
	clock.Advance(auth.LaunchTokenTTL - time.Second)
	if !l.Consume(tok) {
		t.Fatal("token rejected before expiry")
	}
	l2, clock2, _ := newLauncher(t)
	tok2 := l2.Current()
	clock2.Advance(auth.LaunchTokenTTL)
	if l2.Consume(tok2) {
		t.Fatal("expired token accepted")
	}
}

func TestLauncherPreviousTokenAccepted(t *testing.T) {
	l, clock, path := newLauncher(t)
	first := l.Current()
	clock.Advance(auth.LaunchRotateEvery)
	if err := l.Rotate(); err != nil {
		t.Fatal(err)
	}
	second := l.Current()
	if first == second {
		t.Fatal("rotate kept the same token")
	}
	if got, _ := os.ReadFile(path); string(got) != second {
		t.Fatal("file does not hold the newest token")
	}
	if !l.Consume(first) {
		t.Fatal("previous token rejected while unexpired")
	}
	if !l.Consume(second) {
		t.Fatal("current token rejected after previous was spent")
	}
}

func TestLauncherKeepsOnlyTwoTokens(t *testing.T) {
	l, clock, _ := newLauncher(t)
	first := l.Current()
	for range 2 {
		clock.Advance(time.Second)
		if err := l.Rotate(); err != nil {
			t.Fatal(err)
		}
	}
	if l.Consume(first) {
		t.Fatal("third-newest token accepted")
	}
}

func TestLauncherPreviousExpires(t *testing.T) {
	l, clock, _ := newLauncher(t)
	first := l.Current()
	clock.Advance(auth.LaunchRotateEvery)
	_ = l.Rotate()
	clock.Advance(auth.LaunchRotateEvery)
	if l.Consume(first) {
		t.Fatal("expired previous token accepted")
	}
}

func TestLauncherRunRotates(t *testing.T) {
	l, _, _ := newLauncher(t)
	l.SetInterval(5 * time.Millisecond)
	first := l.Current()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for l.Current() == first && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done
	if l.Current() == first {
		t.Fatal("token never rotated")
	}
}

func TestLauncherClose(t *testing.T) {
	l, _, path := newLauncher(t)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token file still present: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestNewLauncherUnwritable(t *testing.T) {
	_, err := auth.NewLauncher(filepath.Join(t.TempDir(), "missing", "launch.token"), testutil.NewClock().Now, &testutil.Rand{}, discard)
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}
