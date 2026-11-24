package mw

import (
	"context"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
)

type sessionKey struct{}

// WithSession stores the authenticated session in ctx.
func WithSession(ctx context.Context, s auth.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFrom returns the session placed by Identify.
func SessionFrom(ctx context.Context) (auth.Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(auth.Session)
	return s, ok
}
