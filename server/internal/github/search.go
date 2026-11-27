package github

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
)

//go:embed open_prs.graphql
var openPRsSource string

//go:embed pr_fields.graphql
var prFields string

//go:embed pull_request.graphql
var pullRequestSource string

// Both queries select the PullRequestFields fragment, so a search result and a single pull
// request have the same shape.
var (
	openPRsQuery     = openPRsSource + "\n" + prFields
	pullRequestQuery = pullRequestSource + "\n" + prFields
)

//go:embed merged_prs.graphql
var mergedPRsQuery string

const (
	// OpenAssignedQuery is the search for open pull requests assigned to the viewer. With
	// OpenAuthoredQuery it makes the two searches of TECH_SPEC 10.2, and the dashboard lists the
	// union.
	OpenAssignedQuery = "is:pr is:open assignee:@me archived:false"
	// OpenAuthoredQuery is the search for open pull requests opened by the viewer.
	OpenAuthoredQuery = "is:pr is:open author:@me archived:false"
	// SearchCap is the number of results GitHub's search returns at most.
	SearchCap = 1000

	pageSize = 50
	maxPages = SearchCap / pageSize
)

// CheckState is the normalized state of one check.
type CheckState string

// Check states.
const (
	CheckPassing   CheckState = "passing"
	CheckFailing   CheckState = "failing"
	CheckRunning   CheckState = "running"
	CheckPending   CheckState = "pending"
	CheckSkipped   CheckState = "skipped"
	CheckNeutral   CheckState = "neutral"
	CheckCancelled CheckState = "cancelled"
)

