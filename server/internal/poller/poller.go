// Package poller keeps the dashboard data fresh. It runs one refresh at a time, builds an immutable
// snapshot from GitHub and Jira, stores it, and announces it on the event hub (TECH_SPEC 10).
package poller

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/events"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/linking"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

// Store is the part of the database the poller uses.
type Store interface {
	GetSettings(ctx context.Context) (store.Settings, error)
	LoadSnapshot(ctx context.Context) (store.StoredSnapshot, error)
	SaveRefresh(ctx context.Context, r store.Refresh) error
	SaveSnapshot(ctx context.Context, version int64, data []byte, now time.Time) error
	SaveSync(ctx context.Context, version int64, data []byte, now time.Time, w store.SyncWrite) error
	SyncState(ctx context.Context, since time.Time, justMerged []string) (store.SyncState, error)
	OpenPRs(ctx context.Context) (map[string]time.Time, error)
	Repos(ctx context.Context) ([]store.RepoConfig, error)
	UserLinks(ctx context.Context) (map[string]store.UserLinks, error)
}

// GitHub is the search client.
type GitHub interface {
	SearchOpen(ctx context.Context, token string) (github.SearchResult, error)
	SearchMerged(ctx context.Context, token string, since time.Time) (github.MergedResult, error)
}

// Jira is the lookup client.
type Jira interface {
	Search(ctx context.Context, site string, keys []string) (jira.SearchResult, error)
	Projects(ctx context.Context, site string) ([]string, error)
	Transitions(ctx context.Context, site, key string) ([]jira.Transition, error)
	Transition(ctx context.Context, site, key, id string, fields map[string]any) error
}

// Accounts says who the poller talks as. GitHubToken returns an error that wraps
// auth.ErrNotConnected when GitHub is not connected. JiraSite returns one that wraps
// auth.ErrNotConnected, auth.ErrNeedsReconnect or ErrNoSite.
type Accounts interface {
	GitHubToken(ctx context.Context) (string, error)
	JiraSite(ctx context.Context) (jira.Site, error)
}

// Publisher announces events to the browsers.
type Publisher interface {
	Publish(event string, payload any) error
}

// Config holds the poller's collaborators. Clock and After are injected by tests.
type Config struct {
	Store     Store
	GitHub    GitHub
	Jira      Jira
	Accounts  Accounts
	Publisher Publisher
	Log       *slog.Logger
	Clock     func() time.Time
	Backoff   backoff.Policy
	After     func(d time.Duration) <-chan time.Time
}

// Published is the snapshot as served: the structure and its JSON, encoded once.
type Published struct {
	Snapshot gen.Snapshot
	JSON     []byte
}

// ETag is the quoted version.
func (p *Published) ETag() string { return `"` + strconv.FormatInt(p.Snapshot.Version, 10) + `"` }

type request struct {
	github, jira, relink bool
}

type flight struct {
	done chan struct{}
	err  error
}

type ghData struct {
	prs      []github.PR
	blocked  []github.OrgBlocked
	warnings []gen.Warning
	loaded   bool
}

type jiraData struct {
	site     jira.Site
	state    gen.SourceState
	err      string
	projects linking.Projects
	issues   map[string]jira.Issue
	missing  map[string]struct{}
	syncedAt time.Time
}

// Poller schedules and runs refreshes.
type Poller struct {
	cfg Config

	mu      sync.Mutex
	base    context.Context
	cancel  context.CancelFunc
	closed  bool
	flights sync.WaitGroup
	fl      *flight
	version int64
	gh      ghData
	ls      []linked
	jira    jiraData
	ghSrc   gen.SourceStatus
	rl      github.RateLimit

	lastGH, lastJira       time.Time
	nextGH, nextJira       time.Time
	ghBackoff, jiraBackoff time.Time
	ghFails, jiraFails     int

	cur  atomic.Pointer[Published]
	wake chan struct{}
}

