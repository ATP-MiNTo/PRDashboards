//go:build e2e

package fake

import (
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
	// authored and unassigned say how the viewer relates to the pull request. By default it is
	// assigned to the viewer and opened by someone else. authored marks one the viewer opened
	// (assignee:@me and author:@me both match when it is also assigned), and unassigned drops
	// the assignment (only author:@me matches).
	authored, unassigned bool
}

// matchesViewer reports whether the search query selects the pull request for the viewer.
// A query with neither assignee:@me nor author:@me matches everything.
func (p fixturePR) matchesViewer(query string) bool {
	assignee, author := strings.Contains(query, "assignee:@me"), strings.Contains(query, "author:@me")
	switch {
	case assignee && author:
		return !p.unassigned || p.authored
	case assignee:
		return !p.unassigned
	case author:
		return p.authored
	}
	return true
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

// fixturePRs are the pull requests of the fake search, with the approvals and the extra pull
// requests the config asks for.
func fixturePRs(now time.Time, cfg githubConfig) []fixturePR {
	prs := basePRs(now)
	if cfg.MultiPR {
		prs = append(prs, multiPRs()...)
	}
	for i, p := range prs {
		if slices.Contains(cfg.Approved, p.number) {
			prs[i].decision, prs[i].reviewApproved = "APPROVED", true
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
			repo: "acme/web", org: "acme", number: 101, title: "INP-101 Add login", head: "feat/INP-101-login", base: "main",
			mergeable: "MERGEABLE", mergeState: "BLOCKED", decision: "APPROVED", committedAgo: 3 * time.Hour, rollup: "FAILURE", reviewApproved: true,
			checks: []map[string]any{
				checkRun("build", "GitHub Actions", "COMPLETED", "SUCCESS", run("acme/web", 1001)+"/job/1", ago(20*time.Minute), ago(18*time.Minute)),
				checkRun("unit-tests", "GitHub Actions", "COMPLETED", "FAILURE", run("acme/web", 1001)+"/job/2", ago(20*time.Minute), ago(15*time.Minute)),
				checkRun("e2e", "GitHub Actions", "IN_PROGRESS", "", run("acme/web", 1001)+"/job/3", ago(4*time.Minute), time.Time{}),
				statusContext("ci/jenkins", "SUCCESS", "https://ci.example.com/job/web/42", ago(19*time.Minute)),
			},
		},
		{
			repo: "acme/web", org: "acme", number: 102, title: "INP-102 Single sign-on", head: "feat/INP-102-sso", base: "feat/INP-101-login",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "REVIEW_REQUIRED", committedAgo: 90 * time.Minute, rollup: "SUCCESS", requested: true,
			authored: true, // assigned and opened by the viewer: both searches find it
			checks: []map[string]any{
				checkRun("build", "GitHub Actions", "COMPLETED", "SUCCESS", run("acme/web", 1002)+"/job/1", ago(30*time.Minute), ago(28*time.Minute)),
				checkRun("lint", "GitHub Actions", "COMPLETED", "NEUTRAL", run("acme/web", 1002)+"/job/2", ago(30*time.Minute), ago(29*time.Minute)),
				checkRun("docs", "GitHub Actions", "COMPLETED", "SKIPPED", run("acme/web", 1002)+"/job/3", ago(30*time.Minute), ago(30*time.Minute)),
			},
		},
		{
			repo: "acme/web", org: "acme", number: 103, title: "INP-103 Account menu", head: "feat/INP-103-menu", base: "feat/INP-102-sso",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "", committedAgo: 40 * time.Minute, rollup: "PENDING",
			checks: []map[string]any{
				checkRun("build", "GitHub Actions", "QUEUED", "", run("acme/web", 1003)+"/job/1", ago(time.Minute), time.Time{}),
			},
		},
		{
			repo: "acme/api", org: "acme", number: 7, title: "WIP schema cleanup", head: "chore/schema-cleanup", base: "main", draft: true,
			mergeable: "UNKNOWN", mergeState: "DRAFT", decision: "", committedAgo: 5 * 24 * time.Hour, rollup: "",
			authored: true, unassigned: true, // opened by the viewer, assigned to nobody: only author:@me finds it
		},
		{
			repo: "acme/api", org: "acme", number: 8, title: "OPS-12 Rotate signing keys", head: "fix/OPS-12-keys", base: "main",
			mergeable: "CONFLICTING", mergeState: "DIRTY", decision: "APPROVED", committedAgo: 26 * time.Hour, rollup: "FAILURE", reviewApproved: true,
			checks: []map[string]any{
				statusContext("buildkite/api", "FAILURE", "https://buildkite.example.org/acme/api/builds/311", ago(25*time.Hour)),
				checkRun("unit-tests", "CircleCI Checks", "COMPLETED", "CANCELLED", "https://circleci.example.com/jobs/9", ago(25*time.Hour), ago(25*time.Hour)),
			},
		},
		{
			repo: "acme/api", org: "acme", number: 9, title: "Bump dependencies", head: "chore/deps", base: "main",
			mergeable: "MERGEABLE", mergeState: "CLEAN", decision: "APPROVED", committedAgo: time.Hour, rollup: "SUCCESS", reviewApproved: true,
			totalChecks: 62,
			checks: []map[string]any{
				checkRun("build", "GitHub Actions", "COMPLETED", "SUCCESS", run("acme/api", 2001)+"/job/1", ago(30*time.Minute), ago(25*time.Minute)),
			},
		},
		{
			repo: "acme-corp/billing", org: "acme-corp", number: 5, title: "INP-104 Invoice export", head: "feat/INP-104-export", base: "main",
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
	requests := []any{}
	if p.requested {
		requests = append(requests, map[string]any{"requestedReviewer": map[string]any{"__typename": "User", "id": "U_bob"}})
	}
	var decision any
	if p.decision != "" {
		decision = p.decision
	}
	return map[string]any{
		"__typename": "PullRequest",
		"id":         p.id(), "state": state, "isCrossRepository": false,
		"number": p.number, "title": p.title, "url": fmt.Sprintf("https://github.com/%s/pull/%d", p.repo, p.number),
		"isDraft": p.draft, "mergeable": p.mergeable, "mergeStateStatus": p.mergeState, "reviewDecision": decision,
		"headRefName": p.head, "headRefOid": p.headSHA(moved), "baseRefName": p.base,
		"repository": map[string]any{
			"id": "R_" + strings.ReplaceAll(p.repo, "/", "_"), "nameWithOwner": p.repo,
			"squashMergeAllowed": true, "mergeCommitAllowed": true, "rebaseMergeAllowed": p.repo != "acme/api",
		},
		"commits": map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{
			"committedDate":     now.Add(-p.committedAgo).UTC().Format(time.RFC3339),
			"statusCheckRollup": rollup,
		}}}},
		"latestReviews":  map[string]any{"nodes": reviews},
		"reviewRequests": map[string]any{"nodes": requests},
	}
}

