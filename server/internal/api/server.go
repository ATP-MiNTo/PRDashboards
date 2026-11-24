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
)

// Config holds what the handler needs.
type Config struct {
	Port     int
	Version  string
	Sessions *auth.Sessions
	Launcher *auth.Launcher
	// Web is the exported UI. It may be empty when the UI has not been built.
	Web   fs.FS
	Log   *slog.Logger
	Clock func() time.Time
}

// New builds the full handler.
func New(cfg Config) (http.Handler, error) {
	spec, err := gen.GetSwagger()
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
			cfg.Log.ErrorContext(r.Context(), "handler failed", "err", err)
			mw.WriteProblem(w, http.StatusInternalServerError, mw.CodeInternal, "")
		},
	})
	mux := http.NewServeMux()
	gen.HandlerFromMux(strict, mux)
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

func (h *handlers) CreateSession(_ context.Context, req gen.CreateSessionRequestObject) (gen.CreateSessionResponseObject, error) {
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
	s, err := h.cfg.Sessions.Create()
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
