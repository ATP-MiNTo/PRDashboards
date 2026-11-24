package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

const defaultActivityPage = 50

// Merger carries out merges and retries.
type Merger interface {
	Merge(ctx context.Context, req merge.Request) (merge.Result, error)
	Retry(ctx context.Context, id int64) (merge.Step, error)
}

func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toGenStep(s merge.Step) gen.MergeStep {
	out := gen.MergeStep{
		Kind: gen.MergeStepKind(s.Kind), State: gen.MergeStepState(s.State),
		Ticket: strp(s.Ticket), Message: strp(s.Message), ToStatus: strp(s.ToStatus),
	}
	if s.RetryID != 0 {
		id := s.RetryID
		out.RetryId = &id
	}
	return out
}

// mergeProblem writes the 409 of a merge that was refused, with the state GitHub just reported.
func mergeProblem(e *merge.Error) gen.MergePr409ApplicationProblemPlusJSONResponse {
	out := gen.MergePr409ApplicationProblemPlusJSONResponse{
		Type: "/problems/" + e.Code, Title: http.StatusText(http.StatusConflict), Status: http.StatusConflict,
		Code: e.Code, Detail: strp(e.Detail),
	}
	if f := e.Fresh; f != nil {
		out.Pr = &gen.FreshPr{
			State: f.State, HeadSha: f.HeadSHA, BaseRef: f.BaseRef, IsDraft: f.IsDraft,
			Mergeable: f.Mergeable, MergeStateStatus: f.MergeState, ReviewDecision: strp(f.ReviewDecision),
		}
	}
	return out
}

func conflictCode(code string) bool {
	switch code {
	case merge.CodeHeadMoved, merge.CodeNotMergeable, merge.CodeStackBaseOpen, merge.CodeDraft, merge.CodeNotOpen,
		merge.CodeTransitionUnavailable, merge.CodeNotConnected:
		return true
	}
	return false
}

// mergeFailure maps an error of the merge service to a problem. Errors that are neither a refusal
// nor a provider failure are internal.
func (h *handlers) mergeFailure(ctx context.Context, err error) error {
	var rl *github.RateLimitedError
	var blocked *github.OrgBlocked
	var apiErr *github.APIError
	var jiraErr *jira.StatusError
	var netErr net.Error
	var urlErr *url.Error
	switch {
	case errors.As(err, &rl):
		return problem(http.StatusTooManyRequests, mw.CodeRateLimited, "GitHub's rate limit is used up")
	case errors.As(err, &blocked):
		return problem(http.StatusForbidden, codeOrgBlocked, "An organization has not authorized the token")
	case errors.As(err, &apiErr), errors.As(err, &jiraErr), errors.As(err, &netErr), errors.As(err, &urlErr):
		h.cfg.Log.ErrorContext(ctx, "merge call failed", "err", err)
		return problem(http.StatusBadGateway, codeUpstream, "GitHub or Jira could not be reached")
	}
	return err
}

func (h *handlers) MergePr(ctx context.Context, req gen.MergePrRequestObject) (gen.MergePrResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	in := merge.Request{
		PRID: req.Id, IdempotencyKey: req.Params.IdempotencyKey, Method: merge.Method(req.Body.Method),
		ExpectedHeadSHA: req.Body.ExpectedHeadSha, DeleteBranch: req.Body.DeleteBranch,
		Tickets: make([]merge.Ticket, len(req.Body.Tickets)),
	}
	seen := make(map[string]struct{}, len(req.Body.Tickets))
	for i, t := range req.Body.Tickets {
		if _, dup := seen[t.Key]; dup {
			return nil, problem(http.StatusUnprocessableEntity, mw.CodeInvalidBody, "a ticket appears twice")
		}
		seen[t.Key] = struct{}{}
		in.Tickets[i] = merge.Ticket{Key: t.Key, Fields: t.Fields}
		if t.TransitionId != nil {
			in.Tickets[i].TransitionID = *t.TransitionId
		}
	}
	res, err := h.cfg.Merger.Merge(ctx, in)
	var refused *merge.Error
	switch {
	case errors.As(err, &refused) && conflictCode(refused.Code):
		return mergeProblem(refused), nil
	case errors.As(err, &refused) && refused.Code == merge.CodeNotFound:
		return nil, problem(http.StatusNotFound, mw.CodeNotFound, refused.Detail)
	case errors.As(err, &refused):
		return nil, problem(http.StatusUnprocessableEntity, refused.Code, refused.Detail)
	case err != nil:
		return nil, h.mergeFailure(ctx, err)
	}
	out := gen.MergeResult{PrId: res.PRID, Method: gen.MergeMethodName(res.Method), Sha: res.SHA, Steps: make([]gen.MergeStep, len(res.Steps))}
	for i, s := range res.Steps {
		out.Steps[i] = toGenStep(s)
	}
	return gen.MergePr200JSONResponse(out), nil
}