// searchBody answers the open and merged pull request searches, honouring assignee:@me and
// author:@me, so the dashboard's union of the two searches is exercised. With the sso option the pull
// requests of acme-corp are left out, as GitHub does for an organization that has not authorized
// the token, and the answer carries the SAML header and a FORBIDDEN error next to the data.
func searchBody(query string, cfg githubConfig, merged []string, now time.Time) (header map[string]string, body map[string]any) {
	sso, moved := cfg.SSO, cfg.HeadMoved
	data := map[string]any{
		"rateLimit": map[string]any{"limit": 5000, "cost": 1, "remaining": 4900, "resetAt": now.Add(time.Hour).UTC().Format(time.RFC3339)},
	}
	nodes := []any{}
	for _, p := range fixturePRs(now, cfg) {
		if (sso && p.org == "acme-corp") || !p.matchesViewer(query) {
			continue
		}
		switch isMerged := slices.Contains(merged, p.id()); {
		case strings.Contains(query, "is:merged") && isMerged:
			nodes = append(nodes, map[string]any{"__typename": "PullRequest", "id": p.id(), "mergedAt": now.UTC().Format(time.RFC3339)})
		case !strings.Contains(query, "is:merged") && !isMerged:
			nodes = append(nodes, p.node(now, moved, "OPEN"))
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
	data := map[string]any{
		"rateLimit": map[string]any{"limit": 5000, "cost": 1, "remaining": 4899, "resetAt": now.Add(time.Hour).UTC().Format(time.RFC3339)},
		"node":      nil,
	}
	for _, p := range fixturePRs(now, cfg) {
		if p.id() == id {
			state := "OPEN"
			if slices.Contains(merged, id) {
				state = "MERGED"
			}
			data["node"] = p.node(now, cfg.HeadMoved, state)
		}
	}
	return map[string]any{"data": data}
}
