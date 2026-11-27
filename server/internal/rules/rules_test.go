package rules_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/rules"
)

var now = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) *time.Time { t := now.Add(-d); return &t }

func pr(id string, mut ...func(*rules.PR)) rules.PR {
	p := rules.PR{
		ID: id, Number: 1, Repo: "acme/web", Base: "main", State: rules.StateOpen, HeadSHA: "sha-" + id,
		ReadySHA: "ready-" + id, CI: "none",
	}
	for _, m := range mut {
		m(&p)
	}
	return p
}

func ticket(key string, prs ...rules.PR) rules.Ticket {
	return rules.Ticket{Key: key, Project: "INP", IssueType: "Story", Status: "In Review", Available: true, PRs: prs}
}

func rule(id string, tr rules.Trigger, mut ...func(*rules.Rule)) rules.Rule {
	r := rules.Rule{ID: id, Name: id, Enabled: true, Trigger: tr, Target: "Done", Mode: rules.All}
	for _, m := range mut {
		m(&r)
	}
	return r
}

func modeAny(r *rules.Rule) { r.Mode = rules.Any }

func eval(rs []rules.Rule, ts []rules.Ticket, fired map[rules.Key]string) rules.Result {
	return rules.Evaluate(rules.Input{Now: now, Rules: rs, Tickets: ts, Fired: fired})
}

