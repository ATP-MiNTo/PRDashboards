package api

import "fmt"

// Problem codes raised by handlers. The codes of the middleware are in package mw.
const (
	codeUnknownSite       = "unknown_site"
	codeInvalidToken      = "invalid_token"
	codeInsufficientScope = "insufficient_scopes"
	codeOrgBlocked        = "org_blocked"
	codeUpstream          = "upstream_error"
	codeNotConnected      = "not_connected"
	codeNotConfigured     = "provider_not_configured"
	codeInvalidState      = "invalid_oauth_state"
	codeExchangeFailed    = "oauth_exchange_failed"
)

// problemError is returned by a handler to answer with an application/problem+json response.
type problemError struct {
	status int
	code   string
	detail string
}

func (e *problemError) Error() string { return fmt.Sprintf("%d %s %s", e.status, e.code, e.detail) }

func problem(status int, code, detail string) error {
	return &problemError{status: status, code: code, detail: detail}
}
