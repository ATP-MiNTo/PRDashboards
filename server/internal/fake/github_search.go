//go:build e2e

package fake

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// fixturePR describes one pull request of the fake search.
type fixturePR struct {
	repo, org      string
	number         int
	title          string
	head, base     string
	draft          bool
	mergeable      string
	mergeState     string
	decision       string
	committedAgo   time.Duration
	rollup         string
	checks         []map[string]any
	totalChecks    int
	reviewApproved bool
	requested      bool
	// authored, unassigned and reviewer say how the viewer relates to the pull request. By
	// default it is assigned to the viewer and opened by someone else. authored marks one the
	// viewer opened (assignee:@me and author:@me both match when it is also assigned), unassigned
	// drops the assignment (only author:@me matches) and reviewer marks one the viewer is asked to
	// review (review-requested:@me matches; with unassigned it is the only search that does).
	authored, unassigned, reviewer bool
	// viewerPermission is the viewer's role on the repository (ADMIN, MAINTAIN, WRITE, TRIAGE or
	// READ); empty means WRITE.
	viewerPermission string
	// stacked marks the bottom pull request of a GitHub-native stack: the synchronous merge refuses it.
	stacked bool
	// author, updatedAgo, additions and deletions fill the author, updatedAt and diff size. Left
	// zero they default to a colleague (the viewer, octocat, for an authored pull request), 5
	// half the age of the last commit, and 12 and 3 lines.
	author               string
	updatedAgo           time.Duration
	additions, deletions int
	// extra are the reviews the viewer submitted through the fake.
	extra []fakeReview
}

// viewerOpened reports whether the viewer (octocat) opened the pull request.
func (p fixturePR) viewerOpened() bool { return p.authored }

// openedBy reports whether the account with the given login opened the pull request.
func (p fixturePR) openedBy(login string) bool { return p.authored && login == defaultLogin }

// matchesViewer reports whether the search query selects the pull request for the viewer.
// A query with none of assignee:@me, author:@me and review-requested:@me matches everything.
func (p fixturePR) matchesViewer(query string) bool { return p.matchesViewerAs(query, defaultLogin) }

// matchesViewerAs is matchesViewer for the account with the given login. The pull requests the
// viewer opened were opened by defaultLogin, so author:@me finds them only for that account.
func (p fixturePR) matchesViewerAs(query, login string) bool {
	assignee, author, review := strings.Contains(query, "assignee:@me"), strings.Contains(query, "author:@me"), strings.Contains(query, "review-requested:@me")
	if !assignee && !author && !review {
		return true
	}
	return (assignee && !p.unassigned) || (author && p.authored && login == defaultLogin) || (review && p.reviewer)
}

func checkRun(name, app, status, conclusion, url string, started, completed time.Time) map[string]any {
	c := map[string]any{
		"__typename": "CheckRun", "name": name, "status": status, "detailsUrl": url,
		"startedAt":  started.UTC().Format(time.RFC3339),
		"checkSuite": map[string]any{"app": map[string]any{"name": app}},
	}
	if conclusion != "" {
		c["conclusion"] = conclusion
		c["completedAt"] = completed.UTC().Format(time.RFC3339)
	}
	return c
}

func statusContext(name, state, url string, created time.Time) map[string]any {
	return map[string]any{
		"__typename": "StatusContext", "context": name, "state": state, "targetUrl": url,
		"createdAt": created.UTC().Format(time.RFC3339),
	}
}

// multiPRs are the pull requests of the multi-PR ticket INP-105, one per repository.
func multiPRs() []fixturePR {
	pr := func(repo string, number int, name string) fixturePR {
		return fixturePR{
			repo: repo, org: "acme", number: number, title: "INP-105 " + name, head: "feat/INP-105-" + name, base: "main",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "REVIEW_REQUIRED", committedAgo: time.Hour, rollup: "SUCCESS",
		}
	}
	return []fixturePR{pr("acme/fe", 34, "frontend"), pr("acme/be", 12, "backend"), pr("acme/proxy", 56, "proxy")}
}

