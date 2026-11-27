// Package logging provides the slog handler that keeps secrets out of logs.
package logging

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

const redacted = "[redacted]"

var (
	sensitiveKeys = map[string]struct{}{
		"authorization": {}, "cookie": {}, "set-cookie": {}, "x-prd-csrf": {}, "token": {},
		"launch_token": {}, "session": {}, "code": {}, "state": {}, "secret": {}, "password": {},
	}
	// githubToken matches the prefixed token formats of GitHub: personal, OAuth, user-to-server,
	// server-to-server and refresh tokens, and fine-grained personal access tokens.
	githubToken = regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{16,})`)
	// jwtShape matches three base64url segments separated by dots, the first starting with eyJ.
	jwtShape = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`)
	// tokenRun matches a run of base64url characters. A run of exactly 43 is 32 random bytes in
	// unpadded base64url, the format of launch tokens, session ids and CSRF tokens. A longer or
	// shorter run, such as a 64-character hex digest, is left alone.
	tokenRun = regexp.MustCompile(`[A-Za-z0-9_-]{43,}`)
)

const tokenLen = 43

// minScan is the length below which a string cannot hold any of the token shapes.
const minScan = 20

// scrub replaces the token-shaped substrings of s.
func scrub(s string) string {
	if len(s) < minScan {
		return s
	}
	s = githubToken.ReplaceAllLiteralString(s, redacted)
	s = jwtShape.ReplaceAllLiteralString(s, redacted)
	if len(s) < tokenLen {
		return s
	}
	return tokenRun.ReplaceAllStringFunc(s, func(run string) string {
		if len(run) == tokenLen {
			return redacted
		}
		return run
	})
}

// NewRedactingHandler wraps h. Attributes whose key is sensitive (case-insensitive) are dropped.
// Under any other key, token-shaped substrings of the message, of string values and of error
// texts are replaced: GitHub tokens, JWTs and 43-character base64url launch tokens.
func NewRedactingHandler(h slog.Handler) slog.Handler {
	return &redactingHandler{inner: h}
}

type redactingHandler struct {
	inner slog.Handler
}

func (r *redactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return r.inner.Enabled(ctx, l)
}

func (r *redactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, scrub(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		if a, ok := redact(a); ok {
			out.AddAttrs(a)
		}
		return true
	})
	return r.inner.Handle(ctx, out)
}

func (r *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	kept := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if a, ok := redact(a); ok {
			kept = append(kept, a)
		}
	}
	return &redactingHandler{inner: r.inner.WithAttrs(kept)}
}

func (r *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: r.inner.WithGroup(name)}
}

func redact(a slog.Attr) (slog.Attr, bool) {
	a.Value = a.Value.Resolve()
	if _, ok := sensitiveKeys[strings.ToLower(a.Key)]; ok {
		return slog.Attr{}, false
	}
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(scrub(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok && err != nil {
			a.Value = slog.StringValue(scrub(err.Error()))
		}
	case slog.KindGroup:
		group := a.Value.Group()
		kept := make([]slog.Attr, 0, len(group))
		for _, g := range group {
			if g, ok := redact(g); ok {
				kept = append(kept, g)
			}
		}
		a.Value = slog.GroupValue(kept...)
	}
	return a, true
}
