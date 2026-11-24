package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/linking"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

const maxManualTickets = 20

func jiraTTL(s store.Settings) time.Duration { return time.Duration(s.JiraSec) * time.Second }

// snapshotResponse writes the snapshot JSON the poller encoded once, so a request costs no
// encoding.
type snapshotResponse struct{ pub *poller.Published }

func (r snapshotResponse) VisitGetSnapshotResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", r.pub.ETag())
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(r.pub.JSON)
	return err
}

func (h *handlers) GetSnapshot(_ context.Context, req gen.GetSnapshotRequestObject) (gen.GetSnapshotResponseObject, error) {
	pub := h.cfg.Poller.Current()
	if m := req.Params.IfNoneMatch; m != nil && etagMatches(*m, pub.ETag()) {
		return gen.GetSnapshot304Response{}, nil
	}
	return snapshotResponse{pub: pub}, nil
}

// etagMatches implements the weak comparison of If-None-Match.
func etagMatches(header, etag string) bool {
	for v := range strings.SplitSeq(header, ",") {
		v = strings.TrimPrefix(strings.TrimSpace(v), "W/")
		if v == "*" || v == etag {
			return true
		}
	}
	return false
}

func (h *handlers) SyncNow(_ context.Context, _ gen.SyncNowRequestObject) (gen.SyncNowResponseObject, error) {
	return gen.SyncNow202JSONResponse(h.cfg.Poller.Trigger()), nil
}

// kick starts a refresh after a connection changed, so the dashboard follows without waiting for
// the interval.
func (h *handlers) kick() {
	if h.cfg.Poller != nil {
		h.cfg.Poller.Trigger()
	}
}

func (h *handlers) reschedule() {
	if h.cfg.Poller != nil {
		h.cfg.Poller.Reschedule()
	}
}

// jiraSite resolves the Jira site to call, or the problem that explains why there is none.
func (h *handlers) jiraSite(ctx context.Context) (jira.Site, error) {
	site, err := h.cfg.Accounts.JiraSite(ctx)
	switch {
	case err == nil:
		return site, nil
	case errors.Is(err, auth.ErrNotConnected), errors.Is(err, auth.ErrNeedsReconnect), errors.Is(err, poller.ErrNoSite):
		return jira.Site{}, problem(http.StatusConflict, codeNotConnected, "Jira is not connected")
	}
	return jira.Site{}, err
}

func (h *handlers) jiraProblem(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, jira.ErrNotFound):
		return problem(http.StatusNotFound, mw.CodeNotFound, "")
	case errors.Is(err, jira.ErrUnauthorized), errors.Is(err, auth.ErrNotConnected), errors.Is(err, auth.ErrNeedsReconnect):
		return problem(http.StatusConflict, codeNotConnected, "Jira is not connected")
	}
	h.cfg.Log.ErrorContext(ctx, "jira call failed", "err", err)
	return problem(http.StatusBadGateway, codeUpstream, "Jira could not be reached")
}

func (h *handlers) GetAssignedTickets(ctx context.Context, _ gen.GetAssignedTicketsRequestObject) (gen.GetAssignedTicketsResponseObject, error) {
	site, err := h.jiraSite(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := h.cfg.Store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	issues, err := h.cfg.Jira.Assigned(ctx, site.ID, jiraTTL(settings))
	if err != nil {
		return nil, h.jiraProblem(ctx, err)
	}
	out := gen.AssignedTickets{Tickets: make([]gen.TicketSummary, len(issues))}
	for i, is := range issues {
		out.Tickets[i] = gen.TicketSummary{
			Key: is.Key, Summary: is.Summary, Status: is.Status, Project: is.Project, IssueType: is.IssueType,
			StatusCategory: gen.TicketSummaryStatusCategory(is.StatusCategory),
		}
	}
	return gen.GetAssignedTickets200JSONResponse(out), nil
}

func (h *handlers) GetTransitions(ctx context.Context, req gen.GetTransitionsRequestObject) (gen.GetTransitionsResponseObject, error) {
	site, err := h.jiraSite(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := h.cfg.Jira.Transitions(ctx, site.ID, req.Key)
	if err != nil {
		return nil, h.jiraProblem(ctx, err)
	}
	out := gen.Transitions{Key: req.Key, Transitions: make([]gen.Transition, len(ts))}
	for i, t := range ts {
		fields := make([]gen.TransitionField, len(t.Fields))
		for j, f := range t.Fields {
			allowed := make([]gen.NamedId, len(f.AllowedValues))
			for k, o := range f.AllowedValues {
				allowed[k] = gen.NamedId{Id: o.ID, Name: o.Name}
			}
			fields[j] = gen.TransitionField{Id: f.ID, Name: f.Name, Required: f.Required, Type: f.Type, AllowedValues: allowed}
		}
		out.Transitions[i] = gen.Transition{Id: t.ID, Name: t.Name, ToStatus: t.ToStatus, ToCategory: t.ToCategory, Fields: fields}
	}
	return gen.GetTransitions200JSONResponse(out), nil
}

func (h *handlers) PutPrTickets(ctx context.Context, req gen.PutPrTicketsRequestObject) (gen.PutPrTicketsResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	manual, removed := req.Body.Manual, req.Body.Removed
	if err := validateTicketLinks(manual, removed); err != nil {
		return nil, err
	}
	row, ok := h.cfg.Poller.PR(req.Id)
	if !ok {
		return nil, problem(http.StatusNotFound, mw.CodeNotFound, "")
	}
	sources := make(map[string]string, len(row.Tickets))
	for _, t := range row.Tickets {
		sources[t.Key] = string(t.Source)
	}
	hidden := make([]store.HiddenKey, 0, len(removed))
	for _, k := range removed {
		src := sources[k]
		if src == "" || src == string(gen.Manual) {
			src = string(gen.Branch)
		}
		hidden = append(hidden, store.HiddenKey{Key: k, Source: src})
	}
	if err := h.cfg.Store.SetUserLinks(ctx, req.Id, manual, hidden); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, problem(http.StatusNotFound, mw.CodeNotFound, "")
		}
		return nil, err
	}
	if err := h.cfg.Poller.Relink(ctx); err != nil {
		return nil, err
	}
	return gen.PutPrTickets204Response{}, nil
}

func validateTicketLinks(manual, removed []string) error {
	if len(manual) > maxManualTickets || len(removed) > maxManualTickets {
		return problem(http.StatusUnprocessableEntity, mw.CodeInvalidBody, "at most 20 tickets can be linked or hidden")
	}
	seen := make(map[string]struct{}, len(manual)+len(removed))
	for _, list := range [][]string{manual, removed} {
		for _, k := range list {
			if !linking.ValidKey(k) {
				return problem(http.StatusUnprocessableEntity, mw.CodeInvalidBody, "invalid ticket key")
			}
			if _, dup := seen[k]; dup {
				return problem(http.StatusUnprocessableEntity, mw.CodeInvalidBody, "a ticket key appears twice")
			}
			seen[k] = struct{}{}
		}
	}
	return nil
}