// Check is one CI check of the head commit: a check run or a commit status.
type Check struct {
	Name        string
	App         string
	State       CheckState
	URL         string
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// CIState is the rollup of all checks.
type CIState string

// Rollup states. CINone means the commit has no checks.
const (
	CIPassing CIState = "passing"
	CIFailing CIState = "failing"
	CIRunning CIState = "running"
	CINone    CIState = "none"
)

// CI is the status rollup of the head commit. Total can exceed len(Checks): GitHub is asked for
// the first 50 checks only.
type CI struct {
	State  CIState
	Total  int
	Checks []Check
}

// Review is the latest review of one reviewer.
type Review struct {
	ID          string
	State       string
	SubmittedAt *time.Time
	Author      string
}

// Repo is the repository of a pull request with the merge methods it allows.
type Repo struct {
	ID            string
	NameWithOwner string
	SquashAllowed bool
	MergeAllowed  bool
	RebaseAllowed bool
}

// Roles say why a pull request is listed.
const (
	RoleAssigned = "assigned"
	RoleAuthored = "authored"
)

// PR is an open pull request assigned to the viewer or opened by the viewer.
type PR struct {
	ID               string
	Number           int
	State            string
	CrossRepo        bool
	Title            string
	URL              string
	Draft            bool
	Mergeable        string
	MergeStateStatus string
	ReviewDecision   string
	HeadRef          string
	HeadSHA          string
	BaseRef          string
	Repo             Repo
	CommittedAt      *time.Time
	CI               CI
	Reviews          []Review
	ReviewRequests   []string
	// Author is the login of the user who opened the pull request, empty for a deleted account.
	Author string
	// UpdatedAt is when the pull request last changed on GitHub; Additions and Deletions are the
	// lines of its diff. They come with the search, without an extra call.
	UpdatedAt *time.Time
	Additions int
	Deletions int
	// Roles holds RoleAssigned when the assigned search returned the pull request and RoleAuthored
	// when the authored search did. SearchOpen sets it; it is never empty there.
	Roles []string
}

// RateLimit is the budget GitHub reports with every GraphQL answer.
type RateLimit struct {
	Limit     int
	Cost      int
	Remaining int
	ResetAt   time.Time
}

// Low reports whether less than a tenth of the budget is left (TECH_SPEC 10.4).
func (r RateLimit) Low() bool { return r.Limit > 0 && r.Remaining*10 < r.Limit }

// SearchResult is what a search returned. Blocked organizations and per-error messages do not
// fail the search: the pull requests that did come back are still in PRs.
type SearchResult struct {
	PRs        []PR
	Total      int
	Capped     bool
	Incomplete bool
	RateLimit  RateLimit
	Blocked    []OrgBlocked
	Errors     []string
	Requests   int
}

// MergedResult maps the id of a merged pull request to when it was merged.
type MergedResult struct {
	Merged    map[string]time.Time
	RateLimit RateLimit
	Requests  int
}

// RateLimitedError says GitHub refused the call because a rate limit is used up.
type RateLimitedError struct {
	RetryAfter time.Duration
	ResetAt    time.Time
}

func (e *RateLimitedError) Error() string { return "github rate limit reached" }

type searchPage struct {
	Data struct {
		RateLimit struct {
			Limit     int       `json:"limit"`
			Cost      int       `json:"cost"`
			Remaining int       `json:"remaining"`
			ResetAt   time.Time `json:"resetAt"`
		} `json:"rateLimit"`
		Search struct {
			IssueCount int `json:"issueCount"`
			PageInfo   struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
			Nodes []json.RawMessage `json:"nodes"`
		} `json:"search"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type rawPR struct {
	Typename         string     `json:"__typename"`
	ID               string     `json:"id"`
	Number           int        `json:"number"`
	State            string     `json:"state"`
	IsCrossRepo      bool       `json:"isCrossRepository"`
	Title            string     `json:"title"`
	URL              string     `json:"url"`
	IsDraft          bool       `json:"isDraft"`
	Mergeable        string     `json:"mergeable"`
	MergeStateStatus string     `json:"mergeStateStatus"`
	ReviewDecision   string     `json:"reviewDecision"`
	HeadRefName      string     `json:"headRefName"`
	HeadRefOid       string     `json:"headRefOid"`
	BaseRefName      string     `json:"baseRefName"`
	MergedAt         *time.Time `json:"mergedAt"`
	Repository       struct {
		ID                 string `json:"id"`
		NameWithOwner      string `json:"nameWithOwner"`
		SquashMergeAllowed bool   `json:"squashMergeAllowed"`
		MergeCommitAllowed bool   `json:"mergeCommitAllowed"`
		RebaseMergeAllowed bool   `json:"rebaseMergeAllowed"`
	} `json:"repository"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				CommittedDate     *time.Time `json:"committedDate"`
				StatusCheckRollup *struct {
					State    string `json:"state"`
					Contexts struct {
						TotalCount int        `json:"totalCount"`
						Nodes      []rawCheck `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	LatestReviews struct {
		Nodes []struct {
			ID          string     `json:"id"`
			State       string     `json:"state"`
			SubmittedAt *time.Time `json:"submittedAt"`
			Author      *struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"nodes"`
	} `json:"latestReviews"`
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *struct {
				ID string `json:"id"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	UpdatedAt *time.Time `json:"updatedAt"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Author    *struct {
		Login string `json:"login"`
	} `json:"author"`
}

type rawCheck struct {
	Typename    string     `json:"__typename"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	DetailsURL  string     `json:"detailsUrl"`
	StartedAt   *time.Time `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
	CheckSuite  *struct {
		App *struct {
			Name string `json:"name"`
		} `json:"app"`
	} `json:"checkSuite"`
	Context   string     `json:"context"`
	State     string     `json:"state"`
	TargetURL string     `json:"targetUrl"`
	CreatedAt *time.Time `json:"createdAt"`
}

// SearchOpen runs the open pull request searches, assigned to the token's user and opened by the
// token's user, concurrently (two requests in flight), and merges them. Each search is paginated,
// 50 per page, following the cursor until the last page or the 1,000 result cap. Pull requests
// of organizations that blocked the token are left out and listed in Blocked.
//
// A pull request found by both searches appears once, in the position of the assigned search.
// Total is the larger of the unique pull requests returned and the biggest count GitHub reported
// for either search, so it is never below what the dashboard shows. Capped is set when either
// search is capped. If a search fails on its first page, SearchOpen returns that error.
func (c *Client) SearchOpen(ctx context.Context, token string) (SearchResult, error) {
	var res [2]SearchResult
	var errs [2]error
	var wg sync.WaitGroup
	for i, q := range []string{OpenAssignedQuery, OpenAuthoredQuery} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i], errs[i] = c.searchOpenQuery(ctx, token, q)
		}()
	}
	wg.Wait()
	if err := pickError(errs[:]); err != nil {
		return SearchResult{}, err
	}
	return mergeSearches(res[0], res[1]), nil
}

// searchOpenQuery runs one paginated open pull request search.
func (c *Client) searchOpenQuery(ctx context.Context, token, q string) (SearchResult, error) {
	var res SearchResult
	err := c.paginate(ctx, token, openPRsQuery, q, &res.Requests, &res.RateLimit, func(p *searchPage, _ int, h http.Header) error {
		res.Total = p.Data.Search.IssueCount
		res.Blocked = mergeBlocked(res.Blocked, blockedOf(h, p.Errors))
		res.Errors = append(res.Errors, otherErrors(p.Errors)...)
		for _, raw := range p.Data.Search.Nodes {
			var n rawPR
			if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &n) != nil || n.Typename != "PullRequest" || n.ID == "" {
				continue
			}
			res.PRs = append(res.PRs, n.pr())
		}
		return nil
	}, &res.Incomplete)
	res.Capped = res.Total > SearchCap
	return res, err
}

