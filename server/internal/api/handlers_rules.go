package api

import (
	"context"
	"net/http"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/rules"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func toGenRule(r store.SyncRule) gen.SyncRule {
	return gen.SyncRule{
		Id: r.ID, Name: r.Name, Enabled: r.Enabled, Trigger: gen.RuleTrigger(r.Trigger), Hours: r.Hours,
		Filters: gen.RuleFilters{
			Repo: r.Filters.Repo, Branch: r.Filters.Branch, Project: r.Filters.Project,
			IssueType: r.Filters.IssueType, Status: r.Filters.Status,
		},
		Target: r.Target, Mode: gen.RuleMode(r.Mode),
	}
}

func fromGenRule(r gen.SyncRule) store.SyncRule {
	out := store.SyncRule{
		ID: r.Id, Name: r.Name, Enabled: r.Enabled, Trigger: string(r.Trigger), Hours: r.Hours,
		Filters: store.RuleFilters{
			Repo: r.Filters.Repo, Branch: r.Filters.Branch, Project: r.Filters.Project,
			IssueType: r.Filters.IssueType, Status: r.Filters.Status,
		},
		Target: r.Target, Mode: string(r.Mode),
	}
	if r.Trigger != gen.CommitStale {
		out.Hours = 0
	}
	return out
}

func toEngine(r store.SyncRule) rules.Rule {
	return rules.Rule{
		ID: r.ID, Name: r.Name, Enabled: r.Enabled, Trigger: rules.Trigger(r.Trigger), Hours: r.Hours,
		Filters: rules.Filters(r.Filters), Target: r.Target, Mode: rules.Mode(r.Mode),
	}
}

func listRules(list []store.SyncRule) gen.Rules {
	out := gen.Rules{Rules: make([]gen.SyncRule, len(list))}
	for i, r := range list {
		out.Rules[i] = toGenRule(r)
	}
	return out
}

func (h *handlers) GetRules(ctx context.Context, _ gen.GetRulesRequestObject) (gen.GetRulesResponseObject, error) {
	list, err := h.cfg.Store.Rules(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetRules200JSONResponse(listRules(list)), nil
}

// changed reports whether the rule needs a fresh evaluation: it is new, or something that decides
// when it fires differs. A rename or a move in the list does not.
func changed(old map[string]store.SyncRule, r store.SyncRule) bool {
	o, ok := old[r.ID]
	if !ok {
		return true
	}
	o.Name, r.Name = "", ""
	return o != r
}

// PutRules replaces the whole ordered list in one transaction. New and edited rules start without
// fingerprints, or with the ones that hold now when the person chose to leave existing tickets, and
// the rules are evaluated again from cached data.
func (h *handlers) PutRules(ctx context.Context, req gen.PutRulesRequestObject) (gen.PutRulesResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, "invalid_body", "")
	}
	list := make([]store.SyncRule, len(req.Body.Rules))
	seen := make(map[string]struct{}, len(list))
	for i, r := range req.Body.Rules {
		list[i] = fromGenRule(r)
		if msg := rules.Validate(toEngine(list[i])); msg != "" {
			return nil, problem(http.StatusUnprocessableEntity, codeInvalidRule, msg)
		}
		if _, dup := seen[r.Id]; dup {
			return nil, problem(http.StatusUnprocessableEntity, codeInvalidRule, "duplicate rule id")
		}
		seen[r.Id] = struct{}{}
	}
	stored, err := h.cfg.Store.Rules(ctx)
	if err != nil {
		return nil, err
	}
	old := make(map[string]store.SyncRule, len(stored))
	for _, r := range stored {
		old[r.ID] = r
	}
	var reset []string
	var fresh []rules.Rule
	for _, r := range list {
		if changed(old, r) {
			reset = append(reset, r.ID)
			fresh = append(fresh, toEngine(r))
		}
	}
	var baseline []store.Firing
	if req.Body.Baseline && len(fresh) > 0 {
		if baseline, err = h.cfg.Poller.Baseline(ctx, fresh); err != nil {
			return nil, err
		}
	}
	if err := h.cfg.Store.PutRules(ctx, list, reset, baseline, h.cfg.Clock()); err != nil {
		return nil, err
	}
	if err := h.cfg.Poller.Reevaluate(ctx); err != nil {
		return nil, err
	}
	return gen.PutRules200JSONResponse(listRules(list)), nil
}

func (h *handlers) PreviewRule(ctx context.Context, req gen.PreviewRuleRequestObject) (gen.PreviewRuleResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, "invalid_body", "")
	}
	r := toEngine(fromGenRule(req.Body.Rule))
	if msg := rules.Validate(r); msg != "" {
		return nil, problem(http.StatusUnprocessableEntity, codeInvalidRule, msg)
	}
	pv, err := h.cfg.Poller.Preview(ctx, r)
	if err != nil {
		return nil, err
	}
	out := gen.RulePreview{Moves: make([]gen.RuleMove, len(pv.Moves)), Waiting: make([]gen.RuleWaiting, len(pv.Waiting))}
	for i, m := range pv.Moves {
		out.Moves[i] = gen.RuleMove{TicketKey: m.Ticket, Status: m.Status, AlreadyInTarget: m.InTarget}
	}
	for i, w := range pv.Waiting {
		refs := make([]gen.PrRef, len(w.WaitingOn))
		for j, ref := range w.WaitingOn {
			refs[j] = gen.PrRef{PrId: ref.ID, Number: ref.Number, Repo: ref.Repo}
		}
		out.Waiting[i] = gen.RuleWaiting{TicketKey: w.Ticket, Total: w.Total, WaitingOn: refs}
	}
	return gen.PreviewRule200JSONResponse(out), nil
}