func TestTriggers(t *testing.T) {
	tests := []struct {
		name    string
		rule    rules.Rule
		pr      rules.PR
		want    string
		meeting bool
	}{
		{"review submitted", rule("r", rules.ReviewSubmitted), pr("A", func(p *rules.PR) { p.LatestReview = "rev1" }), "A=rev1", true},
		{"review submitted without reviews", rule("r", rules.ReviewSubmitted), pr("A"), "", false},
		{"approved", rule("r", rules.ReviewApproved), pr("A", func(p *rules.PR) { p.ReviewDecision = "APPROVED" }), "A=sha-A:APPROVED", true},
		{"approved needs the decision", rule("r", rules.ReviewApproved), pr("A", func(p *rules.PR) { p.ReviewDecision = "REVIEW_REQUIRED" }), "", false},
		{"approved without a head", rule("r", rules.ReviewApproved), pr("A", func(p *rules.PR) { p.ReviewDecision, p.HeadSHA = "APPROVED", "" }), "", false},
		{"changes requested", rule("r", rules.ChangesRequested), pr("A", func(p *rules.PR) { p.ReviewDecision, p.LatestReview = "CHANGES_REQUESTED", "rev2" }), "A=rev2", true},
		{"changes requested without review id", rule("r", rules.ChangesRequested), pr("A", func(p *rules.PR) { p.ReviewDecision = "CHANGES_REQUESTED" }), "", false},
		{"review requested sorted", rule("r", rules.ReviewRequested), pr("A", func(p *rules.PR) { p.ReviewRequests = []string{"u2", "u1"} }), "A=u1,u2", true},
		{"no review requested", rule("r", rules.ReviewRequested), pr("A"), "", false},
		{"ready", rule("r", rules.ReadyForReview), pr("A"), "A=ready-A", true},
		{"draft is not ready", rule("r", rules.ReadyForReview), pr("A", func(p *rules.PR) { p.Draft = true }), "", false},
		{"ready sha unknown", rule("r", rules.ReadyForReview), pr("A", func(p *rules.PR) { p.ReadySHA = "" }), "", false},
		{"stale", rule("r", rules.CommitStale, func(r *rules.Rule) { r.Hours = 24 }), pr("A", func(p *rules.PR) { p.CommittedAt = ago(25 * time.Hour) }), "A=sha-A", true},
		{"not stale yet", rule("r", rules.CommitStale, func(r *rules.Rule) { r.Hours = 24 }), pr("A", func(p *rules.PR) { p.CommittedAt = ago(23 * time.Hour) }), "", false},
		{"stale exactly at the limit is not stale", rule("r", rules.CommitStale, func(r *rules.Rule) { r.Hours = 24 }), pr("A", func(p *rules.PR) { p.CommittedAt = ago(24 * time.Hour) }), "", false},
		{"stale without commit time", rule("r", rules.CommitStale, func(r *rules.Rule) { r.Hours = 24 }), pr("A"), "", false},
		{"stale without hours", rule("r", rules.CommitStale), pr("A", func(p *rules.PR) { p.CommittedAt = ago(1000 * time.Hour) }), "", false},
		{"stale without head", rule("r", rules.CommitStale, func(r *rules.Rule) { r.Hours = 1 }), pr("A", func(p *rules.PR) { p.CommittedAt, p.HeadSHA = ago(5*time.Hour), "" }), "", false},
		{"ci failing", rule("r", rules.CIFailing), pr("A", func(p *rules.PR) { p.CI = "failing" }), "A=sha-A:failing", true},
		{"ci failing while running", rule("r", rules.CIFailing), pr("A", func(p *rules.PR) { p.CI = "running" }), "", false},
		{"ci passing", rule("r", rules.CIPassing), pr("A", func(p *rules.PR) { p.CI = "passing" }), "A=sha-A:passing", true},
		{"ci not reported", rule("r", rules.CIPassing), pr("A"), "", false},
		{"merged", rule("r", rules.Merged), pr("A", func(p *rules.PR) { p.State = rules.StateMerged }), "A=A", true},
		{"open is not merged", rule("r", rules.Merged), pr("A"), "", false},
		{"merged pull request has no review state", rule("r", rules.ReviewApproved), pr("A", func(p *rules.PR) { p.State, p.ReviewDecision = rules.StateMerged, "APPROVED" }), "", false},
		{"no review yet", rule("r", rules.NoReviewYet), pr("A"), "A=sha-A:unreviewed", true},
		{"no review yet while a review is required", rule("r", rules.NoReviewYet), pr("A", func(p *rules.PR) { p.ReviewDecision = "REVIEW_REQUIRED" }), "A=sha-A:unreviewed", true},
		{"no review yet after a review", rule("r", rules.NoReviewYet), pr("A", func(p *rules.PR) { p.LatestReview = "rev1" }), "", false},
		{"no review yet after a decision", rule("r", rules.NoReviewYet), pr("A", func(p *rules.PR) { p.ReviewDecision = "APPROVED" }), "", false},
		{"no review yet for a draft", rule("r", rules.NoReviewYet), pr("A", func(p *rules.PR) { p.Draft = true }), "", false},
		{"no review yet without a head", rule("r", rules.NoReviewYet), pr("A", func(p *rules.PR) { p.HeadSHA = "" }), "", false},
		{"no review yet for a merged pull request", rule("r", rules.NoReviewYet), pr("A", func(p *rules.PR) { p.State = rules.StateMerged }), "", false},
		{"waiting for approvals", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.ApprovalsHave, p.ApprovalsNeed = 1, 2 }), "A=sha-A:waiting", true},
		{"waiting for approvals, 2 of 3", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.ApprovalsHave, p.ApprovalsNeed = 2, 3 }), "A=sha-A:waiting", true},
		{"waiting for approvals with none yet", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.ApprovalsNeed = 2 }), "", false},
		{"waiting for approvals with enough", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.ApprovalsHave, p.ApprovalsNeed = 2, 2 }), "", false},
		{"waiting for approvals with more than enough", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.ApprovalsHave, p.ApprovalsNeed = 3, 2 }), "", false},
		{"waiting for approvals with nothing required", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.ApprovalsHave, p.ApprovalsNeed = 2, 0 }), "", false},
		{"waiting for approvals with nothing required and none given", rule("r", rules.WaitingApprovals), pr("A"), "", false},
		{"waiting for approvals with an unknown need", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.ApprovalsHave = 1 }), "", false},
		{"waiting for approvals for a draft", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.Draft, p.ApprovalsHave, p.ApprovalsNeed = true, 1, 2 }), "", false},
		{"waiting for approvals without a head", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.HeadSHA, p.ApprovalsHave, p.ApprovalsNeed = "", 1, 2 }), "", false},
		{"waiting for approvals for a merged pull request", rule("r", rules.WaitingApprovals), pr("A", func(p *rules.PR) { p.State, p.ApprovalsHave, p.ApprovalsNeed = rules.StateMerged, 1, 2 }), "", false},
		{"unknown trigger", rule("r", "nope"), pr("A"), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := eval([]rules.Rule{tt.rule}, []rules.Ticket{ticket("INP-1", tt.pr)}, nil)
			if !tt.meeting {
				if len(res.Firings) != 0 || len(res.Holds) != 0 {
					t.Fatalf("unexpected %+v", res)
				}
				return
			}
			if len(res.Firings) != 1 || res.Firings[0].Fingerprint != tt.want || res.Firings[0].Target != "Done" || res.Firings[0].Ticket != "INP-1" {
				t.Fatalf("got %+v, want fingerprint %q", res.Firings, tt.want)
			}
		})
	}
}

