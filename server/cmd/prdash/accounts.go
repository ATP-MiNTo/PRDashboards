package main

import (
	"context"
	"fmt"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

// accounts tells the poller and the handlers which tokens and which Jira site to use.
type accounts struct {
	oauth *auth.OAuth
	store *store.Store
}

var _ poller.Accounts = accounts{}

func (a accounts) GitHubToken(ctx context.Context) (string, error) { return a.oauth.GitHubAccess(ctx) }

// JiraSite returns the selected site, or the only one the account can reach.
func (a accounts) JiraSite(ctx context.Context) (jira.Site, error) {
	tok, connected, err := a.oauth.AtlassianAccount()
	switch {
	case err != nil:
		return jira.Site{}, err
	case !connected:
		return jira.Site{}, auth.ErrNotConnected
	case tok.NeedsReconnect:
		return jira.Site{}, auth.ErrNeedsReconnect
	}
	settings, err := a.store.GetSettings(ctx)
	if err != nil {
		return jira.Site{}, fmt.Errorf("read settings: %w", err)
	}
	for _, s := range tok.Sites {
		if (settings.JiraSiteID != nil && s.ID == *settings.JiraSiteID) || (settings.JiraSiteID == nil && len(tok.Sites) == 1) {
			return jira.Site{ID: s.ID, URL: s.URL}, nil
		}
	}
	return jira.Site{}, poller.ErrNoSite
}
