package mw

import (
	"net/http"
	"strings"
)

// HostAllowlist rejects requests whose Host is not listed, which blocks DNS rebinding.
func HostAllowlist(hosts ...string) func(http.Handler) http.Handler {
	allowed := toSet(hosts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := allowed[strings.ToLower(r.Host)]; !ok {
				WriteProblem(w, http.StatusMisdirectedRequest, CodeMisdirectedHost, "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NoCORS rejects OPTIONS requests. The server never sends Access-Control-* headers.
func NoCORS() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				w.Header().Set("Allow", "GET, HEAD, POST, PUT, DELETE")
				WriteProblem(w, http.StatusMethodNotAllowed, CodeCORSNotAllowed, "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SameOrigin requires Sec-Fetch-Site to be same-origin (or absent) on the protected surface, and
// the Origin header to be allowlisted on writes. GET /api/session, which returns the CSRF token,
// also rejects an absent header.
func SameOrigin(origins ...string) func(http.Handler) http.Handler {
	allowed := toSet(origins)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !Protected(r) {
				next.ServeHTTP(w, r)
				return
			}
			site := r.Header.Get("Sec-Fetch-Site")
			if (site == "" && isSessionRead(r)) || (site != "" && site != "same-origin") {
				WriteProblem(w, http.StatusForbidden, CodeCrossSite, "")
				return
			}
			if isWrite(r) {
				if _, ok := allowed[r.Header.Get("Origin")]; !ok {
					WriteProblem(w, http.StatusForbidden, CodeBadOrigin, "")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func toSet(items []string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, s := range items {
		m[strings.ToLower(s)] = struct{}{}
	}
	return m
}
