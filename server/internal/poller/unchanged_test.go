package poller_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
)

func TestUnchangedRefreshKeepsVersionBlobAndEventButAnnouncesTheSync(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.sync()
	first := r.p.Current()
	stored, err := r.store.LoadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r.clock.Advance(time.Minute)
	statuses := len(r.events.named("sync-status"))
	r.sync()

	if r.p.Current() != first || r.snapshot().Version != 1 {
		t.Fatalf("an unchanged refresh keeps the published snapshot: version %d", r.snapshot().Version)
	}
	if got := r.events.named("snapshot"); len(got) != 1 {
		t.Fatalf("snapshot events %v", got)
	}
	if n := len(r.events.named("sync-status")); n-statuses < 2 {
		t.Fatalf("sync-status must still be published, got %d new", n-statuses)
	}
	again, err := r.store.LoadSnapshot(context.Background())
	if err != nil || again.Version != 1 || !bytes.Equal(again.Data, stored.Data) || !again.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Fatalf("the stored blob is left as it was: %+v %v", again, err)
	}
	if refreshes, snapshotOnly := r.store.counts(); refreshes != 2 || snapshotOnly != 0 {
		t.Fatalf("the pull requests are still stored: %d %d", refreshes, snapshotOnly)
	}
	st := r.p.Status()
	if st.GithubAt == nil || !st.GithubAt.Equal(r.clock.Now()) || st.Refreshes == nil || *st.Refreshes != 2 || *st.SnapshotVersion != 1 {
		t.Fatalf("status %+v", st)
	}
	if r.snapshot().Sources.Github.SyncedAt.Equal(r.clock.Now()) {
		t.Fatal("the snapshot itself is not rewritten for a new sync time")
	}
}

func TestChangedRefreshBumpsVersionAndAnnounces(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.sync()
	r.gh.set(func(f *fakeGitHub) { f.result.PRs[0].Title = "renamed" })
	r.sync()
	if r.snapshot().Version != 2 || len(r.events.named("snapshot")) != 2 {
		t.Fatalf("version %d events %v", r.snapshot().Version, r.events.named("snapshot"))
	}
}

func TestPublishedGzipMatchesTheJSONAfterRefreshAndLoad(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.sync()
	unzip := func(b []byte) []byte {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	cur := r.p.Current()
	if !bytes.Equal(unzip(cur.Gzip), cur.JSON) {
		t.Fatal("refresh: gzip differs from the JSON")
	}
	fresh := r.newPoller(r.store.Store, nil)
	if err := fresh.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := fresh.Current()
	if len(got.Gzip) == 0 || !bytes.Equal(unzip(got.Gzip), got.JSON) {
		t.Fatal("load: gzip differs from the JSON")
	}
	// The first refresh after a restart compares with the loaded snapshot.
	if err := fresh.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fresh.Current().Snapshot.Version != 1 {
		t.Fatalf("version after restart %d", fresh.Current().Snapshot.Version)
	}
}

func TestActionRefreshSkipsGitHubInBackoffButSyncOverridesIt(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.sync()
	r.gh.set(func(f *fakeGitHub) { f.err = &github.RateLimitedError{RetryAfter: time.Hour} })
	r.sync()
	open, _ := r.gh.calls()
	if open != 2 {
		t.Fatalf("the person's Sync ignores the backoff: %d calls", open)
	}
	r.p.TriggerAfterAction(nil)
	r.p.Idle()
	if after, _ := r.gh.calls(); after != open {
		t.Fatalf("an action refresh during backoff called GitHub: %d", after)
	}
	r.clock.Advance(2 * time.Hour)
	r.gh.set(func(f *fakeGitHub) { f.err = nil })
	r.p.TriggerAfterAction(nil)
	r.p.Idle()
	if after, _ := r.gh.calls(); after != open+1 {
		t.Fatalf("after the backoff an action refresh asks GitHub: %d", after)
	}
}

func TestActionRefreshAsksJiraOnlyForTheActedKeysTheSnapshotShows(t *testing.T) {
	r := newRig(t,
		pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"),
		pr("PR_b", 2, "acme/web", "feat/INP-2", "main", "b"))
	r.sync()
	base := len(r.jira.searchLog())
	projects := r.jira.projectCall

	r.p.TriggerAfterAction(nil)
	r.p.Idle()
	if len(r.jira.searchLog()) != base {
		t.Fatalf("no ticket moved, so Jira is not asked: %v", r.jira.searchLog())
	}
	r.p.TriggerAfterAction([]string{"INP-2", "GONE-9"})
	r.p.Idle()
	log := r.jira.searchLog()
	if len(log) != base+1 || !slices.Equal(log[base], []string{"INP-2"}) {
		t.Fatalf("only the shown acted key is asked: %v", log)
	}
	if r.jira.projectCall != projects {
		t.Fatal("an action refresh does not list the projects again")
	}
	if open, _ := r.gh.calls(); open != 3 {
		t.Fatalf("GitHub is asked once per action refresh: %d", open)
	}
}

func TestOldRowsArePrunedOnTheFirstRefreshAndThenOnceADay(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	prunes := func() int {
		r.store.mu.Lock()
		defer r.store.mu.Unlock()
		return r.store.prunes
	}
	r.sync()
	r.clock.Advance(time.Hour)
	r.sync()
	r.clock.Advance(poller.PruneEvery - 2*time.Hour)
	r.sync()
	if n := prunes(); n != 1 {
		t.Fatalf("the first refresh prunes and the next ones within 24 h do not: %d", n)
	}
	r.clock.Advance(2 * time.Hour)
	r.sync()
	if n := prunes(); n != 2 {
		t.Fatalf("a refresh 24 h after the last prune prunes again: %d", n)
	}
	r.sync()
	if n := prunes(); n != 2 {
		t.Fatalf("%d", n)
	}
}
