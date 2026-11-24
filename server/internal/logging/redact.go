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
	// tokenShape matches 32 random bytes in unpadded base64url, the format of launch tokens,
	// session ids and CSRF tokens.
	tokenShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// NewRedactingHandler wraps h. Attributes whose key is sensitive (case-insensitive) are dropped,
// and string values shaped like a launch token are replaced under any other key.
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
	out := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
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
		if tokenShape.MatchString(a.Value.String()) {
			a.Value = slog.StringValue(redacted)
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
