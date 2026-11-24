package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func baseRefresh(clock time.Time) store.Refresh {
	return store.Refresh{
		Now:           clock,
		DefaultMethod: "rebase",
		Repos:         []store.RepoRow{{ID: "R1", NameWithOwner: "acme/web"}, {ID: "R2", NameWithOwner: "acme/api"}},
		PRs: []store.PRRow{
			{ID: "P1", RepoID: "R1", Number: 1, Title: "One", HeadBranch: "feat/INP-1", BaseBranch: "main", HeadSHA: "aaa"},
			{ID: "P2", RepoID: "R2", Number: 2, Title: "Two", HeadBranch: "feat/b", BaseBranch: "main", HeadSHA: "bbb"},
		},
		Links:    []store.LinkRow{{PRID: "P1", Key: "INP-1", Source: "branch"}, {PRID: "P1", Key: "INP-2", Source: "title", Position: 1}},
		Version:  3,
		Snapshot: []byte("snap"),
	}
}

func count(t *testing.T, path, query string, args ...any) int {
	t.Helper()
	var n int
	if err := rawDB(t, path).QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSaveRefreshWritesEverythingAndSeedsNewRepos(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM prs WHERE state = 'open'`); n != 2 {
		t.Fatalf("prs %d", n)
	}
	if n := count(t, path, `SELECT count(*) FROM repos WHERE is_new = 1 AND default_method = 'rebase'`); n != 2 {
		t.Fatalf("new repos %d", n)
	}
	if n := count(t, path, `SELECT count(*) FROM repo_branch_rules WHERE repo_id = 'R1' AND method = 'merge' AND pattern IN ('main', 'production')`); n != 2 {
		t.Fatalf("seeded rules %d", n)
	}
	if n := count(t, path, `SELECT count(*) FROM ticket_links WHERE pr_id = 'P1' AND removed = 0`); n != 2 {
		t.Fatalf("links %d", n)
	}
	snap, err := s.LoadSnapshot(ctx)
	if err != nil || snap.Version != 3 || string(snap.Data) != "snap" {
		t.Fatalf("snapshot %+v %v", snap, err)
	}
}

func TestSaveRefreshKeepsExistingRepoRulesAndUserChoices(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	r := baseRefresh(clock.Now())
	if err := s.SaveRefresh(ctx, r); err != nil {
		t.Fatal(err)
	}
	db := rawDB(t, path)
	if _, err := db.Exec(`DELETE FROM repo_branch_rules WHERE repo_id = 'R1'`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserLinks(ctx, "P1", []string{"INP-9"}, []store.HiddenKey{{Key: "INP-2", Source: "title"}}); err != nil {
		t.Fatal(err)
	}

	clock.Advance(time.Minute)
	r = baseRefresh(clock.Now())
	r.Repos[0].NameWithOwner = "acme/web-renamed"
	r.Links = []store.LinkRow{{PRID: "P1", Key: "INP-2", Source: "title"}, {PRID: "P1", Key: "INP-3", Source: "branch", Position: 1}}
	r.Version = 4
	if err := s.SaveRefresh(ctx, r); err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM repo_branch_rules WHERE repo_id = 'R1'`); n != 0 {
		t.Fatalf("a known repo must not be reseeded, got %d rules", n)
	}
	if n := count(t, path, `SELECT count(*) FROM repos WHERE name_with_owner = 'acme/web-renamed'`); n != 1 {
		t.Fatal("repo name not updated")
	}
	got, err := s.UserLinks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]store.UserLinks{"P1": {Manual: []string{"INP-9"}, Removed: []string{"INP-2"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("user links %+v, want %+v", got, want)
	}
	if n := count(t, path, `SELECT count(*) FROM ticket_links WHERE pr_id = 'P1' AND ticket_key = 'INP-1'`); n != 0 {
		t.Fatal("an automatic link that no longer matches must be dropped")
	}
	if n := count(t, path, `SELECT count(*) FROM ticket_links WHERE pr_id = 'P1' AND ticket_key = 'INP-3' AND source = 'branch'`); n != 1 {
		t.Fatal("new automatic link missing")
	}
}

func TestSaveRefreshResolvesAndTracksOpenPRs(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	open1, err := s.OpenPRs(ctx)
	if err != nil || len(open1) != 2 || !open1["P1"].Equal(clock.Now()) {
		t.Fatalf("open %v %v", open1, err)
	}

	clock.Advance(time.Minute)
	merged := clock.Now().Add(-time.Second).Unix()
	r := baseRefresh(clock.Now())
	r.PRs = r.PRs[:1]
	r.Resolved = []store.Resolution{{ID: "P2", State: "merged", MergedAt: &merged}}
	if err := s.SaveRefresh(ctx, r); err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM prs WHERE id = 'P2' AND state = 'merged' AND merged_at = ?`, merged); n != 1 {
		t.Fatal("P2 not marked merged")
	}
	open2, _ := s.OpenPRs(ctx)
	if len(open2) != 1 {
		t.Fatalf("open after resolve %v", open2)
	}

	clock.Advance(time.Minute)
	r = baseRefresh(clock.Now())
	r.PRs = r.PRs[:1]
	r.Resolved = []store.Resolution{{ID: "P2", State: "closed"}}
	if err := s.SaveRefresh(ctx, r); err != nil {
		t.Fatal(err)
	}
	if n := count(t, path, `SELECT count(*) FROM prs WHERE id = 'P2' AND state = 'closed' AND merged_at IS NULL`); n != 1 {
		t.Fatal("P2 not marked closed")
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	s, _, clock := open(t)
	ctx := context.Background()
	if _, err := s.LoadSnapshot(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("empty store: %v", err)
	}
	if err := s.SaveSnapshot(ctx, 7, []byte("x"), clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshot(ctx, 8, []byte("y"), clock.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadSnapshot(ctx)
	if err != nil || got.Version != 8 || string(got.Data) != "y" || !got.UpdatedAt.Equal(clock.Now()) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSetUserLinksIsOneReplacement(t *testing.T) {
	s, path, clock := open(t)
	ctx := context.Background()
	if err := s.SaveRefresh(ctx, baseRefresh(clock.Now())); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserLinks(ctx, "nope", nil, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown pr: %v", err)
	}
	if err := s.SetUserLinks(ctx, "P1", []string{"A-1", "A-2"}, []store.HiddenKey{{Key: "INP-1", Source: "branch"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserLinks(ctx, "P1", []string{"A-2"}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := s.UserLinks(ctx)
	if !reflect.DeepEqual(got["P1"], store.UserLinks{Manual: []string{"A-2"}}) {
		t.Fatalf("%+v", got)
	}
	if n := count(t, path, `SELECT count(*) FROM ticket_links WHERE pr_id = 'P1' AND ticket_key = 'INP-1' AND removed = 0`); n != 0 {
		t.Fatalf("an unhidden key is re-added by the next refresh, not by SetUserLinks: %d rows", n)
	}
}
