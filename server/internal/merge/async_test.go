package merge_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
)

func TestAsyncMergeOutcomes(t *testing.T) {
	t.Run("merged is a normal merge", func(t *testing.T) {
		// The client reports a merge made through the async API exactly like a synchronous one.
		r := newRig(t, "INP-1")
		res, err := r.svc.Merge(context.Background(), request(done("INP-1")))
		if err != nil || stepOf(t, res, merge.StepMerge, "").State != merge.StateOK || r.jira.moveCount() != 1 {
			t.Fatalf("%+v %v", res, err)
		}
	})

	t.Run("enqueued is recorded and moves nothing", func(t *testing.T) {
		r := newRig(t, "INP-1")
		r.gh.mergeErr = fmt.Errorf("wrapped: %w", github.ErrEnqueued)
		req := request(done("INP-1"))
		req.DeleteBranch = true
		res, err := r.svc.Merge(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		m := stepOf(t, res, merge.StepMerge, "")
		if m.State != merge.StateQueued || m.Message != "Added to the merge queue" || res.SHA != "" {
			t.Fatalf("%+v", res)
		}
		if tr := stepOf(t, res, merge.StepTransition, "INP-1"); tr.State != merge.StateSkipped {
			t.Fatalf("%+v", tr)
		}
		if r.jira.moveCount() != 0 || len(r.gh.deletes) != 0 || r.jira.listCalls != 1 {
			t.Fatalf("tickets moved or branch deleted: moves %d deletes %v", r.jira.moveCount(), r.gh.deletes)
		}
		acts, _ := r.store.Activity(context.Background(), 0, 10)
		if len(acts) != 1 || !strings.Contains(acts[0].Summary, "Added to the merge queue") || r.store.records.Load() != 1 {
			t.Fatalf("%+v", acts)
		}
		if r.refreshes.Load() == 0 {
			t.Error("no refresh")
		}
		// A repeat with the same key replays the stored result and does not submit again.
		again, err := r.svc.Merge(context.Background(), req)
		if err != nil || stepOf(t, again, merge.StepMerge, "").State != merge.StateQueued || r.gh.mergeCount() != 1 {
			t.Fatalf("%+v %v", again, err)
		}
	})

	t.Run("failed is a refusal with GitHub's reason", func(t *testing.T) {
		r := newRig(t, "INP-1")
		r.gh.mergeErr = &github.MergeFailedError{Reason: "2 approving reviews are required"}
		e := wantError(t, merge2(r, request(done("INP-1"))), merge.CodeMergeFailed)
		if e.Detail != "GitHub did not merge: 2 approving reviews are required" || e.Fresh == nil {
			t.Fatalf("%+v", e)
		}
		if r.jira.moveCount() != 0 || r.store.records.Load() != 0 || len(r.gh.deletes) != 0 {
			t.Error("steps ran after a failed merge")
		}
	})

	t.Run("pending asks for a refresh", func(t *testing.T) {
		r := newRig(t, "INP-1")
		r.gh.mergeErr = github.ErrMergePending
		e := wantError(t, merge2(r, request(done("INP-1"))), merge.CodeMergePending)
		if !strings.Contains(e.Detail, "refresh") || e.Fresh == nil || r.jira.moveCount() != 0 || r.store.records.Load() != 0 {
			t.Fatalf("%+v", e)
		}
		if r.refreshes.Load() == 0 {
			t.Error("a refresh is started so the poller sees the merge")
		}
	})
}
