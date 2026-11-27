package merge

import (
	"context"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

// Problem codes of a refused merge or retry. The first group is a conflict with the state on
// GitHub, the second a request that cannot be carried out as sent.
const (
	CodeHeadMoved             = "head_moved"
	CodeNotMergeable          = "not_mergeable"
	CodeStackBaseOpen         = "stack_base_open"
	CodeDraft                 = "draft"
	CodeNotOpen               = "not_open"
	CodeTransitionUnavailable = "transition_unavailable"
	// CodeMergeFailed is a merge GitHub accepted and then failed (for example a branch protection
	// rule); the detail carries GitHub's reason. CodeMergePending is a merge GitHub was still
	// working on when the wait ended.
	CodeMergeFailed  = "merge_failed"
	CodeMergePending = "merge_pending"

	// Codes of the review actions (approve, request changes, mark ready), see package review.
	CodeNotDraft       = "not_draft"
	CodeOwnPullRequest = "own_pull_request"
	CodeBodyRequired   = "body_required"
	CodeReviewRejected = "review_rejected"

	CodeNotFound         = "not_found"
	CodeNotConnected     = "not_connected"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeTicketNotLinked  = "ticket_not_linked"
	CodeMissingField     = "missing_field"
	CodeInvalidField     = "invalid_field"
	CodeKeyReused        = "idempotency_key_reused"
)

// Error is a merge that was refused. Nothing was merged. Fresh is what GitHub reported just now,
// for the codes that come from the state of the pull request.
type Error struct {
	Code   string
	Detail string
	Fresh  *Fresh
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

// Fresh is the state of a pull request as GitHub reported it for a check.
type Fresh struct {
	State          string
	HeadSHA        string
	BaseRef        string
	IsDraft        bool
	Mergeable      string
	MergeState     string
	ReviewDecision string
}

// Ref names the branches of an open pull request, to find the pull request a stacked one waits on
// and the pull requests that still use a branch.
type Ref struct {
	ID     string
	Number int
	Head   string
	Base   string
}

// Linked is what the dashboard knows of a pull request before the merge.
type Linked struct {
	RepoID        string
	NameWithOwner string
	Number        int
	Tickets       []string
	// ApprovalsRequired is the repository's approvals setting as the current snapshot has it; a
	// value outside 1 to 10 counts as the default of 1 (NeedOf).
	ApprovalsRequired int
}

// Ticket is one linked ticket as confirmed in the merge panel. An empty TransitionID leaves the
// ticket where it is.
type Ticket struct {
	Key          string
	TransitionID string
	Fields       map[string]string
}

// Request is a merge as the person confirmed it.
type Request struct {
	PRID            string
	IdempotencyKey  string
	Method          Method
	ExpectedHeadSHA string
	DeleteBranch    bool
	Tickets         []Ticket
}

// Step kinds and states.
const (
	StepMerge        = "merge"
	StepDeleteBranch = "delete_branch"
	StepTransition   = "transition"

	StateOK      = "ok"
	StateFailed  = "failed"
	StateSkipped = "skipped"
	// StateQueued is a merge step that GitHub put in a merge queue: not merged yet.
	StateQueued = "queued"
)

// Step is the outcome of one part of a merge.
type Step struct {
	Kind     string `json:"kind"`
	Ticket   string `json:"ticket,omitempty"`
	State    string `json:"state"`
	Message  string `json:"message,omitempty"`
	ToStatus string `json:"toStatus,omitempty"`
	// RetryID names the pending retry of a failed transition.
	RetryID int64 `json:"retryId,omitempty"`
}

// Result is what a merge did, step by step. The merge itself is never rolled back.
type Result struct {
	PRID   string `json:"prId"`
	Method Method `json:"method"`
	SHA    string `json:"sha"`
	Steps  []Step `json:"steps"`
}

// GitHub is the part of the GitHub client a merge uses.
type GitHub interface {
	PullRequest(ctx context.Context, token, id string) (github.PR, error)
	Merge(ctx context.Context, token, nameWithOwner string, number int, method, sha string) (github.MergeResult, error)
	DeleteBranch(ctx context.Context, token, nameWithOwner, branch string) error
}

// Jira is the part of the Jira client a merge uses.
type Jira interface {
	Transitions(ctx context.Context, site, key string) ([]jira.Transition, error)
	Transition(ctx context.Context, site, key, id string, fields map[string]any) error
}

// Store is the part of the database a merge uses.
type Store interface {
	Idempotent(ctx context.Context, key string, now time.Time) (prID string, result []byte, ok bool, err error)
	RecordMerge(ctx context.Context, m store.Merged) error
	Retry(ctx context.Context, id int64) (store.RetryRow, error)
	FinishRetry(ctx context.Context, id int64, now time.Time, act store.ActivityRow) error
}

// Snapshot is the dashboard's view of the open pull requests.
type Snapshot interface {
	Linked(id string) (Linked, bool)
	Refs(repoID string) []Ref
	// MergedFirings returns the fingerprints of the merged rules that hold for the tickets of a pull
	// request that was just merged, to record with the merge.
	MergedFirings(ctx context.Context, prID string, tickets []string) ([]store.Firing, error)
}

// Accounts says who the merge acts as. Both methods return errors that wrap auth.ErrNotConnected
// (or auth.ErrNeedsReconnect, poller.ErrNoSite) when the account cannot be used.
type Accounts interface {
	GitHubToken(ctx context.Context) (string, error)
	JiraSite(ctx context.Context) (jira.Site, error)
}

// Publisher announces events to the browsers.
type Publisher interface {
	Publish(event string, payload any) error
}
