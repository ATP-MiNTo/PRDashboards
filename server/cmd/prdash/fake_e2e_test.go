//go:build e2e

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
)

func TestFakeFlagStartsFakesAndPreconfiguresClients(t *testing.T) {
	if _, err := parseFlags([]string{"--fake"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	dir := newDataDir(t)
	port := freePort(t)
	opts := options{port: port, dataDir: dir, noBrowser: true, fake: true}
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, opts, testEnv(out, &syncBuffer{}, nil)) }()
	t.Cleanup(func() { cancel(); <-done })
	waitFor(t, "server to listen", func() bool { return strings.Contains(out.String(), `"msg":"listening"`) })

	lf, err := auth.ReadLaunchFile(dir + "/launch.token")
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	resp := httpDo(t, http.MethodPost, base+"/api/session", `{"launchToken":"`+lf.Token+`"}`, "", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("launch: %d", resp.StatusCode)
	}
	cookie := resp.Cookies()[0].Value
	var setup struct{ Github, Atlassian struct{ Configured bool } }
	resp = httpDo(t, http.MethodGet, base+"/api/setup", "", cookie, "")
	if err := json.NewDecoder(resp.Body).Decode(&setup); err != nil || !setup.Github.Configured || !setup.Atlassian.Configured {
		t.Fatalf("clients not pre-configured: %+v %v", setup, err)
	}

	// The app starts the OAuth flow against the fake, which is reachable only in this build.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest(http.MethodGet, base+"/oauth/github/start", nil)
	req.Header.Set("Cookie", "prd_session="+cookie)
	r, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	loc, _ := url.Parse(r.Header.Get("Location"))
	if r.StatusCode != http.StatusFound || loc.Host != "localhost:4411" {
		t.Fatalf("%d %s", r.StatusCode, loc)
	}
	// Follow the authorize step as the browser would: the fake answers with the callback URL.
	r, err = noRedirect.Get(strings.Replace(loc.String(), "localhost", "127.0.0.1", 1))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	cb, _ := url.Parse(r.Header.Get("Location"))
	if r.StatusCode != http.StatusFound || cb.Path != "/oauth/github/callback" || cb.Query().Get("code") == "" || cb.Query().Get("state") == "" {
		t.Fatalf("%d %s", r.StatusCode, cb)
	}
}
