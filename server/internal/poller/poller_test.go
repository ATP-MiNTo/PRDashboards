package poller_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func stackFixture(r *rig) []github.PR {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1-login", "main", "Add login")
	a.CI = github.CI{State: github.CIFailing, Total: 2, Checks: []github.Check{
		{Name: "unit", App: "GitHub Actions", State: github.CheckFailing, URL: "https://github.com/acme/web/actions/runs/1", StartedAt: ago(r.clock, time.Minute)},
		{Name: "ci/jenkins", State: github.CheckPassing, URL: "https://ci.example.com/42"},
	}}
	a.Reviews = []github.Review{{ID: "RV1", State: "APPROVED", Author: "alice"}}
	a.ReviewRequests = []string{"U_bob"}
	a.CommittedAt = ago(r.clock, 3*time.Hour)
	b := pr("PR_b", 2, "acme/web", "feat/INP-2-sso", "feat/INP-1-login", "SSO on top")
	c := pr("PR_c", 3, "acme/api", "fix/no-ticket", "main", "Plain fix")
	c.Draft, c.Mergeable, c.ReviewDecision = true, "UNKNOWN", ""
	return []github.PR{c, b, a}
}

func TestRefreshBuildsPersistsAndAnnouncesTheSnapshot(t *testing.T) {
	r := newRig(t)
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = stackFixture(r) })
	r.sync()

	snap := r.snapshot()
	if snap.Version != 1 || len(snap.Prs) != 3 || !snap.GeneratedAt.Equal(r.clock.Now()) {
		t.Fatalf("snapshot %d prs=%d at %v", snap.Version, len(snap.Prs), snap.GeneratedAt)
	}
	if snap.Sources.Github.State != "ok" || snap.Sources.Jira.State != "ok" || len(snap.Sources.BlockedOrgs) != 0 || len(snap.Warnings) != 0 {
		t.Fatalf("sources %+v warnings %v", snap.Sources, snap.Warnings)
	}
	if got := []int{snap.Prs[0].Number, snap.Prs[1].Number, snap.Prs[2].Number}; !slices.Equal(got, []int{3, 1, 2}) {
		t.Fatalf("order by repo and number: %v", got)
	}
	api, a, b := snap.Prs[0], snap.Prs[1], snap.Prs[2]
	if api.Stack != nil || !api.IsDraft || api.Mergeable != "UNKNOWN" || api.ReviewDecision != nil || len(api.Tickets) != 0 || api.TicketLookup != "ok" {
		t.Fatalf("api pr %+v", api)
	}
	if a.Stack == nil || a.Stack.Position != 1 || a.Stack.Size != 2 || a.Stack.ParentId != nil || a.Stack.Id != "PR_a" {
		t.Fatalf("root stack %+v", a.Stack)
	}
	if b.Stack == nil || b.Stack.Position != 2 || *b.Stack.ParentId != "PR_a" || *b.Stack.ParentNumber != 1 {
		t.Fatalf("child stack %+v", b.Stack)
	}
	if len(a.Tickets) != 1 {
		t.Fatalf("tickets %+v", a.Tickets)
	}
	tk := a.Tickets[0]
	if tk.Key != "INP-1" || tk.Source != "branch" || !tk.Found || *tk.Summary != "Add login" || *tk.Status != "In Review" ||
		*tk.StatusCategory != "indeterminate" || tk.Project != "INP" || *tk.IssueType != "Story" || *tk.Url != "https://acme.atlassian.net/browse/INP-1" {
		t.Fatalf("ticket %+v", tk)
	}
	if a.Ci.State != "failing" || a.Ci.Total != 2 || len(a.Ci.Checks) != 2 || *a.Ci.Checks[0].App != "GitHub Actions" || a.Ci.Checks[1].App != nil {
		t.Fatalf("ci %+v", a.Ci)
	}
	if !a.Repo.IsNew || !a.Repo.SquashAllowed || a.Reviews[0].Author == nil || a.ReviewRequests[0] != "U_bob" || *a.ReviewDecision != "APPROVED" {
		t.Fatalf("details %+v", a)
	}

	refreshes, snapshotOnly := r.store.counts()
	if refreshes != 1 || snapshotOnly != 0 {
		t.Fatalf("one transaction per refresh: refresh=%d snapshot=%d", refreshes, snapshotOnly)
	}
	stored, err := r.store.LoadSnapshot(context.Background())
	if err != nil || stored.Version != 1 {
		t.Fatalf("stored %+v %v", stored, err)
	}
	if got := r.events.named("snapshot"); len(got) != 1 || got[0].(map[string]int64)["version"] != 1 {
		t.Fatalf("snapshot events %v", got)
	}
	if st := r.events.named("sync-status"); len(st) < 2 || !st[0].(gen.SyncStatus).Running || st[len(st)-1].(gen.SyncStatus).Running {
		t.Fatalf("sync-status events %+v", st)
	}
	if g, j := r.gh.openCalls, len(r.jira.searchLog()); g != 1 || j != 1 || r.jira.projectCall != 1 {
		t.Fatalf("calls github=%d jira=%d projects=%d", g, j, r.jira.projectCall)
	}
	if got := slices.Sorted(slices.Values(r.jira.searchLog()[0])); !slices.Equal(got, []string{"INP-1", "INP-2"}) {
		t.Fatalf("keys %v", got)
	}
}