func TestFilters(t *testing.T) {
	base := func(f rules.Filters) []rules.Rule {
		return []rules.Rule{rule("r", rules.Merged, func(r *rules.Rule) { r.Filters = f })}
	}
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged; p.Repo = "acme/web"; p.Base = "release/1.2" })
	tests := []struct {
		name string
		f    rules.Filters
		want bool
	}{
		{"none", rules.Filters{}, true},
		{"repo exact", rules.Filters{Repo: "acme/web"}, true},
		{"repo glob", rules.Filters{Repo: "acme/*"}, true},
		{"repo other", rules.Filters{Repo: "acme/api"}, false},
		{"branch glob", rules.Filters{Branch: "release/*"}, true},
		{"branch exact miss", rules.Filters{Branch: "main"}, false},
		{"branch glob miss", rules.Filters{Branch: "hotfix/*"}, false},
		{"branch middle glob", rules.Filters{Branch: "re*/1.2"}, true},
		{"branch glob suffix miss", rules.Filters{Branch: "release/*x"}, false},
		{"project", rules.Filters{Project: "inp"}, true},
		{"project miss", rules.Filters{Project: "OPS"}, false},
		{"issue type", rules.Filters{IssueType: "story"}, true},
		{"issue type miss", rules.Filters{IssueType: "Bug"}, false},
		{"status", rules.Filters{Status: "in review"}, true},
		{"status miss", rules.Filters{Status: "To Do"}, false},
		{"status punctuation", rules.Filters{Status: "In-Review"}, true},
		{"status spaces", rules.Filters{Status: "inreview"}, true},
		{"all together", rules.Filters{Repo: "acme/*", Branch: "release/*", Project: "INP", IssueType: "Story", Status: "In Review"}, true},
		{"one fails", rules.Filters{Repo: "acme/*", Branch: "main", Project: "INP"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := eval(base(tt.f), []rules.Ticket{ticket("INP-1", merged)}, nil)
			if got := len(res.Firings) == 1; got != tt.want {
				t.Fatalf("fires %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPRFiltersLimitWhichPRsCount(t *testing.T) {
	fe := pr("FE", func(p *rules.PR) { p.State = rules.StateMerged; p.Repo = "acme/fe" })
	be := pr("BE", func(p *rules.PR) { p.Repo = "acme/be" })
	r := rule("r", rules.Merged, func(r *rules.Rule) { r.Filters.Repo = "acme/fe" })
	res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", fe, be)}, nil)
	if len(res.Firings) != 1 || res.Firings[0].Fingerprint != "FE=FE" {
		t.Fatalf("the backend PR does not pass the filter, so it must not hold the ticket back: %+v", res)
	}
}

func TestModeAllWaitsForEveryPR(t *testing.T) {
	approved := func(p *rules.PR) { p.ReviewDecision = "APPROVED" }
	a, b, c := pr("A", approved), pr("B", approved), pr("C")
	r := rule("r", rules.ReviewApproved)

	res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", a, b, c)}, nil)
	if len(res.Firings) != 0 {
		t.Fatalf("fired with one PR still open: %+v", res.Firings)
	}
	want := []rules.Waiting{{Ticket: "INP-1", Rule: "r", Total: 3, WaitingOn: []rules.Ref{{ID: "C", Number: 1, Repo: "acme/web"}}}}
	if !reflect.DeepEqual(res.Waiting, want) {
		t.Fatalf("waiting %+v", res.Waiting)
	}

	c.ReviewDecision = "APPROVED"
	res = eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", c, a, b)}, nil)
	if len(res.Firings) != 1 || res.Firings[0].Fingerprint != "A=sha-A:APPROVED,B=sha-B:APPROVED,C=sha-C:APPROVED" {
		t.Fatalf("firings %+v", res.Firings)
	}
	if len(res.Waiting) != 0 {
		t.Fatalf("waiting %+v", res.Waiting)
	}
}

func TestWaitingNeedsProgressAndATicketThatIsNotInTarget(t *testing.T) {
	r := rule("r", rules.ReviewApproved)
	if res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", pr("A"), pr("B"))}, nil); len(res.Waiting) != 0 {
		t.Fatalf("no PR meets the rule yet, so nothing is waiting: %+v", res.Waiting)
	}
	tk := ticket("INP-1", pr("A", func(p *rules.PR) { p.ReviewDecision = "APPROVED" }), pr("B"))
	tk.Status = "done"
	if res := eval([]rules.Rule{r}, []rules.Ticket{tk}, nil); len(res.Waiting) != 0 {
		t.Fatalf("the ticket is in the target status already: %+v", res.Waiting)
	}
}

func TestModeAnyFiresOnTheFirstPRAndAgainOnANewOne(t *testing.T) {
	r := rule("r", rules.ReviewApproved, modeAny)
	a := pr("A", func(p *rules.PR) { p.ReviewDecision = "APPROVED" })
	b := pr("B")
	res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", a, b)}, nil)
	if len(res.Firings) != 1 || res.Firings[0].Fingerprint != "A=sha-A:APPROVED" || len(res.Waiting) != 0 {
		t.Fatalf("%+v", res)
	}
	stored := map[rules.Key]string{{Rule: "r", Ticket: "INP-1"}: "A=sha-A:APPROVED"}
	if res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", a, b)}, stored); len(res.Firings) != 0 {
		t.Fatalf("same fingerprint fired again: %+v", res.Firings)
	}
	b.ReviewDecision = "APPROVED"
	if res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", a, b)}, stored); len(res.Firings) != 1 {
		t.Fatalf("a second PR is a new event: %+v", res.Firings)
	}
}

func TestEveryTriggerIsListedAndValid(t *testing.T) {
	for _, tr := range []rules.Trigger{rules.NoReviewYet, rules.WaitingApprovals} {
		if !slices.Contains(rules.Triggers, tr) {
			t.Errorf("%s is not in Triggers", tr)
		}
	}
	if len(rules.Triggers) != 11 {
		t.Errorf("%d triggers, want 11", len(rules.Triggers))
	}
}

func TestApprovalTriggersFireOncePerHeadCommit(t *testing.T) {
	for _, tc := range []struct {
		name string
		tr   rules.Trigger
		set  func(*rules.PR)
		fp   string
	}{
		{"no review yet", rules.NoReviewYet, func(*rules.PR) {}, "A=sha-A:unreviewed"},
		{"waiting for approvals", rules.WaitingApprovals, func(p *rules.PR) { p.ApprovalsHave, p.ApprovalsNeed = 1, 3 }, "A=sha-A:waiting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := rule("r", tc.tr)
			a := pr("A", tc.set)
			if res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", a)}, nil); len(res.Firings) != 1 || res.Firings[0].Fingerprint != tc.fp {
				t.Fatalf("%+v", res)
			}
			stored := map[rules.Key]string{{Rule: "r", Ticket: "INP-1"}: tc.fp}
			if res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", a)}, stored); len(res.Firings) != 0 || len(res.Holds) != 1 {
				t.Fatalf("fired twice for one head: %+v", res)
			}
			a.HeadSHA = "sha-new"
			if res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", a)}, stored); len(res.Firings) != 1 {
				t.Fatalf("a new head commit is a new event: %+v", res)
			}
		})
	}
}

