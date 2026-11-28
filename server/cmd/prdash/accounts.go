package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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

// GitHubTokens lists the connected GitHub credentials, primary first, without the token values.
// Nothing is sent to GitHub.
func (a accounts) GitHubTokens(context.Context) ([]auth.GitHubCredential, error) {
	list, err := a.oauth.GitHubTokens()
	if err != nil {
		return nil, err
	}
	out := make([]auth.GitHubCredential, len(list))
	for i, t := range list {
		out[i] = auth.GitHubCredential{ID: t.ID, Name: t.Name, Login: t.Login, Method: t.Method}
	}
	return out, nil
}

// GitHubToken returns a token that is valid now (refreshed when due). The merge and review actions
// ask for the token of a repository as the snapshot chose it; a snapshot stored before several
// tokens, and a token removed since, have no usable id and get the primary token.
func (a accounts) GitHubToken(ctx context.Context, id string) (auth.GitHubCredential, error) {
	cred, err := a.oauth.GitHubAccess(ctx, id)
	if errors.Is(err, auth.ErrTokenNotFound) {
		return a.oauth.GitHubAccess(ctx, "")
	}
	return cred, err
}

// GitHubRejected flags the credential GitHub rejected, so the Accounts dialog shows it.
func (a accounts) GitHubRejected(ctx context.Context, id string) {
	if err := a.oauth.MarkGitHubRejected(id); err != nil {
		slog.WarnContext(ctx, "mark github token rejected", "err", err)
	}
}

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