// pickError returns the error of a set of concurrent searches: a rejected token first, then a
// rate limit, then the first other error, so the outcome does not depend on timing.
func pickError(errs []error) error {
	for _, e := range errs {
		if errors.Is(e, ErrBadCredentials) {
			return e
		}
	}
	for _, e := range errs {
		var rl *RateLimitedError
		if errors.As(e, &rl) {
			return e
		}
	}
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// mergeSearches unions the assigned (a) and authored (b) results in one pass, deduplicating pull
// requests by node id in O(n) and recording in Roles which search found each.
func mergeSearches(a, b SearchResult) SearchResult {
	out := SearchResult{
		PRs:        make([]PR, 0, len(a.PRs)+len(b.PRs)),
		Capped:     a.Capped || b.Capped,
		Incomplete: a.Incomplete || b.Incomplete,
		Requests:   a.Requests + b.Requests,
		Errors:     append(slices.Clone(a.Errors), b.Errors...),
		Blocked:    mergeBlocked(slices.Clone(a.Blocked), b.Blocked),
		RateLimit:  mergeRateLimit(a.RateLimit, b.RateLimit),
	}
	at := make(map[string]int, len(a.PRs)+len(b.PRs))
	for _, src := range []struct {
		prs  []PR
		role string
	}{{a.PRs, RoleAssigned}, {b.PRs, RoleAuthored}} {
		for _, p := range src.prs {
			if i, dup := at[p.ID]; dup {
				if !slices.Contains(out.PRs[i].Roles, src.role) {
					out.PRs[i].Roles = append(out.PRs[i].Roles, src.role)
				}
				continue
			}
			at[p.ID] = len(out.PRs)
			p.Roles = []string{src.role}
			out.PRs = append(out.PRs, p)
		}
	}
	out.Total = max(len(out.PRs), a.Total, b.Total)
	return out
}

// mergeRateLimit sums the cost. Limit, Remaining and ResetAt come from the answer with the lowest
// remaining budget, the latest view of the budget the searches share.
func mergeRateLimit(a, b RateLimit) RateLimit {
	out := a
	if a.Limit == 0 || (b.Limit > 0 && b.Remaining < a.Remaining) {
		out = b
	}
	out.Cost = a.Cost + b.Cost
	return out
}

// SearchMerged finds pull requests assigned to the token's user or opened by the token's user that
// were merged since the given time, with two concurrent searches. One pair of searches resolves
// every pull request that left the open list. Either search failing is an error.
func (c *Client) SearchMerged(ctx context.Context, token string, since time.Time) (MergedResult, error) {
	stamp := " is:merged updated:>=" + since.UTC().Format("2006-01-02T15:04:05Z")
	var res [2]MergedResult
	var errs [2]error
	var wg sync.WaitGroup
	for i, q := range []string{"is:pr assignee:@me" + stamp, "is:pr author:@me" + stamp} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i], errs[i] = c.searchMergedQuery(ctx, token, q)
		}()
	}
	wg.Wait()
	if err := pickError(errs[:]); err != nil {
		return MergedResult{}, err
	}
	out := MergedResult{Merged: res[0].Merged, RateLimit: mergeRateLimit(res[0].RateLimit, res[1].RateLimit), Requests: res[0].Requests + res[1].Requests}
	maps.Copy(out.Merged, res[1].Merged)
	return out, nil
}

