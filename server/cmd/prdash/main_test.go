package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

type instance struct {
	opts   options
	env    env
	out    *syncBuffer
	errOut *syncBuffer
	opened chan string
	done   chan error
	cancel context.CancelFunc
}

func testEnv(out, errOut *syncBuffer, opened chan string) env {
	clock := testutil.NewClock()
	return env{
		stdout: out,
		stderr: errOut,
		open: func(url string) error {
			opened <- url
			return nil
		},
		clock:   clock.Now,
		rand:    &testutil.Rand{},
		version: "test",
	}
}

func start(t *testing.T, dir string, port int, noBrowser bool) *instance {
	t.Helper()
	in := &instance{
		opts:   options{port: port, dataDir: dir, noBrowser: noBrowser},
		out:    &syncBuffer{},
		errOut: &syncBuffer{},
		opened: make(chan string, 4),
		done:   make(chan error, 1),
	}
	in.env = testEnv(in.out, in.errOut, in.opened)
	ctx, cancel := context.WithCancel(context.Background())
	in.cancel = cancel
	go func() { in.done <- run(ctx, in.opts, in.env) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-in.done:
		case <-time.After(5 * time.Second):
			t.Error("instance did not stop")
		}
	})
	waitFor(t, "server to listen", func() bool { return strings.Contains(in.out.String(), `"msg":"listening"`) })
	return in
}

func (in *instance) stop(t *testing.T) error {
	t.Helper()
	in.cancel()
	select {
	case err := <-in.done:
		in.done <- err
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("instance did not stop")
		return nil
	}
}

func newDataDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestParseFlags(t *testing.T) {
	o, err := parseFlags(nil, io.Discard)
	if err != nil || o.port != 4317 || o.dataDir != "~/.prdashboards" || o.noBrowser {
		t.Fatalf("defaults: %+v %v", o, err)
	}
	o, err = parseFlags([]string{"--port", "5000", "--data-dir", "/x", "--no-browser"}, io.Discard)
	if err != nil || o.port != 5000 || o.dataDir != "/x" || !o.noBrowser {
		t.Fatalf("flags: %+v %v", o, err)
	}
	for _, args := range [][]string{{"--port", "0"}, {"--port", "70000"}, {"--port", "x"}, {"extra"}, {"--nope"}} {
		if _, err := parseFlags(args, io.Discard); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if _, err := parseFlags([]string{"-h"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h: %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	for in, want := range map[string]string{
		"~":                 home,
		"~/.prdashboards":   filepath.Join(home, ".prdashboards"),
		"/abs/path":         "/abs/path",
		"~other/not-a-home": "",
	} {
		got, err := expandHome(in)
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if !filepath.IsAbs(got) {
				t.Errorf("%s: %s not absolute", in, got)
			}
			continue
		}
		if runtime.GOOS != "windows" && got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestPrepareDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := prepareDataDir(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("not created: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v, want 0700", info.Mode().Perm())
	}
	if err := prepareDataDir(dir); err != nil {
		t.Fatalf("existing dir rejected: %v", err)
	}
}

func TestPrepareDataDirRefusesLooseModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes only")
	}
	dir := newDataDir(t)
	for _, mode := range []os.FileMode{0o770, 0o707, 0o777, 0o720, 0o702} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		err := prepareDataDir(dir)
		if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
			t.Errorf("mode %04o: err %v", mode, err)
		}
	}
	for _, mode := range []os.FileMode{0o700, 0o750, 0o755} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if err := prepareDataDir(dir); err != nil {
			t.Errorf("mode %04o rejected: %v", mode, err)
		}
	}
}

func TestPrepareDataDirNotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareDataDir(f); err == nil {
		t.Fatal("file accepted as data dir")
	}
	if err := prepareDataDir(filepath.Join(f, "sub")); err == nil {
		t.Fatal("dir under a file accepted")
	}
}

