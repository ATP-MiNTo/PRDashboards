package poller_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/events"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/rules"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func doneTransition() jira.Transition {
	return jira.Transition{ID: "31", Name: "Done", ToStatus: "Done", ToCategory: "done", Fields: []jira.Field{}}
}

func syncRule(id, trigger, target, mode string) store.SyncRule {
	return store.SyncRule{ID: id, Name: id, Enabled: true, Trigger: trigger, Target: target, Mode: mode}
}

func (r *rig) putRules(rs ...store.SyncRule) {
	r.t.Helper()
	if err := r.store.PutRules(context.Background(), rs, nil, nil, r.clock.Now()); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) activity() []store.ActivityRow {
	r.t.Helper()
	rows, err := r.store.Activity(context.Background(), 0, 100)
	if err != nil {
		r.t.Fatal(err)
	}
	return rows
}

func (r *rig) fired() map[string]string {
	r.t.Helper()
	st, err := r.store.SyncState(context.Background(), r.clock.Now().Add(-time.Hour), nil)
	if err != nil {
		r.t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range st.Fired {
		out[f.Rule+"|"+f.Ticket] = f.Fingerprint
	}
	return out
}

func (r *rig) allowTransitions(tickets ...string) {
	r.jira.set(func(f *fakeJira) {
		f.transitions = map[string][]jira.Transition{}
		f.transitionsErr, f.moveErr = map[string]error{}, map[string]error{}
		for _, k := range tickets {
			f.transitions[k] = []jira.Transition{doneTransition()}
		}
	})
}

// merge makes the pull requests leave the open list as merged.
func (r *rig) merge(ids ...string) {
	r.gh.set(func(f *fakeGitHub) {
		f.merged = map[string]time.Time{}
		var keep []github.PR
		for _, p := range f.result.PRs {
			if slices.Contains(ids, p.ID) {
				f.merged[p.ID] = r.clock.Now()
			} else {
				keep = append(keep, p)
			}
		}
		f.result.PRs = keep
	})
}

func TestMergedRuleMovesTheTicketOfAMergedPR(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	b := pr("PR_b", 2, "acme/web", "feat/INP-2", "main", "b")
	r := newRig(t, a, b)
	r.putRules(syncRule("done", "merged", "done", "all"))
	r.allowTransitions("INP-1", "INP-2")
	r.sync()
	if len(r.jira.moveLog()) != 0 {
		t.Fatal("nothing merged yet")
	}

	r.merge("PR_a")
	r.clock.Advance(time.Minute)
	refreshes, _ := r.store.counts()
	r.sync()
	if got := r.jira.transitionLog(); !slices.Equal(got, []string{"INP-1"}) {
		t.Fatalf("transitions are fetched only for the ticket that fires: %v", got)
	}
	if got := r.jira.moveLog(); !slices.Equal(got, []string{"INP-1:31"}) {
		t.Fatalf("moves %v", got)
	}
	if after, _ := r.store.counts(); after != refreshes+1 {
		t.Fatalf("the refresh, the activity, the retries and the firings are one transaction: %d -> %d", refreshes, after)
	}
	acts := r.activity()
	if len(acts) != 1 || acts[0].Result != "ok" || acts[0].TriggeredBy != "rule:done" || acts[0].TicketKey != "INP-1" {
		t.Fatalf("activity %+v", acts)
	}
	if got := r.fired()["done|INP-1"]; got != "PR_a=PR_a" {
		t.Fatalf("firings %v", r.fired())
	}
	if n := len(r.events.named(events.Activity)); n != 1 {
		t.Fatalf("activity events %d", n)
	}
	for _, p := range r.snapshot().Prs {
		for _, tk := range p.Tickets {
			if tk.Key == "INP-1" && (tk.Status == nil || *tk.Status != "Done") {
				t.Fatalf("the snapshot still shows the old status: %+v", tk)
			}
		}
	}

	// The same state again does nothing: no new calls, no new activity.
	r.clock.Advance(time.Minute)
	r.sync()
	if len(r.jira.moveLog()) != 1 || len(r.jira.transitionLog()) != 1 || len(r.activity()) != 1 {
		t.Fatal("a fired rule fired again")
	}
}

func TestTransitionIsChosenByStatusKey(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.putRules(syncRule("done", "merged", "D-one", "all"))
	r.allowTransitions("INP-1")
	r.sync()
	r.merge("PR_a")
	r.clock.Advance(time.Minute)
	r.sync()
	if got := r.jira.moveLog(); !slices.Equal(got, []string{"INP-1:31"}) {
		t.Fatalf("a target that differs from the status only in punctuation still picks its transition: %v", got)
	}
}

func TestRuleFiringsMakeNoCallsForTicketsThatDoNotFire(t *testing.T) {
	var prs []github.PR
	for i, k := range []string{"INP-1", "INP-2", "INP-1", "OPS-9"} {
		prs = append(prs, pr("PR_"+string(rune('a'+i)), i+1, "acme/web", "feat/"+k+"-x", "main", "t"))
	}
	r := newRig(t, prs...)
	r.putRules(syncRule("done", "merged", "Done", "any"))
	r.allowTransitions("INP-1", "INP-2", "OPS-9")
	r.sync()
	r.merge("PR_b")
	r.clock.Advance(time.Minute)
	r.sync()
	if got := r.jira.transitionLog(); !slices.Equal(got, []string{"INP-2"}) {
		t.Fatalf("%v", got)
	}
}

func TestModeAllMovesTheTicketOnlyAfterTheThirdPR(t *testing.T) {
	approved := func(p github.PR) github.PR { p.ReviewDecision = "APPROVED"; return p }
	open := func(p github.PR) github.PR { p.ReviewDecision = "REVIEW_REQUIRED"; return p }
	fe := pr("PR_fe", 34, "acme/fe", "feat/INP-1-fe", "main", "fe")
	be := pr("PR_be", 12, "acme/be", "feat/INP-1-be", "main", "be")
	px := pr("PR_px", 56, "acme/proxy", "feat/INP-1-px", "main", "px")
	r := newRig(t, approved(fe), open(be), open(px))
	r.jira.issues["INP-1"] = jira.Issue{Key: "INP-1", Summary: "multi", Status: "In Review", StatusCategory: "indeterminate", Project: "INP", IssueType: "Story"}
	r.putRules(syncRule("ready", "review_approved", "Done", "all"))
	r.allowTransitions("INP-1")

	set := func(prs ...github.PR) {
		r.gh.set(func(f *fakeGitHub) { f.result.PRs = prs })
		r.clock.Advance(time.Minute)
		r.sync()
	}
	r.sync()
	var hint string
	for _, p := range r.snapshot().Prs {
		if w := p.Tickets[0].Waiting; w != nil && p.Id == "PR_fe" {
			hint = w.Rule
			if w.Total != 3 || len(w.WaitingOn) != 2 {
				t.Fatalf("waiting %+v", w)
			}
		}
	}
	if hint != "ready" {
		t.Fatal("the ticket cell has no waiting hint")
	}
	set(approved(fe), approved(be), open(px))
	if len(r.jira.moveLog()) != 0 {
		t.Fatal("moved with one PR still waiting")
	}
	set(approved(fe), approved(be), approved(px))
	if got := r.jira.moveLog(); !slices.Equal(got, []string{"INP-1:31"}) {
		t.Fatalf("moves %v", got)
	}
	for _, p := range r.snapshot().Prs {
		if p.Tickets[0].Waiting != nil {
			t.Fatal("still waiting")
		}
	}
}

func TestModeAnyMovesOnTheFirstPR(t *testing.T) {
	fe := pr("PR_fe", 34, "acme/fe", "feat/INP-1-fe", "main", "fe")
	be := pr("PR_be", 12, "acme/be", "feat/INP-1-be", "main", "be")
	fe.ReviewDecision, be.ReviewDecision = "APPROVED", "REVIEW_REQUIRED"
	r := newRig(t, fe, be)
	r.putRules(syncRule("any", "review_approved", "Done", "any"))
	r.allowTransitions("INP-1")
	r.sync()
	if len(r.jira.moveLog()) != 1 {
		t.Fatalf("%v", r.jira.moveLog())
	}
}

func TestTicketInAnExcludedStatusIsNeverTouched(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/OPS-9", "main", "a")
	r := newRig(t, a)
	r.putRules(syncRule("done", "review_approved", "DONE", "all"))
	r.allowTransitions("OPS-9")
	r.sync()
	if len(r.jira.transitionLog()) != 0 || len(r.jira.moveLog()) != 0 || len(r.activity()) != 0 || len(r.fired()) != 0 {
		t.Fatalf("OPS-9 is in Done, which is excluded by default: %v", r.fired())
	}
}

func TestTicketAlreadyInTargetRecordsTheFiringWithoutACall(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/OPS-9", "main", "a")
	r := newRig(t, a)
	noExclusions := store.DefaultSettings()
	noExclusions.ExcludedStatuses = nil
	if err := r.store.PutSettings(context.Background(), noExclusions); err != nil {
		t.Fatal(err)
	}
	r.putRules(syncRule("done", "review_approved", "DONE", "all"))
	r.allowTransitions("OPS-9")
	r.sync()
	if len(r.jira.transitionLog()) != 0 || len(r.jira.moveLog()) != 0 || len(r.activity()) != 0 {
		t.Fatal("a ticket already in the target was touched")
	}
	if len(r.fired()) != 1 {
		t.Fatalf("firing not recorded: %v", r.fired())
	}
}

func TestTransitionWithRequiredFieldsNeedsInput(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.putRules(syncRule("done", "review_approved", "Done", "all"))
	r.allowTransitions()
	r.jira.set(func(f *fakeJira) {
		tr := doneTransition()
		tr.Fields = []jira.Field{{ID: "resolution", Name: "Resolution", Required: true, Type: "resolution"}}
		f.transitions["INP-1"] = []jira.Transition{tr}
	})
	r.sync()
	acts := r.activity()
	if len(r.jira.moveLog()) != 0 || len(acts) != 1 || acts[0].Result != "needs_input" || acts[0].Error == "" {
		t.Fatalf("moves %v activity %+v", r.jira.moveLog(), acts)
	}
	r.clock.Advance(time.Minute)
	r.sync()
	if len(r.activity()) != 1 || len(r.jira.transitionLog()) != 1 {
		t.Fatal("the same input request was raised again")
	}
}

func TestFailedMoveIsKeptForRetryAndNotRepeated(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.putRules(syncRule("done", "review_approved", "Done", "all"))
	r.allowTransitions("INP-1")
	r.jira.set(func(f *fakeJira) {
		f.moveErr["INP-1"] = &jira.StatusError{Status: 400, Detail: "resolution is required"}
	})
	r.sync()
	retries, err := r.store.Retries(context.Background())
	if err != nil || len(retries) != 1 || retries[0].TicketKey != "INP-1" || retries[0].ToStatus != "Done" || retries[0].TransitionID != "31" {
		t.Fatalf("%+v %v", retries, err)
	}
	if acts := r.activity(); len(acts) != 1 || acts[0].Result != "failed" {
		t.Fatalf("%+v", acts)
	}
	r.clock.Advance(time.Minute)
	r.sync()
	if len(r.jira.moveLog()) != 1 {
		t.Fatal("a failed move was repeated by the engine")
	}
}

func TestNoTransitionToTheTargetIsLoggedOnce(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.putRules(syncRule("qa", "review_approved", "QA", "all"))
	r.allowTransitions("INP-1")
	r.sync()
	r.clock.Advance(time.Minute)
	r.sync()
	acts := r.activity()
	if len(acts) != 1 || acts[0].Result != "skipped" || len(r.jira.moveLog()) != 0 {
		t.Fatalf("%+v", acts)
	}
}

func TestTransitionLookupFailureIsRetriedNextRefresh(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.putRules(syncRule("done", "review_approved", "Done", "all"))
	r.allowTransitions("INP-1")
	r.jira.set(func(f *fakeJira) { f.transitionsErr["INP-1"] = errors.New("boom") })
	r.sync()
	if len(r.activity()) != 0 || len(r.fired()) != 0 {
		t.Fatal("nothing may be recorded when Jira could not be asked")
	}
	r.jira.set(func(f *fakeJira) { f.transitionsErr = map[string]error{} })
	r.clock.Advance(time.Minute)
	r.sync()
	if len(r.jira.moveLog()) != 1 {
		t.Fatalf("%v", r.jira.moveLog())
	}
}

func TestCatchUpAfterRestartAndNoRepeatWhenTheDatabaseKnowsTheFiring(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.allowTransitions("INP-1")
	r.sync() // the app saw the open PR, then closed
	r.putRules(syncRule("done", "merged", "Done", "all"))
	r.merge("PR_a") // merged while the app was closed

	restarted := r.newPoller(nil, nil)
	if err := restarted.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.jira.moveLog(); !slices.Equal(got, []string{"INP-1:31"}) {
		t.Fatalf("the missed merge was not caught up: %v", got)
	}
	again := r.newPoller(nil, nil)
	r.clock.Advance(time.Minute)
	if err := again.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.jira.moveLog()) != 1 {
		t.Fatal("a restart repeated a move")
	}
}

