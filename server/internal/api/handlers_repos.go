package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func toGenRepo(r store.RepoConfig) gen.RepoSettings {
	rules := make([]gen.BranchRule, len(r.Rules))
	for i, rule := range r.Rules {
		rules[i] = gen.BranchRule{Pattern: rule.Pattern, Method: gen.MergeMethodName(rule.Method)}
	}
	return gen.RepoSettings{
		Id: r.ID, NameWithOwner: r.NameWithOwner, IsNew: r.IsNew, OpenPrs: r.OpenPRs,
		DefaultMethod: gen.MergeMethodName(r.DefaultMethod), BranchRules: rules,
		ApprovalsRequired: r.ApprovalsRequired,
		InheritMethod:     r.InheritMethod, DeleteBranch: gen.DeleteBranchChoice(r.DeleteBranch),
	}
}

// inheritOf is whether a repo update asks the repo to follow the default method: false when it
// leaves the field out, as the method itself is replaced by every update.
func inheritOf(b *bool) bool { return b != nil && *b }

// deleteBranchOf is the delete-branch choice of a repo update, "" (keep what is stored) when it
// leaves it out.
func deleteBranchOf(c *gen.DeleteBranchChoice) string {
	if c == nil {
		return ""
	}
	return string(*c)
}

// approvalsOf is the approvals a repo update asks for, nil (keep what is stored) when it leaves them
// out. 0 is a value of its own: nothing required.
func approvalsOf(n *gen.ApprovalsRequired) *int {
	if n == nil {
		return nil
	}
	v := *n
	return &v
}

func (h *handlers) ListRepos(ctx context.Context, _ gen.ListReposRequestObject) (gen.ListReposResponseObject, error) {
	repos, err := h.cfg.Store.Repos(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.Repos{Repos: make([]gen.RepoSettings, len(repos))}
	for i, r := range repos {
		out.Repos[i] = toGenRepo(r)
	}
	return gen.ListRepos200JSONResponse(out), nil
}

// PutRepos stores every update in one transaction and rebuilds the snapshot once. An unknown id
// leaves everything unchanged and answers 404.
func (h *handlers) PutRepos(ctx context.Context, req gen.PutReposRequestObject) (gen.PutReposResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	updates := make([]store.RepoUpdate, len(req.Body.Repos))
	for i, u := range req.Body.Repos {
		rules := make([]store.BranchRule, len(u.Update.BranchRules))
		for j, r := range u.Update.BranchRules {
			rules[j] = store.BranchRule{Pattern: r.Pattern, Method: string(r.Method)}
		}
		updates[i] = store.RepoUpdate{
			ID: u.Id, Method: string(u.Update.DefaultMethod), Rules: rules, ClearNew: u.Update.ClearNew,
			Approvals: approvalsOf(u.Update.ApprovalsRequired),
			Inherit:   inheritOf(u.Update.InheritMethod), DeleteBranch: deleteBranchOf(u.Update.DeleteBranch),
		}
	}
	err := h.cfg.Store.PutRepos(ctx, updates)
	if errors.Is(err, store.ErrNotFound) {
		return nil, problem(http.StatusNotFound, mw.CodeNotFound, "")
	}
	if err != nil {
		return nil, err
	}
	if err := h.cfg.Poller.Relink(ctx); err != nil {
		return nil, err
	}
	repos, err := h.cfg.Store.Repos(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.Repos{Repos: make([]gen.RepoSettings, len(repos))}
	for i, r := range repos {
		out.Repos[i] = toGenRepo(r)
	}
	return gen.PutRepos200JSONResponse(out), nil
}

func (h *handlers) GetRepo(ctx context.Context, req gen.GetRepoRequestObject) (gen.GetRepoResponseObject, error) {
	r, err := h.cfg.Store.Repo(ctx, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, problem(http.StatusNotFound, mw.CodeNotFound, "")
	}
	if err != nil {
		return nil, err
	}
	return gen.GetRepo200JSONResponse(toGenRepo(r)), nil
}

// PutRepo stores the default method and the whole rule list in one transaction, then rebuilds the
// snapshot from cached data so the Method column follows at once.
func (h *handlers) PutRepo(ctx context.Context, req gen.PutRepoRequestObject) (gen.PutRepoResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	rules := make([]store.BranchRule, len(req.Body.BranchRules))
	for i, r := range req.Body.BranchRules {
		rules[i] = store.BranchRule{Pattern: r.Pattern, Method: string(r.Method)}
	}
	err := h.cfg.Store.PutRepos(ctx, []store.RepoUpdate{{
		ID: req.Id, Method: string(req.Body.DefaultMethod), Rules: rules, ClearNew: req.Body.ClearNew,
		Approvals: approvalsOf(req.Body.ApprovalsRequired), Inherit: inheritOf(req.Body.InheritMethod),
		DeleteBranch: deleteBranchOf(req.Body.DeleteBranch),
	}})
	if errors.Is(err, store.ErrNotFound) {
		return nil, problem(http.StatusNotFound, mw.CodeNotFound, "")
	}
	if err != nil {
		return nil, err
	}
	if err := h.cfg.Poller.Relink(ctx); err != nil {
		return nil, err
	}
	r, err := h.cfg.Store.Repo(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.PutRepo200JSONResponse(toGenRepo(r)), nil
}
