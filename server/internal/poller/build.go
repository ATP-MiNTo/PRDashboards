package poller

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/linking"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/stacks"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

// linked is a pull request with its resolved tickets.
type linked struct {
	pr      github.PR
	res     linking.Result
	removed []string
}

// resolve links every pull request to its tickets, in one pass.
func resolve(prs []github.PR, user map[string]store.UserLinks, projects linking.Projects) []linked {
	out := make([]linked, len(prs))
	for i, pr := range prs {
		u := user[pr.ID]
		out[i] = linked{
			pr:      pr,
			res:     linking.Resolve(linking.Input{HeadRef: pr.HeadRef, Title: pr.Title, Manual: u.Manual, Removed: u.Removed}, projects),
			removed: u.Removed,
		}
	}
	return out
}

func keysOf(ls []linked) []string {
	in := make([]linking.PRLinks, len(ls))
	for i, l := range ls {
		in[i] = linking.PRLinks{PRID: l.pr.ID, Links: l.res.Links}
	}
	return linking.AllKeys(in)
}

type buildInput struct {
	Now     time.Time
	Version int64
	Linked  []linked
	// Repos holds the merge settings of the repos the store knows. A repo missing from it is seen for
	// the first time and gets NewRepo.
	Repos   map[string]merge.Repo
	NewRepo merge.Repo
	Issues  map[string]jira.Issue
	Site    jira.Site
	GitHub  gen.SourceStatus
	// RateLimit is the GitHub budget the last refresh saw; the zero value (no data) leaves it out.
	RateLimit github.RateLimit
	Jira      gen.SourceStatus
	Blocked   []github.OrgBlocked
	Warnings  []gen.Warning
	// Waiting is the hint of each ticket whose mode all rule is waiting on other pull requests.
	Waiting map[string]gen.TicketWaiting
	// Plans maps "prID|ticket" to the merge plan of that ticket on that row.
	Plans map[string]gen.TicketMergePlan
}

// rateLimitUsage maps the budget GitHub reported with the searches to the snapshot's used and limit
// (used is limit minus remaining, never below 0 or above the limit). It is nil when GitHub reported
// no limit, so the UI shows nothing.
func rateLimitUsage(rl github.RateLimit) *gen.RateLimitUsage {
	if rl.Limit <= 0 {
		return nil
	}
	out := &gen.RateLimitUsage{Used: min(max(rl.Limit-rl.Remaining, 0), rl.Limit), Limit: rl.Limit}
	if !rl.ResetAt.IsZero() {
		out.ResetAt = tptr(rl.ResetAt)
	}
	return out
}

// rateLimitOf is the inverse of rateLimitUsage, for a snapshot read back from the store.
func rateLimitOf(u *gen.RateLimitUsage) github.RateLimit {
	if u == nil || u.Limit <= 0 {
		return github.RateLimit{}
	}
	rl := github.RateLimit{Limit: u.Limit, Remaining: max(u.Limit-u.Used, 0)}
	if u.ResetAt != nil {
		rl.ResetAt = *u.ResetAt
	}
	return rl
}

