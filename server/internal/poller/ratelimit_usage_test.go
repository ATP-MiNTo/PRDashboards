package poller_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
)

func TestRateLimitUsageMapsTheBudgetToUsedAndLimit(t *testing.T) {
	reset := time.Date(2026, 11, 27, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		in   github.RateLimit
		want *gen.RateLimitUsage
	}{
		{"no data", github.RateLimit{}, nil},
		{"no limit reported", github.RateLimit{Remaining: 10, ResetAt: reset}, nil},
		{"used is limit minus remaining", github.RateLimit{Limit: 5000, Remaining: 820, Cost: 2, ResetAt: reset}, &gen.RateLimitUsage{Used: 4180, Limit: 5000, ResetAt: &reset}},
		{"untouched budget", github.RateLimit{Limit: 5000, Remaining: 5000, ResetAt: reset}, &gen.RateLimitUsage{Used: 0, Limit: 5000, ResetAt: &reset}},
		{"used up", github.RateLimit{Limit: 5000, Remaining: 0, ResetAt: reset}, &gen.RateLimitUsage{Used: 5000, Limit: 5000, ResetAt: &reset}},
		{"remaining above the limit stays at 0 used", github.RateLimit{Limit: 5000, Remaining: 5100}, &gen.RateLimitUsage{Used: 0, Limit: 5000}},
		{"negative remaining stays at the limit", github.RateLimit{Limit: 5000, Remaining: -3}, &gen.RateLimitUsage{Used: 5000, Limit: 5000}},
		{"no reset time", github.RateLimit{Limit: 1000, Remaining: 400}, &gen.RateLimitUsage{Used: 600, Limit: 1000}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := poller.RateLimitUsage(tc.in)
			switch {
			case got == nil || tc.want == nil:
				if got != tc.want {
					t.Fatalf("got %+v, want %+v", got, tc.want)
				}
			case got.Used != tc.want.Used || got.Limit != tc.want.Limit:
				t.Fatalf("got %+v, want %+v", got, tc.want)
			case (got.ResetAt == nil) != (tc.want.ResetAt == nil) || (got.ResetAt != nil && !got.ResetAt.Equal(*tc.want.ResetAt)):
				t.Fatalf("resetAt: got %v, want %v", got.ResetAt, tc.want.ResetAt)
			}
		})
	}
}

func TestSnapshotCarriesTheGitHubRateLimitOfTheLastRefresh(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/a", "main", "a"))
	if r.snapshot().Sources.Github.RateLimit != nil {
		t.Fatal("no rate limit before the first refresh")
	}
	r.gh.set(func(f *fakeGitHub) {
		f.result.RateLimit = github.RateLimit{Limit: 5000, Remaining: 820, ResetAt: r.clock.Now().Add(time.Hour)}
	})
	r.sync()
	rl := r.snapshot().Sources.Github.RateLimit
	if rl == nil || rl.Used != 4180 || rl.Limit != 5000 || rl.ResetAt == nil || !rl.ResetAt.Equal(r.clock.Now().Add(time.Hour)) {
		t.Fatalf("rate limit after a refresh: %+v", rl)
	}
	if r.snapshot().Sources.Jira.RateLimit != nil {
		t.Fatal("only the github source carries a rate limit")
	}

	// A failed refresh keeps the last known budget; a lost connection clears it.
	r.clock.Advance(time.Minute)
	r.gh.set(func(f *fakeGitHub) { f.err = errors.New("502") })
	r.sync()
	if rl := r.snapshot().Sources.Github.RateLimit; rl == nil || rl.Used != 4180 {
		t.Fatalf("a failed refresh keeps the last budget: %+v", rl)
	}
	r.gh.set(func(f *fakeGitHub) { f.err = nil })
	r.accounts.set(func(a *fakeAccounts) { a.tokenEr = auth.ErrNotConnected })
	r.sync()
	if rl := r.snapshot().Sources.Github.RateLimit; rl != nil {
		t.Fatalf("disconnected clears the budget: %+v", rl)
	}
}

func TestRateLimitIsServedInJSONAndSurvivesRestart(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/a", "main", "a"))
	r.sync() // the rig's search reports 5000 with 4900 remaining
	var doc struct {
		Sources struct {
			Github struct {
				RateLimit *struct {
					Used  int    `json:"used"`
					Limit int    `json:"limit"`
					Reset string `json:"resetAt"`
				} `json:"rateLimit"`
			} `json:"github"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(r.p.Current().JSON, &doc); err != nil || doc.Sources.Github.RateLimit == nil {
		t.Fatalf("served JSON: %v %+v", err, doc)
	}
	if got := doc.Sources.Github.RateLimit; got.Used != 100 || got.Limit != 5000 || got.Reset == "" {
		t.Fatalf("served rate limit: %+v", got)
	}

	fresh := r.newPoller(nil, nil)
	if err := fresh.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rl := fresh.Current().Snapshot.Sources.Github.RateLimit; rl == nil || rl.Used != 100 || rl.Limit != 5000 {
		t.Fatalf("after a restart: %+v", rl)
	}
}

func TestStoredSnapshotWithoutRateLimitStillLoads(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/a", "main", "a"))
	r.sync()
	var doc map[string]any
	if err := json.Unmarshal(r.p.Current().JSON, &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc["sources"].(map[string]any)["github"].(map[string]any), "rateLimit")
	old, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.store.SaveSnapshot(context.Background(), 9, gzipBytes(t, old), r.clock.Now()); err != nil {
		t.Fatal(err)
	}
	fresh := r.newPoller(nil, nil)
	if err := fresh.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := fresh.Current().Snapshot
	if got.Sources.Github.RateLimit != nil || len(got.Prs) != 1 {
		t.Fatalf("older snapshot: %+v", got.Sources.Github)
	}
}

func gzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