func (c *Client) searchMergedQuery(ctx context.Context, token, q string) (MergedResult, error) {
	res := MergedResult{Merged: map[string]time.Time{}}
	var incomplete bool
	err := c.paginate(ctx, token, mergedPRsQuery, q, &res.Requests, &res.RateLimit, func(p *searchPage, _ int, _ http.Header) error {
		for _, raw := range p.Data.Search.Nodes {
			var n rawPR
			if len(raw) > 0 && json.Unmarshal(raw, &n) == nil && n.ID != "" && n.MergedAt != nil {
				res.Merged[n.ID] = *n.MergedAt
			}
		}
		return nil
	}, &incomplete)
	if err == nil && incomplete {
		err = errors.New("github merged search: incomplete result")
	}
	return res, err
}

// paginate runs query page by page. The first page must succeed. A failure on a later page ends the
// loop and sets incomplete, so the pull requests already collected are kept.
func (c *Client) paginate(ctx context.Context, token, query, q string, requests *int, rl *RateLimit,
	handle func(p *searchPage, status int, h http.Header) error, incomplete *bool) error {
	cursor := ""
	for page := 0; page < maxPages; page++ {
		vars := map[string]any{"q": q}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		*requests++
		p, status, h, err := c.searchPage(ctx, token, query, vars)
		if err != nil {
			if page == 0 {
				return err
			}
			*incomplete = true
			return nil
		}
		if p.Data.RateLimit.Limit > 0 {
			rl.Limit = p.Data.RateLimit.Limit
			rl.Remaining = p.Data.RateLimit.Remaining
			rl.ResetAt = p.Data.RateLimit.ResetAt
		}
		rl.Cost += p.Data.RateLimit.Cost
		if err := handle(p, status, h); err != nil {
			return err
		}
		if !p.Data.Search.PageInfo.HasNextPage || p.Data.Search.PageInfo.EndCursor == "" {
			return nil
		}
		cursor = p.Data.Search.PageInfo.EndCursor
	}
	return nil
}

// graphql posts one query. A rejected token is ErrBadCredentials and a rate limit answer is
// *RateLimitedError. Any other answer is returned with its status and headers.
func (c *Client) graphql(ctx context.Context, token, query string, vars map[string]any) ([]byte, int, http.Header, error) {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return nil, 0, nil, err
	}
	return c.send(ctx, token, http.MethodPost, c.base()+"/graphql", payload)
}

// send runs one request with the shared retry policy, reads the answer and maps a rejected token
// and a rate limit answer to errors. Other statuses are the caller's to judge.
func (c *Client) send(ctx context.Context, token, method, target string, payload []byte) ([]byte, int, http.Header, error) {
	return c.sendVersion(ctx, token, apiVersion, method, target, payload)
}

