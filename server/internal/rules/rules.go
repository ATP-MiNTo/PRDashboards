// Package rules is the status sync engine (TECH_SPEC 7.3 to 7.5). It is pure: no network, no
// database and no clock. The caller passes the data and the time, and gets back what to do.
package rules

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Trigger is the event a rule waits for.
type Trigger string

// Triggers of v1.
const (
	ReviewSubmitted  Trigger = "review_submitted"
	ReviewApproved   Trigger = "review_approved"
	ChangesRequested Trigger = "changes_requested"
	ReviewRequested  Trigger = "review_requested"
	ReadyForReview   Trigger = "ready_for_review"
	CommitStale      Trigger = "commit_stale"
	CIFailing        Trigger = "ci_failing"
	CIPassing        Trigger = "ci_passing"
	Merged           Trigger = "merged"
	// NoReviewYet holds for a ready pull request nobody has reviewed. WaitingApprovals holds for a
	// ready pull request that has some approvals, but fewer than its repository requires.
	NoReviewYet      Trigger = "no_review_yet"
	WaitingApprovals Trigger = "waiting_approvals"
)

// Triggers lists every trigger.
var Triggers = []Trigger{
	ReviewSubmitted, ReviewApproved, ChangesRequested, ReviewRequested, ReadyForReview,
	CommitStale, CIFailing, CIPassing, Merged, NoReviewYet, WaitingApprovals,
}

// Limits of a rule.
const (
	MinHours   = 1
	MaxHours   = 720
	MaxRules   = 100
	MaxTarget  = 100
	MaxName    = 100
	MaxPattern = 200
)

// Mode says how a ticket with several pull requests meets a rule.
type Mode string

// Modes. All is the default of the global setting. Default is only valid on a rule: it follows the
// global mode that the caller passes in, so this package never reads settings itself.
const (
	All     Mode = "all"
	Any     Mode = "any"
	Default Mode = "default"
)

// Resolve is the mode a rule runs with: its own, or the global one when it follows the default. A
// global mode that is not Any counts as All.
func (m Mode) Resolve(global Mode) Mode {
	if m == Default {
		m = global
	}
	if m == Any {
		return Any
	}
	return All
}

// resolveAll returns the rules with every Default mode replaced by the global mode.
func resolveAll(rs []Rule, global Mode) []Rule {
	out := make([]Rule, len(rs))
	for i, r := range rs {
		r.Mode = r.Mode.Resolve(global)
		out[i] = r
	}
	return out
}

// Filters narrow a rule. Every filter is optional and they combine with AND. Repo and Branch
// accept `*` globs, the others are compared without regard to case.
type Filters struct {
	Repo      string
	Branch    string
	Project   string
	IssueType string
	Status    string
}

// Rule is "when Trigger [and Filters] then move the ticket to Target".
type Rule struct {
	ID      string
	Name    string
	Enabled bool
	Trigger Trigger
	// Hours is the age of the latest commit for CommitStale.
	Hours   int
	Filters Filters
	Target  string
	Mode    Mode
}

// State of a pull request. A closed pull request that was not merged no longer counts.
type State string

// Pull request states.
const (
	StateOpen   State = "open"
	StateMerged State = "merged"
	StateClosed State = "closed"
)

// PR is what the engine needs to know about one pull request linked to a ticket.
type PR struct {
	ID     string
	Number int
	Repo   string
	Base   string
	State  State
	Draft  bool
	// HeadSHA is the current head commit.
	HeadSHA string
	// ReadySHA is the head commit at the time the pull request became ready for review.
	ReadySHA       string
	ReviewDecision string
	// LatestReview is the id of the most recent review, empty when there is none.
	LatestReview   string
	ReviewRequests []string
	// CommittedAt is the time of the latest commit, nil when unknown.
	CommittedAt *time.Time
	// CI is the rollup: "passing", "failing", "running" or "none" (nothing reported).
	CI string
	// ApprovalsHave is the number of reviewers whose latest review is an approval, and
	// ApprovalsNeed the number the repository requires. The caller counts them, so this package
	// stays pure. A zero ApprovalsNeed means the repository requires no approvals (the default), so
	// WaitingApprovals never holds for it.
	ApprovalsHave int
	ApprovalsNeed int
}