func TestManualMoveInJiraIsNotUndone(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.putRules(syncRule("approved", "review_approved", "Done", "all"))
	r.allowTransitions("INP-1")
	r.sync()
	r.jira.set(func(f *fakeJira) {
		i := f.issues["INP-1"]
		i.Status = "To Do"
		f.issues["INP-1"] = i
	})
	r.clock.Advance(10 * time.Minute)
	r.sync()
	if len(r.jira.moveLog()) != 1 {
		t.Fatalf("the person's move back to To Do was undone: %v", r.jira.moveLog())
	}
}

func TestRulesDoNotMatchWhileJiraIsUnavailable(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.putRules(syncRule("approved", "review_approved", "Done", "all"))
	r.allowTransitions("INP-1")
	r.jira.set(func(f *fakeJira) { f.err = errors.New("down") })
	r.sync()
	if len(r.jira.transitionLog()) != 0 || len(r.fired()) != 0 {
		t.Fatal("a rule matched without Jira data")
	}
}

func TestOnlyFourMovesRunAtOnce(t *testing.T) {
	var prs []github.PR
	var tickets []string
	for i := range 9 {
		k := "INP-" + string(rune('1'+i))
		tickets = append(tickets, k)
		prs = append(prs, pr("PR_"+k, i+1, "acme/web", "feat/"+k, "main", "t"))
	}
	r := newRig(t, prs...)
	for _, k := range tickets {
		r.jira.issues[k] = jira.Issue{Key: k, Status: "To Do", StatusCategory: "new", Project: "INP", IssueType: "Story"}
	}
	r.putRules(syncRule("approved", "review_approved", "Done", "all"))
	r.allowTransitions(tickets...)
	r.sync()
	if len(r.jira.moveLog()) != 9 {
		t.Fatalf("%v", r.jira.moveLog())
	}
	if peak := r.jira.peak(); peak < 2 || peak > 4 {
		t.Fatalf("moves ran %d at a time, want at most 4 and more than 1", peak)
	}
}