// New creates a poller. Call Load to restore the stored snapshot and Run to start polling.
func New(cfg Config) *Poller {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	base, cancel := context.WithCancel(context.Background())
	p := &Poller{cfg: cfg, base: base, cancel: cancel, wake: make(chan struct{}, 1)}
	p.ghSrc = gen.SourceStatus{State: "pending"}
	p.jira = jiraData{state: "pending", projects: linking.Projects{}}
	p.cur.Store(emptySnapshot(cfg.Clock(), p.ghSrc, p.jiraStatus()))
	return p
}

func emptySnapshot(now time.Time, g, j gen.SourceStatus) *Published {
	s := build(buildInput{Now: now, GitHub: g, Jira: j})
	b, _ := encode(s)
	return &Published{Snapshot: s, JSON: b}
}

// Current is the latest snapshot. Before the first refresh it is empty with version 0.
func (p *Poller) Current() *Published { return p.cur.Load() }

// PR finds a pull request of the current snapshot.
func (p *Poller) PR(id string) (gen.SnapshotPr, bool) {
	for _, pr := range p.cur.Load().Snapshot.Prs {
		if pr.Id == id {
			return pr, true
		}
	}
	return gen.SnapshotPr{}, false
}

// Linked describes a pull request of the current snapshot for a merge.
func (p *Poller) Linked(id string) (merge.Linked, bool) {
	pr, ok := p.PR(id)
	if !ok {
		return merge.Linked{}, false
	}
	keys := make([]string, len(pr.Tickets))
	for i, t := range pr.Tickets {
		keys[i] = t.Key
	}
	return merge.Linked{RepoID: pr.Repo.Id, NameWithOwner: pr.Repo.NameWithOwner, Number: pr.Number, Tickets: keys}, true
}

// Refs lists the branches of the open pull requests of one repository in the current snapshot.
func (p *Poller) Refs(repoID string) []merge.Ref {
	var out []merge.Ref
	for _, pr := range p.cur.Load().Snapshot.Prs {
		if pr.Repo.Id == repoID {
			out = append(out, merge.Ref{ID: pr.Id, Number: pr.Number, Head: pr.HeadRef, Base: pr.BaseRef})
		}
	}
	return out
}

// Load restores the snapshot saved by the last run, so the UI has data before the first refresh.
// A missing or unreadable snapshot is not an error.
func (p *Poller) Load(ctx context.Context) error {
	stored, err := p.cfg.Store.LoadSnapshot(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	raw, err := decompress(stored.Data)
	if err != nil {
		p.cfg.Log.WarnContext(ctx, "stored snapshot is unreadable, starting empty", "err", err)
		return nil
	}
	var snap gen.Snapshot
	if err := jsonUnmarshal(raw, &snap); err != nil {
		p.cfg.Log.WarnContext(ctx, "stored snapshot is unreadable, starting empty", "err", err)
		return nil
	}
	p.mu.Lock()
	p.version = stored.Version
	p.ghSrc, p.jira.state = snap.Sources.Github, snap.Sources.Jira.State
	if t := snap.Sources.Jira.SyncedAt; t != nil {
		p.jira.syncedAt = *t
	}
	if t := snap.Sources.Github.SyncedAt; t != nil {
		p.lastGH = *t
	}
	p.mu.Unlock()
	p.cur.Store(&Published{Snapshot: snap, JSON: raw})
	return nil
}

// Status is the state of the sync machinery for the top bar.
func (p *Poller) Status() gen.SyncStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statusLocked()
}

func (p *Poller) statusLocked() gen.SyncStatus {
	st := gen.SyncStatus{Running: p.fl != nil}
	set := func(dst **time.Time, t time.Time) {
		if !t.IsZero() {
			v := t
			*dst = &v
		}
	}
	if p.ghSrc.SyncedAt != nil {
		set(&st.GithubAt, *p.ghSrc.SyncedAt)
	}
	set(&st.JiraAt, p.jira.syncedAt)
	set(&st.NextGithubAt, p.nextGH)
	set(&st.NextJiraAt, p.nextJira)
	return st
}

