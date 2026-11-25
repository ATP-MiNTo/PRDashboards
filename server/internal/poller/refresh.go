package poller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/events"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/linking"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

// ErrNoSite means Jira is connected but no site is selected.
var ErrNoSite = jira.ErrNoSite

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func tptr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func sptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (p *Poller) jiraStatus() gen.SourceStatus {
	return gen.SourceStatus{State: p.jira.state, SyncedAt: tptr(p.jira.syncedAt), Error: sptr(p.jira.err)}
}

// refresh runs one refresh: the due fetches, then linking, snapshot building, persistence and the
// announcement. Fetch failures never fail the refresh. They are recorded in the snapshot's sources
// and the last good data stays.
func (p *Poller) refresh(ctx context.Context, req request) error {
	now := p.cfg.Clock()
	st, err := p.cfg.Store.GetSettings(ctx)
	if err != nil {
		return err
	}
	if req.relink && !p.gh.loaded {
		req.github = true
	}

	var resolved []store.Resolution
	fetched := false
	if req.github {
		resolved, fetched = p.fetchGitHub(ctx, now)
		p.mu.Lock()
		p.lastGH = now
		p.mu.Unlock()
	}

	user, err := p.cfg.Store.UserLinks(ctx)
	if err != nil {
		return err
	}
	state, err := p.cfg.Store.SyncState(ctx, now.Add(-mergedWindow), resolvedMerged(resolved))
	if err != nil {
		p.cfg.Log.WarnContext(ctx, "read status sync state", "err", err)
		state = store.SyncState{}
	}
	site, jiraDue := p.connectJira(ctx, req)
	projectsOK := true
	if site.ID != "" && jiraDue {
		projectsOK = p.fetchProjects(ctx, now, site)
	}
	ls := resolve(p.gh.prs, user, p.jira.projects)
	keys := keysOf(ls)
	if hasEnabled(state.Rules) {
		keys = withKeys(keys, mergedKeys(state))
	}
	if site.ID != "" && projectsOK {
		p.lookupTickets(ctx, now, site, keys, jiraDue)
	}
	if jiraDue || req.jira {
		p.mu.Lock()
		p.lastJira = now
		p.mu.Unlock()
	}

	repos, err := p.cfg.Store.Repos(ctx)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.ls = ls
	p.mu.Unlock()
	out := p.runRules(ctx, now, state, ls, site, st.ExcludedStatuses)

	p.mu.Lock()
	p.version++
	version := p.version
	snap := build(buildInput{
		Now: now, Version: version, Linked: ls, Repos: mergeRepos(repos), NewRepo: newRepo(st.DefaultMethod), Issues: p.jira.issues, Site: p.jira.site,
		GitHub: p.ghSrc, Jira: p.jiraStatus(), Blocked: p.gh.blocked, Warnings: p.gh.warnings, Waiting: out.waiting, Plans: out.plans,
	})
	p.mu.Unlock()

	raw, err := encode(snap)
	if err != nil {
		return err
	}
	data, err := compress(raw)
	if err != nil {
		return err
	}
	var saveErr error
	if fetched {
		saveErr = p.cfg.Store.SaveRefresh(ctx, store.Refresh{
			Now: now, DefaultMethod: st.DefaultMethod, Repos: repoRows(p.gh.prs), PRs: prRows(p.gh.prs),
			Links: linkRows(ls), Resolved: resolved, Version: version, Snapshot: data, Sync: out.write,
		})
	} else if out.write.Empty() {
		saveErr = p.cfg.Store.SaveSnapshot(ctx, version, data, now)
	} else {
		saveErr = p.cfg.Store.SaveSync(ctx, version, data, now, out.write)
	}
	p.cur.Store(&Published{Snapshot: snap, JSON: raw})
	if p.cfg.Publisher != nil {
		_ = p.cfg.Publisher.Publish(events.Snapshot, map[string]int64{"version": version})
	}
	if saveErr == nil {
		p.publishActivity(out.write)
	}
	return saveErr
}

// withKeys adds the extra keys to the sorted, unique list.
func withKeys(keys, extra []string) []string {
	all := slices.Concat(keys, extra)
	slices.Sort(all)
	return slices.Compact(all)
}

func repoRows(prs []github.PR) []store.RepoRow {
	seen := make(map[string]struct{}, len(prs))
	var out []store.RepoRow
	for _, pr := range prs {
		if _, dup := seen[pr.Repo.ID]; !dup {
			seen[pr.Repo.ID] = struct{}{}
			out = append(out, store.RepoRow{ID: pr.Repo.ID, NameWithOwner: pr.Repo.NameWithOwner})
		}
	}
	return out
}

