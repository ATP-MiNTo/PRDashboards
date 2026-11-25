package api

import (
	"context"
	"net/http"
	"slices"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func toGenSettings(s store.Settings) gen.Settings {
	var out gen.Settings
	out.Polling.GithubSec = gen.PollingSeconds(s.GitHubSec)
	out.Polling.JiraSec = gen.PollingSeconds(s.JiraSec)
	out.Theme = gen.SettingsTheme(s.Theme)
	out.DefaultMethodForNewRepos = gen.SettingsDefaultMethodForNewRepos(s.DefaultMethod)
	out.DeleteBranchAfterMerge = s.DeleteBranchAfterMerge
	out.JiraSiteId = s.JiraSiteID
	out.ExcludedStatuses = append([]string{}, s.ExcludedStatuses...)
	return out
}

func fromGenSettings(s gen.Settings) store.Settings {
	return store.Settings{
		GitHubSec:              int(s.Polling.GithubSec),
		JiraSec:                int(s.Polling.JiraSec),
		Theme:                  string(s.Theme),
		DefaultMethod:          string(s.DefaultMethodForNewRepos),
		DeleteBranchAfterMerge: s.DeleteBranchAfterMerge,
		JiraSiteID:             s.JiraSiteId,
		ExcludedStatuses:       s.ExcludedStatuses,
	}
}

func (h *handlers) GetSettings(ctx context.Context, _ gen.GetSettingsRequestObject) (gen.GetSettingsResponseObject, error) {
	s, err := h.cfg.Store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetSettings200JSONResponse(toGenSettings(s)), nil
}

func (h *handlers) PutSettings(ctx context.Context, req gen.PutSettingsRequestObject) (gen.PutSettingsResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, "invalid_body", "")
	}
	next := fromGenSettings(*req.Body)
	if next.JiraSiteID != nil {
		tok, _, err := h.cfg.OAuth.AtlassianAccount()
		if err != nil {
			return nil, err
		}
		if !hasSite(tok.Sites, *next.JiraSiteID) {
			return nil, problem(http.StatusUnprocessableEntity, codeUnknownSite, "the site is not reachable with the connected Atlassian account")
		}
	}
	prev, err := h.cfg.Store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.cfg.Store.PutSettings(ctx, next); err != nil {
		return nil, err
	}
	saved, err := h.cfg.Store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if sameSite(prev.JiraSiteID, saved.JiraSiteID) {
		h.reschedule()
	} else {
		h.kick()
	}
	if !slices.Equal(prev.ExcludedStatuses, saved.ExcludedStatuses) && h.cfg.Poller != nil {
		// The hints and plans on the dashboard depend on the excluded statuses, so evaluate again from cached data.
		if err := h.cfg.Poller.Reevaluate(ctx); err != nil {
			return nil, err
		}
	}
	return gen.PutSettings200JSONResponse(toGenSettings(saved)), nil
}

func sameSite(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
