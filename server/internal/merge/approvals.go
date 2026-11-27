package merge

import (
	"strconv"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

// Approvals a repository can ask for before the dashboard lets a pull request merge.
const (
	DefaultApprovals = 1
	MaxApprovals     = 10
)

// Review states of GitHub's latest review per reviewer that the count looks at.
const (
	reviewApproved         = "APPROVED"
	reviewChangesRequested = "CHANGES_REQUESTED"
)

// Approvals is the review tally of one pull request against what its repository requires. It is
// pure: the poller fills it for the snapshot and the merge check uses the same value, so the page
// and the server agree.
type Approvals struct {
	// Have is the number of distinct reviewers whose latest review is an approval.
	Have int
	// Need is the repository's approvalsRequired, 1 to 10.
	Need int
	// ChangesRequested is the number of reviewers whose latest review requests changes.
	ChangesRequested int
}

// NeedOf is the number of approvals a repository requires: a value outside 1 to 10, which is an
// unset one, means the default of 1.
func NeedOf(n int) int {
	if n < DefaultApprovals || n > MaxApprovals {
		return DefaultApprovals
	}
	return n
}

// CountApprovals tallies the latest review of each reviewer (GitHub's latestReviews, one per
// reviewer). Reviewers are counted once, by login; a review whose author is gone (empty login) is
// counted on its own, by review id.
func CountApprovals(reviews []github.Review, need int) Approvals {
	a := Approvals{Need: NeedOf(need)}
	seen := make(map[string]struct{}, len(reviews))
	for _, r := range reviews {
		who := r.Author
		if who == "" {
			who = "#" + r.ID
		}
		if _, dup := seen[who]; dup {
			continue
		}
		seen[who] = struct{}{}
		switch r.State {
		case reviewApproved:
			a.Have++
		case reviewChangesRequested:
			a.ChangesRequested++
		}
	}
	return a
}

// Met reports whether enough reviewers approved.
func (a Approvals) Met() bool { return a.Have >= NeedOf(a.Need) }

// Block is why the approvals alone keep the pull request from merging, in the words of the Merge
// column ("Needs 2 approvals, has 1"), or "" when there are enough.
func (a Approvals) Block() string {
	if a.Met() {
		return ""
	}
	return "Needs " + strconv.Itoa(NeedOf(a.Need)) + " approvals, has " + strconv.Itoa(a.Have)
}
