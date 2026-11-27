package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

func (h *handlers) RevokeAllSessions(ctx context.Context, _ gen.RevokeAllSessionsRequestObject) (gen.RevokeAllSessionsResponseObject, error) {
	if err := h.cfg.Sessions.RevokeAll(ctx); err != nil {
		return nil, err
	}
	return gen.RevokeAllSessions204Response{
		Headers: gen.RevokeAllSessions204ResponseHeaders{SetCookie: auth.ExpiredCookie().String()},
	}, nil
}

func (h *handlers) GetSetup(_ context.Context, _ gen.GetSetupRequestObject) (gen.GetSetupResponseObject, error) {
	o := h.cfg.OAuth
	gh := gen.ProviderSetup{Configured: o.ClientConfigured(auth.GitHub), CallbackUrl: o.CallbackURL(auth.GitHub)}
	if u := o.InstallURL(); u != "" {
		gh.InstallUrl = &u
	}
	return gen.GetSetup200JSONResponse{
		Github:    gh,
		Atlassian: gen.ProviderSetup{Configured: o.ClientConfigured(auth.Atlassian), CallbackUrl: o.CallbackURL(auth.Atlassian)},
	}, nil
}

func (h *handlers) PutSetupClient(_ context.Context, req gen.PutSetupClientRequestObject) (gen.PutSetupClientResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	id, secret := strings.TrimSpace(req.Body.ClientId), strings.TrimSpace(req.Body.ClientSecret)
	if id == "" || secret == "" {
		return nil, problem(http.StatusUnprocessableEntity, mw.CodeInvalidBody, "client id and secret must not be blank")
	}
	p, _ := auth.ParseProvider(string(req.Provider))
	cred := auth.ClientCredentials{ClientID: id, ClientSecret: secret}
	if req.Body.AppSlug != nil {
		cred.AppSlug = strings.TrimSpace(*req.Body.AppSlug)
	}
	if err := h.cfg.OAuth.SetClient(p, cred); err != nil {
		if errors.Is(err, auth.ErrInvalidSlug) {
			return nil, problem(http.StatusUnprocessableEntity, mw.CodeInvalidBody, "the app slug is the lowercase name in the app's public link")
		}
		return nil, err
	}
	return gen.PutSetupClient204Response{}, nil
}

// githubProblem maps errors of the GitHub client and the PAT check to problems.
func githubProblem(err error) error {
	var scope *auth.ScopeError
	var blocked *github.OrgBlocked
	switch {
	case errors.Is(err, github.ErrBadCredentials):
		return problem(http.StatusUnprocessableEntity, codeInvalidToken, "GitHub rejected the token")
	case errors.As(err, &scope):
		return problem(http.StatusUnprocessableEntity, codeInsufficientScope, "the token needs the scopes: "+strings.Join(scope.Missing, ", "))
	case errors.As(err, &blocked):
		detail := "an organization blocked access to the token"
		if blocked.Org != "" {
			detail = "organization " + blocked.Org + " blocked access to the token"
		}
		return problem(http.StatusForbidden, codeOrgBlocked, blockedDetail(blocked, detail))
	case errors.Is(err, auth.ErrNotConnected):
		return problem(http.StatusConflict, codeNotConnected, "")
	}
	return problem(http.StatusBadGateway, codeUpstream, "GitHub could not be reached")
}

// githubSummary is the GitHub connection as one account: connected when any token is stored, the
// login, method and warning of the primary (first) token, and needsReconnect when any token needs
// it.
func (h *handlers) githubSummary() (gen.GithubAccount, error) {
	list, err := h.cfg.OAuth.GitHubTokens()
	if err != nil {
		return gen.GithubAccount{}, err
	}
	return githubAccount(list), nil
}

