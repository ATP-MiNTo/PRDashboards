// Package mw holds the HTTP middleware chain described in docs/features/security.md.
package mw

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Stable problem codes returned by the middleware.
const (
	CodeMisdirectedHost  = "misdirected_host"
	CodeCORSNotAllowed   = "cors_not_allowed"
	CodeCrossSite        = "cross_site"
	CodeBadOrigin        = "bad_origin"
	CodeRateLimited      = "rate_limited"
	CodeUnauthenticated  = "unauthenticated"
	CodeBadCSRF          = "bad_csrf"
	CodeUnsupportedMedia = "unsupported_media_type"
	CodeBodyTooLarge     = "body_too_large"
	CodeInvalidBody      = "invalid_body"
	CodeInvalidRequest   = "invalid_request"
	CodeNotFound         = "not_found"
	CodeInternal         = "internal_error"
)

// Problem is the application/problem+json error body.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// WriteProblem writes an application/problem+json response.
func WriteProblem(w http.ResponseWriter, status int, code, detail string) {
	h := w.Header()
	h.Set("Content-Type", "application/problem+json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{
		Type:   "/problems/" + code,
		Title:  http.StatusText(status),
		Status: status,
		Code:   code,
		Detail: detail,
	})
}

// Protected reports whether the request targets the authenticated surface.
func Protected(r *http.Request) bool {
	p := r.URL.Path
	return p == "/api" || strings.HasPrefix(p, "/api/") || p == "/events"
}

func isWrite(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead
}

// Chain wraps h so that the first middleware listed is the outermost.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