// Ticket is a Jira ticket with the pull requests linked to it. Available is false when Jira data
// for it is missing or stale, so no rule matches it.
type Ticket struct {
	Key       string
	Project   string
	IssueType string
	Status    string
	Available bool
	PRs       []PR
}

// Key identifies the last firing of a rule for a ticket.
type Key struct {
	Rule   string
	Ticket string
}

// Input is everything one evaluation needs.
type Input struct {
	Now     time.Time
	Rules   []Rule
	Tickets []Ticket
	// Fired is the fingerprint stored for each rule and ticket that fired before.
	Fired map[Key]string
	// Exclude lists the statuses whose tickets no rule moves, fires or lists as waiting. They are
	// compared without regard to case.
	Exclude []string
	// DefaultMode is the global "Multi-PR tickets move when" setting. Rules in mode Default use it.
	DefaultMode Mode
}

// Firing is a rule whose condition holds with a fingerprint that is not stored yet.
type Firing struct {
	Rule        string
	Ticket      string
	Fingerprint string
	Target      string
	// InTarget is true when the ticket already is in the target status: record it, send nothing.
	InTarget bool
}

// Ref names a pull request in a hint.
type Ref struct {
	ID     string
	Number int
	Repo   string
}

// Waiting is a mode all rule that some of the ticket's pull requests meet and others do not yet.
type Waiting struct {
	Ticket string
	Rule   string
	// Total is the number of pull requests that count for the rule.
	Total     int
	WaitingOn []Ref
}

// Hold is a rule whose condition holds for a ticket, fired or not.
type Hold struct {
	Rule        string
	Trigger     Trigger
	Ticket      string
	Fingerprint string
}

// Result is the outcome of one evaluation. Firings has at most one entry per ticket.
type Result struct {
	Firings []Firing
	Waiting []Waiting
	Holds   []Hold
}

// Evaluate walks every ticket that has a linked pull request and the enabled rules in order
// (TECH_SPEC 7.3). The first rule whose condition holds and whose fingerprint differs from the
// stored one fires, at most one per ticket. Tickets keep the order of the input.
func Evaluate(in Input) Result {
	var out Result
	enabled := make([]Rule, 0, len(in.Rules))
	for _, r := range resolveAll(in.Rules, in.DefaultMode) {
		if r.Enabled {
			enabled = append(enabled, r)
		}
	}
	if len(enabled) == 0 {
		return out
	}
	for _, t := range in.Tickets {
		if !t.Available || excluded(in.Exclude, t.Status) {
			continue
		}
		counted := make([]PR, 0, len(t.PRs))
		for _, p := range t.PRs {
			if p.State != StateClosed {
				counted = append(counted, p)
			}
		}
		if len(counted) == 0 {
			continue
		}
		fired := false
		for _, r := range enabled {
			if !ticketPasses(r.Filters, t) {
				continue
			}
			c := condition(r, counted, in.Now)
			if len(c.waiting) > 0 && len(c.met) > 0 && StatusKey(t.Status) != StatusKey(r.Target) {
				out.Waiting = append(out.Waiting, Waiting{Ticket: t.Key, Rule: r.ID, Total: c.total, WaitingOn: c.waiting})
			}
			if !c.holds {
				continue
			}
			out.Holds = append(out.Holds, Hold{Rule: r.ID, Trigger: r.Trigger, Ticket: t.Key, Fingerprint: c.fingerprint})
			if fired || in.Fired[Key{r.ID, t.Key}] == c.fingerprint {
				continue
			}
			fired = true
			out.Firings = append(out.Firings, Firing{
				Rule: r.ID, Ticket: t.Key, Fingerprint: c.fingerprint, Target: r.Target,
				InTarget: StatusKey(t.Status) == StatusKey(r.Target),
			})
		}
	}
	return out
}

type cond struct {
	holds       bool
	fingerprint string
	met         []string
	total       int
	waiting     []Ref
}

