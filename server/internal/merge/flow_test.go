package merge_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func stepOf(t *testing.T, res merge.Result, kind, ticket string) merge.Step {
	t.Helper()
	for _, s := range res.Steps {
		if s.Kind == kind && s.Ticket == ticket {
			return s
		}
	}
	t.Fatalf("no %s step for %q in %+v", kind, ticket, res.Steps)
	return merge.Step{}
}

func TestMergeMovesEveryTicketAndRecordsOnceInOneTransaction(t *testing.T) {
	r := newRig(t, "INP-1", "INP-2", "INP-3")
	req := request(done("INP-1"), merge.Ticket{Key: "INP-2", TransitionID: "21"}, merge.Ticket{Key: "INP-3"})
	req.DeleteBranch = true
	res, err := r.svc.Merge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.SHA != "mergesha" || res.Method != merge.Squash || len(res.Steps) != 5 {
		t.Fatalf("%+v", res)
	}
	moved := stepOf(t, res, merge.StepTransition, "INP-1")
	if stepOf(t, res, merge.StepMerge, "").State != merge.StateOK ||
		stepOf(t, res, merge.StepDeleteBranch, "").State != merge.StateOK ||
		moved.State != merge.StateOK || moved.ToStatus != "Done" {
		t.Fatalf("%+v", res.Steps)
	}
	if s := stepOf(t, res, merge.StepTransition, "INP-3"); s.State != merge.StateSkipped {
		t.Fatalf("a ticket left alone is skipped: %+v", s)
	}
	if got := r.gh.merges; len(got) != 1 || got[0] != (mergeCall{"acme/web", "squash", "sha1"}) {
		t.Fatalf("merge calls %+v", got)
	}
	if r.gh.pullCalls != 1 || len(r.gh.deletes) != 1 || r.gh.deletes[0] != "acme/web:feat/x" {
		t.Fatalf("github calls: pull %d deletes %v", r.gh.pullCalls, r.gh.deletes)
	}
	if r.jira.listCalls != 2 || r.jira.moveCount() != 2 {
		t.Fatalf("jira calls: lists %d moves %d", r.jira.listCalls, r.jira.moveCount())
	}
	if got := r.jira.movedWith["INP-1"]["resolution"]; got == nil {
		t.Fatalf("fields sent: %+v", r.jira.movedWith)
	}
	if n := r.store.records.Load(); n != 1 {
		t.Fatalf("RecordMerge ran %d times", n)
	}
	if r.refreshes.Load() != 1 || len(r.events.list) != 1 || r.events.list[0] != "activity" {
		t.Fatalf("refreshes %d events %v", r.refreshes.Load(), r.events.list)
	}

	acts, err := r.store.Activity(context.Background(), 0, 50)
	if err != nil || len(acts) != 4 {
		t.Fatalf("activity %+v %v", acts, err)
	}
	kinds := map[string]int{}
	for _, a := range acts {
		kinds[a.Kind]++
		if a.TriggeredBy != "manual" || a.Result != "ok" || a.PRID != prID {
			t.Fatalf("%+v", a)
		}
	}
	if kinds["merge"] != 1 || kinds["branch_delete"] != 1 || kinds["transition"] != 2 {
		t.Fatalf("kinds %v", kinds)
	}
	if _, _, ok, _ := r.store.Idempotent(context.Background(), req.IdempotencyKey, r.clock.Now()); !ok {
		t.Fatal("the result is not kept for the idempotency key")
	}
}