func TestApprovalTriggersFollowFiltersExclusionsModeAndPreview(t *testing.T) {
	a := pr("A", func(p *rules.PR) { p.ApprovalsHave, p.ApprovalsNeed = 1, 2 })
	b := pr("B")
	for _, tr := range []rules.Trigger{rules.NoReviewYet, rules.WaitingApprovals} {
		fe := pr("FE", func(p *rules.PR) { p.Repo = "acme/fe"; p.ApprovalsHave, p.ApprovalsNeed = 1, 2 })
		r := rule("r", tr, func(r *rules.Rule) { r.Filters = rules.Filters{Repo: "acme/fe", Status: "In Review"} })
		if res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", fe, b)}, nil); len(res.Firings) != 1 {
			t.Errorf("%s: the repo filter ignores the other pull request: %+v", tr, res)
		}
		r.Filters.Repo = "acme/other"
		if res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", a)}, nil); len(res.Firings) != 0 {
			t.Errorf("%s: repo filter: %+v", tr, res)
		}
	}
	// Mode all waits for the pull request that does not meet it, and mode any does not.
	r := rule("r", rules.WaitingApprovals)
	if res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", a, b)}, nil); len(res.Firings) != 0 || len(res.Waiting) != 1 {
		t.Errorf("mode all: %+v", res)
	}
	if res := eval([]rules.Rule{rule("r", rules.WaitingApprovals, modeAny)}, []rules.Ticket{ticket("INP-1", a, b)}, nil); len(res.Firings) != 1 {
		t.Errorf("mode any: %+v", res)
	}
	// An excluded status is never moved, and a preview ignores what fired before.
	excl := rules.Input{Now: now, Rules: []rules.Rule{r}, Tickets: []rules.Ticket{ticket("INP-1", a)}, Exclude: []string{"in review"}}
	if res := rules.Evaluate(excl); len(res.Firings) != 0 {
		t.Errorf("excluded: %+v", res)
	}
	if pv := rules.PreviewRule(r, now, []rules.Ticket{ticket("INP-1", a)}, nil); len(pv.Moves) != 1 {
		t.Errorf("preview: %+v", pv)
	}
}

