package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/logging"
)

func newLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(logging.NewRedactingHandler(slog.NewJSONHandler(buf, nil)))
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("bad log line %q: %v", buf.String(), err)
	}
	return m
}

func TestRedactsSensitiveKeys(t *testing.T) {
	keys := []string{
		"authorization", "Authorization", "cookie", "Set-Cookie", "x-prd-csrf", "X-PRD-CSRF", "token",
		"launch_token", "session", "code", "state", "secret", "password", "PASSWORD",
	}
	for _, k := range keys {
		var buf bytes.Buffer
		newLogger(&buf).Info("msg", k, "hunter2", "keep", "ok")
		if strings.Contains(buf.String(), "hunter2") {
			t.Errorf("%s: value leaked: %s", k, buf.String())
		}
		got := decode(t, &buf)
		if _, present := got[k]; present {
			t.Errorf("%s: key still present", k)
		}
		if got["keep"] != "ok" {
			t.Errorf("%s: other attributes lost: %v", k, got)
		}
	}
}

func TestRedactsInGroupsAndWith(t *testing.T) {
	var buf bytes.Buffer
	log := newLogger(&buf).With("cookie", "a", "who", "me").WithGroup("req")
	log.Info("msg", slog.Group("headers", "Authorization", "Bearer x", "host", "h"), "password", "p")
	out := buf.String()
	for _, leak := range []string{"Bearer x", `"a"`, `"p"`} {
		if strings.Contains(out, leak) {
			t.Fatalf("%s leaked in %s", leak, out)
		}
	}
	got := decode(t, &buf)
	if got["who"] != "me" {
		t.Fatalf("With attrs lost: %v", got)
	}
	req := got["req"].(map[string]any)
	if req["headers"].(map[string]any)["host"] != "h" {
		t.Fatalf("group attrs lost: %v", got)
	}
}

func TestRedactsTokenShapedValuesUnderAnyKey(t *testing.T) {
	var buf bytes.Buffer
	tok := strings.Repeat("Ab3_-", 8) + "xyz"
	newLogger(&buf).Info("msg", "note", tok, "short", "abc")
	if strings.Contains(buf.String(), tok) {
		t.Fatalf("token leaked: %s", buf.String())
	}
	got := decode(t, &buf)
	if got["note"] != "[redacted]" || got["short"] != "abc" {
		t.Fatalf("unexpected output %v", got)
	}
}

func TestEnabledDelegates(t *testing.T) {
	var buf bytes.Buffer
	h := logging.NewRedactingHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if h.Enabled(context.Background(), slog.LevelInfo) || !h.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("Enabled does not follow the inner handler")
	}
}