func (h *handlers) ListRetries(ctx context.Context, _ gen.ListRetriesRequestObject) (gen.ListRetriesResponseObject, error) {
	rows, err := h.cfg.Store.Retries(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.Retries{Retries: make([]gen.Retry, len(rows))}
	for i, r := range rows {
		out.Retries[i] = gen.Retry{
			Id: r.ID, PrId: strp(r.PRID), TicketKey: r.TicketKey, TransitionId: r.TransitionID,
			ToStatus: r.ToStatus, LastError: r.LastError, CreatedAt: r.CreatedAt,
		}
	}
	return gen.ListRetries200JSONResponse(out), nil
}

func (h *handlers) RetryTransition(ctx context.Context, req gen.RetryTransitionRequestObject) (gen.RetryTransitionResponseObject, error) {
	step, err := h.cfg.Merger.Retry(ctx, req.Id)
	var refused *merge.Error
	switch {
	case errors.As(err, &refused) && refused.Code == merge.CodeNotFound:
		return nil, problem(http.StatusNotFound, mw.CodeNotFound, "")
	case errors.As(err, &refused) && conflictCode(refused.Code):
		return nil, problem(http.StatusConflict, refused.Code, refused.Detail)
	case errors.As(err, &refused):
		return nil, problem(http.StatusUnprocessableEntity, refused.Code, refused.Detail)
	case err != nil:
		return nil, h.mergeFailure(ctx, err)
	}
	return gen.RetryTransition200JSONResponse{Step: toGenStep(step)}, nil
}

func (h *handlers) DismissRetry(ctx context.Context, req gen.DismissRetryRequestObject) (gen.DismissRetryResponseObject, error) {
	err := h.cfg.Store.DeleteRetry(ctx, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, problem(http.StatusNotFound, mw.CodeNotFound, "")
	}
	if err != nil {
		return nil, err
	}
	return gen.DismissRetry204Response{}, nil
}

// GetActivity pages the log by id, newest first. It reads one row more than asked to know whether
// another page exists.
func (h *handlers) GetActivity(ctx context.Context, req gen.GetActivityRequestObject) (gen.GetActivityResponseObject, error) {
	limit, before := defaultActivityPage, int64(0)
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if req.Params.Cursor != nil {
		before = *req.Params.Cursor
	}
	rows, err := h.cfg.Store.Activity(ctx, before, limit+1)
	if err != nil {
		return nil, err
	}
	out := gen.ActivityPage{}
	if len(rows) > limit {
		rows = rows[:limit]
		next := rows[limit-1].ID
		out.NextCursor = &next
	}
	out.Items = make([]gen.ActivityItem, len(rows))
	for i, r := range rows {
		out.Items[i] = gen.ActivityItem{
			Id: r.ID, CreatedAt: r.CreatedAt, Kind: r.Kind, TicketKey: strp(r.TicketKey), PrId: strp(r.PRID),
			TriggeredBy: r.TriggeredBy, Result: r.Result, Summary: r.Summary, Error: strp(r.Error),
		}
	}
	return gen.GetActivity200JSONResponse(out), nil
}
