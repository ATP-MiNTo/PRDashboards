// Package review carries out the review actions of the dashboard: approve a pull request, request
// changes and mark a draft ready for review. Each one follows the pattern of the merge: the pull
// request is fetched again from GitHub, checked against what the person saw, and only then is the
// write sent with the viewer's own token. The refusals are *merge.Error values with the same codes
// and fresh state as a refused merge, so the API answers them the same way.
package review

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
)

// Actions a Service performs.
const (
	ActionApprove        = "approve"
	ActionRequestChanges = "request_changes"
	ActionReady          = "ready"
)

// GitHub is the part of the GitHub client the review actions use.
type GitHub interface {
	PullRequest(ctx context.Context, token, id string) (github.PR, error)
	SubmitReview(ctx context.Context, token, nameWithOwner string, number int, event, sha, body string) error
	MarkReady(ctx context.Context, token, id string) error
}

// Snapshot tells whether the dashboard lists a pull request.
type Snapshot interface {
	Linked(id string) (merge.Linked, bool)
}

// Accounts says who the review acts as. GitHubToken returns the GitHub token with the id (the
// primary one for an empty id or an id that is gone), with its name and the login of its account
// (empty when not known), or an error that wraps auth.ErrNotConnected (or auth.ErrNeedsReconnect)
// when GitHub cannot be used.
type Accounts interface {
	GitHubToken(ctx context.Context, tokenID string) (auth.GitHubCredential, error)
}

// Config holds the collaborators of a Service.
type Config struct {
	GitHub   GitHub
	Snapshot Snapshot
	Accounts Accounts
	// Refresh starts a refresh without waiting for it. It is how the snapshot learns the new review
	// state, the same call a merge makes.
	Refresh func()
	Log     *slog.Logger
}

// Service performs review actions.
type Service struct{ cfg Config }

// New creates a service.
func New(cfg Config) *Service {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Refresh == nil {
		cfg.Refresh = func() {}
	}
	return &Service{cfg: cfg}
}

// Request is a review action as the person confirmed it. Body is the text of the review; request
// changes needs it, an approval may leave it empty and ready ignores it.
type Request struct {
	PRID            string
	ExpectedHeadSHA string
	Body            string
}

// Result is what a review action did.
type Result struct {
	PRID   string
	Action string
}

// Approve submits an approving review.
func (s *Service) Approve(ctx context.Context, req Request) (Result, error) {
	return s.run(ctx, ActionApprove, req)
}

// RequestChanges submits a review that requests changes. The text is required.
func (s *Service) RequestChanges(ctx context.Context, req Request) (Result, error) {
	if strings.TrimSpace(req.Body) == "" {
		return Result{}, &merge.Error{Code: merge.CodeBodyRequired, Detail: "say what has to change"}
	}
	return s.run(ctx, ActionRequestChanges, req)
}

// MarkReady takes a draft pull request out of draft.
func (s *Service) MarkReady(ctx context.Context, req Request) (Result, error) {
	return s.run(ctx, ActionReady, req)
}

func notConnected(err error) bool {
	return errors.Is(err, auth.ErrNotConnected) || errors.Is(err, auth.ErrNeedsReconnect) ||
		errors.Is(err, github.ErrBadCredentials)
}

