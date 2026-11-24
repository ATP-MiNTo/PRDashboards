package poller_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

// countingStore wraps the real store and counts its write transactions.
type countingStore struct {
	*store.Store
	mu           sync.Mutex
	refreshes    int
	snapshotOnly int
	syncWrites   int
}

func (s *countingStore) SaveRefresh(ctx context.Context, r store.Refresh) error {
	s.mu.Lock()
	s.refreshes++
	s.mu.Unlock()
	return s.Store.SaveRefresh(ctx, r)
}

func (s *countingStore) SaveSnapshot(ctx context.Context, v int64, d []byte, now time.Time) error {
	s.mu.Lock()
	s.snapshotOnly++
	s.mu.Unlock()
	return s.Store.SaveSnapshot(ctx, v, d, now)
}

func (s *countingStore) SaveSync(ctx context.Context, v int64, d []byte, now time.Time, w store.SyncWrite) error {
	s.mu.Lock()
	s.syncWrites++
	s.mu.Unlock()
	return s.Store.SaveSync(ctx, v, d, now, w)
}

func (s *countingStore) counts() (refreshes, snapshotOnly int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshes, s.snapshotOnly
}

type fakeGitHub struct {
	mu          sync.Mutex
	result      github.SearchResult
	err         error
	merged      map[string]time.Time
	mergedErr   error
	openCalls   int
	mergedCalls int
	lastSince   time.Time
	gate        chan struct{}
	entered     chan struct{}
}

func (f *fakeGitHub) SearchOpen(_ context.Context, token string) (github.SearchResult, error) {
	f.mu.Lock()
	f.openCalls++
	gate, entered := f.gate, f.entered
	res, err := f.result, f.err
	f.mu.Unlock()
	if token != "tok" {
		return github.SearchResult{}, github.ErrBadCredentials
	}
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		<-gate
	}
	return res, err
}

func (f *fakeGitHub) SearchMerged(_ context.Context, _ string, since time.Time) (github.MergedResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mergedCalls++
	f.lastSince = since
	return github.MergedResult{Merged: f.merged}, f.mergedErr
}

func (f *fakeGitHub) set(fn func(f *fakeGitHub)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeGitHub) calls() (open, merged int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.openCalls, f.mergedCalls
}

type fakeJira struct {
	mu          sync.Mutex
	issues      map[string]jira.Issue
	projects    []string
	err         error
	projectsErr error
	searches    [][]string
	projectCall int

	transitions     map[string][]jira.Transition
	transitionsErr  map[string]error
	moveErr         map[string]error
	transitionCalls []string
	moves           []string
	inflight        int
	maxInflight     int
}

func (f *fakeJira) Search(_ context.Context, _ string, keys []string) (jira.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searches = append(f.searches, append([]string(nil), keys...))
	if f.err != nil {
		return jira.SearchResult{}, f.err
	}
	res := jira.SearchResult{Issues: map[string]jira.Issue{}}
	for _, k := range keys {
		if i, ok := f.issues[k]; ok {
			res.Issues[k] = i
		} else {
			res.Missing = append(res.Missing, k)
		}
	}
	return res, nil
}

func (f *fakeJira) Projects(context.Context, string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectCall++
	return f.projects, f.projectsErr
}

func (f *fakeJira) Transitions(_ context.Context, _, key string) ([]jira.Transition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transitionCalls = append(f.transitionCalls, key)
	if err := f.transitionsErr[key]; err != nil {
		return nil, err
	}
	return f.transitions[key], nil
}

func (f *fakeJira) Transition(_ context.Context, _, key, id string, _ map[string]any) error {
	f.mu.Lock()
	f.inflight++
	f.maxInflight = max(f.maxInflight, f.inflight)
	f.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inflight--
	f.moves = append(f.moves, key+":"+id)
	return f.moveErr[key]
}

func (f *fakeJira) peak() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxInflight
}