func TestRuleChangeReevaluatesFromCachedDataWithOneTransaction(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	r := newRig(t, a)
	r.allowTransitions("INP-1")
	r.sync()
	openCalls, _ := r.gh.calls()
	r.putRules(syncRule("approved", "review_approved", "Done", "all"))
	if err := r.p.Reevaluate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after, _ := r.gh.calls(); after != openCalls {
		t.Fatal("re-evaluating asked GitHub")
	}
	if len(r.jira.moveLog()) != 1 {
		t.Fatalf("%v", r.jira.moveLog())
	}
	r.store.mu.Lock()
	writes := r.store.syncWrites
	r.store.mu.Unlock()
	if writes != 1 {
		t.Fatalf("snapshot and firing are one transaction, got %d", writes)
	}
}

func TestPreviewAndBaseline(t *testing.T) {
	fe := pr("PR_fe", 34, "acme/fe", "feat/INP-1-fe", "main", "fe")
	be := pr("PR_be", 12, "acme/be", "feat/INP-1-be", "main", "be")
	solo := pr("PR_solo", 3, "acme/web", "feat/INP-2", "main", "solo")
	fe.ReviewDecision, be.ReviewDecision, solo.ReviewDecision = "APPROVED", "REVIEW_REQUIRED", "APPROVED"
	r := newRig(t, fe, be, solo)
	r.sync()
	rule := rules.Rule{ID: "x", Name: "x", Enabled: true, Trigger: rules.ReviewApproved, Target: "Done", Mode: rules.All}
	pv, err := r.p.Preview(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.Moves) != 1 || pv.Moves[0].Ticket != "INP-2" || pv.Moves[0].Status != "To Do" || len(pv.Waiting) != 1 || pv.Waiting[0].Ticket != "INP-1" {
		t.Fatalf("%+v", pv)
	}
	if len(r.jira.moveLog()) != 0 || len(r.jira.transitionLog()) != 0 {
		t.Fatal("previewing called Jira")
	}
	base, err := r.p.Baseline(context.Background(), []rules.Rule{rule})
	if err != nil || len(base) != 1 || base[0].Ticket != "INP-2" || base[0].Rule != "x" {
		t.Fatalf("%+v %v", base, err)
	}
}