// stackedPRs are a GitHub-native stack of two approved pull requests, #21 below #22.
func stackedPRs() []fixturePR {
	pr := func(number int, name, head, base string, stacked bool) fixturePR {
		return fixturePR{
			repo: "acme/stack", org: "acme", number: number, title: name, head: head, base: base, stacked: stacked,
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "APPROVED", reviewApproved: true, committedAgo: time.Hour, rollup: "SUCCESS",
		}
	}
	return []fixturePR{
		pr(21, "Stack bottom", "feat/stack-a", "main", true),
		pr(22, "Stack top", "feat/stack-b", "feat/stack-a", false),
	}
}

// reviewPRs are the pull requests of the review actions, all in acme/review: #31 has one approval,
// #32 is a draft and #33 was opened by the viewer.
func reviewPRs() []fixturePR {
	return []fixturePR{
		{
			repo: "acme/review", org: "acme", number: 31, title: "REV-31 Needs two approvals", head: "feat/rev-31", base: "main",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "APPROVED", reviewApproved: true, committedAgo: time.Hour, rollup: "SUCCESS",
		},
		{
			repo: "acme/review", org: "acme", number: 32, title: "REV-32 Draft to finish", head: "feat/rev-32", base: "main", draft: true,
			mergeable: "MERGEABLE", mergeState: "DRAFT", decision: "", committedAgo: 2 * time.Hour, rollup: "",
		},
		{
			repo: "acme/review", org: "acme", number: 33, title: "REV-33 My own change", head: "feat/rev-33", base: "main", authored: true,
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "REVIEW_REQUIRED", committedAgo: 3 * time.Hour, rollup: "SUCCESS",
		},
	}
}

// readOnlyPRs is one pull request in a repository where the viewer can only read (a public
// repository of another owner). It is approved and green, so only the role keeps it from merging.
func readOnlyPRs() []fixturePR {
	return []fixturePR{{
		repo: "oss/lib", org: "oss", number: 41, title: "RO-41 Someone else's repository", head: "feat/ro-41", base: "main",
		mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "APPROVED", reviewApproved: true, committedAgo: time.Hour, rollup: "SUCCESS",
		viewerPermission: "READ",
	}}
}

// fixturePRs are the pull requests of the fake search, with the approvals and the extra pull
// requests the config asks for.
func fixturePRs(now time.Time, cfg githubConfig) []fixturePR {
	prs := basePRs(now)
	if cfg.MultiPR {
		prs = append(prs, multiPRs()...)
	}
	if cfg.Stacked {
		prs = append(prs, stackedPRs()...)
	}
	if cfg.Review {
		prs = append(prs, reviewPRs()...)
	}
	if cfg.ReadOnly {
		prs = append(prs, readOnlyPRs()...)
	}
	for i, p := range prs {
		if slices.Contains(cfg.Approved, p.number) {
			prs[i].decision, prs[i].reviewApproved = "APPROVED", true
		}
		// What the review actions changed: a draft marked ready, and the viewer's reviews.
		if slices.Contains(cfg.ready, p.id()) {
			prs[i].draft, prs[i].mergeState = false, "CLEAN"
		}
		for _, rv := range cfg.reviews {
			if rv.PR != p.id() {
				continue
			}
			prs[i].extra = append(prs[i].extra, rv)
			prs[i].decision = map[string]string{"APPROVE": "APPROVED", "REQUEST_CHANGES": "CHANGES_REQUESTED"}[rv.Event]
		}
	}
	return prs
}

