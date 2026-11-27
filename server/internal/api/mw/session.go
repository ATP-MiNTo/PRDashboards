package mw

import (
	"net/http"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
)

// CSRFHeader carries the CSRF token on writes.
const CSRFHeader = "X-PRD-CSRF"

func isSessionCreate(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/session"
}

func isSessionRead(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/session"
}

// Identify attaches the session of a valid cookie to the request context and slides its
// lifetime. It never rejects: later middleware decide whether a session is required.
func Identify(sessions *auth.Sessions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !Protected(r) {
				next.ServeHTTP(w, r)
				return
			}
			if c, err := r.Cookie(auth.CookieName); err == nil {
				if s, ok := sessions.Authenticate(c.Value); ok {
					if s.Reissue {
						http.SetCookie(w, s.Cookie())
					}
					r = r.WithContext(WithSession(r.Context(), s))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSession rejects protected requests without a session, except session creation.
func RequireSession() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if Protected(r) && !isSessionCreate(r) {
				if _, ok := SessionFrom(r.Context()); !ok {
					WriteProblem(w, http.StatusUnauthorized, CodeUnauthenticated, "")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireCSRF rejects protected writes whose X-PRD-CSRF does not match the session, except
// session creation.
func RequireCSRF() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if Protected(r) && isWrite(r) && !isSessionCreate(r) {
				s, ok := SessionFrom(r.Context())
				if !ok || !s.CheckCSRF(r.Header.Get(CSRFHeader)) {
					WriteProblem(w, http.StatusForbidden, CodeBadCSRF, "")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