// condition decides whether a rule holds for the pull requests of one ticket. Pull requests that do
// not pass the repo and branch filters are ignored. A pull request whose data is unavailable does
// not meet the trigger.
func condition(r Rule, prs []PR, now time.Time) cond {
	var c cond
	var parts []string
	var open []PR
	for _, p := range prs {
		if !prPasses(r.Filters, p) {
			continue
		}
		if r.Trigger != Merged && p.State == StateMerged {
			continue // a merged pull request has no review or CI state left to meet
		}
		open = append(open, p)
	}
	slices.SortFunc(open, func(a, b PR) int { return cmp.Compare(a.ID, b.ID) })
	for _, p := range open {
		if fp, ok := meets(r, p, now); ok {
			c.met = append(c.met, p.ID)
			parts = append(parts, p.ID+"="+fp)
		} else {
			c.waiting = append(c.waiting, Ref{ID: p.ID, Number: p.Number, Repo: p.Repo})
		}
	}
	c.total = len(open)
	if r.Mode == Any {
		c.holds = len(c.met) > 0
	} else {
		c.holds = len(open) > 0 && len(c.waiting) == 0
	}
	if c.holds {
		c.fingerprint = strings.Join(parts, ",")
	}
	if r.Mode == Any {
		c.waiting = nil
	}
	return c
}

// meets reports whether one pull request meets the trigger, with the fingerprint of that event.
func meets(r Rule, p PR, now time.Time) (string, bool) {
	if r.Trigger == Merged {
		return p.ID, p.State == StateMerged
	}
	if p.State != StateOpen {
		return "", false
	}
	switch r.Trigger {
	case ReviewSubmitted:
		return p.LatestReview, p.LatestReview != ""
	case ReviewApproved:
		return p.HeadSHA + ":" + p.ReviewDecision, p.ReviewDecision == "APPROVED" && p.HeadSHA != ""
	case ChangesRequested:
		return p.LatestReview, p.ReviewDecision == "CHANGES_REQUESTED" && p.LatestReview != ""
	case ReviewRequested:
		ids := slices.Sorted(slices.Values(p.ReviewRequests))
		return strings.Join(ids, ","), len(ids) > 0
	case ReadyForReview:
		return p.ReadySHA, !p.Draft && p.ReadySHA != ""
	case CommitStale:
		if p.CommittedAt == nil || p.HeadSHA == "" || r.Hours < MinHours {
			return "", false
		}
		return p.HeadSHA, now.Sub(*p.CommittedAt) > time.Duration(r.Hours)*time.Hour
	case CIFailing:
		return p.HeadSHA + ":failing", p.CI == "failing" && p.HeadSHA != ""
	case CIPassing:
		return p.HeadSHA + ":passing", p.CI == "passing" && p.HeadSHA != ""
	case NoReviewYet:
		// A repository that requires reviews reports REVIEW_REQUIRED until someone reviews: that is
		// still no decision.
		undecided := p.ReviewDecision == "" || p.ReviewDecision == "REVIEW_REQUIRED"
		return p.HeadSHA + ":unreviewed", !p.Draft && p.LatestReview == "" && undecided && p.HeadSHA != ""
	case WaitingApprovals:
		return p.HeadSHA + ":waiting", !p.Draft && p.HeadSHA != "" && p.ApprovalsNeed > 0 &&
			p.ApprovalsHave >= 1 && p.ApprovalsHave < p.ApprovalsNeed
	}
	return "", false
}

// excluded reports whether status is one of the excluded statuses.
func excluded(exclude []string, status string) bool {
	key := StatusKey(status)
	return slices.ContainsFunc(exclude, func(e string) bool { return StatusKey(e) == key })
}

// StatusKey is the comparison key of a Jira status name: lower case with every rune that is not a
// letter or a number dropped, so "Re-open", "re open" and "REOPEN" all give "reopen" while
// "Reopened" stays distinct. A name with no letter or number keeps its trimmed lower case form, so
// "---" is still a status. Every status comparison goes through it.
func StatusKey(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(lower))
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return lower
	}
	return b.String()
}

