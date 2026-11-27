package poller_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
)

func TestSnapshotCarriesAuthorUpdateTimeAndSize(t *testing.T) {
	updated := time.Date(2026, 11, 24, 8, 30, 0, 0, time.UTC)
	a := pr("PR_a", 1, "acme/web", "feat/a", "main", "a")
	a.Author, a.UpdatedAt, a.Additions, a.Deletions = "priya", &updated, 214, 38
	b := pr("PR_b", 2, "acme/web", "feat/b", "main", "b") // a deleted author, an empty diff
	r := newRig(t, a, b)
	r.sync()

	by := map[string]gen.SnapshotPr{}
	for _, p := range r.snapshot().Prs {
		by[p.Id] = p
	}
	got := by["PR_a"]
	if got.Author == nil || *got.Author != "priya" || got.UpdatedAt == nil || !got.UpdatedAt.Equal(updated) ||
		got.Additions == nil || *got.Additions != 214 || got.Deletions == nil || *got.Deletions != 38 {
		t.Fatalf("PR_a: %+v", got)
	}
	empty := by["PR_b"]
	if empty.Author != nil || empty.UpdatedAt != nil {
		t.Fatalf("a missing author and time stay null: %+v", empty)
	}
	if empty.Additions == nil || *empty.Additions != 0 || empty.Deletions == nil || *empty.Deletions != 0 {
		t.Fatalf("a refreshed pull request always has its size, even 0: %+v", empty)
	}
}

func TestStoredSnapshotWithoutAuthorAndSizeLoadsWithNulls(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/a", "main", "a"))
	r.sync()
	var doc map[string]any
	if err := json.Unmarshal(r.p.Current().JSON, &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range doc["prs"].([]any) {
		for _, k := range []string{"author", "updatedAt", "additions", "deletions"} {
			delete(p.(map[string]any), k)
		}
	}
	old, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
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
	got := fresh.Current()
	if len(got.Snapshot.Prs) != 1 {
		t.Fatalf("prs %d", len(got.Snapshot.Prs))
	}
	p := got.Snapshot.Prs[0]
	if p.Author != nil || p.UpdatedAt != nil || p.Additions != nil || p.Deletions != nil {
		t.Fatalf("an old snapshot has no author, time or size: %+v", p)
	}
	var served struct {
		Prs []map[string]any `json:"prs"`
	}
	if err := json.Unmarshal(got.JSON, &served); err != nil || len(served.Prs) != 1 {
		t.Fatalf("served JSON: %v", err)
	}
	for _, k := range []string{"author", "updatedAt", "additions", "deletions"} {
		if v, ok := served.Prs[0][k]; !ok || v != nil {
			t.Errorf("%s is served as null, got %v (present %v)", k, v, ok)
		}
	}
}