func TestMergedFiringsCoverTheJustMergedPR(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	b := pr("PR_b", 2, "acme/web", "feat/INP-2", "main", "b")
	c := pr("PR_c", 3, "acme/web", "feat/INP-2-c", "main", "c")
	r := newRig(t, a, b, c)
	r.putRules(syncRule("done", "merged", "Done", "all"), syncRule("approved", "review_approved", "Done", "all"))
	r.sync()
	got, err := r.p.MergedFirings(context.Background(), "PR_a", []string{"INP-1", "INP-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Rule != "done" || got[0].Ticket != "INP-1" || got[0].Fingerprint != "PR_a=PR_a" {
		t.Fatalf("INP-2 still has an open PR and the review rule is not a merged rule: %+v", got)
	}
}

func TestReadyForReviewFingerprintStaysWhenTheHeadMoves(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/INP-1", "main", "a")
	a.Draft = true
	r := newRig(t, a)
	r.putRules(syncRule("ready", "ready_for_review", "Done", "all"))
	r.allowTransitions("INP-1")
	r.sync()
	if len(r.jira.moveLog()) != 0 {
		t.Fatal("a draft is not ready")
	}
	a.Draft = false
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = []github.PR{a} })
	r.clock.Advance(time.Minute)
	r.sync()
	first := r.fired()["ready|INP-1"]
	if len(r.jira.moveLog()) != 1 || first != "PR_a=sha-PR_a" {
		t.Fatalf("%v %q", r.jira.moveLog(), first)
	}
	a.HeadSHA = "sha-new"
	r.gh.set(func(f *fakeGitHub) { f.result.PRs = []github.PR{a} })
	r.jira.set(func(f *fakeJira) { i := f.issues["INP-1"]; i.Status = "To Do"; f.issues["INP-1"] = i })
	r.clock.Advance(10 * time.Minute)
	r.sync()
	if r.fired()["ready|INP-1"] != first || len(r.jira.moveLog()) != 1 {
		t.Fatal("a push after the PR became ready moved the ticket again")
	}
}

