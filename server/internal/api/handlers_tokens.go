package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
)

// Problem codes of the list of GitHub tokens.
const (
	codeTokenLimit     = "token_limit"
	codeTokenDuplicate = "token_duplicate"
	codeTokenNameTaken = "token_name_taken"
)

// githubToken is the API view of a stored token: everything but the token itself.
func githubToken(t auth.GitHubToken) gen.GithubToken {
	out := gen.GithubToken{
		Id: t.ID, Name: t.Name, Method: gen.GithubTokenMethod(t.Method), FineGrained: t.FineGrained,
		Login: t.Login, NeedsReconnect: t.NeedsReconnect,
	}
	if t.Warning != "" {
		out.Warning = &t.Warning
	}
	return out
}

// tokenProblem maps the errors of the list of tokens to problems, and anything else to
// githubProblem.
func tokenProblem(err error) error {
	var dup *auth.DuplicateError
	switch {
	case errors.Is(err, auth.ErrTokenLimit):
		return problem(http.StatusConflict, codeTokenLimit,
			"At most 10 GitHub tokens can be connected. Remove one before adding another.")
	case errors.As(err, &dup):
		return problem(http.StatusConflict, codeTokenDuplicate, "This token is already added as \""+dup.Name+"\".")
	case errors.Is(err, auth.ErrTokenNameTaken):
		return problem(http.StatusConflict, codeTokenNameTaken, "Another GitHub token already has this name.")
	case errors.Is(err, auth.ErrTokenName):
		return problem(http.StatusUnprocessableEntity, mw.CodeInvalidBody, "The name must be 1 to 40 characters, without control characters.")
	case errors.Is(err, auth.ErrTokenNotFound):
		return problem(http.StatusNotFound, mw.CodeNotFound, "No such GitHub token.")
	}
	return githubProblem(err)
}

// addPAT validates and stores a personal access token with the optional name.
func (h *handlers) addPAT(ctx context.Context, token string, name *string) (auth.GitHubToken, error) {
	given := ""
	if name != nil {
		if given = strings.TrimSpace(*name); given == "" {
			return auth.GitHubToken{}, auth.ErrTokenName
		}
	}
	tok, err := h.cfg.OAuth.AddPAT(ctx, strings.TrimSpace(token), given)
	if err != nil {
		return auth.GitHubToken{}, err
	}
	h.kick()
	return tok, nil
}

func (h *handlers) PutGithubPat(ctx context.Context, req gen.PutGithubPatRequestObject) (gen.PutGithubPatResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	if _, err := h.addPAT(ctx, req.Body.Token, req.Body.Name); err != nil {
		return nil, tokenProblem(err)
	}
	summary, err := h.githubSummary()
	if err != nil {
		return nil, err
	}
	return gen.PutGithubPat200JSONResponse(summary), nil
}

func (h *handlers) AddGithubToken(ctx context.Context, req gen.AddGithubTokenRequestObject) (gen.AddGithubTokenResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	tok, err := h.addPAT(ctx, req.Body.Token, req.Body.Name)
	if err != nil {
		return nil, tokenProblem(err)
	}
	return gen.AddGithubToken201JSONResponse(githubToken(tok)), nil
}

func (h *handlers) RenameGithubToken(_ context.Context, req gen.RenameGithubTokenRequestObject) (gen.RenameGithubTokenResponseObject, error) {
	if req.Body == nil {
		return nil, problem(http.StatusBadRequest, mw.CodeInvalidBody, "")
	}
	tok, err := h.cfg.OAuth.RenameGitHubToken(req.Id, req.Body.Name)
	if err != nil {
		return nil, tokenProblem(err)
	}
	// Names show in the snapshot (tokenName), so the next refresh carries the new one.
	h.kick()
	return gen.RenameGithubToken200JSONResponse(githubToken(tok)), nil
}

func (h *handlers) DeleteGithubToken(_ context.Context, req gen.DeleteGithubTokenRequestObject) (gen.DeleteGithubTokenResponseObject, error) {
	if _, err := h.cfg.OAuth.RemoveGitHubToken(req.Id); err != nil {
		return nil, tokenProblem(err)
	}
	h.kick()
	return gen.DeleteGithubToken204Response{}, nil
}

func (h *handlers) CheckGithubToken(ctx context.Context, req gen.CheckGithubTokenRequestObject) (gen.CheckGithubTokenResponseObject, error) {
	if req.Id == "" {
		return nil, tokenProblem(auth.ErrTokenNotFound)
	}
	if _, err := h.cfg.OAuth.CheckGitHubToken(ctx, req.Id); err != nil && !errors.Is(err, auth.ErrNeedsReconnect) {
		return nil, tokenProblem(err)
	}
	a, err := h.accounts(ctx)
	if err != nil {
		return nil, err
	}
	return gen.CheckGithubToken200JSONResponse(a), nil
}