func basePRs(now time.Time) []fixturePR {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	run := func(repo string, id int) string {
		return fmt.Sprintf("https://github.com/%s/actions/runs/%d", repo, id)
	}
	return []fixturePR{
		{
			repo: "acme/web", org: "acme", number: 101, additions: 214, deletions: 38, title: "INP-101 Add login", head: "feat/INP-101-login", base: "main",
			mergeable: "MERGEABLE", mergeState: "BLOCKED", decision: "APPROVED", committedAgo: 3 * time.Hour, rollup: "FAILURE", reviewApproved: true,
			checks: []map[string]any{
				checkRun("build", "GitHub Actions", "COMPLETED", "SUCCESS", run("acme/web", 1001)+"/job/1", ago(20*time.Minute), ago(18*time.Minute)),
				checkRun("unit-tests", "GitHub Actions", "COMPLETED", "FAILURE", run("acme/web", 1001)+"/job/2", ago(20*time.Minute), ago(15*time.Minute)),
				checkRun("e2e", "GitHub Actions", "IN_PROGRESS", "", run("acme/web", 1001)+"/job/3", ago(4*time.Minute), time.Time{}),
				statusContext("ci/jenkins", "SUCCESS", "https://ci.example.com/job/web/42", ago(19*time.Minute)),
			},
		},
		{
			repo: "acme/web", org: "acme", number: 102, additions: 96, deletions: 12, title: "INP-102 Single sign-on", head: "feat/INP-102-sso", base: "feat/INP-101-login",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "REVIEW_REQUIRED", committedAgo: 90 * time.Minute, rollup: "SUCCESS", requested: true,
			authored: true, // assigned and opened by the viewer: the assignee and author searches find it
			checks: []map[string]any{
				checkRun("build", "GitHub Actions", "COMPLETED", "SUCCESS", run("acme/web", 1002)+"/job/1", ago(30*time.Minute), ago(28*time.Minute)),
				checkRun("lint", "GitHub Actions", "COMPLETED", "NEUTRAL", run("acme/web", 1002)+"/job/2", ago(30*time.Minute), ago(29*time.Minute)),
				checkRun("docs", "GitHub Actions", "COMPLETED", "SKIPPED", run("acme/web", 1002)+"/job/3", ago(30*time.Minute), ago(30*time.Minute)),
			},
		},
		{
			repo: "acme/web", org: "acme", number: 103, additions: 140, deletions: 4, title: "INP-103 Account menu", head: "feat/INP-103-menu", base: "feat/INP-102-sso",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "", committedAgo: 40 * time.Minute, rollup: "PENDING",
			checks: []map[string]any{
				checkRun("build", "GitHub Actions", "QUEUED", "", run("acme/web", 1003)+"/job/1", ago(time.Minute), time.Time{}),
			},
		},
		{
			repo: "acme/api", org: "acme", number: 7, additions: 58, deletions: 61, title: "WIP schema cleanup", head: "chore/schema-cleanup", base: "main", draft: true,
			mergeable: "UNKNOWN", mergeState: "DRAFT", decision: "", committedAgo: 5 * 24 * time.Hour, rollup: "",
			authored: true, unassigned: true, // opened by the viewer, assigned to nobody: only author:@me finds it
		},
		{
			repo: "acme/api", org: "acme", number: 10, additions: 31, deletions: 9, title: "Tighten rate limiter", head: "fix/rate-limiter", base: "main",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "REVIEW_REQUIRED", committedAgo: 5 * time.Hour, rollup: "SUCCESS",
			author: "dana", reviewer: true, unassigned: true, // someone else's, I am asked to review: only review-requested:@me finds it
		},
		{
			repo: "acme/web", org: "acme", number: 104, additions: 17, deletions: 2, title: "Fix focus ring on tabs", head: "fix/tab-focus", base: "main",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "REVIEW_REQUIRED", committedAgo: 2 * time.Hour, rollup: "SUCCESS",
			author: "eve", reviewer: true, unassigned: true,
		},
		{
			repo: "acme/api", org: "acme", number: 8, additions: 22, deletions: 7, title: "OPS-12 Rotate signing keys", head: "fix/OPS-12-keys", base: "main",
			mergeable: "CONFLICTING", mergeState: "DIRTY", decision: "APPROVED", committedAgo: 26 * time.Hour, rollup: "FAILURE", reviewApproved: true,
			checks: []map[string]any{
				statusContext("buildkite/api", "FAILURE", "https://buildkite.example.org/acme/api/builds/311", ago(25*time.Hour)),
				checkRun("unit-tests", "CircleCI Checks", "COMPLETED", "CANCELLED", "https://circleci.example.com/jobs/9", ago(25*time.Hour), ago(25*time.Hour)),
			},
		},
		{
			repo: "acme/api", org: "acme", number: 9, additions: 512, deletions: 690, title: "Bump dependencies", head: "chore/deps", base: "main",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "APPROVED", committedAgo: time.Hour, rollup: "SUCCESS", reviewApproved: true,
			totalChecks: 62,
			checks: []map[string]any{
				checkRun("build", "GitHub Actions", "COMPLETED", "SUCCESS", run("acme/api", 2001)+"/job/1", ago(30*time.Minute), ago(25*time.Minute)),
			},
		},
		{
			repo: "acme-corp/billing", org: "acme-corp", number: 5, additions: 88, deletions: 15, title: "INP-104 Invoice export", head: "feat/INP-104-export", base: "main",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "APPROVED", committedAgo: 2 * time.Hour, rollup: "SUCCESS", reviewApproved: true,
			checks: []map[string]any{
				checkRun("build", "GitHub Actions", "COMPLETED", "SUCCESS", run("acme-corp/billing", 3001)+"/job/1", ago(60*time.Minute), ago(58*time.Minute)),
			},
		},
	}
}