func TestSnapshotMatchesTheOpenAPISchema(t *testing.T) {
	r := newRig(t)
	r.gh.set(func(f *fakeGitHub) {
		f.result.PRs = stackFixture(r)
		f.result.Blocked = []github.OrgBlocked{{Org: "acme-corp"}}
	})
	r.sync()
	spec, err := gen.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(r.p.Current().JSON, &v); err != nil {
		t.Fatal(err)
	}
	if err := spec.Components.Schemas["Snapshot"].Value.VisitJSON(v); err != nil {
		t.Fatalf("snapshot does not match the schema: %v", err)
	}
	empty := poller.New(poller.Config{Store: r.store})
	var e any
	_ = json.Unmarshal(empty.Current().JSON, &e)
	if err := spec.Components.Schemas["Snapshot"].Value.VisitJSON(e); err != nil {
		t.Fatalf("empty snapshot does not match the schema: %v", err)
	}
	if empty.Current().Snapshot.Version != 0 || empty.Current().ETag() != `"0"` {
		t.Fatal("empty snapshot is version 0")
	}
}

func TestConcurrentSyncCallsShareOneRefresh(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "t"))
	r.gh.set(func(f *fakeGitHub) {
		f.gate = make(chan struct{})
		f.entered = make(chan struct{}, 1)
	})
	const callers = 25
	var wg sync.WaitGroup
	errs := make(chan error, callers+1)
	wg.Add(1)
	go func() { defer wg.Done(); errs <- r.p.Sync(context.Background()) }()
	<-r.gh.entered
	if !r.p.Status().Running {
		t.Fatal("status must say running")
	}
	for range callers {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- r.p.Sync(context.Background()) }()
	}
	if st := r.p.Trigger(); !st.Running {
		t.Fatal("Trigger joins the running refresh")
	}
	time.Sleep(50 * time.Millisecond)
	r.gh.set(func(f *fakeGitHub) { close(f.gate) })
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if open, _ := r.gh.calls(); open != 1 {
		t.Fatalf("outbound GitHub searches: %d, want exactly 1", open)
	}
	if n := len(r.jira.searchLog()); n != 1 || r.jira.projectCall != 1 {
		t.Fatalf("jira calls %d / projects %d", n, r.jira.projectCall)
	}
	if refreshes, _ := r.store.counts(); refreshes != 1 || r.snapshot().Version != 1 {
		t.Fatalf("refreshes %d version %d", refreshes, r.snapshot().Version)
	}

	r.gh.set(func(f *fakeGitHub) { f.gate = nil })
	r.sync()
	if open, _ := r.gh.calls(); open != 2 {
		t.Fatalf("a Sync after the refresh ended starts a new one: %d", open)
	}
}