// Reschedule makes the run loop re-read the settings, for example after the intervals changed.
func (p *Poller) Reschedule() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Sync runs a full refresh now and waits for it. A call during a refresh joins that refresh.
func (p *Poller) Sync(ctx context.Context) error {
	return p.wait(ctx, p.start(request{github: true, jira: true}))
}

// Trigger starts a full refresh without waiting, or joins the running one, and returns the status.
func (p *Poller) Trigger() gen.SyncStatus {
	p.start(request{github: true, jira: true})
	return p.Status()
}

// Relink rebuilds the snapshot from the data already fetched after the manual links or hidden keys
// of a pull request changed. It waits for a running refresh first, so the rebuild sees the change.
// It asks Jira only for tickets it has not seen, and GitHub only when it has no data yet.
func (p *Poller) Relink(ctx context.Context) error {
	p.mu.Lock()
	running := p.fl
	p.mu.Unlock()
	if running != nil {
		if err := p.wait(ctx, running); err != nil && ctx.Err() != nil {
			return err
		}
	}
	return p.wait(ctx, p.start(request{relink: true}))
}

func (p *Poller) wait(ctx context.Context, f *flight) error {
	select {
	case <-f.done:
		return f.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Poller) start(req request) *flight {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		f := &flight{done: make(chan struct{}), err: context.Canceled}
		close(f.done)
		return f
	}
	if p.fl != nil {
		f := p.fl
		p.mu.Unlock()
		return f
	}
	f := &flight{done: make(chan struct{})}
	p.fl = f
	ctx := p.base
	p.flights.Add(1)
	p.mu.Unlock()
	p.publishStatus()
	go func() {
		defer p.flights.Done()
		f.err = p.refresh(ctx, req)
		if f.err != nil {
			p.cfg.Log.ErrorContext(ctx, "refresh failed", "err", f.err)
		}
		p.mu.Lock()
		p.fl = nil
		p.mu.Unlock()
		p.publishStatus()
		close(f.done)
	}()
	return f
}

func (p *Poller) publishStatus() {
	if p.cfg.Publisher != nil {
		_ = p.cfg.Publisher.Publish(events.SyncStatus, p.Status())
	}
}

// Interval is the wait between GitHub refreshes: the configured interval, doubled while less than
// a tenth of the rate limit is left and the limit has not reset yet (TECH_SPEC 10.4).
func Interval(base time.Duration, rl github.RateLimit, now time.Time) time.Duration {
	if rl.Low() && now.Before(rl.ResetAt) {
		return 2 * base
	}
	return base
}

// Run polls until ctx ends. It refreshes at once, then when the GitHub or the Jira interval of the
// settings elapses. The settings are read again before every wait.
func (p *Poller) Run(ctx context.Context) {
	p.mu.Lock()
	if p.closed {
		p.base, p.cancel = context.WithCancel(context.Background())
		p.closed = false
	}
	cancel := p.cancel
	p.mu.Unlock()
	stop := context.AfterFunc(ctx, cancel)
	defer func() {
		stop()
		cancel()
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		p.flights.Wait()
	}()
	for {
		st, err := p.cfg.Store.GetSettings(ctx)
		if err != nil {
			p.cfg.Log.ErrorContext(ctx, "read settings", "err", err)
			st = store.DefaultSettings()
		}
		now := p.cfg.Clock()
		req, wait := p.plan(st, now)
		if req.github || req.jira {
			f := p.start(req)
			select {
			case <-f.done:
			case <-ctx.Done():
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
		case <-p.cfg.After(wait):
		}
	}
}

// plan decides what is due at now and, when nothing is, how long to wait.
func (p *Poller) plan(st store.Settings, now time.Time) (request, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextGH = later(p.lastGH.Add(Interval(time.Duration(st.GitHubSec)*time.Second, p.rl, now)), p.ghBackoff)
	p.nextJira = later(p.lastJira.Add(time.Duration(st.JiraSec)*time.Second), p.jiraBackoff)
	req := request{github: !now.Before(p.nextGH), jira: !now.Before(p.nextJira)}
	if req.github || req.jira {
		return req, 0
	}
	return req, earlier(p.nextGH, p.nextJira).Sub(now)
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