func ptr[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// build assembles the immutable snapshot. Pull requests are ordered by repo and number. Stack
// positions come from one pass over the list.
func build(in buildInput) gen.Snapshot {
	ls := slices.Clone(in.Linked)
	slices.SortFunc(ls, func(a, b linked) int {
		if c := strings.Compare(a.pr.Repo.NameWithOwner, b.pr.Repo.NameWithOwner); c != 0 {
			return c
		}
		return a.pr.Number - b.pr.Number
	})
	sp := make([]stacks.PR, len(ls))
	for i, l := range ls {
		sp[i] = stacks.PR{ID: l.pr.ID, Repo: l.pr.Repo.ID, Number: l.pr.Number, HeadRef: l.pr.HeadRef, BaseRef: l.pr.BaseRef}
	}
	stackOf := stacks.Detect(sp)

	ghSrc := in.GitHub
	ghSrc.RateLimit = rateLimitUsage(in.RateLimit)
	snap := gen.Snapshot{
		Version:     in.Version,
		GeneratedAt: in.Now,
		Prs:         make([]gen.SnapshotPr, 0, len(ls)),
		Warnings:    in.Warnings,
		Sources:     gen.Sources{Github: ghSrc, Jira: in.Jira, BlockedOrgs: make([]gen.BlockedOrg, len(in.Blocked))},
	}
	if snap.Warnings == nil {
		snap.Warnings = []gen.Warning{}
	}
	for i, b := range in.Blocked {
		snap.Sources.BlockedOrgs[i] = gen.BlockedOrg{Org: b.Org}
	}
	for _, l := range ls {
		snap.Prs = append(snap.Prs, snapshotPR(l, in, stackOf))
	}
	return snap
}

// mergeRepos maps the stored repos to the resolver's input. A repo that follows the default takes
// the global default method (Settings defaultMethodForNewRepos), not the one it has stored.
func mergeRepos(repos []store.RepoConfig, global string) map[string]merge.Repo {
	out := make(map[string]merge.Repo, len(repos))
	for _, r := range repos {
		rules := make([]merge.Rule, len(r.Rules))
		for i, rule := range r.Rules {
			rules[i] = merge.Rule{Pattern: rule.Pattern, Method: merge.Method(rule.Method)}
		}
		method := r.DefaultMethod
		if r.InheritMethod {
			method = global
		}
		out[r.ID] = merge.Repo{Default: merge.Method(method), IsNew: r.IsNew, Follows: r.InheritMethod, Rules: rules, Approvals: r.ApprovalsRequired}
	}
	return out
}

// newRepo is the configuration of a repo the store has not seen: the global default for new repos
// and the seeded branch rules.
func newRepo(method string) merge.Repo {
	rules := make([]merge.Rule, len(store.SeedRules))
	for i, rule := range store.SeedRules {
		rules[i] = merge.Rule{Pattern: rule.Pattern, Method: merge.Method(rule.Method)}
	}
	return merge.Repo{Default: merge.Method(method), IsNew: true, Rules: rules, Approvals: merge.DefaultApprovals}
}

func approvalsOf(a merge.Approvals) gen.Approvals {
	return gen.Approvals{Have: a.Have, Need: a.Need, ChangesRequested: a.ChangesRequested}
}

func resolutionOf(r merge.Resolution) gen.MethodResolution {
	allowed := make([]gen.MergeMethodName, len(r.Allowed))
	for i, m := range r.Allowed {
		allowed[i] = gen.MergeMethodName(m)
	}
	return gen.MethodResolution{
		Method: gen.MergeMethodName(r.Method), Default: gen.MergeMethodName(r.Default), Source: r.Source,
		Allowed: allowed, Warning: ptr(r.Warning),
	}
}

func snapshotPR(l linked, in buildInput, stackOf map[string]stacks.Info) gen.SnapshotPr {
	pr := l.pr
	repo, known := in.Repos[pr.Repo.ID]
	if !known {
		repo = in.NewRepo
	}
	row := gen.SnapshotPr{
		Id: pr.ID, Number: pr.Number, Title: pr.Title,
		Repo: gen.SnapshotRepo{
			Id: pr.Repo.ID, NameWithOwner: pr.Repo.NameWithOwner, IsNew: repo.IsNew,
			SquashAllowed: pr.Repo.SquashAllowed, MergeAllowed: pr.Repo.MergeAllowed, RebaseAllowed: pr.Repo.RebaseAllowed,
		},
		IsDraft:          pr.Draft,
		Roles:            rolesOf(pr.Roles),
		Mergeable:        gen.SnapshotPrMergeable(mergeable(pr.Mergeable)),
		MergeStateStatus: pr.MergeStateStatus,
		HeadRef:          pr.HeadRef, HeadSha: pr.HeadSHA, BaseRef: pr.BaseRef,
		CommittedAt:    pr.CommittedAt,
		Author:         ptr(pr.Author),
		UpdatedAt:      pr.UpdatedAt,
		Additions:      &pr.Additions,
		Deletions:      &pr.Deletions,
		Ci:             ciOf(pr.CI),
		Reviews:        make([]gen.Review, len(pr.Reviews)),
		ReviewRequests: append([]string{}, pr.ReviewRequests...),
		RemovedKeys:    append([]string{}, l.removed...),
		Tickets:        make([]gen.SnapshotTicket, 0, len(l.res.Links)),
		MergeMethod: resolutionOf(merge.Resolve(repo, pr.BaseRef,
			merge.Allowed{Squash: pr.Repo.SquashAllowed, Merge: pr.Repo.MergeAllowed, Rebase: pr.Repo.RebaseAllowed})),
		Approvals: approvalsOf(merge.CountApprovals(pr.Reviews, repo.Approvals)),
	}
	if d := gen.SnapshotPrReviewDecision(pr.ReviewDecision); d != "" {
		row.ReviewDecision = &d
	}
	for i, r := range pr.Reviews {
		row.Reviews[i] = gen.Review{Id: r.ID, State: r.State, SubmittedAt: r.SubmittedAt, Author: ptr(r.Author)}
	}
	if s, ok := stackOf[pr.ID]; ok {
		info := &gen.StackInfo{Id: s.RootID, Position: s.Position, Size: s.Size}
		if s.ParentID != "" {
			n := s.ParentNumber
			info.ParentId, info.ParentNumber = &s.ParentID, &n
		}
		row.Stack = info
	}
	for _, link := range l.res.Links {
		row.Tickets = append(row.Tickets, ticketOf(link, pr.ID, in))
	}
	row.TicketLookup = lookupState(len(row.Tickets), l.res.Unverified, in.Jira.State)
	return row
}

// rolesOf maps the roles the search recorded. A pull request without any (a refresh that predates
// roles) counts for assigned and authored, so the dashboard still lists it under the Mine Show
// state; it never counts as review requested, which only the review search can say.
func rolesOf(roles []string) []gen.SnapshotPrRoles {
	out := make([]gen.SnapshotPrRoles, 0, 3)
	if len(roles) == 0 {
		roles = []string{github.RoleAssigned, github.RoleAuthored}
	}
	for _, r := range []string{github.RoleAssigned, github.RoleAuthored, github.RoleReviewer} {
		if slices.Contains(roles, r) {
			out = append(out, gen.SnapshotPrRoles(r))
		}
	}
	return out
}

// fillRoles gives every pull request of a snapshot stored before roles existed assigned and
// authored. It reports whether it changed anything.
func fillRoles(s *gen.Snapshot) bool {
	changed := false
	for i := range s.Prs {
		if len(s.Prs[i].Roles) == 0 {
			s.Prs[i].Roles, changed = rolesOf(nil), true
		}
	}
	return changed
}

// fillApprovals gives every pull request of a stored snapshot the tally of its stored reviews
// against need(repoID), which is the repository's setting now (0, nothing required, when unknown).
// It does not look at what the snapshot holds, because 0 is a real value of Need: a snapshot stored
// before approvals existed, or before the setting changed (migration 0007 turned the old default of
// 1 into 0), gets the same fresh tally as any other. It reports whether it changed anything. The
// next refresh replaces them.
func fillApprovals(s *gen.Snapshot, need func(repoID string) int) bool {
	changed := false
	for i := range s.Prs {
		reviews := make([]github.Review, len(s.Prs[i].Reviews))
		for j, r := range s.Prs[i].Reviews {
			reviews[j] = github.Review{ID: r.Id, State: r.State}
			if r.Author != nil {
				reviews[j].Author = *r.Author
			}
		}
		tally := approvalsOf(merge.CountApprovals(reviews, need(s.Prs[i].Repo.Id)))
		if s.Prs[i].Approvals != tally {
			s.Prs[i].Approvals, changed = tally, true
		}
	}
	return changed
}

func mergeable(s string) string {
	switch s {
	case "MERGEABLE", "CONFLICTING":
		return s
	}
	return "UNKNOWN"
}

func lookupState(tickets int, unverified bool, jira gen.SourceState) gen.TicketLookup {
	switch {
	case tickets == 0:
		return "ok"
	case jira == "unreachable":
		return "unreachable"
	case jira != "ok" || unverified:
		return "unverified"
	}
	return "ok"
}

func ticketOf(link linking.Link, prID string, in buildInput) gen.SnapshotTicket {
	t := gen.SnapshotTicket{Key: link.Key, Source: gen.TicketSource(link.Source), Project: linking.ProjectOf(link.Key)}
	if in.Site.URL != "" {
		u := strings.TrimRight(in.Site.URL, "/") + "/browse/" + link.Key
		t.Url = &u
	}
	if w, ok := in.Waiting[link.Key]; ok {
		t.Waiting = &w
	}
	if plan, ok := in.Plans[prID+"|"+link.Key]; ok {
		t.MergePlan = &plan
	}
	if issue, ok := in.Issues[link.Key]; ok {
		t.Found = true
		t.Summary, t.Status, t.StatusCategory = &issue.Summary, &issue.Status, ptr(issue.StatusCategory)
		t.IssueType = ptr(issue.IssueType)
		if issue.Project != "" {
			t.Project = issue.Project
		}
	}
	return t
}

func ciOf(ci github.CI) gen.Ci {
	out := gen.Ci{State: gen.CiState(ci.State), Total: ci.Total, Checks: make([]gen.Check, len(ci.Checks))}
	for i, c := range ci.Checks {
		out.Checks[i] = gen.Check{
			Name: c.Name, App: ptr(c.App), State: gen.CheckState(c.State), Url: ptr(c.URL),
			StartedAt: c.StartedAt, CompletedAt: c.CompletedAt,
		}
	}
	return out
}

// linkRows are the automatically found links to persist, in order per pull request.
func linkRows(ls []linked) []store.LinkRow {
	var rows []store.LinkRow
	for _, l := range ls {
		for i, link := range l.res.Links {
			if link.Source != linking.Manual {
				rows = append(rows, store.LinkRow{PRID: l.pr.ID, Key: link.Key, Source: string(link.Source), Position: i})
			}
		}
	}
	return rows
}

func encode(s gen.Snapshot) ([]byte, error) { return json.Marshal(s) }

func compress(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decompress(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	return io.ReadAll(io.LimitReader(zr, 64<<20))
}