func TestSyncWaitsOnlyForItsContext(t *testing.T) {
	r := newRig(t)
	r.gh.set(func(f *fakeGitHub) { f.gate = make(chan struct{}); f.entered = make(chan struct{}, 1) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.p.Sync(ctx) }()
	<-r.gh.entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	r.gh.set(func(f *fakeGitHub) { close(f.gate) })
	r.sync()
}

func TestSnapshotIsStoredCompressedAndLoadedAtStart(t *testing.T) {
	r := newRig(t)
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = stackFixture(r) })
	r.sync()
	r.sync()
	want := r.p.Current()

	fresh := r.newPoller(r.store.Store, nil)
	if fresh.Current().Snapshot.Version != 0 {
		t.Fatal("a new poller starts empty")
	}
	if err := fresh.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := fresh.Current()
	if got.Snapshot.Version != 2 || len(got.Snapshot.Prs) != 3 || string(got.JSON) != string(want.JSON) {
		t.Fatalf("loaded version %d prs %d", got.Snapshot.Version, len(got.Snapshot.Prs))
	}
	if st := fresh.Status(); st.GithubAt == nil || !st.GithubAt.Equal(r.clock.Now()) {
		t.Fatalf("status after load %+v", st)
	}
	if pr, ok := fresh.PR("PR_b"); !ok || pr.Number != 2 {
		t.Fatalf("PR lookup %+v %v", pr, ok)
	}
	if _, ok := fresh.PR("nope"); ok {
		t.Fatal("unknown id")
	}
	r.clock.Advance(time.Minute)
	if err := fresh.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fresh.Current().Snapshot.Version != 3 {
		t.Fatalf("versions continue after a restart: %d", fresh.Current().Snapshot.Version)
	}
}

func TestLoadIgnoresMissingAndCorruptSnapshots(t *testing.T) {
	r := newRig(t)
	if err := r.p.Load(context.Background()); err != nil || r.p.Current().Snapshot.Version != 0 {
		t.Fatalf("empty store: %v", err)
	}
	if err := r.store.SaveSnapshot(context.Background(), 5, []byte("not gzip"), r.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.p.Load(context.Background()); err != nil || r.p.Current().Snapshot.Version != 0 {
		t.Fatalf("corrupt snapshot must be ignored: %v", err)
	}
}

func TestPRsThatLeftTheListAreResolvedByOneMergedSearch(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/a", "main", "a")
	b := pr("PR_b", 2, "acme/web", "feat/b", "main", "b")
	c := pr("PR_c", 3, "acme/web", "feat/c", "main", "c")
	r := newRig(t, a, b, c)
	r.sync()
	if _, merged := r.gh.calls(); merged != 0 {
		t.Fatalf("nothing left yet, merged searches: %d", merged)
	}
	r.sync()
	if _, merged := r.gh.calls(); merged != 0 {
		t.Fatalf("an unchanged list needs no merged search: %d", merged)
	}

	mergedAt := r.clock.Now().Add(-2 * time.Minute)
	r.clock.Advance(time.Minute)
	r.gh.set(func(f *fakeGitHub) {
		f.result.PRs = []github.PR{a}
		f.merged = map[string]time.Time{"PR_b": mergedAt}
	})
	r.sync()
	open, merged := r.gh.calls()
	if merged != 1 || open != 3 {
		t.Fatalf("open=%d merged=%d, want one merged search for both PRs", open, merged)
	}
	if want := r.clock.Now().Add(-time.Minute).Add(-time.Minute); !r.gh.lastSince.Equal(want) {
		t.Fatalf("since %v, want the last seen time of the oldest left PR minus a minute: %v", r.gh.lastSince, want)
	}
	if refreshes, _ := r.store.counts(); refreshes != 3 {
		t.Fatalf("one transaction per refresh, got %d for 3 refreshes", refreshes)
	}
	open1, err := r.store.OpenPRs(context.Background())
	if err != nil || len(open1) != 1 {
		t.Fatalf("open %v %v", open1, err)
	}

	r.sync()
	if _, merged := r.gh.calls(); merged != 1 {
		t.Fatalf("resolved PRs are not searched again: %d", merged)
	}
}

func TestMergedSearchIsSkippedWhenTheListIsIncomplete(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/a", "main", "a")
	b := pr("PR_b", 2, "acme/web", "feat/b", "main", "b")
	cases := map[string]func(*github.SearchResult){
		"blocked org":  func(s *github.SearchResult) { s.Blocked = []github.OrgBlocked{{Org: "acme-corp"}} },
		"capped":       func(s *github.SearchResult) { s.Capped = true },
		"incomplete":   func(s *github.SearchResult) { s.Incomplete = true },
		"graphql errs": func(s *github.SearchResult) { s.Errors = []string{"INTERNAL boom"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, a, b)
			r.sync()
			r.gh.set(func(f *fakeGitHub) {
				f.result.PRs = []github.PR{a}
				mutate(&f.result)
			})
			r.sync()
			if _, merged := r.gh.calls(); merged != 0 {
				t.Fatal("a pull request missing from a partial list may still be open")
			}
			if open, _ := r.store.OpenPRs(context.Background()); len(open) != 2 {
				t.Fatalf("both PRs stay open in the store: %v", open)
			}
		})
	}
}

