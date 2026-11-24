package merge_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

type fakeGitHub struct {
	mu        sync.Mutex
	pr        github.PR
	prErr     error
	mergeErr  error
	deleteErr error
	mergeGate chan struct{}
	pullCalls int
	merges    []mergeCall
	deletes   []string
	mergeSHA  string
}

type mergeCall struct{ repo, method, sha string }

func (f *fakeGitHub) PullRequest(context.Context, string, string) (github.PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullCalls++
	return f.pr, f.prErr
}

func (f *fakeGitHub) Merge(_ context.Context, _, repo string, _ int, method, sha string) (github.MergeResult, error) {
	f.mu.Lock()
	gate := f.mergeGate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merges = append(f.merges, mergeCall{repo, method, sha})
	if f.mergeErr != nil {
		return github.MergeResult{}, f.mergeErr
	}
	return github.MergeResult{SHA: f.mergeSHA}, nil
}

func (f *fakeGitHub) DeleteBranch(_ context.Context, _, repo, branch string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, repo+":"+branch)
	return f.deleteErr
}

func (f *fakeGitHub) mergeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.merges)
}

type fakeJira struct {
	mu          sync.Mutex
	transitions map[string][]jira.Transition
	listErr     map[string]error
	moveErr     map[string]error
	listCalls   int
	moves       []string
	movedWith   map[string]map[string]any
	running     atomic.Int32
	peak        atomic.Int32
	hold        chan struct{}
}

func (f *fakeJira) Transitions(_ context.Context, _, key string) ([]jira.Transition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if err := f.listErr[key]; err != nil {
		return nil, err
	}
	return f.transitions[key], nil
}

func (f *fakeJira) Transition(_ context.Context, _, key, id string, fields map[string]any) error {
	now := f.running.Add(1)
	defer f.running.Add(-1)
	for {
		peak := f.peak.Load()
		if now <= peak || f.peak.CompareAndSwap(peak, now) {
			break
		}
	}
	if f.hold != nil {
		<-f.hold
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, key+":"+id)
	if f.movedWith == nil {
		f.movedWith = map[string]map[string]any{}
	}
	f.movedWith[key] = fields
	return f.moveErr[key]
}

func (f *fakeJira) moveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.moves)
}

type fakeSnapshot struct {
	linked  map[string]merge.Linked
	refs    map[string][]merge.Ref
	firings []store.Firing
	askedPR string
	askedTK []string
	err     error
}

func (s fakeSnapshot) Linked(id string) (merge.Linked, bool) { l, ok := s.linked[id]; return l, ok }
func (s fakeSnapshot) Refs(repo string) []merge.Ref          { return s.refs[repo] }

func (s *fakeSnapshot) MergedFirings(_ context.Context, pr string, tickets []string) ([]store.Firing, error) {
	s.askedPR, s.askedTK = pr, tickets
	return s.firings, s.err
}

type fakeAccounts struct {
	tokenErr, siteErr error
}

func (a fakeAccounts) GitHubToken() (string, error) { return "tok", a.tokenErr }
func (a fakeAccounts) JiraSite(context.Context) (jira.Site, error) {
	return jira.Site{ID: "cloud-1"}, a.siteErr
}

type events struct {
	mu   sync.Mutex
	list []string
}

func (e *events) Publish(name string, _ any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, name)
	return nil
}

// countingStore counts the write transactions a merge makes.
type countingStore struct {
	*store.Store
	records atomic.Int32
}

func (s *countingStore) RecordMerge(ctx context.Context, m store.Merged) error {
	s.records.Add(1)
	return s.Store.RecordMerge(ctx, m)
}

type rig struct {
	t         *testing.T
	svc       *merge.Service
	gh        *fakeGitHub
	jira      *fakeJira
	store     *countingStore
	snapshot  *fakeSnapshot
	accounts  *fakeAccounts
	events    *events
	refreshes atomic.Int32
	clock     *testutil.Clock
	path      string
}

const prID = "PR_1"

func basePR() github.PR {
	return github.PR{
		ID: prID, Number: 7, State: "OPEN", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", ReviewDecision: "APPROVED",
		HeadRef: "feat/x", HeadSHA: "sha1", BaseRef: "main",
		Repo: github.Repo{ID: "R1", NameWithOwner: "acme/web", SquashAllowed: true, MergeAllowed: true, RebaseAllowed: true},
	}
}

func doneTransition() jira.Transition {
	return jira.Transition{ID: "31", Name: "Done", ToStatus: "Done", ToCategory: "done", Fields: []jira.Field{
		{ID: "resolution", Name: "Resolution", Required: true, Type: "resolution", AllowedValues: []jira.Option{{ID: "10000", Name: "Done"}, {ID: "10001", Name: "Won't Do"}}},
	}}
}

func progress() jira.Transition {
	return jira.Transition{ID: "21", Name: "Start", ToStatus: "In Progress", ToCategory: "indeterminate", Fields: []jira.Field{}}
}

func newRig(t *testing.T, tickets ...string) *rig {
	t.Helper()
	clock := testutil.NewClock()
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := store.Open(path, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r := &rig{
		t: t, clock: clock, path: path, events: &events{}, accounts: &fakeAccounts{},
		gh:    &fakeGitHub{pr: basePR(), mergeSHA: "mergesha"},
		jira:  &fakeJira{transitions: map[string][]jira.Transition{}, listErr: map[string]error{}, moveErr: map[string]error{}},
		store: &countingStore{Store: db},
		snapshot: &fakeSnapshot{
			linked: map[string]merge.Linked{prID: {RepoID: "R1", NameWithOwner: "acme/web", Number: 7, Tickets: tickets}},
			refs:   map[string][]merge.Ref{"R1": {{ID: prID, Number: 7, Head: "feat/x", Base: "main"}}},
		},
	}
	for _, k := range tickets {
		r.jira.transitions[k] = []jira.Transition{doneTransition(), progress()}
	}
	r.svc = merge.New(merge.Config{
		GitHub: r.gh, Jira: r.jira, Store: r.store, Snapshot: r.snapshot, Accounts: r.accounts,
		Refresh: func() { r.refreshes.Add(1) }, Publisher: r.events,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Clock: clock.Now,
	})
	return r
}

var keySeq atomic.Int32

func key() string {
	return "00000000-0000-4000-8000-" + time12(keySeq.Add(1))
}

func time12(n int32) string {
	const digits = "0123456789"
	out := []byte("000000000000")
	for i := len(out) - 1; n > 0 && i >= 0; i-- {
		out[i] = digits[n%10]
		n /= 10
	}
	return string(out)
}

func request(tickets ...merge.Ticket) merge.Request {
	return merge.Request{PRID: prID, IdempotencyKey: key(), Method: merge.Squash, ExpectedHeadSHA: "sha1", Tickets: tickets}
}

func done(key string) merge.Ticket {
	return merge.Ticket{Key: key, TransitionID: "31", Fields: map[string]string{"resolution": "10000"}}
}

func wantError(t *testing.T, err error, code string) *merge.Error {
	t.Helper()
	var e *merge.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("error %v, want code %s", err, code)
	}
	return e
}

func within(d time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}