func prRows(prs []github.PR) []store.PRRow {
	out := make([]store.PRRow, len(prs))
	for i, pr := range prs {
		out[i] = store.PRRow{
			ID: pr.ID, RepoID: pr.Repo.ID, Number: pr.Number, Title: pr.Title,
			HeadBranch: pr.HeadRef, BaseBranch: pr.BaseRef, HeadSHA: pr.HeadSHA, Draft: pr.Draft,
		}
	}
	return out
}

// fetchGitHub runs the search, and the merged search when pull requests left the list. It returns
// the resolutions to store and whether the list was replaced by fresh data.
func (p *Poller) fetchGitHub(ctx context.Context, now time.Time) ([]store.Resolution, bool) {
	token, err := p.cfg.Accounts.GitHubToken(ctx)
	if err != nil {
		if errors.Is(err, auth.ErrNotConnected) {
			p.mu.Lock()
			p.gh, p.ghSrc = ghData{loaded: true}, gen.SourceStatus{State: "disconnected"}
			p.mu.Unlock()
			return nil, false
		}
		p.failGitHub(now, err)
		return nil, false
	}
	res, err := p.cfg.GitHub.SearchOpen(ctx, token)
	if err != nil {
		p.failGitHub(now, err)
		return nil, false
	}

	var warnings []gen.Warning
	if res.Capped {
		warnings = append(warnings, gen.Warning{Code: "github_search_cap", Message: "GitHub search returns at most 1,000 results, so some pull requests may be missing."})
	}
	if res.Incomplete {
		warnings = append(warnings, gen.Warning{Code: "github_incomplete", Message: "GitHub did not return every page, so some pull requests may be missing."})
	}
	if len(res.Errors) > 0 {
		warnings = append(warnings, gen.Warning{Code: "github_partial", Message: "GitHub reported errors for part of the list: " + truncate(res.Errors[0], 200)})
	}
	var resolved []store.Resolution
	if !res.Incomplete && !res.Capped && len(res.Errors) == 0 && len(res.Blocked) == 0 {
		var w *gen.Warning
		resolved, w = p.resolveLeft(ctx, token, res.PRs)
		if w != nil {
			warnings = append(warnings, *w)
		}
	}

	p.mu.Lock()
	p.gh = ghData{prs: res.PRs, blocked: res.Blocked, warnings: warnings, loaded: true}
	p.ghSrc = gen.SourceStatus{State: "ok", SyncedAt: &now}
	p.rl = res.RateLimit
	p.ghFails, p.ghBackoff = 0, time.Time{}
	p.mu.Unlock()
	return resolved, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (p *Poller) failGitHub(now time.Time, err error) {
	p.cfg.Log.Warn("github refresh failed", "err", err)
	p.mu.Lock()
	defer p.mu.Unlock()
	src := gen.SourceStatus{State: "error", SyncedAt: p.ghSrc.SyncedAt, Error: sptr("GitHub could not be reached")}
	var rl *github.RateLimitedError
	switch {
	case errors.As(err, &rl):
		src.State, src.Error = "rate_limited", sptr("GitHub rate limit reached")
		wait := rl.RetryAfter
		if wait <= 0 {
			wait, _ = p.cfg.Backoff.Delay(nil, p.ghFails)
		}
		p.ghBackoff = now.Add(wait)
		src.ResetAt = tptr(p.ghBackoff)
	case errors.Is(err, github.ErrBadCredentials):
		src.State, src.Error = "disconnected", sptr("GitHub rejected the token")
		p.gh = ghData{loaded: true}
	case errors.Is(err, auth.ErrNeedsReconnect):
		src.State, src.Error = "disconnected", sptr("Reconnect GitHub")
		p.gh = ghData{loaded: true}
	default:
		wait, _ := p.cfg.Backoff.Delay(http.Header(nil), p.ghFails)
		p.ghBackoff = now.Add(wait)
	}
	p.ghFails++
	p.ghSrc = src
}

// resolveLeft settles the pull requests that were open at the last refresh and are not in the list
// now, with one merged search for all of them.
func (p *Poller) resolveLeft(ctx context.Context, token string, current []github.PR) ([]store.Resolution, *gen.Warning) {
	stored, err := p.cfg.Store.OpenPRs(ctx)
	if err != nil {
		p.cfg.Log.Warn("read open pull requests", "err", err)
		return nil, nil
	}
	here := make(map[string]struct{}, len(current))
	for _, pr := range current {
		here[pr.ID] = struct{}{}
	}
	var left []string
	var oldest time.Time
	for id, seen := range stored {
		if _, ok := here[id]; ok {
			continue
		}
		left = append(left, id)
		if oldest.IsZero() || seen.Before(oldest) {
			oldest = seen
		}
	}
	if len(left) == 0 {
		return nil, nil
	}
	slices.Sort(left)
	merged, err := p.cfg.GitHub.SearchMerged(ctx, token, oldest.Add(-time.Minute))
	if err != nil {
		p.cfg.Log.Warn("merged pull request search failed", "err", err)
		return nil, &gen.Warning{Code: "merged_lookup_failed", Message: "Pull requests that left the list could not be checked yet. They will be checked again."}
	}
	out := make([]store.Resolution, len(left))
	for i, id := range left {
		out[i] = store.Resolution{ID: id, State: "closed"}
		if at, ok := merged.Merged[id]; ok {
			unix := at.Unix()
			out[i] = store.Resolution{ID: id, State: "merged", MergedAt: &unix}
		}
	}
	return out, nil
}

// connectJira reads the Jira connection. It returns the site, or an empty site when Jira cannot be
// used, and whether the full Jira refresh is due.
func (p *Poller) connectJira(ctx context.Context, req request) (jira.Site, bool) {
	site, err := p.cfg.Accounts.JiraSite(ctx)
	if err != nil {
		if errors.Is(err, auth.ErrNotConnected) || errors.Is(err, auth.ErrNeedsReconnect) || errors.Is(err, ErrNoSite) {
			p.mu.Lock()
			p.jira = jiraData{state: "disconnected", err: "Jira is not connected", projects: linking.Projects{}}
			p.mu.Unlock()
		} else {
			p.cfg.Log.Warn("read jira site", "err", err)
		}
		return jira.Site{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.jira.site.ID != site.ID {
		p.jira = jiraData{state: "pending", projects: linking.Projects{}}
	}
	p.jira.site = site
	return site, req.jira || p.jira.state == "pending" && !req.relink
}

// ProjectKeys lists, sorted, the Jira project keys the last full Jira refresh stored. It makes no
// call and is nil until that refresh succeeded.
func (p *Poller) ProjectKeys() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.jira.projects.Keys()
}

// fetchProjects loads the project keys, once per full Jira refresh.
func (p *Poller) fetchProjects(ctx context.Context, now time.Time, site jira.Site) bool {
	keys, err := p.cfg.Jira.Projects(ctx, site.ID)
	if err != nil {
		p.failJira(now, err)
		return false
	}
	p.mu.Lock()
	p.jira.projects = linking.NewProjects(keys)
	p.mu.Unlock()
	return true
}

// lookupTickets asks Jira for the tickets of the snapshot. A full refresh asks for every key. Otherwise
// only keys never looked up are asked, so a new pull request does not wait for the interval.
func (p *Poller) lookupTickets(ctx context.Context, now time.Time, site jira.Site, keys []string, full bool) {
	p.mu.Lock()
	ask := keys
	if !full {
		if p.jira.state != "ok" {
			p.mu.Unlock()
			return
		}
		ask = nil
		for _, k := range keys {
			_, have := p.jira.issues[k]
			_, gone := p.jira.missing[k]
			if !have && !gone {
				ask = append(ask, k)
			}
		}
	}
	p.mu.Unlock()
	if len(ask) == 0 && !full {
		return
	}
	res, err := p.cfg.Jira.Search(ctx, site.ID, ask)
	if err != nil {
		p.failJira(now, err)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if full || p.jira.issues == nil {
		p.jira.issues, p.jira.missing = map[string]jira.Issue{}, map[string]struct{}{}
	}
	for k, i := range res.Issues {
		p.jira.issues[k] = i
	}
	for _, k := range res.Missing {
		p.jira.missing[k] = struct{}{}
	}
	if full {
		p.jira.state, p.jira.err, p.jira.syncedAt = "ok", "", now
		p.jiraFails, p.jiraBackoff = 0, time.Time{}
	}
}

func (p *Poller) failJira(now time.Time, err error) {
	p.cfg.Log.Warn("jira refresh failed", "err", err)
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case errors.Is(err, auth.ErrNeedsReconnect), errors.Is(err, auth.ErrNotConnected), errors.Is(err, jira.ErrUnauthorized):
		p.jira.state, p.jira.err = "disconnected", "Jira needs to be connected again"
		p.jira.issues, p.jira.missing, p.jira.projects = nil, nil, linking.Projects{}
	default:
		p.jira.state, p.jira.err = "unreachable", "Jira unreachable, retry the lookup"
	}
	var se *jira.StatusError
	wait, _ := p.cfg.Backoff.Delay(http.Header(nil), p.jiraFails)
	if errors.As(err, &se) && se.RetryAfter > 0 {
		wait = se.RetryAfter
	}
	p.jiraBackoff = now.Add(wait)
	p.jiraFails++
}