func TestMergedSearchFailureKeepsThePRsOpenAndWarns(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/a", "main", "a")
	b := pr("PR_b", 2, "acme/web", "feat/b", "main", "b")
	r := newRig(t, a, b)
	r.sync()
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = []github.PR{a}; f.mergedErr = errors.New("boom") })
	r.sync()
	if open, _ := r.store.OpenPRs(context.Background()); len(open) != 2 {
		t.Fatalf("%v", open)
	}
	if w := r.snapshot().Warnings; len(w) != 1 || w[0].Code != "merged_lookup_failed" {
		t.Fatalf("warnings %v", w)
	}
	r.gh.set(func(f *fakeGitHub) { f.mergedErr = nil })
	r.sync()
	if open, _ := r.store.OpenPRs(context.Background()); len(open) != 1 || len(r.snapshot().Warnings) != 0 {
		t.Fatalf("resolved on retry: %v %v", open, r.snapshot().Warnings)
	}
}

func TestPartialResultsStillYieldPRsAndBlockedOrgs(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/a", "main", "a"))
	r.gh.set(func(f *fakeGitHub) {
		f.result.Blocked = []github.OrgBlocked{{Org: "acme-corp"}, {}}
		f.result.Errors = []string{"INTERNAL something"}
		f.result.Capped = true
		f.result.Incomplete = true
	})
	r.sync()
	snap := r.snapshot()
	if len(snap.Prs) != 1 || len(snap.Sources.BlockedOrgs) != 2 || snap.Sources.BlockedOrgs[0].Org != "acme-corp" || snap.Sources.BlockedOrgs[1].Org != "" {
		t.Fatalf("%+v", snap.Sources.BlockedOrgs)
	}
	var codes []string
	for _, w := range snap.Warnings {
		codes = append(codes, w.Code)
	}
	if !slices.Equal(codes, []string{"github_search_cap", "github_incomplete", "github_partial"}) || snap.Sources.Github.State != "ok" {
		t.Fatalf("warnings %v state %s", codes, snap.Sources.Github.State)
	}
}

func TestGitHubFailuresKeepTheLastGoodData(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.sync()
	good := *r.snapshot().Sources.Github.SyncedAt

	r.clock.Advance(time.Minute)
	reset := r.clock.Now().Add(10 * time.Minute)
	r.gh.set(func(f *fakeGitHub) { f.err = &github.RateLimitedError{RetryAfter: 10 * time.Minute, ResetAt: reset} })
	r.sync()
	snap := r.snapshot()
	if snap.Sources.Github.State != "rate_limited" || !snap.Sources.Github.ResetAt.Equal(reset) || !snap.Sources.Github.SyncedAt.Equal(good) || len(snap.Prs) != 1 {
		t.Fatalf("rate limited: %+v", snap.Sources.Github)
	}
	if refreshes, snapshotOnly := r.store.counts(); refreshes != 1 || snapshotOnly != 1 {
		t.Fatalf("a failed fetch stores the snapshot only: %d %d", refreshes, snapshotOnly)
	}

	r.gh.set(func(f *fakeGitHub) { f.err = errors.New("502") })
	r.sync()
	if s := r.snapshot().Sources.Github; s.State != "error" || *s.Error != "GitHub could not be reached" || len(r.snapshot().Prs) != 1 {
		t.Fatalf("error: %+v", s)
	}

	r.gh.set(func(f *fakeGitHub) { f.err = nil })
	r.sync()
	if s := r.snapshot().Sources.Github; s.State != "ok" || s.Error != nil || s.ResetAt != nil || !s.SyncedAt.Equal(r.clock.Now()) {
		t.Fatalf("recovered: %+v", s)
	}
}