func prPasses(f Filters, p PR) bool {
	return (f.Repo == "" || Match(f.Repo, p.Repo)) && (f.Branch == "" || Match(f.Branch, p.Base))
}

func ticketPasses(f Filters, t Ticket) bool {
	return (f.Project == "" || strings.EqualFold(f.Project, t.Project)) &&
		(f.IssueType == "" || strings.EqualFold(f.IssueType, t.IssueType)) &&
		(f.Status == "" || StatusKey(f.Status) == StatusKey(t.Status))
}

// Match reports whether name matches pattern. A pattern without `*` must equal name. Each `*`
// matches any run of characters, slashes included.
func Match(pattern, name string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	parts := strings.Split(pattern, "*")
	rest, ok := strings.CutPrefix(name, parts[0])
	if !ok {
		return false
	}
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, p)
		if i < 0 {
			return false
		}
		rest = rest[i+len(p):]
	}
	return len(rest) >= len(last) && strings.HasSuffix(rest, last)
}

// Preview is what a rule would do against the current data, ignoring what fired before.
type Preview struct {
	Moves   []Firing
	Waiting []Waiting
}

// PreviewRule evaluates one rule as if it had never fired, enabled and in isolation. Tickets in an
// excluded status are left out. A rule in mode Default runs with the global mode.
func PreviewRule(r Rule, now time.Time, tickets []Ticket, exclude []string, global Mode) Preview {
	r.Enabled = true
	res := Evaluate(Input{Now: now, Rules: []Rule{r}, Tickets: tickets, Exclude: exclude, DefaultMode: global})
	return Preview{Moves: res.Firings, Waiting: res.Waiting}
}

// Validate checks a rule the way the API does after the schema: a known trigger and mode (all, any
// or default), hours for commit_stale, and a target.
func Validate(r Rule) string {
	switch {
	case !slices.Contains(Triggers, r.Trigger):
		return "unknown trigger"
	case r.Mode != All && r.Mode != Any && r.Mode != Default:
		return "unknown mode"
	case strings.TrimSpace(r.Target) == "" || len(r.Target) > MaxTarget:
		return "target status must be 1 to " + strconv.Itoa(MaxTarget) + " characters"
	case len(r.Name) > MaxName:
		return "name is too long"
	case r.Trigger == CommitStale && (r.Hours < MinHours || r.Hours > MaxHours):
		return "hours must be " + strconv.Itoa(MinHours) + " to " + strconv.Itoa(MaxHours)
	}
	return ""
}

// MergePlan is what the first applicable merged rule does for a ticket once one of its pull
// requests is merged: the rule, and the pull requests that must merge first when its mode is all.
type MergePlan struct {
	Rule      string
	Target    string
	Total     int
	WaitingOn []Ref
}

// AfterMerge evaluates the enabled merged rules in order for a ticket as if the pull request prID
// were merged, ignoring what fired before. The first rule whose filters accept the ticket and that
// pull request is the plan, whether or not it has to wait. It reports false when no rule applies,
// or when the ticket is in an excluded status. Rules in mode Default run with the global mode.
func AfterMerge(rs []Rule, t Ticket, prID string, now time.Time, exclude []string, global Mode) (MergePlan, bool) {
	if !t.Available || excluded(exclude, t.Status) {
		return MergePlan{}, false
	}
	prs := make([]PR, 0, len(t.PRs))
	var this *PR
	for _, p := range t.PRs {
		if p.State == StateClosed {
			continue
		}
		if p.ID == prID {
			p.State = StateMerged
		}
		prs = append(prs, p)
		if p.ID == prID {
			this = &prs[len(prs)-1]
		}
	}
	if this == nil {
		return MergePlan{}, false
	}
	for _, r := range resolveAll(rs, global) {
		if !r.Enabled || r.Trigger != Merged || !ticketPasses(r.Filters, t) || !prPasses(r.Filters, *this) {
			continue
		}
		c := condition(r, prs, now)
		return MergePlan{Rule: r.ID, Target: r.Target, Total: c.total, WaitingOn: c.waiting}, true
	}
	return MergePlan{}, false
}