func TestFingerprintChangeVersusNoChange(t *testing.T) {
	r := rule("r", rules.ReviewApproved)
	p := pr("A", func(p *rules.PR) { p.ReviewDecision = "APPROVED" })
	key := rules.Key{Rule: "r", Ticket: "INP-1"}
	res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", p)}, map[rules.Key]string{key: "A=sha-A:APPROVED"})
	if len(res.Firings) != 0 || len(res.Holds) != 1 {
		t.Fatalf("an unchanged fingerprint must not fire: %+v", res)
	}
	p.HeadSHA = "sha-new"
	res = eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", p)}, map[rules.Key]string{key: "A=sha-A:APPROVED"})
	if len(res.Firings) != 1 || res.Firings[0].Fingerprint != "A=sha-new:APPROVED" {
		t.Fatalf("a new head approved again is a new event: %+v", res)
	}
}

func TestManualMoveInJiraIsNotUndone(t *testing.T) {
	r := rule("r", rules.ReviewApproved, func(r *rules.Rule) { r.Target = "In Review" })
	p := pr("A", func(p *rules.PR) { p.ReviewDecision = "APPROVED" })
	tk := ticket("INP-1", p)
	tk.Status = "Backlog" // the person moved it back by hand
	stored := map[rules.Key]string{{Rule: "r", Ticket: "INP-1"}: "A=sha-A:APPROVED"}
	if res := eval([]rules.Rule{r}, []rules.Ticket{tk}, stored); len(res.Firings) != 0 {
		t.Fatalf("a manual move was undone: %+v", res.Firings)
	}
}

func TestCatchUpAfterRestart(t *testing.T) {
	r := rule("r", rules.Merged)
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged })
	// Nothing is stored for the rule: the merge happened while the app was closed.
	res := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", merged)}, nil)
	if len(res.Firings) != 1 {
		t.Fatalf("missed event was not caught up: %+v", res)
	}
	// After it was recorded, a restart finds the same fingerprint and does nothing.
	again := eval([]rules.Rule{r}, []rules.Ticket{ticket("INP-1", merged)}, map[rules.Key]string{{Rule: "r", Ticket: "INP-1"}: res.Firings[0].Fingerprint})
	if len(again.Firings) != 0 {
		t.Fatalf("fired twice: %+v", again)
	}
}

func TestFirstMatchWinsAndOnlyOneTransitionPerTicket(t *testing.T) {
	approved := pr("A", func(p *rules.PR) { p.ReviewDecision = "APPROVED"; p.CI = "passing" })
	first := rule("first", rules.ReviewApproved, func(r *rules.Rule) { r.Target = "Ready" })
	second := rule("second", rules.CIPassing, func(r *rules.Rule) { r.Target = "QA" })
	res := eval([]rules.Rule{first, second}, []rules.Ticket{ticket("INP-1", approved), ticket("INP-2", approved)}, nil)
	if len(res.Firings) != 2 || res.Firings[0].Rule != "first" || res.Firings[1].Rule != "first" || res.Firings[0].Target != "Ready" {
		t.Fatalf("%+v", res.Firings)
	}
	if len(res.Holds) != 4 {
		t.Fatalf("both rules hold for both tickets: %+v", res.Holds)
	}
}

func TestRuleThatAlreadyFiredLetsTheNextOneFire(t *testing.T) {
	approved := pr("A", func(p *rules.PR) { p.ReviewDecision = "APPROVED"; p.CI = "passing" })
	first := rule("first", rules.ReviewApproved)
	second := rule("second", rules.CIPassing, func(r *rules.Rule) { r.Target = "QA" })
	stored := map[rules.Key]string{{Rule: "first", Ticket: "INP-1"}: "A=sha-A:APPROVED"}
	res := eval([]rules.Rule{first, second}, []rules.Ticket{ticket("INP-1", approved)}, stored)
	if len(res.Firings) != 1 || res.Firings[0].Rule != "second" {
		t.Fatalf("%+v", res.Firings)
	}
}

func TestAlreadyInTargetIsRecordedNotSent(t *testing.T) {
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged })
	tk := ticket("INP-1", merged)
	tk.Status = "DONE"
	res := eval([]rules.Rule{rule("r", rules.Merged)}, []rules.Ticket{tk}, nil)
	if len(res.Firings) != 1 || !res.Firings[0].InTarget {
		t.Fatalf("%+v", res.Firings)
	}
}

