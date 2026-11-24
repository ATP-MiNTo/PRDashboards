package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gofrs/flock"

	webui "github.com/ATP-MiNTo/PRDashboards/server"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/events"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/logging"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/netguard"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

const oauthPort = 4317

type env struct {
	stdout, stderr io.Writer
	open           func(url string) error
	clock          func() time.Time
	rand           io.Reader
	version        string
	// prompt reads the passphrase of the encrypted secrets file when there is no keychain.
	prompt secrets.Prompter
}

// fakeEnv is what the e2e build supplies for --fake: fake provider endpoints and a secrets store
// that needs no keychain or passphrase.
type fakeEnv struct {
	Endpoints auth.Endpoints
	GitHubAPI string
	JiraAPI   string
	Secrets   secrets.Store
	Close     func()
}

// startFakes is set only by the e2e build. It starts the fake servers, allows their loopback
// addresses in the guard and stores their client credentials.
var startFakes func(ctx context.Context, guard *netguard.Guard, log *slog.Logger) (*fakeEnv, error)

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
	if err := checkPrivateFiles(dir); err != nil {
		return err
	}

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

	guard := netguard.New()
	endpoints := auth.DefaultEndpoints()
	githubAPI, jiraAPI := github.DefaultAPIBase, jira.DefaultBase
	var sec secrets.Store
	if o.fake {
		if startFakes == nil {
			return errors.New("--fake is not available in this build")
		}
		fake, err := startFakes(ctx, guard, log)
		if err != nil {
			return fmt.Errorf("start fakes: %w", err)
		}
		defer fake.Close()
		sec, endpoints, githubAPI, jiraAPI = fake.Secrets, fake.Endpoints, fake.GitHubAPI, fake.JiraAPI
	} else {
		var backend string
		if sec, backend, err = secrets.Open(dir, e.rand, e.prompt, log); err != nil {
			return fmt.Errorf("secrets: %w", err)
		}
		log.Info("secrets backend", "backend", backend)
	}

	db, err := store.Open(filepath.Join(dir, "data.db"), e.clock)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("close database", "err", err)
		}
	}()

	key, err := auth.LoadOrCreateKey(sec, filepath.Join(dir, "session.key"), e.rand, log)
	if err != nil {
		return fmt.Errorf("session key: %w", err)
	}
	sessions := auth.NewSessions(key, e.clock, e.rand, db, log)
	if err := sessions.Load(ctx); err != nil {
		return err
	}
	launcher, err := auth.NewLauncher(filepath.Join(dir, "launch.token"), o.port, e.clock, e.rand, log)
	if err != nil {
		return fmt.Errorf("launch token: %w", err)
	}
	defer func() { _ = launcher.Close() }()

	httpClient := guard.Client()
	ghClient := &github.Client{HTTP: httpClient, APIBase: githubAPI}
	oauth := auth.NewOAuth(auth.Config{
		Secrets:   sec,
		States:    db,
		HTTP:      httpClient,
		GitHub:    ghClient,
		Endpoints: endpoints,
		Clock:     e.clock,
		Rand:      e.rand,
		Port:      o.port,
	})
	jiraClient := &jira.Client{HTTP: httpClient, Base: jiraAPI, Access: oauth.AtlassianAccess}
	hub := events.NewHub(events.Options{})
	acc := accounts{oauth: oauth, store: db}
	snapshots := poller.New(poller.Config{
		Store:     db,
		GitHub:    ghClient,
		Jira:      jiraClient,
		Accounts:  acc,
		Publisher: hub,
		Log:       log,
		Clock:     e.clock,
	})
	if err := snapshots.Load(ctx); err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	merger := merge.New(merge.Config{
		GitHub:    ghClient,
		Jira:      jiraClient,
		Store:     db,
		Snapshot:  snapshots,
		Accounts:  acc,
		Refresh:   func() { snapshots.Trigger() },
		Publisher: hub,
		Log:       log,
		Clock:     e.clock,
	})
	handler, err := api.New(api.Config{
		Port:     o.port,
		Version:  e.version,
		Sessions: sessions,
		Launcher: launcher,
		Store:    db,
		OAuth:    oauth,
		Poller:   snapshots,
		Merger:   merger,
		Jira:     jiraClient,
		Accounts: acc,
		Events:   hub,
		Web:      webui.WebFS(),
		Log:      log,
		Clock:    e.clock,
	})
	if err != nil {
		return err
	}
	srv := newHTTPServer(handler, log)
	srv.RegisterOnShutdown(hub.Close)

	pollCtx, stopPoll := context.WithCancel(ctx)
	polled := make(chan struct{})
	go func() {
		defer close(polled)
		snapshots.Run(pollCtx)
	}()
	defer func() {
		stopPoll()
		<-polled
	}()

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

	url := launchURL(o.port, launcher.Current())
	if o.noBrowser {
		fmt.Fprintln(e.stderr, "Open", url)
	} else if err := e.open(url); err != nil {
		fmt.Fprintf(e.stderr, "Could not open the browser (%v). Open %s\n", err, url)
	}
	log.Info("listening", "addr", ln.Addr().String(), "version", e.version)

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

func newHTTPServer(h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		// Bodies are capped at 64 KiB, so 30 s is generous and stops slow-body clients from holding
		// a connection. WriteTimeout stays unset on purpose: the SSE stream (/events) is a
		// long-lived response and a write deadline would cut it.
		ReadTimeout:    30 * time.Second,
		IdleTimeout:    2 * time.Minute,
		MaxHeaderBytes: 16 << 10,
		ErrorLog:       slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// openRunning handles a second start: it opens the browser on the running instance, using the
// port that instance wrote to the launch file.
func openRunning(dir string, o options, e env) error {
	lf, err := auth.ReadLaunchFile(filepath.Join(dir, "launch.token"))
	if err != nil {
		return fmt.Errorf("prdash is already running but its launch file is unreadable: %w", err)
	}
	url := launchURL(lf.Port, lf.Token)
	if o.noBrowser {
		fmt.Fprintln(e.stderr, "Open", url)
		return nil
	}
	if err := e.open(url); err != nil {
		fmt.Fprintf(e.stderr, "Could not open the browser (%v). Open %s\n", err, url)
	}
	return nil
}
