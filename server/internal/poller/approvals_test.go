package poller_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
)

func approved(id, who string) github.Review {
	return github.Review{ID: id, State: "APPROVED", Author: who}
}

func TestSnapshotCountsApprovalsAgainstTheRepoSetting(t *testing.T) {
	one := pr("PR_a", 1, "acme/web", "feat/a", "main", "One approval")
	one.Reviews = []github.Review{approved("1", "alice")}
	changes := pr("PR_b", 2, "acme/web", "feat/b", "main", "Changes requested")
	changes.Reviews = []github.Review{approved("2", "alice"), {ID: "3", State: "CHANGES_REQUESTED", Author: "bob"}, {ID: "4", State: "COMMENTED", Author: "carol"}}
	none := pr("PR_c", 3, "acme/api", "feat/c", "main", "No review")
	r := newRig(t)
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = append(f.result.PRs, one, changes, none) })
	r.sync()

	by := func() map[int]gen.Approvals {
		out := map[int]gen.Approvals{}
		for _, p := range r.snapshot().Prs {
			out[p.Number] = p.Approvals
		}
		return out
	}
	got := by()
	if got[1] != (gen.Approvals{Have: 1}) || got[2] != (gen.Approvals{Have: 1, ChangesRequested: 1}) || got[3] != (gen.Approvals{}) {
		t.Fatalf("the default requires no approval: %+v", got)
	}

	ctx := context.Background()
	two := 2
	if err := r.store.PutRepo(ctx, "R-acme/web", "squash", nil, true, &two); err != nil {
		t.Fatal(err)
	}
	if err := r.p.Relink(ctx); err != nil {
		t.Fatal(err)
	}
	got = by()
	if got[1] != (gen.Approvals{Have: 1, Need: 2}) || got[2].Need != 2 || got[3].Need != 0 {
		t.Fatalf("the setting is per repository and a rebuild applies it: %+v", got)
	}
	// The merge sees the same number as the page.
	l, ok := r.p.Linked("PR_a")
	if !ok || l.ApprovalsRequired != 2 {
		t.Fatalf("%+v %v", l, ok)
	}
	// 0 stored explicitly is a setting like any other, not a reason to keep the old one.
	zero := 0
	if err := r.store.PutRepo(ctx, "R-acme/web", "squash", nil, true, &zero); err != nil {
		t.Fatal(err)
	}
	if err := r.p.Relink(ctx); err != nil {
		t.Fatal(err)
	}
	if got = by(); got[1] != (gen.Approvals{Have: 1}) || got[2].Need != 0 {
		t.Fatalf("back to nothing required: %+v", got)
	}
}

func TestStoredSnapshotWithoutApprovalsGetsThemFromItsReviews(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/a", "main", "a")
	a.Reviews = []github.Review{approved("1", "alice"), approved("2", "bob")}
	r := newRig(t, a)
	r.sync()
	three := 3
	if err := r.store.PutRepo(context.Background(), "R-acme/web", "squash", nil, true, &three); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(r.p.Current().JSON, &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range doc["prs"].([]any) {
		delete(p.(map[string]any), "approvals")
	}
	old, _ := json.Marshal(doc)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(old)
	_ = zw.Close()
	if err := r.store.SaveSnapshot(context.Background(), 9, buf.Bytes(), r.clock.Now()); err != nil {
		t.Fatal(err)
	}
	fresh := r.newPoller(nil, nil)
	if err := fresh.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := fresh.Current().Snapshot.Prs[0]
	if p.Approvals != (gen.Approvals{Have: 2, Need: 3}) {
		t.Fatalf("%+v", p.Approvals)
	}
	var served struct {
		Prs []struct {
			Approvals map[string]int `json:"approvals"`
		} `json:"prs"`
	}
	if err := json.Unmarshal(fresh.Current().JSON, &served); err != nil || served.Prs[0].Approvals["have"] != 2 || served.Prs[0].Approvals["need"] != 3 {
		t.Fatalf("served %s %v", fresh.Current().JSON, err)
	}
}

func TestStoredSnapshotTakesTheCurrentApprovalSettingEvenAtZero(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/a", "main", "a")
	a.Reviews = []github.Review{approved("1", "alice")}
	r := newRig(t, a)
	r.sync()
	// The snapshot was stored while the repo needed 2 (or the old default of 1); the setting is 0 now.
	// A need of 0 is a real value, so the stored 2 must not be mistaken for "already filled in".
	var doc map[string]any
	if err := json.Unmarshal(r.p.Current().JSON, &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range doc["prs"].([]any) {
		p.(map[string]any)["approvals"] = map[string]any{"have": 1, "need": 2, "changesRequested": 0}
	}
	old, _ := json.Marshal(doc)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(old)
	_ = zw.Close()
	if err := r.store.SaveSnapshot(context.Background(), 9, buf.Bytes(), r.clock.Now()); err != nil {
		t.Fatal(err)
	}
	fresh := r.newPoller(nil, nil)
	if err := fresh.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fresh.Current().Snapshot.Prs[0].Approvals; got != (gen.Approvals{Have: 1}) {
		t.Fatalf("%+v", got)
	}
}

func TestIntervalNeverGoesBelowThirtySeconds(t *testing.T) {
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	if got := poller.Interval(30*time.Second, github.RateLimit{}, now); got != 30*time.Second {
		t.Fatalf("%v", got)
	}
	if got := poller.Interval(5*time.Second, github.RateLimit{}, now); got != poller.MinInterval {
		t.Fatalf("a hand-edited value below the floor: %v", got)
	}
	low := github.RateLimit{Limit: 5000, Remaining: 100, ResetAt: now.Add(time.Hour)}
	if got := poller.Interval(30*time.Second, low, now); got != time.Minute {
		t.Fatalf("a low rate limit still doubles a 30 second interval: %v", got)
	}
}
