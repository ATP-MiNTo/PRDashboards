package api

import (
	"fmt"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

// Problem codes raised by handlers. The codes of the middleware are in package mw.
const (
	codeUnknownSite       = "unknown_site"
	codeInvalidRule       = "invalid_rule"
	codeInvalidToken      = "invalid_token"
	codeInsufficientScope = "insufficient_scopes"
	codeOrgBlocked        = "org_blocked"
	codeMissingPermission = "missing_permission"
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

// blockedDetail words an organization block. An organization policy names the organization when
// GitHub did, and never repeats GitHub's free text; SSO keeps the given sentence.
func blockedDetail(b *github.OrgBlocked, sso string) string {
	if b.Reason != github.ReasonPolicy {
		return sso
	}
	if b.Org == "" {
		return "An organization policy does not allow this token"
	}
	return "The " + b.Org + " organization policy does not allow this token"
}