func TestGitHubRateLimitWithoutAHintUsesBackoff(t *testing.T) {
	r := newRig(t)
	r.gh.set(func(f *fakeGitHub) { f.err = &github.RateLimitedError{} })
	r.sync()
	s := r.snapshot().Sources.Github
	if s.State != "rate_limited" || s.ResetAt == nil || !s.ResetAt.Equal(r.clock.Now().Add(1*time.Second+0)) && s.ResetAt.Sub(r.clock.Now()) <= 0 {
		t.Fatalf("%+v", s)
	}
}

func TestGitHubDisconnectedClearsTheList(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/a", "main", "a"))
	r.sync()
	r.accounts.set(func(a *fakeAccounts) { a.tokenEr = auth.ErrNotConnected })
	r.sync()
	snap := r.snapshot()
	if snap.Sources.Github.State != "disconnected" || len(snap.Prs) != 0 {
		t.Fatalf("%+v", snap.Sources.Github)
	}
	r.accounts.set(func(a *fakeAccounts) { a.tokenEr = errors.New("keychain locked") })
	r.sync()
	if r.snapshot().Sources.Github.State != "error" {
		t.Fatal("an unreadable token is an error")
	}
	r.accounts.set(func(a *fakeAccounts) { a.tokenEr, a.token = nil, "revoked" })
	r.sync()
	if r.snapshot().Sources.Github.State != "disconnected" {
		t.Fatal("a rejected token is disconnected")
	}
}

func TestJiraUnreachableMarksRowsAndKeepsLastGoodTickets(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"), pr("PR_b", 2, "acme/web", "feat/none", "main", "b"))
	r.sync()
	r.clock.Advance(time.Minute)
	r.jira.set(func(f *fakeJira) { f.err = &jira.StatusError{Status: 503, RetryAfter: 30 * time.Second} })
	r.sync()
	snap := r.snapshot()
	a, b := snap.Prs[0], snap.Prs[1]
	if snap.Sources.Jira.State != "unreachable" || *snap.Sources.Jira.Error != "Jira unreachable, retry the lookup" || snap.Sources.Jira.SyncedAt == nil {
		t.Fatalf("%+v", snap.Sources.Jira)
	}
	if a.TicketLookup != "unreachable" || *a.Tickets[0].Summary != "Add login" || b.TicketLookup != "ok" {
		t.Fatalf("rows %+v / %+v", a.TicketLookup, b.TicketLookup)
	}
	if snap.Sources.Github.State != "ok" || len(snap.Prs) != 2 {
		t.Fatal("GitHub rows stay usable")
	}

	r.jira.set(func(f *fakeJira) { f.err = nil })
	r.sync()
	if a := r.snapshot().Prs[0]; a.TicketLookup != "ok" || r.snapshot().Sources.Jira.State != "ok" {
		t.Fatalf("recovered %+v", a.TicketLookup)
	}

	r.jira.set(func(f *fakeJira) { f.projectsErr = errors.New("down") })
	r.sync()
	if r.snapshot().Sources.Jira.State != "unreachable" {
		t.Fatal("a failed project call fails the Jira refresh")
	}
	r.jira.set(func(f *fakeJira) { f.projectsErr = nil; f.err = jira.ErrUnauthorized })
	r.sync()
	if s := r.snapshot(); s.Sources.Jira.State != "disconnected" || s.Prs[0].TicketLookup != "unverified" || s.Prs[0].Tickets[0].Found {
		t.Fatalf("an unauthorized token drops the cache: %+v", s.Sources.Jira)
	}
}

