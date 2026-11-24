package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gofrs/flock"

	webui "github.com/ATP-MiNTo/PRDashboards/server"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/logging"
)

const oauthPort = 4317

type env struct {
	stdout, stderr io.Writer
	open           func(url string) error
	clock          func() time.Time
	rand           io.Reader
	version        string
}

func launchURL(port int, token string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/auth/launch#t=%s", port, token)
}

func run(ctx context.Context, o options, e env) error {
	dir, err := expandHome(o.dataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	if err := prepareDataDir(dir); err != nil {
		return err
	}

	lock := flock.New(filepath.Join(dir, "prdash.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("lock data dir: %w", err)
	}
	if !locked {
		return openRunning(dir, o, e)
	}
	defer func() { _ = lock.Unlock() }()

	ln, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", o.port))
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return fmt.Errorf("port %d is busy; stop the program using it or pass --port", o.port)
		}
		return fmt.Errorf("listen on 127.0.0.1:%d: %w", o.port, err)
	}
	defer func() { _ = ln.Close() }()

	log := slog.New(logging.NewRedactingHandler(slog.NewJSONHandler(e.stdout, nil)))
	if o.port != oauthPort {
		log.Warn("OAuth callbacks need port 4317; sign-in will not work on this port", "port", o.port)
	}

	key, err := auth.LoadOrCreateKey(filepath.Join(dir, "session.key"), e.rand)
	if err != nil {
		return fmt.Errorf("session key: %w", err)
	}
	launcher, err := auth.NewLauncher(filepath.Join(dir, "launch.token"), e.clock, e.rand, log)
	if err != nil {
		return fmt.Errorf("launch token: %w", err)
	}
	defer func() { _ = launcher.Close() }()

	handler, err := api.New(api.Config{
		Port:     o.port,
		Version:  e.version,
		Sessions: auth.NewSessions(key, e.clock, e.rand),
		Launcher: launcher,
		Web:      webui.WebFS(),
		Log:      log,
		Clock:    e.clock,
	})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	rotateCtx, stopRotate := context.WithCancel(ctx)
	rotated := make(chan struct{})
	go func() {
		defer close(rotated)
		launcher.Run(rotateCtx)
	}()
	defer func() {
		stopRotate()
		<-rotated
	}()

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	log.Info("listening", "addr", ln.Addr().String(), "version", e.version)

	url := launchURL(o.port, launcher.Current())
	if o.noBrowser {
		fmt.Fprintln(e.stderr, "Open", url)
	} else if err := e.open(url); err != nil {
		fmt.Fprintf(e.stderr, "Could not open the browser (%v). Open %s\n", err, url)
	}

	select {
	case err := <-served:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("stopped")
	return nil
}

// openRunning handles a second start: it opens the browser on the running instance.
func openRunning(dir string, o options, e env) error {
	token, err := os.ReadFile(filepath.Join(dir, "launch.token")) //nolint:gosec // path is built from the data dir
	if err != nil {
		return fmt.Errorf("prdash is already running but its launch token is unreadable: %w", err)
	}
	url := launchURL(o.port, string(token))
	if o.noBrowser {
		fmt.Fprintln(e.stderr, "Open", url)
		return nil
	}
	if err := e.open(url); err != nil {
		fmt.Fprintf(e.stderr, "Could not open the browser (%v). Open %s\n", err, url)
	}
	return nil
}
