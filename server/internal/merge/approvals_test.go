package merge_test

import (
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
)

func review(id, author, state string) github.Review {
	return github.Review{ID: id, Author: author, State: state}
}

func TestCountApprovals(t *testing.T) {
	tests := []struct {
		name    string
		reviews []github.Review
		need    int
		want    merge.Approvals
		block   string
	}{
		{"none", nil, 1, merge.Approvals{Need: 1}, "Needs 1 approvals, has 0"},
		{"one of one", []github.Review{review("1", "alice", "APPROVED")}, 1, merge.Approvals{Have: 1, Need: 1}, ""},
		{"one of two", []github.Review{review("1", "alice", "APPROVED")}, 2, merge.Approvals{Have: 1, Need: 2}, "Needs 2 approvals, has 1"},
		{"two of two", []github.Review{review("1", "alice", "APPROVED"), review("2", "bob", "APPROVED")}, 2, merge.Approvals{Have: 2, Need: 2}, ""},
		{"more than needed", []github.Review{review("1", "a", "APPROVED"), review("2", "b", "APPROVED")}, 1, merge.Approvals{Have: 2, Need: 1}, ""},
		{"a reviewer counts once", []github.Review{review("1", "alice", "APPROVED"), review("2", "alice", "APPROVED")}, 2, merge.Approvals{Have: 1, Need: 2}, "Needs 2 approvals, has 1"},
		{"changes requested is not an approval and is counted", []github.Review{review("1", "alice", "APPROVED"), review("2", "bob", "CHANGES_REQUESTED")}, 1,
			merge.Approvals{Have: 1, Need: 1, ChangesRequested: 1}, ""},
		{"comments and dismissed reviews do not count", []github.Review{review("1", "a", "COMMENTED"), review("2", "b", "DISMISSED"), review("3", "c", "PENDING")}, 1,
			merge.Approvals{Need: 1}, "Needs 1 approvals, has 0"},
		{"deleted accounts count one each", []github.Review{review("1", "", "APPROVED"), review("2", "", "APPROVED")}, 2, merge.Approvals{Have: 2, Need: 2}, ""},
		{"unset need is one", []github.Review{review("1", "alice", "APPROVED")}, 0, merge.Approvals{Have: 1, Need: 1}, ""},
		{"need above the limit is one", nil, 11, merge.Approvals{Need: 1}, "Needs 1 approvals, has 0"},
		{"ten", []github.Review{review("1", "a", "APPROVED")}, 10, merge.Approvals{Have: 1, Need: 10}, "Needs 10 approvals, has 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := merge.CountApprovals(tt.reviews, tt.need)
			if got != tt.want {
				t.Fatalf("%+v, want %+v", got, tt.want)
			}
			if b := got.Block(); b != tt.block {
				t.Fatalf("block %q, want %q", b, tt.block)
			}
			if got.Met() != (tt.block == "") {
				t.Fatalf("Met %v with block %q", got.Met(), tt.block)
			}
		})
	}
}

func TestNeedOf(t *testing.T) {
	for in, want := range map[int]int{-3: 1, 0: 1, 1: 1, 2: 2, 10: 10, 11: 1} {
		if got := merge.NeedOf(in); got != want {
			t.Errorf("NeedOf(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestApprovalsZeroValueNeedsOneApproval(t *testing.T) {
	// A snapshot stored before approvals existed decodes to this: it must not read as "met".
	if (merge.Approvals{}).Met() {
		t.Fatal("zero approvals met the default of one")
	}
}