func (p fixturePR) id() string {
	return fmt.Sprintf("PR_%s_%d", strings.ReplaceAll(p.repo, "/", "_"), p.number)
}

func (p fixturePR) headSHA(moved bool) string {
	n := p.number * 7919
	if moved {
		n++
	}
	return fmt.Sprintf("%040x", n)
}

func findFixture(repo string, number int, cfg githubConfig) (fixturePR, bool) {
	for _, p := range fixturePRs(time.Now(), cfg) {
		if p.repo == repo && p.number == number {
			return p, true
		}
	}
	return fixturePR{}, false
}

func (p fixturePR) node(now time.Time, moved bool, state string) map[string]any {
	return p.nodeAs(now, moved, state, cmp.Or(p.viewerPermission, "WRITE"))
}

// nodeAs is node with the viewer's role on the repository, which depends on the token.
func (p fixturePR) nodeAs(now time.Time, moved bool, state, permission string) map[string]any {
	total := p.totalChecks
	if total == 0 {
		total = len(p.checks)
	}
	var rollup any
	if p.rollup != "" {
		rollup = map[string]any{"state": p.rollup, "contexts": map[string]any{"totalCount": total, "nodes": p.checks}}
	}
	reviews := []any{}
	if p.reviewApproved {
		reviews = append(reviews, map[string]any{
			"id": fmt.Sprintf("RV_%d", p.number), "state": "APPROVED",
			"submittedAt": now.Add(-time.Hour).UTC().Format(time.RFC3339), "author": map[string]any{"login": "alice"},
		})
	}
	// The viewer's latest review replaces the one before it, as latestReviews does.
	if n := len(p.extra); n > 0 {
		last := p.extra[n-1]
		state := map[string]string{"APPROVE": "APPROVED", "REQUEST_CHANGES": "CHANGES_REQUESTED"}[last.Event]
		reviews = append(reviews, map[string]any{
			"id": fmt.Sprintf("RV_%d_viewer", p.number), "state": state,
			"submittedAt": now.UTC().Format(time.RFC3339), "author": map[string]any{"login": "octocat"},
		})
	}
	requests := []any{}
	if p.requested {
		requests = append(requests, map[string]any{"requestedReviewer": map[string]any{"__typename": "User", "id": "U_bob"}})
	}
	var decision any
	if p.decision != "" {
		decision = p.decision
	}
	author, updatedAgo, additions, deletions := p.author, p.updatedAgo, p.additions, p.deletions
	if author == "" {
		author = "priya"
		if p.authored {
			author = "octocat"
		}
	}
	if updatedAgo == 0 {
		updatedAgo = p.committedAgo / 2
	}
	if additions == 0 && deletions == 0 {
		additions, deletions = 12, 3
	}
	return map[string]any{
		"__typename": "PullRequest",
		"id":         p.id(), "state": state, "isCrossRepository": false,
		"number": p.number, "title": p.title, "url": fmt.Sprintf("https://github.com/%s/pull/%d", p.repo, p.number),
		"isDraft": p.draft, "mergeable": p.mergeable, "mergeStateStatus": p.mergeState, "reviewDecision": decision,
		"headRefName": p.head, "headRefOid": p.headSHA(moved), "baseRefName": p.base,
		"author": map[string]any{"login": author}, "updatedAt": now.Add(-updatedAgo).UTC().Format(time.RFC3339),
		"additions": additions, "deletions": deletions,
		"repository": map[string]any{
			"id": "R_" + strings.ReplaceAll(p.repo, "/", "_"), "nameWithOwner": p.repo,
			"squashMergeAllowed": true, "mergeCommitAllowed": true, "rebaseMergeAllowed": p.repo != "acme/api",
			"viewerPermission": permission,
		},
		"commits": map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{
			"committedDate":     now.Add(-p.committedAgo).UTC().Format(time.RFC3339),
			"statusCheckRollup": rollup,
		}}}},
		"latestReviews":  map[string]any{"nodes": reviews},
		"reviewRequests": map[string]any{"nodes": requests},
	}
}