// sendVersion is send with an explicit X-GitHub-Api-Version.
func (c *Client) sendVersion(ctx context.Context, token, version, method, target string, payload []byte) ([]byte, int, http.Header, error) {
	resp, err := c.Backoff.Do(ctx, func(ctx context.Context) (*http.Response, error) {
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", version)
		req.Header.Set("User-Agent", userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return c.HTTP.Do(req)
	})
	if err != nil {
		return nil, 0, nil, fmt.Errorf("github %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("github %s: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		c.logRefusal(ctx, method+" "+requestPath(target), resp.StatusCode, resp.Header, restMessage(body), nil)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, resp.StatusCode, resp.Header, ErrBadCredentials
	case backoff.Retryable(resp):
		now := c.now()
		wait, _ := backoff.Hint(resp.Header, now)
		return nil, resp.StatusCode, resp.Header, &RateLimitedError{RetryAfter: wait, ResetAt: now.Add(wait)}
	case resp.StatusCode == http.StatusForbidden && secondaryLimit(resp.Header, body):
		// No wait hint: the caller backs off exponentially (TECH_SPEC 10.4).
		return nil, resp.StatusCode, resp.Header, &RateLimitedError{}
	}
	return body, resp.StatusCode, resp.Header, nil
}

// requestPath is the path of a request URL, for logs: no host, query or credentials.
func requestPath(target string) string {
	if u, err := url.Parse(target); err == nil {
		return u.EscapedPath()
	}
	return "?"
}

// secondaryLimit recognizes the 403 GitHub sends for a secondary rate limit without Retry-After.
func secondaryLimit(h http.Header, body []byte) bool {
	if h.Get("X-GitHub-SSO") != "" {
		return false
	}
	var parsed struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return false
	}
	m := strings.ToLower(parsed.Message)
	return strings.Contains(m, "secondary rate limit") || strings.Contains(m, "abuse detection")
}

// searchPage posts one search page. A GraphQL rate limit error becomes *RateLimitedError. A blocked
// organization with no data at all is not an error: the page is empty and handle sees the
// classification.
func (c *Client) searchPage(ctx context.Context, token, query string, vars map[string]any) (*searchPage, int, http.Header, error) {
	body, status, h, err := c.graphql(ctx, token, query, vars)
	if err != nil {
		return nil, status, h, err
	}
	var p searchPage
	if status == http.StatusOK || status == http.StatusForbidden {
		if err := json.Unmarshal(body, &p); err != nil && status == http.StatusOK {
			return nil, status, h, fmt.Errorf("github search: bad response: %w", err)
		}
	}
	for _, e := range p.Errors {
		if e.Type == "RATE_LIMITED" {
			wait := max(0, p.Data.RateLimit.ResetAt.Sub(c.now()))
			return nil, status, h, &RateLimitedError{RetryAfter: wait, ResetAt: p.Data.RateLimit.ResetAt}
		}
	}
	if len(p.Errors) > 0 {
		c.logRefusal(ctx, "search", status, h, "", p.Errors)
	}
	if status != http.StatusOK {
		if len(blockedOf(h, p.Errors)) > 0 {
			return &p, status, h, nil
		}
		return nil, status, h, fmt.Errorf("github search: unexpected status %d", status)
	}
	return &p, status, h, nil
}

// blockedOf lists the organizations that refused the token in one answer: the X-GitHub-SSO header
// names one, and every GraphQL FORBIDDEN error about SAML or an organization policy names its own
// when the message does. A FORBIDDEN error about a missing token permission is not a blocked
// organization; otherErrors reports it.
func blockedOf(h http.Header, errs []graphQLError) []OrgBlocked {
	var out []OrgBlocked
	if b := ssoHeader(h); b != nil {
		out = append(out, *b)
	}
	for _, e := range errs {
		if e.Type != "FORBIDDEN" {
			continue
		}
		if b, ok := classifyForbidden(h, e.Message, e.Extensions).(*OrgBlocked); ok {
			out = append(out, *b)
		}
	}
	return out
}

func mergeBlocked(have, more []OrgBlocked) []OrgBlocked {
	for _, b := range more {
		if !slices.ContainsFunc(have, func(x OrgBlocked) bool { return x.Org == b.Org }) {
			have = append(have, b)
		}
	}
	return have
}

// otherErrors lists the errors worth showing, one line per distinct permission message. Errors
// about check runs a fine-grained token cannot read are dropped.
func otherErrors(errs []graphQLError) []string {
	var out []string
	for _, e := range errs {
		if hiddenCheckError(e) {
			continue
		}
		if e.Type != "FORBIDDEN" {
			out = append(out, strings.TrimSpace(e.Type+" "+e.Message))
		} else if p, ok := classifyForbidden(nil, e.Message, e.Extensions).(*PermissionError); ok && !slices.Contains(out, "FORBIDDEN "+p.Message) {
			// One line per message.
			out = append(out, "FORBIDDEN "+p.Message)
		}
	}
	return out
}

func (n rawPR) pr() PR {
	pr := PR{
		ID: n.ID, Number: n.Number, State: n.State, CrossRepo: n.IsCrossRepo, Title: n.Title, URL: n.URL, Draft: n.IsDraft,
		Mergeable: n.Mergeable, MergeStateStatus: n.MergeStateStatus, ReviewDecision: n.ReviewDecision,
		HeadRef: n.HeadRefName, HeadSHA: n.HeadRefOid, BaseRef: n.BaseRefName,
		Repo: Repo{
			ID: n.Repository.ID, NameWithOwner: n.Repository.NameWithOwner,
			SquashAllowed: n.Repository.SquashMergeAllowed, MergeAllowed: n.Repository.MergeCommitAllowed,
			RebaseAllowed: n.Repository.RebaseMergeAllowed,
		},
		UpdatedAt: n.UpdatedAt, Additions: n.Additions, Deletions: n.Deletions,
		CI: CI{State: CINone},
	}
	if n.Author != nil {
		pr.Author = n.Author.Login
	}
	if len(n.Commits.Nodes) > 0 {
		commit := n.Commits.Nodes[0].Commit
		pr.CommittedAt = commit.CommittedDate
		if r := commit.StatusCheckRollup; r != nil {
			pr.CI = CI{State: rollupState(r.State), Total: r.Contexts.TotalCount, Checks: checksOf(r.Contexts.Nodes)}
		}
	}
	for _, rv := range n.LatestReviews.Nodes {
		review := Review{ID: rv.ID, State: rv.State, SubmittedAt: rv.SubmittedAt}
		if rv.Author != nil {
			review.Author = rv.Author.Login
		}
		pr.Reviews = append(pr.Reviews, review)
	}
	for _, rq := range n.ReviewRequests.Nodes {
		if rq.RequestedReviewer != nil && rq.RequestedReviewer.ID != "" {
			pr.ReviewRequests = append(pr.ReviewRequests, rq.RequestedReviewer.ID)
		}
	}
	return pr
}

func rollupState(s string) CIState {
	switch s {
	case "SUCCESS":
		return CIPassing
	case "FAILURE", "ERROR":
		return CIFailing
	case "PENDING", "EXPECTED":
		return CIRunning
	}
	return CINone
}

func checksOf(nodes []rawCheck) []Check {
	out := make([]Check, 0, len(nodes))
	for _, n := range nodes {
		switch n.Typename {
		case "CheckRun":
			c := Check{Name: n.Name, State: checkRunState(n.Status, n.Conclusion), URL: n.DetailsURL, StartedAt: n.StartedAt, CompletedAt: n.CompletedAt}
			if n.CheckSuite != nil && n.CheckSuite.App != nil {
				c.App = n.CheckSuite.App.Name
			}
			out = append(out, c)
		case "StatusContext":
			out = append(out, Check{Name: n.Context, State: statusContextState(n.State), URL: n.TargetURL, StartedAt: n.CreatedAt})
		}
	}
	slices.SortStableFunc(out, func(a, b Check) int {
		return cmp.Or(cmp.Compare(severity(a.State), severity(b.State)), strings.Compare(a.Name, b.Name))
	})
	return out
}

func checkRunState(status, conclusion string) CheckState {
	switch status {
	case "COMPLETED":
		switch conclusion {
		case "SUCCESS":
			return CheckPassing
		case "FAILURE", "TIMED_OUT", "STARTUP_FAILURE", "ACTION_REQUIRED":
			return CheckFailing
		case "SKIPPED":
			return CheckSkipped
		case "CANCELLED":
			return CheckCancelled
		}
		return CheckNeutral
	case "IN_PROGRESS":
		return CheckRunning
	}
	return CheckPending
}

func statusContextState(s string) CheckState {
	switch s {
	case "SUCCESS":
		return CheckPassing
	case "FAILURE", "ERROR":
		return CheckFailing
	}
	return CheckPending
}

// severity orders checks so that the ones needing attention come first.
func severity(s CheckState) int {
	switch s {
	case CheckFailing:
		return 0
	case CheckRunning:
		return 1
	case CheckPending:
		return 2
	case CheckCancelled:
		return 3
	case CheckPassing:
		return 4
	case CheckNeutral:
		return 5
	}
	return 6
}