func TestSnapshotCarriesTheMergePlanOfEachTicket(t *testing.T) {
	fe := pr("PR_fe", 34, "acme/fe", "feat/INP-1-fe", "main", "fe")
	be := pr("PR_be", 12, "acme/be", "feat/INP-1-be", "main", "be")
	solo := pr("PR_solo", 3, "acme/web", "feat/INP-2", "main", "solo")
	r := newRig(t, fe, be, solo)
	r.putRules(syncRule("done", "merged", "Done", "all"))
	r.sync()
	plans := map[string]string{}
	for _, p := range r.snapshot().Prs {
		if plan := p.Tickets[0].MergePlan; plan != nil {
			plans[p.Id] = plan.Target
			if p.Id == "PR_fe" && (plan.Rule != "done" || plan.Total != 2 || len(plan.WaitingOn) != 1 || plan.WaitingOn[0].Number != 12) {
				t.Fatalf("%+v", plan)
			}
			if p.Id == "PR_solo" && len(plan.WaitingOn) != 0 {
				t.Fatalf("%+v", plan)
			}
		}
	}
	if len(plans) != 3 {
		t.Fatalf("plans %v", plans)
	}
}

func TestNoReviewYetAndWaitingApprovalsUseTheRepoApprovalsSetting(t *testing.T) {
	half := pr("PR_half", 1, "acme/web", "feat/INP-1-half", "main", "half")
	half.ReviewDecision, half.Reviews = "REVIEW_REQUIRED", []github.Review{approved("1", "alice")}
	fresh := pr("PR_fresh", 2, "acme/web", "feat/INP-2-fresh", "main", "fresh")
	fresh.ReviewDecision = ""
	draft := pr("PR_draft", 3, "acme/web", "feat/INP-3-draft", "main", "draft")
	draft.ReviewDecision, draft.Draft = "", true
	r := newRig(t, half, fresh, draft)
	r.sync()
	ctx := context.Background()
	preview := func(tr rules.Trigger) []string {
		pv, err := r.p.Preview(ctx, rules.Rule{ID: "x", Enabled: true, Trigger: tr, Target: "Done", Mode: rules.All})
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for _, m := range pv.Moves {
			keys = append(keys, m.Ticket)
		}
		return keys
	}
	if got := preview(rules.NoReviewYet); !slices.Equal(got, []string{"INP-2"}) {
		t.Fatalf("only the ready pull request nobody reviewed: %v", got)
	}
	if got := preview(rules.WaitingApprovals); len(got) != 0 {
		t.Fatalf("a repo that requires nothing never waits: %v", got)
	}
	two := 2
	if err := r.store.PutRepo(ctx, "R-acme/web", "squash", nil, true, &two); err != nil {
		t.Fatal(err)
	}
	if got := preview(rules.WaitingApprovals); !slices.Equal(got, []string{"INP-1"}) {
		t.Fatalf("1 of the 2 approvals the repo requires: %v", got)
	}
	r.putRules(syncRule("waiting", "waiting_approvals", "Done", "all"))
	r.allowTransitions("INP-1")
	r.sync()
	if got := r.jira.moveLog(); !slices.Equal(got, []string{"INP-1:31"}) {
		t.Fatalf("the engine moves the ticket with the same count: %v", got)
	}
}