func TestUnavailableDataNeverMatches(t *testing.T) {
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged })
	tk := ticket("INP-1", merged)
	tk.Available = false
	if res := eval([]rules.Rule{rule("r", rules.Merged)}, []rules.Ticket{tk}, nil); len(res.Firings)+len(res.Holds)+len(res.Waiting) != 0 {
		t.Fatalf("a ticket without Jira data matched: %+v", res)
	}
	// CI that has not reported does not satisfy mode all either.
	passing := pr("A", func(p *rules.PR) { p.CI = "passing" })
	res := eval([]rules.Rule{rule("r", rules.CIPassing)}, []rules.Ticket{ticket("INP-1", passing, pr("B"))}, nil)
	if len(res.Firings) != 0 || len(res.Waiting) != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestClosedPRsStopCounting(t *testing.T) {
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged })
	closed := pr("B", func(p *rules.PR) { p.State = rules.StateClosed })
	res := eval([]rules.Rule{rule("r", rules.Merged)}, []rules.Ticket{ticket("INP-1", merged, closed)}, nil)
	if len(res.Firings) != 1 || res.Firings[0].Fingerprint != "A=A" {
		t.Fatalf("%+v", res)
	}
	only := eval([]rules.Rule{rule("r", rules.Merged)}, []rules.Ticket{ticket("INP-1", closed)}, nil)
	if len(only.Firings)+len(only.Holds) != 0 {
		t.Fatalf("a ticket with only closed PRs matched: %+v", only)
	}
	if none := eval([]rules.Rule{rule("r", rules.Merged)}, []rules.Ticket{ticket("INP-1")}, nil); len(none.Firings) != 0 {
		t.Fatalf("a ticket without PRs matched: %+v", none)
	}
}

func TestMergedRuleWaitsForTheOpenPRs(t *testing.T) {
	fe := pr("FE", func(p *rules.PR) { p.State = rules.StateMerged; p.Number = 34; p.Repo = "acme/fe" })
	be := pr("BE", func(p *rules.PR) { p.State = rules.StateMerged })
	proxy := pr("PX", func(p *rules.PR) { p.Number = 56; p.Repo = "acme/proxy" })
	res := eval([]rules.Rule{rule("r", rules.Merged)}, []rules.Ticket{ticket("INP-1", fe, be, proxy)}, nil)
	if len(res.Firings) != 0 || len(res.Waiting) != 1 || res.Waiting[0].Total != 3 || res.Waiting[0].WaitingOn[0].Number != 56 {
		t.Fatalf("%+v", res)
	}
	proxy.State = rules.StateMerged
	res = eval([]rules.Rule{rule("r", rules.Merged)}, []rules.Ticket{ticket("INP-1", fe, be, proxy)}, nil)
	if len(res.Firings) != 1 || res.Firings[0].Fingerprint != "BE=BE,FE=FE,PX=PX" {
		t.Fatalf("%+v", res)
	}
}

func TestDisabledRulesAndEmptyInput(t *testing.T) {
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged })
	off := rule("r", rules.Merged, func(r *rules.Rule) { r.Enabled = false })
	if res := eval([]rules.Rule{off}, []rules.Ticket{ticket("INP-1", merged)}, nil); len(res.Firings)+len(res.Holds) != 0 {
		t.Fatalf("%+v", res)
	}
	if res := eval(nil, nil, nil); len(res.Firings) != 0 {
		t.Fatal("empty input fired")
	}
}

