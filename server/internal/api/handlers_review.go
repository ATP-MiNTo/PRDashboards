package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/review"
)

// Reviewer carries out the review actions of a pull request.
type Reviewer interface {
	Approve(ctx context.Context, req review.Request) (review.Result, error)
	RequestChanges(ctx context.Context, req review.Request) (review.Result, error)
	MarkReady(ctx context.Context, req review.Request) (review.Result, error)
}

// reviewOutcome is what a review service call became: the result, a 409 with the fresh state, or
// an error for the response error handler (404, 422, 403, 429, 502 or 500).
func (h *handlers) reviewOutcome(ctx context.Context, op string, res review.Result, err error) (*gen.ReviewResult, *gen.MergeProblem, error) {
	var refused *merge.Error
	switch {
	case err == nil:
		return &gen.ReviewResult{PrId: res.PRID, Action: gen.ReviewResultAction(res.Action)}, nil, nil
	case errors.As(err, &refused) && conflictCode(refused.Code):
		p := conflictProblem(refused)
		return nil, &p, nil
	case errors.As(err, &refused) && refused.Code == merge.CodeNotFound:
		return nil, nil, problem(http.StatusNotFound, mw.CodeNotFound, refused.Detail)
	case errors.As(err, &refused):
		return nil, nil, problem(http.StatusUnprocessableEntity, refused.Code, refused.Detail)
	}
	return nil, nil, h.providerFailure(ctx, op, err)
}

func (h *handlers) ApprovePr(ctx context.Context, req gen.ApprovePrRequestObject) (gen.ApprovePrResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	in := review.Request{PRID: req.Id, ExpectedHeadSHA: req.Body.ExpectedHeadSha}
	if req.Body.Body != nil {
		in.Body = *req.Body.Body
	}
	res, err := h.cfg.Reviewer.Approve(ctx, in)
	ok, conflict, err := h.reviewOutcome(ctx, "approve", res, err)
	switch {
	case err != nil:
		return nil, err
	case conflict != nil:
		return gen.ApprovePr409ApplicationProblemPlusJSONResponse{ReviewConflictApplicationProblemPlusJSONResponse: gen.ReviewConflictApplicationProblemPlusJSONResponse(*conflict)}, nil
	}
	return gen.ApprovePr200JSONResponse(*ok), nil
}

func (h *handlers) RequestChangesPr(ctx context.Context, req gen.RequestChangesPrRequestObject) (gen.RequestChangesPrResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	res, err := h.cfg.Reviewer.RequestChanges(ctx, review.Request{PRID: req.Id, ExpectedHeadSHA: req.Body.ExpectedHeadSha, Body: req.Body.Body})
	ok, conflict, err := h.reviewOutcome(ctx, "request changes", res, err)
	switch {
	case err != nil:
		return nil, err
	case conflict != nil:
		return gen.RequestChangesPr409ApplicationProblemPlusJSONResponse{ReviewConflictApplicationProblemPlusJSONResponse: gen.ReviewConflictApplicationProblemPlusJSONResponse(*conflict)}, nil
	}
	return gen.RequestChangesPr200JSONResponse(*ok), nil
}

func (h *handlers) MarkPrReady(ctx context.Context, req gen.MarkPrReadyRequestObject) (gen.MarkPrReadyResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	res, err := h.cfg.Reviewer.MarkReady(ctx, review.Request{PRID: req.Id, ExpectedHeadSHA: req.Body.ExpectedHeadSha})
	ok, conflict, err := h.reviewOutcome(ctx, "mark ready", res, err)
	switch {
	case err != nil:
		return nil, err
	case conflict != nil:
		return gen.MarkPrReady409ApplicationProblemPlusJSONResponse{ReviewConflictApplicationProblemPlusJSONResponse: gen.ReviewConflictApplicationProblemPlusJSONResponse(*conflict)}, nil
	}
	return gen.MarkPrReady200JSONResponse(*ok), nil
}