// searchBody answers the open and merged pull request searches, honouring assignee:@me, author:@me
// and review-requested:@me, so the dashboard's union of the three searches is exercised. With the sso option the pull
// requests of acme-corp are left out, as GitHub does for an organization that has not authorized
// the token, and the answer carries the SAML header and a FORBIDDEN error next to the data.
func searchBody(query string, cfg githubConfig, merged []string, now time.Time) (header map[string]string, body map[string]any) {
	return searchBodyFor("", query, cfg, merged, now)
}

// searchBodyFor is searchBody for one token: only the repositories the token sees are searched,
// with the viewer's role the token has there (see githubTokenAccess).
func searchBodyFor(token, query string, cfg githubConfig, merged []string, now time.Time) (header map[string]string, body map[string]any) {
	sso, moved := cfg.SSO, cfg.HeadMoved
	login := loginOf(token)
	data := map[string]any{
		"rateLimit": map[string]any{"limit": 5000, "cost": 1, "remaining": 4900, "resetAt": now.Add(time.Hour).UTC().Format(time.RFC3339)},
	}
	nodes := []any{}
	for _, p := range fixturePRs(now, cfg) {
		perm, sees := permissionOf(token, p)
		if (sso && p.org == "acme-corp") || !sees || !p.matchesViewerAs(query, login) {
			continue
		}
		switch isMerged := slices.Contains(merged, p.id()); {
		case strings.Contains(query, "is:merged") && isMerged:
			nodes = append(nodes, map[string]any{"__typename": "PullRequest", "id": p.id(), "mergedAt": now.UTC().Format(time.RFC3339)})
		case !strings.Contains(query, "is:merged") && !isMerged:
			nodes = append(nodes, p.nodeAs(now, moved, "OPEN", perm))
		}
	}
	data["search"] = map[string]any{
		"issueCount": len(nodes),
		"pageInfo":   map[string]any{"hasNextPage": false, "endCursor": nil},
		"nodes":      nodes,
	}
	body = map[string]any{"data": data}
	if sso {
		header = map[string]string{"X-GitHub-SSO": "required; url=https://github.com/orgs/acme-corp/sso?authorization_request=FAKE"}
		body["errors"] = []map[string]string{{
			"type":    "FORBIDDEN",
			"message": "Resource protected by organization SAML enforcement. You must grant your OAuth token access to the `acme-corp` organization.",
		}}
	}
	return header, body
}

// nodeBody answers the single pull request lookup by node id.
func nodeBody(id string, cfg githubConfig, merged []string, now time.Time) map[string]any {
	return nodeBodyFor("", id, cfg, merged, now)
}

// nodeBodyFor is nodeBody for one token: a pull request of a repository the token does not see is
// null, as GitHub answers a node the token cannot read.
func nodeBodyFor(token, id string, cfg githubConfig, merged []string, now time.Time) map[string]any {
	data := map[string]any{
		"rateLimit": map[string]any{"limit": 5000, "cost": 1, "remaining": 4899, "resetAt": now.Add(time.Hour).UTC().Format(time.RFC3339)},
		"node":      nil,
	}
	for _, p := range fixturePRs(now, cfg) {
		if perm, sees := permissionOf(token, p); sees && p.id() == id {
			state := "OPEN"
			if slices.Contains(merged, id) {
				state = "MERGED"
			}
			data["node"] = p.nodeAs(now, cfg.HeadMoved, state, perm)
		}
	}
	return map[string]any{"data": data}
}