func TestPreviewIgnoresWhatFiredBeforeAndTheEnabledFlag(t *testing.T) {
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged })
	r := rule("r", rules.Merged, func(r *rules.Rule) { r.Enabled = false })
	pv := rules.PreviewRule(r, now, []rules.Ticket{ticket("INP-1", merged), ticket("INP-2", pr("B"), merged)}, nil)
	if len(pv.Moves) != 1 || pv.Moves[0].Ticket != "INP-1" || len(pv.Waiting) != 1 || pv.Waiting[0].Ticket != "INP-2" {
		t.Fatalf("%+v", pv)
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"main", "main", true}, {"main", "main2", false}, {"*", "anything/at/all", true},
		{"release/*", "release/1.2/hotfix", true}, {"release/*", "release", false},
		{"a*b*c", "aXbYc", true}, {"a*b*c", "aXc", false}, {"*x", "abx", true}, {"x*", "xyz", true}, {"x*", "yx", false},
	}
	for _, tt := range tests {
		if got := rules.Match(tt.pattern, tt.name); got != tt.want {
			t.Errorf("Match(%q, %q) = %v", tt.pattern, tt.name, got)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := rule("r", rules.Merged)
	tests := []struct {
		name string
		mut  func(*rules.Rule)
		bad  bool
	}{
		{"valid", func(*rules.Rule) {}, false},
		{"unknown trigger", func(r *rules.Rule) { r.Trigger = "x" }, true},
		{"unknown mode", func(r *rules.Rule) { r.Mode = "some" }, true},
		{"empty target", func(r *rules.Rule) { r.Target = "  " }, true},
		{"long target", func(r *rules.Rule) { r.Target = string(make([]byte, 101)) }, true},
		{"long name", func(r *rules.Rule) { r.Name = string(make([]byte, 101)) }, true},
		{"stale needs hours", func(r *rules.Rule) { r.Trigger = rules.CommitStale }, true},
		{"stale too many hours", func(r *rules.Rule) { r.Trigger, r.Hours = rules.CommitStale, 721 }, true},
		{"stale 1 hour", func(r *rules.Rule) { r.Trigger, r.Hours = rules.CommitStale, 1 }, false},
		{"stale 720 hours", func(r *rules.Rule) { r.Trigger, r.Hours = rules.CommitStale, 720 }, false},
		{"no review yet needs no hours", func(r *rules.Rule) { r.Trigger = rules.NoReviewYet }, false},
		{"waiting for approvals needs no hours", func(r *rules.Rule) { r.Trigger = rules.WaitingApprovals }, false},
	}
	for _, tt := range tests {
		r := ok
		tt.mut(&r)
		if got := rules.Validate(r) != ""; got != tt.bad {
			t.Errorf("%s: invalid = %v", tt.name, got)
		}
	}
}

func TestAfterMerge(t *testing.T) {
	fe := pr("FE", func(p *rules.PR) { p.Number = 34; p.Repo = "acme/fe" })
	be := pr("BE", func(p *rules.PR) { p.Number = 12; p.Repo = "acme/be" })
	merged := pr("OLD", func(p *rules.PR) { p.State = rules.StateMerged })
	closed := pr("GONE", func(p *rules.PR) { p.State = rules.StateClosed })
	approved := rule("not-merged", rules.ReviewApproved)
	toDone := rule("done", rules.Merged, func(r *rules.Rule) { r.Filters.Repo = "acme/*" })
	toQA := rule("qa", rules.Merged, func(r *rules.Rule) { r.Target = "QA"; r.Mode = rules.Any })

	plan, ok := rules.AfterMerge([]rules.Rule{approved, toDone, toQA}, ticket("INP-1", fe, be, merged, closed), "FE", now, nil)
	if !ok || plan.Rule != "done" || plan.Target != "Done" || plan.Total != 3 || len(plan.WaitingOn) != 1 || plan.WaitingOn[0].Number != 12 {
		t.Fatalf("first applicable rule waits for the backend PR: %+v", plan)
	}
	plan, ok = rules.AfterMerge([]rules.Rule{toDone}, ticket("INP-1", fe, merged), "FE", now, nil)
	if !ok || len(plan.WaitingOn) != 0 || plan.Total != 2 {
		t.Fatalf("the last open PR does not wait: %+v", plan)
	}
	plan, ok = rules.AfterMerge([]rules.Rule{toQA}, ticket("INP-1", fe, be), "FE", now, nil)
	if !ok || plan.Target != "QA" || len(plan.WaitingOn) != 0 {
		t.Fatalf("mode any never waits: %+v", plan)
	}

	off := toDone
	off.Enabled = false
	other := ticket("INP-1", fe)
	other.Status = "To Do"
	otherRepo := rule("elsewhere", rules.Merged, func(r *rules.Rule) { r.Filters.Repo = "other/*" })
	status := rule("status", rules.Merged, func(r *rules.Rule) { r.Filters.Status = "In Review" })
	for name, tc := range map[string]struct {
		rs   []rules.Rule
		t    rules.Ticket
		prID string
	}{
		"disabled":           {[]rules.Rule{off}, ticket("INP-1", fe), "FE"},
		"no merged rule":     {[]rules.Rule{approved}, ticket("INP-1", fe), "FE"},
		"pr filter":          {[]rules.Rule{otherRepo}, ticket("INP-1", fe), "FE"},
		"ticket filter":      {[]rules.Rule{status}, other, "FE"},
		"unknown pr":         {[]rules.Rule{toDone}, ticket("INP-1", fe), "NOPE"},
		"closed pr":          {[]rules.Rule{toDone}, ticket("INP-1", closed), "GONE"},
		"unavailable ticket": {[]rules.Rule{toDone}, func() rules.Ticket { t := ticket("INP-1", fe); t.Available = false; return t }(), "FE"},
	} {
		if _, ok := rules.AfterMerge(tc.rs, tc.t, tc.prID, now, nil); ok {
			t.Errorf("%s: a plan was made", name)
		}
	}
}

func TestExcludedStatusesAreNeverMovedFiredOrListedAsWaiting(t *testing.T) {
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged })
	inReview := ticket("INP-1", merged)
	done := ticket("INP-2", merged)
	done.Status = "done"
	waiting := ticket("INP-3", merged, pr("B"))
	waiting.Status = "DONE"
	rs := []rules.Rule{rule("r", rules.Merged, func(r *rules.Rule) { r.Target = "QA" })}
	in := rules.Input{Now: now, Rules: rs, Tickets: []rules.Ticket{inReview, done, waiting}, Exclude: []string{"Done", "Closed"}}
	res := rules.Evaluate(in)
	if len(res.Firings) != 1 || res.Firings[0].Ticket != "INP-1" || len(res.Holds) != 1 || len(res.Waiting) != 0 {
		t.Fatalf("only the ticket outside the excluded statuses counts: %+v", res)
	}
	in.Exclude = nil
	if res := rules.Evaluate(in); len(res.Firings) != 2 || len(res.Waiting) != 1 {
		t.Fatalf("without exclusions every ticket counts: %+v", res)
	}
	in.Exclude = []string{""}
	if res := rules.Evaluate(in); len(res.Firings) != 2 {
		t.Fatalf("an empty exclusion matches no ticket: %+v", res)
	}
}