func (f *fakeJira) moveLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.moves...)
}

func (f *fakeJira) transitionLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.transitionCalls...)
}

func (f *fakeJira) set(fn func(f *fakeJira)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeJira) searchLog() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.searches...)
}

type fakeAccounts struct {
	mu      sync.Mutex
	token   string
	tokenEr error
	site    jira.Site
	siteErr error
}

func (a *fakeAccounts) GitHubToken() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.token, a.tokenEr
}

func (a *fakeAccounts) JiraSite(context.Context) (jira.Site, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.site, a.siteErr
}

func (a *fakeAccounts) set(fn func(a *fakeAccounts)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn(a)
}

type published struct {
	Event   string
	Payload any
}

type recorder struct {
	mu     sync.Mutex
	events []published
}

func (r *recorder) Publish(event string, payload any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, published{event, payload})
	return nil
}

func (r *recorder) named(name string) []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []any
	for _, e := range r.events {
		if e.Event == name {
			out = append(out, e.Payload)
		}
	}
	return out
}

type rig struct {
	t        *testing.T
	p        *poller.Poller
	store    *countingStore
	gh       *fakeGitHub
	jira     *fakeJira
	accounts *fakeAccounts
	events   *recorder
	clock    *testutil.Clock
}

func newRig(t *testing.T, prs ...github.PR) *rig {
	t.Helper()
	clock := testutil.NewClock()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r := &rig{
		t:     t,
		store: &countingStore{Store: db},
		gh: &fakeGitHub{result: github.SearchResult{
			PRs:       prs,
			RateLimit: github.RateLimit{Limit: 5000, Remaining: 4900, ResetAt: clock.Now().Add(time.Hour)},
		}},
		jira: &fakeJira{
			projects: []string{"INP", "OPS"},
			issues: map[string]jira.Issue{
				"INP-1": {Key: "INP-1", Summary: "Add login", Status: "In Review", StatusCategory: "indeterminate", Project: "INP", IssueType: "Story"},
				"INP-2": {Key: "INP-2", Summary: "Second", Status: "To Do", StatusCategory: "new", Project: "INP", IssueType: "Bug"},
				"OPS-9": {Key: "OPS-9", Summary: "Manual", Status: "Done", StatusCategory: "done", Project: "OPS", IssueType: "Task"},
			},
		},
		accounts: &fakeAccounts{token: "tok", site: jira.Site{ID: "cloud-1", URL: "https://acme.atlassian.net"}},
		events:   &recorder{},
		clock:    clock,
	}
	r.p = r.newPoller(db, nil)
	return r
}

func (r *rig) newPoller(_ *store.Store, after func(time.Duration) <-chan time.Time) *poller.Poller {
	return poller.New(poller.Config{
		Store:     r.store,
		GitHub:    r.gh,
		Jira:      r.jira,
		Accounts:  r.accounts,
		Publisher: r.events,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     r.clock.Now,
		Backoff:   backoff.Policy{Clock: r.clock.Now, Jitter: func() float64 { return 0 }},
		After:     after,
	})
}

func (r *rig) sync() {
	r.t.Helper()
	if err := r.p.Sync(context.Background()); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) snapshot() gen.Snapshot { return r.p.Current().Snapshot }

func ago(clock *testutil.Clock, d time.Duration) *time.Time {
	t := clock.Now().Add(-d)
	return &t
}

func pr(id string, n int, repo, head, base, title string) github.PR {
	return github.PR{
		ID: id, Number: n, Title: title, URL: "https://github.com/" + repo + "/pull/" + strconv.Itoa(n),
		Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", ReviewDecision: "APPROVED",
		HeadRef: head, HeadSHA: "sha-" + id, BaseRef: base,
		Repo: github.Repo{ID: "R-" + repo, NameWithOwner: repo, SquashAllowed: true, MergeAllowed: true, RebaseAllowed: true},
		CI:   github.CI{State: github.CINone},
	}
}