func githubAccount(list []auth.GitHubToken) gen.GithubAccount {
	if len(list) == 0 {
		return gen.GithubAccount{}
	}
	t := list[0]
	out := gen.GithubAccount{Connected: true, Login: &t.Login}
	if m := gen.GithubAccountMethod(t.Method); m.Valid() {
		out.Method = &m
	}
	if t.Warning != "" {
		out.Warning = &t.Warning
	}
	if slices.ContainsFunc(list, func(t auth.GitHubToken) bool { return t.NeedsReconnect }) {
		reconnect := true
		out.NeedsReconnect = &reconnect
	}
	return out
}

func hasSite(sites []auth.Site, id string) bool {
	for _, s := range sites {
		if s.ID == id {
			return true
		}
	}
	return false
}

func (h *handlers) accounts(ctx context.Context) (gen.Accounts, error) {
	var out gen.Accounts
	tokens, err := h.cfg.OAuth.GitHubTokens()
	if err != nil {
		return out, err
	}
	out.Github = githubAccount(tokens)
	out.GithubTokens = make([]gen.GithubToken, len(tokens))
	for i, t := range tokens {
		out.GithubTokens[i] = githubToken(t)
	}
	out.GithubTokenLimit = auth.MaxGitHubTokens

	atl, connected, err := h.cfg.OAuth.AtlassianAccount()
	if err != nil {
		return out, err
	}
	settings, err := h.cfg.Store.GetSettings(ctx)
	if err != nil {
		return out, err
	}
	out.Jira = gen.JiraAccount{
		Connected:      connected && !atl.NeedsReconnect,
		NeedsReconnect: connected && atl.NeedsReconnect,
		Sites:          make([]gen.JiraSite, 0, len(atl.Sites)),
	}
	for _, s := range atl.Sites {
		out.Jira.Sites = append(out.Jira.Sites, gen.JiraSite{Id: s.ID, Name: s.Name, Url: s.URL})
	}
	switch {
	case settings.JiraSiteID != nil && hasSite(atl.Sites, *settings.JiraSiteID):
		out.Jira.SelectedSiteId = settings.JiraSiteID
	case len(atl.Sites) == 1:
		out.Jira.SelectedSiteId = &atl.Sites[0].ID
	}
	return out, nil
}

func (h *handlers) GetAccounts(ctx context.Context, _ gen.GetAccountsRequestObject) (gen.GetAccountsResponseObject, error) {
	a, err := h.accounts(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetAccounts200JSONResponse(a), nil
}

func (h *handlers) DisconnectAccount(ctx context.Context, req gen.DisconnectAccountRequestObject) (gen.DisconnectAccountResponseObject, error) {
	p, _ := auth.ParseProvider(string(req.Provider))
	if err := h.cfg.OAuth.Disconnect(p); err != nil {
		return nil, err
	}
	if p == auth.Atlassian {
		s, err := h.cfg.Store.GetSettings(ctx)
		if err != nil {
			return nil, err
		}
		if s.JiraSiteID != nil {
			s.JiraSiteID = nil
			if err := h.cfg.Store.PutSettings(ctx, s); err != nil {
				return nil, err
			}
		}
	}
	h.kick()
	return gen.DisconnectAccount204Response{}, nil
}

func (h *handlers) CheckAccount(ctx context.Context, req gen.CheckAccountRequestObject) (gen.CheckAccountResponseObject, error) {
	var err error
	if p, _ := auth.ParseProvider(string(req.Provider)); p == auth.GitHub {
		_, err = h.cfg.OAuth.CheckGitHub(ctx)
		if err != nil && !errors.Is(err, auth.ErrNeedsReconnect) {
			return nil, githubProblem(err)
		}
	} else if _, err = h.cfg.OAuth.CheckAtlassian(ctx); err != nil && !errors.Is(err, auth.ErrNeedsReconnect) {
		if errors.Is(err, auth.ErrNotConnected) {
			return nil, problem(http.StatusConflict, codeNotConnected, "")
		}
		h.cfg.Log.ErrorContext(ctx, "check atlassian connection", "err", err)
		return nil, problem(http.StatusBadGateway, codeUpstream, "Atlassian could not be reached")
	}
	a, err := h.accounts(ctx)
	if err != nil {
		return nil, err
	}
	return gen.CheckAccount200JSONResponse(a), nil
}