func TestMergeRecordsTheFingerprintsTheSyncEngineReports(t *testing.T) {
	r := newRig(t, "INP-1", "INP-2")
	r.snapshot.firings = []store.Firing{
		{Rule: "merged-on", Ticket: "INP-1", Fingerprint: "PR_1=PR_1"},
		{Rule: "merged-on", Ticket: "INP-2", Fingerprint: "PR_1=PR_1"},
		{Rule: "deleted", Ticket: "INP-1", Fingerprint: "PR_1=PR_1"},
	}
	db, err := sql.Open("sqlite", "file:"+r.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, row := range []string{
		`('merged-on', 0, 'a', 1, '{"type":"merged"}', 'Done')`,
		`('merged-off', 1, 'b', 0, '{"type":"merged"}', 'Done')`,
		`('approved', 2, 'c', 1, '{"type":"review_approved"}', 'Done')`,
	} {
		if _, err := db.Exec(`INSERT INTO sync_rules (id, position, name, enabled, trigger, target_status) VALUES ` + row); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.svc.Merge(context.Background(), request(done("INP-1"), merge.Ticket{Key: "INP-2"})); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT rule_id, ticket_key, fingerprint FROM rule_firings ORDER BY ticket_key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var rule, ticket, fp string
		_ = rows.Scan(&rule, &ticket, &fp)
		got = append(got, rule+"|"+ticket+"|"+fp)
	}
	want := []string{"merged-on|INP-1|PR_1=PR_1", "merged-on|INP-2|PR_1=PR_1"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("firings %v", got)
	}
	if r.snapshot.askedPR != prID || len(r.snapshot.askedTK) != 2 {
		t.Fatalf("asked %q %v", r.snapshot.askedPR, r.snapshot.askedTK)
	}
}

func TestMergeWithoutTickets(t *testing.T) {
	r := newRig(t)
	res, err := r.svc.Merge(context.Background(), request())
	if err != nil || len(res.Steps) != 1 || r.jira.listCalls != 0 {
		t.Fatalf("%+v %v lists %d", res, err, r.jira.listCalls)
	}
}

func TestMergeRefusals(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(r *rig)
		code   string
	}{
		{"closed meanwhile", func(r *rig) { r.gh.pr.State = "MERGED" }, merge.CodeNotOpen},
		{"draft", func(r *rig) { r.gh.pr.Draft = true }, merge.CodeDraft},
		{"head moved", func(r *rig) { r.gh.pr.HeadSHA = "sha2" }, merge.CodeHeadMoved},
		{"conflicting", func(r *rig) { r.gh.pr.Mergeable = "CONFLICTING" }, merge.CodeNotMergeable},
		{"mergeable unknown", func(r *rig) { r.gh.pr.Mergeable = "UNKNOWN" }, merge.CodeNotMergeable},
		{"blocked", func(r *rig) { r.gh.pr.MergeStateStatus = "BLOCKED" }, merge.CodeNotMergeable},
		{"unstable", func(r *rig) { r.gh.pr.MergeStateStatus = "UNSTABLE" }, merge.CodeNotMergeable},
		{"behind", func(r *rig) { r.gh.pr.MergeStateStatus = "BEHIND" }, merge.CodeNotMergeable},
		{"review required", func(r *rig) { r.gh.pr.ReviewDecision = "REVIEW_REQUIRED" }, merge.CodeNotMergeable},
		{"changes requested", func(r *rig) { r.gh.pr.ReviewDecision = "CHANGES_REQUESTED" }, merge.CodeNotMergeable},
		{"too few approvals", func(r *rig) { r.snapshot.linked[prID] = withApprovals(r.snapshot.linked[prID], 2) }, merge.CodeNotMergeable},
		{"no approval at all", func(r *rig) { r.gh.pr.Reviews = nil }, merge.CodeNotMergeable},
		{"changes requested by a reviewer", func(r *rig) {
			r.gh.pr.Reviews = []github.Review{{ID: "RV2", State: "CHANGES_REQUESTED", Author: "bob"}, {ID: "RV1", State: "APPROVED", Author: "alice"}}
			r.gh.pr.ReviewDecision = "CHANGES_REQUESTED"
		}, merge.CodeNotMergeable},
		{"stack base still open", func(r *rig) {
			r.gh.pr.BaseRef = "feat/base"
			r.snapshot.refs["R1"] = append(r.snapshot.refs["R1"], merge.Ref{ID: "PR_0", Number: 6, Head: "feat/base", Base: "main"})
		}, merge.CodeStackBaseOpen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, "INP-1")
			tt.mutate(r)
			_, err := r.svc.Merge(context.Background(), request(done("INP-1")))
			e := wantError(t, err, tt.code)
			if e.Fresh == nil || e.Fresh.HeadSHA != r.gh.pr.HeadSHA {
				t.Fatalf("fresh state missing: %+v", e)
			}
			if r.gh.mergeCount() != 0 || r.jira.moveCount() != 0 || r.jira.listCalls != 0 {
				t.Fatal("something was done before the checks passed")
			}
			if acts, _ := r.store.Activity(context.Background(), 0, 10); len(acts) != 0 || r.store.records.Load() != 0 {
				t.Fatal("a refused merge wrote to the store")
			}
			if r.refreshes.Load() != 1 {
				t.Fatalf("a refused merge refreshes the snapshot: %d", r.refreshes.Load())
			}
		})
	}
}