func TestBrowserCommand(t *testing.T) {
	const url = "http://127.0.0.1:4317/auth/launch#t=abc"
	for goos, want := range map[string][]string{
		"darwin":  {"open", url},
		"linux":   {"xdg-open", url},
		"windows": {"rundll32", "url.dll,FileProtocolHandler", url},
	} {
		cmd, err := browserCommand(goos, url)
		if err != nil {
			t.Fatal(err)
		}
		if got := cmd.Args; fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s: args %v, want %v", goos, got, want)
		}
	}
	if _, err := browserCommand("plan9", url); err == nil {
		t.Fatal("unsupported OS accepted")
	}
}

func TestOpenBrowserMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if runtime.GOOS == "windows" {
		t.Skip("rundll32 is resolved from the system directory")
	}
	if err := openBrowser("http://127.0.0.1:1/"); err == nil {
		t.Fatal("expected an error when the opener is missing")
	}
}

func TestRunServesAndCleansUp(t *testing.T) {
	dir := newDataDir(t)
	port := freePort(t)
	in := start(t, dir, port, false)

	if !strings.Contains(in.out.String(), fmt.Sprintf(`"addr":"127.0.0.1:%d"`, port)) {
		t.Fatalf("not bound to 127.0.0.1:%d: %s", port, in.out.String())
	}
	if !strings.Contains(in.out.String(), "OAuth callbacks need port 4317") {
		t.Fatalf("no warning for non-default port: %s", in.out.String())
	}
	tokenBytes, err := os.ReadFile(filepath.Join(dir, "launch.token"))
	if err != nil {
		t.Fatal(err)
	}
	token := string(tokenBytes)
	if want := launchURL(port, token); <-in.opened != want {
		t.Fatalf("browser opened on the wrong URL, want %s", want)
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{"launch.token", "session.key", "prdash.lock"} {
			info, err := os.Stat(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s mode %v", name, info.Mode().Perm())
			}
		}
	}

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	req, _ := http.NewRequest(http.MethodGet, base+"/api/session", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("status %d, CSP %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}

	body := strings.NewReader(`{"launchToken":"` + token + `"}`)
	req, _ = http.NewRequest(http.MethodPost, base+"/api/session", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || len(resp.Cookies()) != 1 {
		t.Fatalf("launch exchange: %d, cookies %v", resp.StatusCode, resp.Cookies())
	}
	if rotated, _ := os.ReadFile(filepath.Join(dir, "launch.token")); string(rotated) == token {
		t.Fatal("launch token file not rotated after use")
	}

	if _, err := net.DialTimeout("tcp", fmt.Sprintf("[::1]:%d", port), 200*time.Millisecond); err == nil {
		t.Fatal("reachable on ::1")
	}
	assertNotReachableOnOtherAddrs(t, port)

	if strings.Contains(in.out.String(), token) {
		t.Fatal("launch token present in logs")
	}
	if err := in.stop(t); err != nil {
		t.Fatalf("run returned %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "launch.token")); !os.IsNotExist(err) {
		t.Fatalf("launch.token not removed: %v", err)
	}
	lock := flock.New(filepath.Join(dir, "prdash.lock"))
	if ok, err := lock.TryLock(); err != nil || !ok {
		t.Fatalf("lock not released: %v %v", ok, err)
	}
	_ = lock.Unlock()
	if _, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
		t.Fatal("still listening after shutdown")
	}
}

func assertNotReachableOnOtherAddrs(t *testing.T, port int) {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil {
			continue
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipn.IP.String(), fmt.Sprint(port)), 200*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Errorf("reachable on %s", ipn.IP)
		}
	}
}

func TestSecondInstanceOpensRunningOne(t *testing.T) {
	dir := newDataDir(t)
	port := freePort(t)
	first := start(t, dir, port, true)
	if !strings.Contains(first.errOut.String(), "/auth/launch#t=") {
		t.Fatalf("--no-browser did not print the URL to stderr: %q", first.errOut.String())
	}
	if len(first.opened) != 0 {
		t.Fatal("browser opened despite --no-browser")
	}
	if strings.Contains(first.out.String(), "#t=") {
		t.Fatal("launch URL written to the log")
	}

	token, err := os.ReadFile(filepath.Join(dir, "launch.token"))
	if err != nil {
		t.Fatal(err)
	}
	second := testEnv(&syncBuffer{}, &syncBuffer{}, make(chan string, 1))
	opened := make(chan string, 1)
	second.open = func(url string) error { opened <- url; return nil }
	if err := run(context.Background(), options{port: port, dataDir: dir}, second); err != nil {
		t.Fatalf("second instance: %v", err)
	}
	select {
	case got := <-opened:
		if want := launchURL(port, string(token)); got != want {
			t.Fatalf("opened %s, want %s", got, want)
		}
	default:
		t.Fatal("second instance did not open the browser")
	}
	if err := first.stop(t); err != nil {
		t.Fatal(err)
	}
}

func TestSecondInstanceNoBrowserPrintsURL(t *testing.T) {
	dir := newDataDir(t)
	port := freePort(t)
	start(t, dir, port, true)
	errOut := &syncBuffer{}
	e := testEnv(&syncBuffer{}, errOut, make(chan string, 1))
	e.open = func(string) error { t.Error("browser opened"); return nil }
	if err := run(context.Background(), options{port: port, dataDir: dir, noBrowser: true}, e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "/auth/launch#t=") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestSecondInstanceOpenFailureIsReported(t *testing.T) {
	dir := newDataDir(t)
	port := freePort(t)
	start(t, dir, port, true)
	errOut := &syncBuffer{}
	e := testEnv(&syncBuffer{}, errOut, nil)
	e.open = func(string) error { return errors.New("no display") }
	if err := run(context.Background(), options{port: port, dataDir: dir}, e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "no display") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestSecondInstanceWithoutToken(t *testing.T) {
	dir := newDataDir(t)
	lock := flock.New(filepath.Join(dir, "prdash.lock"))
	if ok, err := lock.TryLock(); err != nil || !ok {
		t.Fatal(err)
	}
	defer lock.Unlock()
	err := run(context.Background(), options{port: freePort(t), dataDir: dir}, testEnv(&syncBuffer{}, &syncBuffer{}, nil))
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err %v", err)
	}
}

func TestRunPortBusy(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	dir := newDataDir(t)
	err = run(context.Background(), options{port: port, dataDir: dir}, testEnv(&syncBuffer{}, &syncBuffer{}, nil))
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "launch.token")); !os.IsNotExist(err) {
		t.Fatal("launch token written although the port is busy")
	}
}

func TestRunRefusesLooseDataDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes only")
	}
	dir := newDataDir(t)
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), options{port: freePort(t), dataDir: dir}, testEnv(&syncBuffer{}, &syncBuffer{}, nil))
	if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prdash.lock")); !os.IsNotExist(err) {
		t.Fatal("lock created in a refused directory")
	}
}

func TestRunBrowserFailureDoesNotStop(t *testing.T) {
	dir := newDataDir(t)
	port := freePort(t)
	errOut := &syncBuffer{}
	out := &syncBuffer{}
	e := testEnv(out, errOut, nil)
	e.open = func(string) error { return errors.New("no display") }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, options{port: port, dataDir: dir}, e) }()
	waitFor(t, "browser failure message", func() bool { return strings.Contains(errOut.String(), "no display") })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRunReusesSessionKey(t *testing.T) {
	dir := newDataDir(t)
	first := start(t, dir, freePort(t), true)
	key1, err := os.ReadFile(filepath.Join(dir, "session.key"))
	if err != nil || len(key1) != 32 {
		t.Fatalf("key: %v len %d", err, len(key1))
	}
	if err := first.stop(t); err != nil {
		t.Fatal(err)
	}
	start(t, dir, freePort(t), true)
	key2, _ := os.ReadFile(filepath.Join(dir, "session.key"))
	if !bytes.Equal(key1, key2) {
		t.Fatal("session key regenerated on restart")
	}
}
