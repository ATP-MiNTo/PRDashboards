// Package api wires the HTTP handler: middleware chain, generated routes and static UI.
package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/rules"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

// Poller is what the handlers need from the poller.
type Poller interface {
	Current() *poller.Published
	PR(id string) (gen.SnapshotPr, bool)
	Trigger() gen.SyncStatus
	Relink(ctx context.Context) error
	Reevaluate(ctx context.Context) error
	Preview(ctx context.Context, r rules.Rule) (poller.RulePreview, error)
	PreviewAll(ctx context.Context, rs []rules.Rule) ([]poller.RulePreview, error)
	Baseline(ctx context.Context, rs []rules.Rule) ([]store.Firing, error)
	Reschedule()
	ProjectKeys() []string
}

// Jira is what the handlers need from the Jira client.
type Jira interface {
	Assigned(ctx context.Context, site string, ttl time.Duration) ([]jira.Issue, error)
	Transitions(ctx context.Context, site, key string) ([]jira.Transition, error)
	Statuses(ctx context.Context, site string) ([]string, error)
}

// Config holds what the handler needs.
type Config struct {
	Port     int
	Version  string
	Sessions *auth.Sessions
	Launcher *auth.Launcher
	Store    *store.Store
	OAuth    *auth.OAuth
	Poller   Poller
	Merger   Merger
	Reviewer Reviewer
	Jira     Jira
	Accounts poller.Accounts
	// Events serves GET /events, the server-sent events stream.
	Events http.Handler
	// Web is the exported UI. It may be empty when the UI has not been built.
	Web   fs.FS
	Log   *slog.Logger
	Clock func() time.Time
}

// New builds the full handler.
func New(cfg Config) (http.Handler, error) {
	spec, err := gen.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load embedded spec: %w", err)
	}
	validate, err := mw.Validate(spec)
	if err != nil {
		return nil, fmt.Errorf("build request validator: %w", err)
	}

	h := &handlers{cfg: cfg}
	strict := gen.NewStrictHandlerWithOptions(h, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			mw.WriteProblem(w, http.StatusBadRequest, mw.CodeInvalidBody, "")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			var p *problemError
			if errors.As(err, &p) {
				mw.WriteProblem(w, p.status, p.code, p.detail)
				return
			}
			cfg.Log.ErrorContext(r.Context(), "handler failed", "err", err)
			mw.WriteProblem(w, http.StatusInternalServerError, mw.CodeInternal, "")
		},
	})
	mux := http.NewServeMux()
	gen.HandlerFromMux(strict, mux)
	mux.HandleFunc("GET /oauth/{provider}/start", h.oauthStart)
	mux.HandleFunc("GET /oauth/{provider}/callback", h.oauthCallback)
	if cfg.Events != nil {
		mux.Handle("GET /events", cfg.Events)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		mw.WriteProblem(w, http.StatusNotFound, mw.CodeNotFound, "")
	})
	mux.Handle("/", NewStatic(cfg.Web))

	host := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	alias := fmt.Sprintf("localhost:%d", cfg.Port)
	return mw.Chain(mux,
		mw.AccessLog(cfg.Log, cfg.Clock),
		mw.Recover(cfg.Log),
		mw.SecurityHeaders(mw.CSP(mw.LoadScriptHashes(cfg.Web))),
		mw.HostAllowlist(host, alias),
		mw.NoCORS(),
		mw.SameOrigin("http://"+host, "http://"+alias),
		mw.Identify(cfg.Sessions),
		mw.RateLimit(cfg.Clock),
		mw.RequireSession(),
		mw.RequireCSRF(),
		validate,
	), nil
}

type handlers struct {
	cfg Config
}

var _ gen.StrictServerInterface = (*handlers)(nil)

func (h *handlers) CreateSession(ctx context.Context, req gen.CreateSessionRequestObject) (gen.CreateSessionResponseObject, error) {
	if req.Body == nil || !h.cfg.Launcher.Consume(req.Body.LaunchToken) {
		return gen.CreateSession401ApplicationProblemPlusJSONResponse{
			InvalidLaunchTokenApplicationProblemPlusJSONResponse: gen.InvalidLaunchTokenApplicationProblemPlusJSONResponse{
				Type:   "/problems/invalid_launch_token",
				Title:  http.StatusText(http.StatusUnauthorized),
				Status: http.StatusUnauthorized,
				Code:   "invalid_launch_token",
			},
		}, nil
	}
	s, err := h.cfg.Sessions.Create(ctx)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return gen.CreateSession204Response{
		Headers: gen.CreateSession204ResponseHeaders{SetCookie: s.Cookie().String()},
	}, nil
}

func (h *handlers) GetSession(ctx context.Context, _ gen.GetSessionRequestObject) (gen.GetSessionResponseObject, error) {
	s, ok := mw.SessionFrom(ctx)
	if !ok {
		return nil, errors.New("session missing from context")
	}
	return gen.GetSession200JSONResponse{
		CsrfToken: s.CSRF,
		ExpiresAt: s.Expires,
		Version:   h.cfg.Version,
	}, nil
}