// putMultiPr sets the global "Multi-PR tickets move when" setting.
func (r *rig) putMultiPr(mode string) {
	r.t.Helper()
	set, err := r.store.GetSettings(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	set.MultiPrTickets = mode
	if err := r.store.PutSettings(context.Background(), set); err != nil {
		r.t.Fatal(err)
	}
}

func TestRuleInModeDefaultFollowsTheGlobalMultiPrSetting(t *testing.T) {
	fe := pr("PR_fe", 34, "acme/fe", "feat/INP-1-fe", "main", "fe")
	be := pr("PR_be", 12, "acme/be", "feat/INP-1-be", "main", "be")
	fe.ReviewDecision, be.ReviewDecision = "APPROVED", "REVIEW_REQUIRED"

	t.Run("all waits for the last PR", func(t *testing.T) {
		r := newRig(t, fe, be)
		r.putRules(syncRule("follow", "review_approved", "Done", "default"))
		r.putMultiPr("all")
		r.allowTransitions("INP-1")
		r.sync()
		if got := r.jira.moveLog(); len(got) != 0 {
			t.Fatalf("moved with one PR waiting: %v", got)
		}
		var hint bool
		for _, p := range r.snapshot().Prs {
			hint = hint || p.Tickets[0].Waiting != nil
		}
		if !hint {
			t.Fatal("the ticket cell has no waiting hint")
		}
	})

	t.Run("any moves on the first PR", func(t *testing.T) {
		r := newRig(t, fe, be)
		r.putRules(syncRule("follow", "review_approved", "Done", "default"))
		r.putMultiPr("any")
		r.allowTransitions("INP-1")
		r.sync()
		if got := r.jira.moveLog(); !slices.Equal(got, []string{"INP-1:31"}) {
			t.Fatalf("moves %v", got)
		}
		for _, p := range r.snapshot().Prs {
			if p.Tickets[0].Waiting != nil {
				t.Fatal("a rule that moves on the first PR has nothing to wait for")
			}
		}
	})

	t.Run("a fresh install follows all", func(t *testing.T) {
		r := newRig(t, fe, be)
		r.putRules(syncRule("follow", "review_approved", "Done", "default"))
		r.allowTransitions("INP-1")
		r.sync()
		if len(r.jira.moveLog()) != 0 {
			t.Fatalf("%v", r.jira.moveLog())
		}
	})

	t.Run("explicit modes ignore the global setting", func(t *testing.T) {
		r := newRig(t, fe, be)
		r.putRules(syncRule("all", "review_approved", "Done", "all"))
		r.putMultiPr("any")
		r.allowTransitions("INP-1")
		r.sync()
		if len(r.jira.moveLog()) != 0 {
			t.Fatalf("%v", r.jira.moveLog())
		}
	})

	t.Run("a changed setting applies to the next evaluation from cached data", func(t *testing.T) {
		r := newRig(t, fe, be)
		r.putRules(syncRule("follow", "review_approved", "Done", "default"))
		r.allowTransitions("INP-1")
		r.sync()
		r.putMultiPr("any")
		if err := r.p.Reevaluate(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := r.jira.moveLog(); !slices.Equal(got, []string{"INP-1:31"}) {
			t.Fatalf("moves %v", got)
		}
	})
}

func TestMergePlanPreviewAndBaselineUseTheGlobalMultiPrSetting(t *testing.T) {
	fe := pr("PR_fe", 34, "acme/fe", "feat/INP-1-fe", "main", "fe")
	be := pr("PR_be", 12, "acme/be", "feat/INP-1-be", "main", "be")
	fe.ReviewDecision, be.ReviewDecision = "APPROVED", "REVIEW_REQUIRED"
	r := newRig(t, fe, be)
	r.putRules(syncRule("done", "merged", "Done", "default"))
	waitingOn := func() int {
		for _, p := range r.snapshot().Prs {
			if p.Id == "PR_fe" && p.Tickets[0].MergePlan != nil {
				return len(p.Tickets[0].MergePlan.WaitingOn)
			}
		}
		t.Fatal("no merge plan")
		return -1
	}
	r.putMultiPr("all")
	r.sync()
	if n := waitingOn(); n != 1 {
		t.Fatalf("global all: merging PR_fe waits on %d", n)
	}
	r.putMultiPr("any")
	r.clock.Advance(time.Minute)
	r.sync()
	if n := waitingOn(); n != 0 {
		t.Fatalf("global any: merging PR_fe waits on %d", n)
	}

	follow := rules.Rule{ID: "x", Name: "x", Enabled: true, Trigger: rules.ReviewApproved, Target: "Done", Mode: rules.Default}
	pv, err := r.p.Preview(context.Background(), follow)
	if err != nil || len(pv.Moves) != 1 || pv.Moves[0].Ticket != "INP-1" || len(pv.Waiting) != 0 {
		t.Fatalf("global any: %+v %v", pv, err)
	}
	base, err := r.p.Baseline(context.Background(), []rules.Rule{follow})
	if err != nil || len(base) != 1 || base[0].Ticket != "INP-1" {
		t.Fatalf("global any: %+v %v", base, err)
	}
	r.putMultiPr("all")
	pv, err = r.p.Preview(context.Background(), follow)
	if err != nil || len(pv.Moves) != 0 || len(pv.Waiting) != 1 {
		t.Fatalf("global all: %+v %v", pv, err)
	}
	if base, err = r.p.Baseline(context.Background(), []rules.Rule{follow}); err != nil || len(base) != 0 {
		t.Fatalf("global all: %+v %v", base, err)
	}
}