func TestJiraNotConnectedKeepsKeysUnverified(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "fix/UTF-8-decoding", "main", "INP-1 and OPS-9"))
	r.accounts.set(func(a *fakeAccounts) { a.siteErr = auth.ErrNotConnected })
	r.sync()
	snap := r.snapshot()
	row := snap.Prs[0]
	if snap.Sources.Jira.State != "disconnected" || row.TicketLookup != "unverified" || len(row.Tickets) != 3 {
		t.Fatalf("%+v tickets %d", snap.Sources.Jira, len(row.Tickets))
	}
	if row.Tickets[0].Found || row.Tickets[0].Url != nil || row.Tickets[0].Summary != nil {
		t.Fatalf("%+v", row.Tickets[0])
	}
	if len(r.jira.searchLog()) != 0 || r.jira.projectCall != 0 {
		t.Fatal("no Jira call without a connection")
	}

	r.accounts.set(func(a *fakeAccounts) { a.siteErr = nil })
	r.sync()
	row = r.p.Current().Snapshot.Prs[0]
	if len(row.Tickets) != 2 || row.TicketLookup != "ok" {
		t.Fatalf("UTF-8 is dropped once projects are known: %+v", row.Tickets)
	}

	r.accounts.set(func(a *fakeAccounts) { a.siteErr = poller.ErrNoSite })
	r.sync()
	if r.snapshot().Sources.Jira.State != "disconnected" {
		t.Fatal("no site selected")
	}
	r.accounts.set(func(a *fakeAccounts) { a.siteErr = errors.New("keychain locked") })
	r.sync()
}

func TestMissingTicketsAndNewKeysAreLookedUpWithoutWaitingForTheInterval(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.sync()
	if got := r.jira.searchLog(); len(got) != 1 {
		t.Fatal("first refresh")
	}
	// The next GitHub-only refresh brings a pull request with a key Jira has not been asked about.
	b := pr("PR_b", 2, "acme/web", "feat/INP-2", "main", "b")
	c := pr("PR_c", 3, "acme/web", "feat/INP-404", "main", "c")
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = []github.PR{a, b, c} })
	r.clock.Advance(time.Minute)
	drive(t, r.p, r.store, r.clock, func(p *poller.Poller) { p.Reschedule() })

	log := r.jira.searchLog()
	if len(log) != 2 || !slices.Equal(log[1], []string{"INP-2", "INP-404"}) || r.jira.projectCall != 1 {
		t.Fatalf("searches %v projects %d", log, r.jira.projectCall)
	}
	snap := r.snapshot()
	if !snap.Prs[1].Tickets[0].Found || snap.Prs[2].Tickets[0].Found {
		t.Fatalf("%+v %+v", snap.Prs[1].Tickets, snap.Prs[2].Tickets)
	}
	// INP-404 is known to be missing, so it is not asked again before the next full refresh.
	r.clock.Advance(time.Minute)
	drive(t, r.p, r.store, r.clock, func(p *poller.Poller) { p.Reschedule() })
	if len(r.jira.searchLog()) != 2 {
		t.Fatalf("missing keys must not be re-asked: %v", r.jira.searchLog())
	}
}

// drive runs the poller loop until it has done what is due at the current fake time and waits for
// the next timer.
func drive(t *testing.T, p *poller.Poller, _ *countingStore, _ interface{ Now() time.Time }, _ func(*poller.Poller)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := make(chan time.Duration, 8)
	hold := make(chan time.Time)
	q := poller.NewLoopForTest(p, func(d time.Duration) <-chan time.Time { waits <- d; return hold })
	done := make(chan struct{})
	go func() { q.Run(ctx); close(done) }()
	select {
	case <-waits:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop never reached its wait")
	}
	cancel()
	<-done
}