func (s *Service) run(ctx context.Context, action string, req Request) (Result, error) {
	view, ok := s.cfg.Snapshot.Linked(req.PRID)
	if !ok {
		return Result{}, &merge.Error{Code: merge.CodeNotFound, Detail: "unknown pull request"}
	}
	// The pull request is read and written with the token of its repository, as the snapshot chose.
	cred, err := s.cfg.Accounts.GitHubToken(ctx, view.TokenID)
	if err != nil {
		if notConnected(err) {
			return Result{}, &merge.Error{Code: merge.CodeNotConnected, Detail: "GitHub is not connected"}
		}
		return Result{}, err
	}
	token := cred.Token
	pr, err := s.cfg.GitHub.PullRequest(ctx, token, req.PRID)
	switch {
	case errors.Is(err, github.ErrPRNotFound):
		return Result{}, &merge.Error{Code: merge.CodeNotFound, Detail: "GitHub has no such pull request"}
	case notConnected(err):
		return Result{}, &merge.Error{Code: merge.CodeNotConnected, Detail: "GitHub is not connected"}
	case err != nil:
		s.cfg.Log.WarnContext(ctx, "pull request re-check failed", "pr", req.PRID, "action", action, "err", err)
		return Result{}, err
	}
	if e := s.check(action, cred.Login, pr, req); e != nil {
		s.cfg.Refresh()
		return Result{}, e
	}
	// A refused write names the repository, so a token of another owner is told as such.
	err = github.WithToken(github.WithRepo(s.send(ctx, action, token, pr, req), pr.Repo.NameWithOwner, ""), cred.Name)
	var rejected *github.ReviewRejectedError
	switch {
	case errors.Is(err, github.ErrHeadMoved):
		s.cfg.Refresh()
		return Result{}, &merge.Error{Code: merge.CodeHeadMoved, Detail: "the branch changed while the review was prepared", Fresh: merge.FreshOf(pr)}
	case errors.Is(err, github.ErrOwnPullRequest):
		return Result{}, &merge.Error{Code: merge.CodeOwnPullRequest, Detail: "GitHub does not let you review a pull request you opened", Fresh: merge.FreshOf(pr)}
	case errors.Is(err, github.ErrNotDraft):
		s.cfg.Refresh()
		return Result{}, &merge.Error{Code: merge.CodeNotDraft, Detail: "the pull request is not a draft", Fresh: merge.FreshOf(pr)}
	case errors.Is(err, github.ErrPRNotFound):
		return Result{}, &merge.Error{Code: merge.CodeNotFound, Detail: "GitHub has no such pull request"}
	case errors.As(err, &rejected):
		return Result{}, &merge.Error{Code: merge.CodeReviewRejected, Detail: "GitHub refused it: " + rejected.Message}
	case notConnected(err):
		return Result{}, &merge.Error{Code: merge.CodeNotConnected, Detail: "GitHub is not connected"}
	case err != nil:
		s.cfg.Log.WarnContext(ctx, "github review call failed", "pr", req.PRID, "action", action, "err", err)
		return Result{}, err
	}
	s.cfg.Log.InfoContext(ctx, "review action done", "pr", req.PRID, "action", action)
	// The snapshot learns the new review state from one refresh, the same as after a merge: two
	// searches, never a call per pull request.
	s.cfg.Refresh()
	return Result{PRID: req.PRID, Action: action}, nil
}

// check is the re-check against the pull request GitHub just reported: it is open, a draft when it
// is to be marked ready, still at the commit the person saw, and not opened by the reviewer (the
// login of the token that is used, which is the repository's, not the primary's).
func (s *Service) check(action, login string, pr github.PR, req Request) *merge.Error {
	refuse := func(code, detail string) *merge.Error {
		return &merge.Error{Code: code, Detail: detail, Fresh: merge.FreshOf(pr)}
	}
	switch {
	case pr.State != "" && pr.State != "OPEN":
		return refuse(merge.CodeNotOpen, "the pull request is no longer open")
	case action == ActionReady && !pr.Draft:
		return refuse(merge.CodeNotDraft, "the pull request is not a draft")
	case pr.HeadSHA != req.ExpectedHeadSHA:
		return refuse(merge.CodeHeadMoved, "the branch changed since the page loaded")
	}
	if action != ActionReady && pr.Author != "" {
		if login != "" && strings.EqualFold(login, pr.Author) {
			return refuse(merge.CodeOwnPullRequest, "GitHub does not let you review a pull request you opened")
		}
	}
	return nil
}

func (s *Service) send(ctx context.Context, action, token string, pr github.PR, req Request) error {
	switch action {
	case ActionApprove:
		return s.cfg.GitHub.SubmitReview(ctx, token, pr.Repo.NameWithOwner, pr.Number, github.ReviewApprove, req.ExpectedHeadSHA, req.Body)
	case ActionRequestChanges:
		return s.cfg.GitHub.SubmitReview(ctx, token, pr.Repo.NameWithOwner, pr.Number, github.ReviewRequestChanges, req.ExpectedHeadSHA, req.Body)
	}
	return s.cfg.GitHub.MarkReady(ctx, token, pr.ID)
}
