package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestRedactsTokensInsideText(t *testing.T) {
	launch := strings.Repeat("Ab3_-", 8) + "xyz"
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	cases := map[string]string{
		"ghp":         "ghp_" + strings.Repeat("a1B2", 9),
		"gho":         "gho_" + strings.Repeat("a1B2", 9),
		"ghu":         "ghu_" + strings.Repeat("a1B2", 9),
		"ghs":         "ghs_" + strings.Repeat("a1B2", 9),
		"ghr":         "ghr_" + strings.Repeat("a1B2", 9),
		"fine grain":  "github_pat_11ABCDEFG0" + strings.Repeat("x_Y9", 15),
		"jwt":         jwt,
		"launch":      launch,
		"launch edge": launch,
	}
	for name, tok := range cases {
		for _, text := range []string{
			"request failed: Bearer " + tok + " was refused",
			"token=" + tok + ",next",
			"(" + tok + ")",
			tok,
			tok + " " + tok,
		} {
			var buf bytes.Buffer
			newLogger(&buf).Info("msg "+tok, "detail", text, "err", errors.New("wrapped: "+text), "wrapped", fmt.Errorf("outer: %w", errors.New(text)))
			if strings.Contains(buf.String(), tok) {
				t.Errorf("%s: token leaked from %q: %s", name, text, buf.String())
			}
			if !strings.Contains(buf.String(), "[redacted]") {
				t.Errorf("%s: nothing redacted in %s", name, buf.String())
			}
		}
	}
}

func TestRedactsErrorValuesAndKeepsTheRest(t *testing.T) {
	var buf bytes.Buffer
	tok := "ghs_" + strings.Repeat("Zz9", 12)
	newLogger(&buf).Error("send", "err", errors.New("POST /x: 401 "+tok+" bad credentials"), "status", 401)
	got := decode(t, &buf)
	if got["err"] != "POST /x: 401 [redacted] bad credentials" || got["status"] != float64(401) {
		t.Fatalf("%v", got)
	}
	buf.Reset()
	newLogger(&buf).Error("send", "err", error(nil))
	if got := decode(t, &buf); got["err"] != nil {
		t.Fatalf("nil error: %v", got)
	}
}

func TestKeepsOrdinaryText(t *testing.T) {
	hex64 := strings.Repeat("0123456789abcdef", 4)
	keep := []string{
		hex64,
		"sha " + hex64 + " ok",
		"3f786850e387550fdab836ed7e6dc881de23001b",
		strings.Repeat("a", 42),
		strings.Repeat("a", 44),
		strings.Repeat("Ab3_-", 8) + "xyzw",
		"ghp_short",
		"github_pat_x",
		"eyJ.only.two",
		"refresh failed: invalid_grant (status 403) for repository octocat/hello-world",
		"https://github.com/octocat/hello-world/pull/1234",
		"",
	}
	for _, v := range keep {
		var buf bytes.Buffer
		newLogger(&buf).Info("msg", "v", v, "err", errors.New(v))
		got := decode(t, &buf)
		if got["v"] != v || got["err"] != v {
			t.Errorf("%q altered: %v", v, got)
		}
	}
}