func TestRelinkUsesManualLinksAndAsksJiraOnlyForNewKeys(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.sync()
	ctx := context.Background()
	if err := r.store.SetUserLinks(ctx, "PR_a", []string{"OPS-9"}, []store.HiddenKey{{Key: "INP-1", Source: "branch"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.p.Relink(ctx); err != nil {
		t.Fatal(err)
	}
	snap := r.snapshot()
	row := snap.Prs[0]
	if snap.Version != 2 || len(row.Tickets) != 1 || row.Tickets[0].Key != "OPS-9" || row.Tickets[0].Source != "manual" || !row.Tickets[0].Found ||
		!slices.Equal(row.RemovedKeys, []string{"INP-1"}) {
		t.Fatalf("%d %+v removed %v", snap.Version, row.Tickets, row.RemovedKeys)
	}
	if open, _ := r.gh.calls(); open != 1 {
		t.Fatalf("a relink makes no GitHub call: %d", open)
	}
	if log := r.jira.searchLog(); len(log) != 2 || !slices.Equal(log[1], []string{"OPS-9"}) || r.jira.projectCall != 1 {
		t.Fatalf("jira %v projects %d", log, r.jira.projectCall)
	}
	if refreshes, snapshotOnly := r.store.counts(); refreshes != 1 || snapshotOnly != 1 {
		t.Fatalf("%d %d", refreshes, snapshotOnly)
	}
}

func TestRelinkBeforeAnyRefreshFetchesGitHubFirst(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	if err := r.p.Relink(context.Background()); err != nil {
		t.Fatal(err)
	}
	if open, _ := r.gh.calls(); open != 1 || len(r.snapshot().Prs) != 1 {
		t.Fatalf("open=%d", open)
	}
}

func TestRelinkWaitsForTheRunningRefresh(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.sync()
	r.gh.set(func(f *fakeGitHub) { f.gate = make(chan struct{}); f.entered = make(chan struct{}, 1) })
	go func() { _ = r.p.Sync(context.Background()) }()
	<-r.gh.entered
	done := make(chan error, 1)
	go func() { done <- r.p.Relink(context.Background()) }()
	select {
	case <-done:
		t.Fatal("Relink must wait for the running refresh")
	case <-time.After(50 * time.Millisecond):
	}
	r.gh.set(func(f *fakeGitHub) { close(f.gate) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if v := r.snapshot().Version; v != 3 {
		t.Fatalf("refresh then relink: version %d", v)
	}
}

func TestRelinkReturnsTheContextErrorWhileWaiting(t *testing.T) {
	r := newRig(t)
	r.gh.set(func(f *fakeGitHub) { f.gate = make(chan struct{}); f.entered = make(chan struct{}, 1) })
	go func() { _ = r.p.Sync(context.Background()) }()
	<-r.gh.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.p.Relink(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	r.gh.set(func(f *fakeGitHub) { close(f.gate) })
	r.sync()
}

func TestIntervalDoublesWhileTheRateLimitIsLow(t *testing.T) {
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	base := time.Minute
	tests := []struct {
		name string
		rl   github.RateLimit
		want time.Duration
	}{
		{"plenty left", github.RateLimit{Limit: 5000, Remaining: 4000, ResetAt: now.Add(time.Hour)}, base},
		{"exactly 10 percent", github.RateLimit{Limit: 5000, Remaining: 500, ResetAt: now.Add(time.Hour)}, base},
		{"just under 10 percent", github.RateLimit{Limit: 5000, Remaining: 499, ResetAt: now.Add(time.Hour)}, 2 * base},
		{"low but already reset", github.RateLimit{Limit: 5000, Remaining: 10, ResetAt: now.Add(-time.Second)}, base},
		{"no data yet", github.RateLimit{}, base},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := poller.Interval(base, tc.rl, now); got != tc.want {
				t.Fatalf("%v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunSchedulesFromSettingsAndAdaptsToTheRateLimit(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.gh.set(func(f *fakeGitHub) {
		f.result.RateLimit = github.RateLimit{Limit: 5000, Remaining: 100, ResetAt: r.clock.Now().Add(30 * time.Minute)}
	})
	waits := make(chan time.Duration, 8)
	fire := make(chan time.Time)
	p := poller.NewLoopForTest(r.p, func(d time.Duration) <-chan time.Time { waits <- d; return fire })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	wait := func() time.Duration {
		t.Helper()
		select {
		case d := <-waits:
			return d
		case <-time.After(5 * time.Second):
			t.Fatal("no wait")
			return 0
		}
	}
	if d := wait(); d != 2*time.Minute {
		t.Fatalf("first wait %v: the GitHub interval of 60 s doubles while less than 10%% is left", d)
	}
	if open, _ := r.gh.calls(); open != 1 || r.snapshot().Version != 1 {
		t.Fatalf("the first refresh runs at once: %d", open)
	}
	if st := r.p.Status(); st.NextGithubAt == nil || !st.NextGithubAt.Equal(r.clock.Now().Add(2*time.Minute)) ||
		st.NextJiraAt == nil || !st.NextJiraAt.Equal(r.clock.Now().Add(5*time.Minute)) {
		t.Fatalf("status %+v", st)
	}

	// The limit resets: back to the configured interval.
	r.clock.Advance(31 * time.Minute)
	r.gh.set(func(f *fakeGitHub) {
		f.result.RateLimit = github.RateLimit{Limit: 5000, Remaining: 4900, ResetAt: r.clock.Now().Add(time.Hour)}
	})
	fire <- r.clock.Now()
	if d := wait(); d != time.Minute {
		t.Fatalf("after the reset %v", d)
	}
	if open, _ := r.gh.calls(); open != 2 {
		t.Fatalf("open %d", open)
	}

	// A settings change is picked up without a restart.
	s := store.DefaultSettings()
	s.GitHubSec, s.JiraSec = 900, 3600
	if err := r.store.PutSettings(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	r.p.Reschedule()
	if d := wait(); d != 15*time.Minute {
		t.Fatalf("new interval %v", d)
	}
	if open, _ := r.gh.calls(); open != 2 {
		t.Fatal("changing the settings must not refresh")
	}
}

func TestRunBacksOffAfterFailuresAndWakesForTheJiraInterval(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.gh.set(func(f *fakeGitHub) { f.err = &github.RateLimitedError{RetryAfter: 20 * time.Minute} })
	waits := make(chan time.Duration, 8)
	fire := make(chan time.Time)
	p := poller.NewLoopForTest(r.p, func(d time.Duration) <-chan time.Time { waits <- d; return fire })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	select {
	case d := <-waits:
		// GitHub is rate limited for 20 minutes, Jira is due in 5.
		if d != 5*time.Minute {
			t.Fatalf("wait %v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no wait")
	}
	if st := r.p.Status(); st.NextGithubAt == nil || !st.NextGithubAt.Equal(r.clock.Now().Add(20*time.Minute)) {
		t.Fatalf("%+v", st)
	}
}

func TestStatusReportsRunning(t *testing.T) {
	r := newRig(t)
	if st := r.p.Status(); st.Running || st.GithubAt != nil || st.NextGithubAt != nil {
		t.Fatalf("%+v", st)
	}
	r.sync()
	st := r.p.Status()
	if st.Running || st.GithubAt == nil || st.JiraAt == nil {
		t.Fatalf("%+v", st)
	}
}

func TestEveryRefreshStoresExactlyOneTransaction(t *testing.T) {
	r := newRig(t, stackFixture(newRig(t))...)
	for range 4 {
		r.sync()
	}
	if refreshes, snapshotOnly := r.store.counts(); refreshes != 4 || snapshotOnly != 0 {
		t.Fatalf("%d %d", refreshes, snapshotOnly)
	}
}

func TestVersionsAreMonotonic(t *testing.T) {
	r := newRig(t)
	var seen []int64
	for range 3 {
		r.sync()
		seen = append(seen, r.snapshot().Version)
	}
	if !slices.Equal(seen, []int64{1, 2, 3}) {
		t.Fatal(fmt.Sprint(seen))
	}
}

func TestRunWaitsWhenJiraIsNotConnected(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a"))
	r.accounts.set(func(a *fakeAccounts) { a.siteErr = auth.ErrNotConnected })
	waits := make(chan time.Duration, 8)
	fire := make(chan time.Time)
	p := poller.NewLoopForTest(r.p, func(d time.Duration) <-chan time.Time { waits <- d; return fire })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	select {
	case d := <-waits:
		if d != time.Minute {
			t.Fatalf("wait %v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the loop must wait instead of refreshing again")
	}
	if v := r.snapshot().Version; v != 1 {
		t.Fatalf("one refresh, got version %d", v)
	}
}
