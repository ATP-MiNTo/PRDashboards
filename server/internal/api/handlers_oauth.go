package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
)

var (
	callbackCode  = regexp.MustCompile(`^[A-Za-z0-9._~+/=:-]+$`)
	callbackState = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	callbackError = regexp.MustCompile(`^[a-z_]+$`)
)

const (
	maxCode  = 2048
	maxState = 256
	maxError = 64
)

func shaped(re *regexp.Regexp, v string, limit int) bool { return len(v) <= limit && re.MatchString(v) }

func redirect(w http.ResponseWriter, location string, status int) {
	w.Header().Set("Location", location)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
}

func donePage(p auth.Provider, result string) string {
	return "/auth/done/?" + url.Values{"provider": {string(p)}, "result": {result}}.Encode()
}

// oauthStart sends the browser to the provider. The route is protected (session, same origin), so
// it is a link click inside the app.
func (h *handlers) oauthStart(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.ParseProvider(r.PathValue("provider"))
	if !ok {
		mw.WriteProblem(w, http.StatusNotFound, mw.CodeNotFound, "")
		return
	}
	sess, ok := mw.SessionFrom(r.Context())
	if !ok {
		mw.WriteProblem(w, http.StatusUnauthorized, mw.CodeUnauthenticated, "")
		return
	}
	target, err := h.cfg.OAuth.Start(r.Context(), p, sess.Key)
	switch {
	case errors.Is(err, auth.ErrNotConfigured):
		redirect(w, donePage(p, "not_configured"), http.StatusSeeOther)
	case err != nil:
		h.cfg.Log.ErrorContext(r.Context(), "start oauth", "provider", string(p), "err", err)
		redirect(w, donePage(p, "error"), http.StatusSeeOther)
	default:
		redirect(w, target, http.StatusFound)
	}
}

// oauthCallback receives the provider's redirect. A cross-site navigation does not carry the
// SameSite=Strict cookie, so this route has no session and does nothing with the code. It
// validates the shape of the parameters and passes them on to the static /oauth/finish/ page,
// which posts them to /api/oauth/complete from a same-origin context.
func (h *handlers) oauthCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.ParseProvider(r.PathValue("provider"))
	if !ok {
		mw.WriteProblem(w, http.StatusNotFound, mw.CodeNotFound, "")
		return
	}
	q := r.URL.Query()
	out := url.Values{"provider": {string(p)}}
	switch code, state, failure := q.Get("code"), q.Get("state"), q.Get("error"); {
	case failure != "" && shaped(callbackError, failure, maxError):
		out.Set("error", failure)
	case failure != "" || !shaped(callbackCode, code, maxCode) || !shaped(callbackState, state, maxState):
		out.Set("error", "invalid_callback")
	default:
		out.Set("code", code)
		out.Set("state", state)
	}
	redirect(w, "/oauth/finish/?"+out.Encode(), http.StatusSeeOther)
}

func (h *handlers) CompleteOAuth(ctx context.Context, req gen.CompleteOAuthRequestObject) (gen.CompleteOAuthResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	sess, ok := mw.SessionFrom(ctx)
	if !ok {
		return nil, errors.New("session missing from context")
	}
	p, _ := auth.ParseProvider(string(req.Body.Provider))
	err := h.cfg.OAuth.Complete(ctx, p, req.Body.Code, req.Body.State, sess.Key)
	switch {
	case err == nil:
		h.kick()
		return gen.CompleteOAuth204Response{}, nil
	case errors.Is(err, auth.ErrInvalidState):
		return nil, problem(http.StatusBadRequest, codeInvalidState, "the authorization expired or was already used; start again")
	case errors.Is(err, auth.ErrNotConfigured):
		return nil, problem(http.StatusConflict, codeNotConfigured, "")
	case errors.Is(err, auth.ErrExchange):
		h.cfg.Log.ErrorContext(ctx, "oauth exchange failed", "provider", string(p), "err", err)
		return nil, problem(http.StatusBadGateway, codeExchangeFailed, "the provider did not accept the authorization")
	}
	return nil, err
}