func TestPreviewAndAfterMergeSkipExcludedStatuses(t *testing.T) {
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged })
	tk := ticket("INP-1", merged, pr("B"))
	tk.Status = "Done"
	r := rule("r", rules.Merged, func(r *rules.Rule) { r.Target = "QA" })
	if pv := rules.PreviewRule(r, now, []rules.Ticket{tk}, []string{"done"}); len(pv.Moves)+len(pv.Waiting) != 0 {
		t.Fatalf("%+v", pv)
	}
	if pv := rules.PreviewRule(r, now, []rules.Ticket{tk}, nil); len(pv.Waiting) != 1 {
		t.Fatalf("%+v", pv)
	}
	if _, ok := rules.AfterMerge([]rules.Rule{r}, tk, "B", now, []string{"Done"}); ok {
		t.Fatal("an excluded ticket has no merge plan")
	}
	if _, ok := rules.AfterMerge([]rules.Rule{r}, tk, "B", now, []string{"QA"}); !ok {
		t.Fatal("another status does not exclude it")
	}
}

func TestStatusKey(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"hyphen", "Re-open", "reopen"},
		{"space", "re open", "reopen"},
		{"upper", "REOPEN", "reopen"},
		{"different word", "Reopened", "reopened"},
		{"many separators", "  In  / Progress_(2) ", "inprogress2"},
		{"digits", "Phase 2", "phase2"},
		{"thai", "รอ ตรวจสอบ", "รอตรวจสอบ"},
		{"unicode case", "ÉTÉ", "été"},
		{"only punctuation", "---", "---"},
		{"only punctuation trimmed and lowered", "  -- ! ", "-- !"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rules.StatusKey(tt.in); got != tt.want {
				t.Fatalf("StatusKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStatusesCompareByKey(t *testing.T) {
	merged := pr("A", func(p *rules.PR) { p.State = rules.StateMerged })
	rs := []rules.Rule{rule("r", rules.Merged, func(r *rules.Rule) { r.Target = "Re-open" })}
	tk := ticket("INP-1", merged)
	tk.Status = "re open"
	if res := eval(rs, []rules.Ticket{tk}, nil); len(res.Firings) != 1 || !res.Firings[0].InTarget {
		t.Fatalf("a near-duplicate of the target is already in it: %+v", res.Firings)
	}
	tk.Status = "Reopened"
	if res := eval(rs, []rules.Ticket{tk}, nil); len(res.Firings) != 1 || res.Firings[0].InTarget {
		t.Fatalf("Reopened is another status: %+v", res.Firings)
	}
	waits := rule("w", rules.ReviewApproved, func(r *rules.Rule) { r.Target = "Re-open" })
	tk = ticket("INP-2", pr("A", func(p *rules.PR) { p.ReviewDecision = "APPROVED" }), pr("B"))
	tk.Status = "REOPEN"
	if res := eval([]rules.Rule{waits}, []rules.Ticket{tk}, nil); len(res.Waiting) != 0 {
		t.Fatalf("a ticket in the target by key is not waiting: %+v", res.Waiting)
	}
	tk.Status = "Reopened"
	if res := eval([]rules.Rule{waits}, []rules.Ticket{tk}, nil); len(res.Waiting) != 1 {
		t.Fatalf("%+v", res.Waiting)
	}
	tk = ticket("INP-3", merged)
	tk.Status = "re-open"
	in := rules.Input{Now: now, Rules: rs, Tickets: []rules.Ticket{tk}, Exclude: []string{"Re open"}}
	if res := rules.Evaluate(in); len(res.Firings) != 0 {
		t.Fatalf("a near-duplicate of an excluded status is excluded: %+v", res.Firings)
	}
}