func withApprovals(l merge.Linked, n int) merge.Linked {
	l.ApprovalsRequired = n
	return l
}

func TestMergeNamesTheMissingApprovals(t *testing.T) {
	r := newRig(t)
	r.snapshot.linked[prID] = withApprovals(r.snapshot.linked[prID], 2)
	_, err := r.svc.Merge(context.Background(), request())
	e := wantError(t, err, merge.CodeNotMergeable)
	if e.Detail != "Needs 2 approvals, has 1" {
		t.Fatalf("%q", e.Detail)
	}
	if r.gh.mergeCount() != 0 {
		t.Fatal("merged without enough approvals")
	}
}

func TestMergeWithEnoughApprovalsGoesThrough(t *testing.T) {
	r := newRig(t)
	r.snapshot.linked[prID] = withApprovals(r.snapshot.linked[prID], 2)
	r.gh.pr.Reviews = append(r.gh.pr.Reviews, github.Review{ID: "RV2", State: "APPROVED", Author: "bob"})
	if _, err := r.svc.Merge(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
}

func TestMergeAcceptsHasHooks(t *testing.T) {
	r := newRig(t)
	r.gh.pr.MergeStateStatus = "HAS_HOOKS"
	r.gh.pr.ReviewDecision = ""
	if _, err := r.svc.Merge(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
}

func TestStackBaseThatIsNotTheParentDoesNotBlock(t *testing.T) {
	r := newRig(t)
	r.snapshot.refs["R1"] = append(r.snapshot.refs["R1"], merge.Ref{ID: "PR_0", Number: 6, Head: "feat/other", Base: "main"})
	if _, err := r.svc.Merge(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
}

func TestMergeRequestProblems(t *testing.T) {
	t.Run("unknown pull request", func(t *testing.T) {
		r := newRig(t)
		req := request()
		req.PRID = "PR_404"
		wantError(t, merge2(r, req), merge.CodeNotFound)
	})
	t.Run("ticket not linked", func(t *testing.T) {
		r := newRig(t, "INP-1")
		wantError(t, merge2(r, request(done("INP-9"))), merge.CodeTicketNotLinked)
	})
	t.Run("method not allowed", func(t *testing.T) {
		r := newRig(t)
		r.gh.pr.Repo.SquashAllowed = false
		e := wantError(t, merge2(r, request()), merge.CodeMethodNotAllowed)
		if e.Fresh != nil || r.gh.mergeCount() != 0 {
			t.Fatal("nothing should have happened")
		}
	})
	t.Run("github not connected", func(t *testing.T) {
		r := newRig(t)
		r.accounts.tokenErr = auth.ErrNotConnected
		wantError(t, merge2(r, request()), merge.CodeNotConnected)
	})
	t.Run("token store failure is not a refusal", func(t *testing.T) {
		r := newRig(t)
		r.accounts.tokenErr = errors.New("keychain locked")
		var e *merge.Error
		if err := merge2(r, request()); err == nil || errors.As(err, &e) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("github rejects the token", func(t *testing.T) {
		r := newRig(t)
		r.gh.prErr = github.ErrBadCredentials
		wantError(t, merge2(r, request()), merge.CodeNotConnected)
	})
	t.Run("github does not know the pull request", func(t *testing.T) {
		r := newRig(t)
		r.gh.prErr = github.ErrPRNotFound
		wantError(t, merge2(r, request()), merge.CodeNotFound)
	})
	t.Run("github fails", func(t *testing.T) {
		r := newRig(t)
		r.gh.prErr = &github.RateLimitedError{}
		var rl *github.RateLimitedError
		if err := merge2(r, request()); !errors.As(err, &rl) {
			t.Fatalf("%v", err)
		}
	})
}

func merge2(r *rig, req merge.Request) error {
	_, err := r.svc.Merge(context.Background(), req)
	return err
}

func TestTransitionValidation(t *testing.T) {
	user := jira.Transition{ID: "41", Name: "Assign", ToStatus: "Review", Fields: []jira.Field{
		{ID: "reviewer", Name: "Reviewer", Required: true, Type: "user"},
		{ID: "note", Name: "Note", Type: "string"},
		{ID: "points", Name: "Points", Required: false, Type: "number"},
	}}
	tests := []struct {
		name    string
		ticket  merge.Ticket
		prepare func(r *rig)
		code    string
	}{
		{"transition not in the list", merge.Ticket{Key: "INP-1", TransitionID: "99"}, nil, merge.CodeTransitionUnavailable},
		{"required field missing", merge.Ticket{Key: "INP-1", TransitionID: "31"}, nil, merge.CodeMissingField},
		{"required field blank", merge.Ticket{Key: "INP-1", TransitionID: "31", Fields: map[string]string{"resolution": "  "}}, nil, merge.CodeMissingField},
		{"unknown field id", merge.Ticket{Key: "INP-1", TransitionID: "31", Fields: map[string]string{"resolution": "10000", "labels": "x"}}, nil, merge.CodeInvalidField},
		{"option not allowed", merge.Ticket{Key: "INP-1", TransitionID: "31", Fields: map[string]string{"resolution": "777"}}, nil, merge.CodeInvalidField},
		{"user id with bad characters", merge.Ticket{Key: "INP-1", TransitionID: "41", Fields: map[string]string{"reviewer": "bob smith"}}, nil, merge.CodeInvalidField},
		{"unsupported field type", merge.Ticket{Key: "INP-1", TransitionID: "41", Fields: map[string]string{"reviewer": "abc", "points": "3"}}, nil, merge.CodeInvalidField},
		{"ticket gone from jira", merge.Ticket{Key: "INP-1", TransitionID: "31"}, func(r *rig) { r.jira.listErr["INP-1"] = jira.ErrNotFound }, merge.CodeTransitionUnavailable},
		{"jira rejects the token", merge.Ticket{Key: "INP-1", TransitionID: "31"}, func(r *rig) { r.jira.listErr["INP-1"] = jira.ErrUnauthorized }, merge.CodeNotConnected},
		{"jira not connected", merge.Ticket{Key: "INP-1", TransitionID: "31"}, func(r *rig) { r.accounts.siteErr = auth.ErrNotConnected }, merge.CodeNotConnected},
		{"no jira site", merge.Ticket{Key: "INP-1", TransitionID: "31"}, func(r *rig) { r.accounts.siteErr = jira.ErrNoSite }, merge.CodeNotConnected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, "INP-1")
			r.jira.transitions["INP-1"] = append(r.jira.transitions["INP-1"], user)
			if tt.prepare != nil {
				tt.prepare(r)
			}
			wantError(t, merge2(r, request(tt.ticket)), tt.code)
			if r.gh.mergeCount() != 0 || r.jira.moveCount() != 0 {
				t.Fatal("a request that fails validation must not merge or move anything")
			}
		})
	}
}

func TestTransitionFieldsAreShapedForJira(t *testing.T) {
	r := newRig(t, "INP-1")
	r.jira.transitions["INP-1"] = []jira.Transition{{ID: "41", ToStatus: "Review", Fields: []jira.Field{
		{ID: "reviewer", Name: "Reviewer", Required: true, Type: "user"},
		{ID: "note", Name: "Note", Type: "string"},
		{ID: "empty", Name: "Empty", Type: "string"},
		{ID: "resolution", Name: "Resolution", Type: "resolution", AllowedValues: []jira.Option{{ID: "1", Name: "Fixed"}}},
	}}}
	_, err := r.svc.Merge(context.Background(), request(merge.Ticket{Key: "INP-1", TransitionID: "41", Fields: map[string]string{
		"reviewer": "5b10a2844c20165700ede21g", "note": " ship it ", "resolution": "1",
	}}))
	if err != nil {
		t.Fatal(err)
	}
	got := r.jira.movedWith["INP-1"]
	if len(got) != 3 || got["note"] != "ship it" ||
		got["reviewer"].(map[string]string)["accountId"] != "5b10a2844c20165700ede21g" || got["resolution"].(map[string]string)["id"] != "1" {
		t.Fatalf("%+v", got)
	}
}

func TestGitHubMergeFailuresAreRefusalsAndNothingElseRuns(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{github.ErrHeadMoved, merge.CodeHeadMoved},
		{github.ErrNotMergeable, merge.CodeNotMergeable},
		{github.ErrPRNotFound, merge.CodeNotFound},
		{github.ErrBadCredentials, merge.CodeNotConnected},
	}
	for _, tt := range tests {
		r := newRig(t, "INP-1")
		r.gh.mergeErr = tt.err
		e := wantError(t, merge2(r, request(done("INP-1"))), tt.code)
		if (tt.code == merge.CodeHeadMoved || tt.code == merge.CodeNotMergeable) && e.Fresh == nil {
			t.Errorf("%s: no fresh state", tt.code)
		}
		if r.jira.moveCount() != 0 || len(r.gh.deletes) != 0 || r.store.records.Load() != 0 {
			t.Errorf("%s: steps ran after a failed merge", tt.code)
		}
	}
	r := newRig(t)
	r.gh.mergeErr = &github.APIError{Status: 422, Message: "invalid"}
	var api *github.APIError
	if err := merge2(r, request()); !errors.As(err, &api) {
		t.Fatalf("%v", err)
	}
}

func TestMergePermissionErrorStopsTheFlow(t *testing.T) {
	r := newRig(t, "INP-1")
	r.gh.mergeErr = &github.PermissionError{Message: "Resource not accessible", Accepted: []string{"contents=write"}}
	req := request(done("INP-1"))
	req.DeleteBranch = true
	var perm *github.PermissionError
	if err := merge2(r, req); !errors.As(err, &perm) {
		t.Fatalf("%v", err)
	}
	if r.jira.moveCount() != 0 || len(r.gh.deletes) != 0 || r.store.records.Load() != 0 {
		t.Error("steps ran after a refused merge")
	}
}

func TestDeleteBranchPermissionErrorIsAStepFailure(t *testing.T) {
	r := newRig(t, "INP-1")
	r.gh.deleteErr = &github.PermissionError{Message: "Resource not accessible", Accepted: []string{"contents=write"}}
	req := request(done("INP-1"))
	req.DeleteBranch = true
	res, err := r.svc.Merge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	s := stepOf(t, res, merge.StepDeleteBranch, "")
	if s.State != merge.StateFailed || s.Message != "The branch could not be deleted. The token needs the GitHub permission contents: write" {
		t.Fatalf("%+v", s)
	}
	if stepOf(t, res, merge.StepMerge, "").State != merge.StateOK || stepOf(t, res, merge.StepTransition, "INP-1").State != merge.StateOK {
		t.Fatal("the merge and the move stay")
	}
}

func TestPartialTransitionFailureKeepsTheMergeAndQueuesARetry(t *testing.T) {
	r := newRig(t, "INP-1", "INP-2", "INP-3")
	r.jira.moveErr["INP-2"] = &jira.StatusError{Status: 400, Detail: "Resolution is required"}
	r.jira.moveErr["INP-3"] = errors.New("connection reset")
	res, err := r.svc.Merge(context.Background(), request(done("INP-1"), done("INP-2"), done("INP-3")))
	if err != nil {
		t.Fatal(err)
	}
	if stepOf(t, res, merge.StepTransition, "INP-1").State != merge.StateOK {
		t.Fatal("the other tickets still move")
	}
	s2, s3 := stepOf(t, res, merge.StepTransition, "INP-2"), stepOf(t, res, merge.StepTransition, "INP-3")
	if s2.State != merge.StateFailed || s2.Message != "Jira rejected the move: Resolution is required" || s2.RetryID == 0 {
		t.Fatalf("%+v", s2)
	}
	if s3.State != merge.StateFailed || s3.Message != "Jira could not be reached" || s3.RetryID == 0 || s3.RetryID == s2.RetryID {
		t.Fatalf("%+v", s3)
	}
	retries, err := r.store.Retries(context.Background())
	if err != nil || len(retries) != 2 {
		t.Fatalf("%+v %v", retries, err)
	}
	for _, rt := range retries {
		if rt.PRID != prID || rt.TransitionID != "31" || rt.ToStatus != "Done" || rt.Fields["resolution"] != "10000" || rt.LastError == "" {
			t.Fatalf("%+v", rt)
		}
	}
	acts, _ := r.store.Activity(context.Background(), 0, 50)
	failed := 0
	for _, a := range acts {
		if a.Result == "failed" {
			failed++
			if a.Error == "" {
				t.Fatal("a failed row has its error")
			}
		}
	}
	if failed != 2 || len(acts) != 4 {
		t.Fatalf("activity %+v", acts)
	}
}

func TestRetryCompletesAFailedTransition(t *testing.T) {
	r := newRig(t, "INP-1")
	r.jira.moveErr["INP-1"] = &jira.StatusError{Status: 503}
	res, err := r.svc.Merge(context.Background(), request(done("INP-1")))
	if err != nil {
		t.Fatal(err)
	}
	id := stepOf(t, res, merge.StepTransition, "INP-1").RetryID
	if m := stepOf(t, res, merge.StepTransition, "INP-1").Message; m != "Jira answered 503" {
		t.Fatalf("%q", m)
	}

	step, err := r.svc.Retry(context.Background(), id)
	if err != nil || step.State != merge.StateFailed || step.RetryID != id {
		t.Fatalf("a failed retry stays pending: %+v %v", step, err)
	}
	if rows, _ := r.store.Retries(context.Background()); len(rows) != 1 {
		t.Fatalf("%+v", rows)
	}

	delete(r.jira.moveErr, "INP-1")
	step, err = r.svc.Retry(context.Background(), id)
	if err != nil || step.State != merge.StateOK || step.ToStatus != "Done" || step.RetryID != 0 {
		t.Fatalf("%+v %v", step, err)
	}
	if rows, _ := r.store.Retries(context.Background()); len(rows) != 0 {
		t.Fatalf("the retry is removed on success: %+v", rows)
	}
	acts, _ := r.store.Activity(context.Background(), 0, 50)
	if acts[0].Kind != "transition" || acts[0].TriggeredBy != "retry" || acts[0].Result != "ok" || acts[0].Summary != "INP-1 moved to Done (retry)" {
		t.Fatalf("%+v", acts[0])
	}
	if r.refreshes.Load() < 3 {
		t.Fatalf("each retry refreshes: %d", r.refreshes.Load())
	}
	wantError(t, retryErr(r, id), merge.CodeNotFound)
}

func retryErr(r *rig, id int64) error {
	_, err := r.svc.Retry(context.Background(), id)
	return err
}

func TestRetryRevalidatesTheTransition(t *testing.T) {
	setup := func(t *testing.T) (*rig, int64) {
		r := newRig(t, "INP-1")
		r.jira.moveErr["INP-1"] = errors.New("down")
		res, err := r.svc.Merge(context.Background(), request(done("INP-1")))
		if err != nil {
			t.Fatal(err)
		}
		delete(r.jira.moveErr, "INP-1")
		return r, stepOf(t, res, merge.StepTransition, "INP-1").RetryID
	}
	t.Run("transition no longer offered", func(t *testing.T) {
		r, id := setup(t)
		r.jira.transitions["INP-1"] = []jira.Transition{progress()}
		wantError(t, retryErr(r, id), merge.CodeTransitionUnavailable)
		if rows, _ := r.store.Retries(context.Background()); len(rows) != 1 {
			t.Fatal("an unavailable transition keeps its retry")
		}
	})
	t.Run("required field no longer satisfied", func(t *testing.T) {
		r, id := setup(t)
		tr := doneTransition()
		tr.Fields = append(tr.Fields, jira.Field{ID: "comment", Name: "Comment", Required: true, Type: "string"})
		r.jira.transitions["INP-1"] = []jira.Transition{tr}
		wantError(t, retryErr(r, id), merge.CodeMissingField)
	})
	t.Run("ticket gone", func(t *testing.T) {
		r, id := setup(t)
		r.jira.listErr["INP-1"] = jira.ErrNotFound
		wantError(t, retryErr(r, id), merge.CodeTransitionUnavailable)
	})
	t.Run("jira not connected", func(t *testing.T) {
		r, id := setup(t)
		r.accounts.siteErr = auth.ErrNeedsReconnect
		wantError(t, retryErr(r, id), merge.CodeNotConnected)
		r.accounts.siteErr = nil
		r.jira.listErr["INP-1"] = jira.ErrUnauthorized
		wantError(t, retryErr(r, id), merge.CodeNotConnected)
	})
	t.Run("jira failures are not refusals", func(t *testing.T) {
		r, id := setup(t)
		r.accounts.siteErr = errors.New("keychain")
		var e *merge.Error
		if err := retryErr(r, id); err == nil || errors.As(err, &e) {
			t.Fatalf("%v", err)
		}
		r.accounts.siteErr = nil
		r.jira.listErr["INP-1"] = errors.New("down")
		if err := retryErr(r, id); err == nil || errors.As(err, &e) {
			t.Fatalf("%v", err)
		}
	})
}

func TestRepeatedKeyReturnsTheFirstResultWithoutMergingAgain(t *testing.T) {
	r := newRig(t, "INP-1")
	req := request(done("INP-1"))
	first, err := r.svc.Merge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	lists, moves := r.jira.listCalls, r.jira.moveCount()
	second, err := r.svc.Merge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if r.gh.mergeCount() != 1 || r.gh.pullCalls != 1 || r.jira.moveCount() != moves || r.jira.listCalls != lists {
		t.Fatal("a repeated key must not call GitHub or Jira again")
	}
	if first.SHA != second.SHA || len(first.Steps) != len(second.Steps) || first.Steps[1] != second.Steps[1] {
		t.Fatalf("%+v vs %+v", first, second)
	}

	other := req
	other.PRID = "PR_2"
	wantError(t, merge2(r, other), merge.CodeKeyReused)
}

func TestARepeatedKeyAfterTheRetentionRunsAgain(t *testing.T) {
	r := newRig(t)
	req := request()
	if _, err := r.svc.Merge(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	r.clock.Advance(store.IdempotencyTTL + time.Minute)
	if _, err := r.svc.Merge(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if r.gh.mergeCount() != 2 {
		t.Fatalf("merges %d", r.gh.mergeCount())
	}
}

func TestConcurrentCallsWithOneKeyMergeOnce(t *testing.T) {
	r := newRig(t)
	r.gh.mergeGate = make(chan struct{})
	req := request()
	var wg sync.WaitGroup
	results := make([]merge.Result, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := r.svc.Merge(context.Background(), req)
			if err != nil {
				t.Error(err)
			}
			results[i] = res
		}()
	}
	if !within(2*time.Second, func() bool { r.gh.mu.Lock(); defer r.gh.mu.Unlock(); return r.gh.pullCalls >= 1 }) {
		t.Fatal("the merge never started")
	}
	time.Sleep(20 * time.Millisecond)
	close(r.gh.mergeGate)
	wg.Wait()
	if r.gh.mergeCount() != 1 {
		t.Fatalf("merges %d", r.gh.mergeCount())
	}
	for _, res := range results {
		if res.SHA != "mergesha" {
			t.Fatalf("%+v", res)
		}
	}
}

func TestAFollowerStopsWaitingWhenItsContextEnds(t *testing.T) {
	r := newRig(t)
	r.gh.mergeGate = make(chan struct{})
	req := request()
	go func() { _, _ = r.svc.Merge(context.Background(), req) }()
	within(2*time.Second, func() bool { r.gh.mu.Lock(); defer r.gh.mu.Unlock(); return r.gh.pullCalls >= 1 })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.svc.Merge(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	close(r.gh.mergeGate)
}

func TestAtMostFourTransitionsRunAtOnce(t *testing.T) {
	keys := []string{"INP-1", "INP-2", "INP-3", "INP-4", "INP-5", "INP-6", "INP-7", "INP-8", "INP-9", "INP-10"}
	r := newRig(t, keys...)
	r.jira.hold = make(chan struct{})
	tickets := make([]merge.Ticket, len(keys))
	for i, k := range keys {
		tickets[i] = done(k)
	}
	finished := make(chan error, 1)
	go func() { _, err := r.svc.Merge(context.Background(), request(tickets...)); finished <- err }()
	if !within(2*time.Second, func() bool { return r.jira.running.Load() == merge.TransitionParallelism }) {
		t.Fatalf("running %d, want %d", r.jira.running.Load(), merge.TransitionParallelism)
	}
	time.Sleep(30 * time.Millisecond)
	if r.jira.running.Load() > merge.TransitionParallelism {
		t.Fatalf("too many at once: %d", r.jira.running.Load())
	}
	close(r.jira.hold)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if peak := r.jira.peak.Load(); peak != merge.TransitionParallelism || r.jira.moveCount() != len(keys) {
		t.Fatalf("peak %d moves %d", peak, r.jira.moveCount())
	}
}

func TestBranchDeletion(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(r *rig)
		request bool
		state   string
		message string
		deleted int
	}{
		{"not requested", nil, false, "", "", 0},
		{"deleted", nil, true, merge.StateOK, "", 1},
		{"head lives in a fork", func(r *rig) { r.gh.pr.CrossRepo = true }, true, merge.StateSkipped, "Branch kept: it lives in a fork", 0},
		{"another open pull request is based on it", func(r *rig) {
			r.snapshot.refs["R1"] = append(r.snapshot.refs["R1"], merge.Ref{ID: "PR_2", Number: 8, Head: "feat/y", Base: "feat/x"})
		}, true, merge.StateSkipped, "Branch kept: #8 uses it as its base", 0},
		{"the merged pull request itself does not count", func(r *rig) {
			r.snapshot.refs["R1"] = append(r.snapshot.refs["R1"], merge.Ref{ID: prID, Number: 7, Head: "x", Base: "feat/x"})
		}, true, merge.StateOK, "", 1},
		{"github refuses", func(r *rig) { r.gh.deleteErr = &github.APIError{Status: 403} }, true, merge.StateFailed, "The branch could not be deleted", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, "INP-1")
			if tt.mutate != nil {
				tt.mutate(r)
			}
			req := request(done("INP-1"))
			req.DeleteBranch = tt.request
			res, err := r.svc.Merge(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.gh.deletes) != tt.deleted {
				t.Fatalf("deletes %v", r.gh.deletes)
			}
			if !tt.request {
				for _, s := range res.Steps {
					if s.Kind == merge.StepDeleteBranch {
						t.Fatalf("unexpected step %+v", s)
					}
				}
				return
			}
			s := stepOf(t, res, merge.StepDeleteBranch, "")
			if s.State != tt.state || s.Message != tt.message {
				t.Fatalf("%+v", s)
			}
			if stepOf(t, res, merge.StepTransition, "INP-1").State != merge.StateOK {
				t.Fatal("a failed branch delete does not stop the transitions")
			}
		})
	}
}

func TestRecordFailureStillReturnsTheResult(t *testing.T) {
	r := newRig(t)
	r.svc = merge.New(merge.Config{
		GitHub: r.gh, Jira: r.jira, Store: failingStore{r.store}, Snapshot: r.snapshot, Accounts: r.accounts,
	})
	res, err := r.svc.Merge(context.Background(), request())
	if err != nil || res.SHA != "mergesha" {
		t.Fatalf("%+v %v", res, err)
	}
}

type failingStore struct{ *countingStore }

func (failingStore) RecordMerge(context.Context, store.Merged) error { return errors.New("disk full") }

func TestIdempotencyLookupFailure(t *testing.T) {
	r := newRig(t)
	r.svc = merge.New(merge.Config{GitHub: r.gh, Jira: r.jira, Store: brokenLookup{r.store}, Snapshot: r.snapshot, Accounts: r.accounts})
	if err := merge2(r, request()); err == nil {
		t.Fatal("expected an error")
	}
	if r.gh.mergeCount() != 0 {
		t.Fatal("merged without knowing whether the key was used")
	}
}

type brokenLookup struct{ *countingStore }

func (brokenLookup) Idempotent(context.Context, string, time.Time) (string, []byte, bool, error) {
	return "", nil, false, errors.New("locked")
}

func TestMergeFinishesAfterTheCallerGoesAway(t *testing.T) {
	r := newRig(t, "INP-1")
	ctx, cancel := context.WithCancel(context.Background())
	r.gh.mergeGate = make(chan struct{})
	finished := make(chan merge.Result, 1)
	go func() {
		res, _ := r.svc.Merge(ctx, request(done("INP-1")))
		finished <- res
	}()
	within(2*time.Second, func() bool { r.gh.mu.Lock(); defer r.gh.mu.Unlock(); return r.gh.pullCalls >= 1 })
	cancel()
	close(r.gh.mergeGate)
	res := <-finished
	if s := stepOf(t, res, merge.StepTransition, "INP-1"); s.State != merge.StateOK {
		t.Fatalf("transitions run after the merge even when the client left: %+v", s)
	}
}
