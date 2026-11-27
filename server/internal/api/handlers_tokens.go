package api

import (
	"context"
	"net/http"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
)

// The handlers of the GitHub tokens list are filled in by the commits that follow the contract.
var errTokensNotBuilt = problem(http.StatusNotImplemented, "not_implemented", "several GitHub tokens are not available yet")

func (h *handlers) AddGithubToken(context.Context, gen.AddGithubTokenRequestObject) (gen.AddGithubTokenResponseObject, error) {
	return nil, errTokensNotBuilt
}

func (h *handlers) RenameGithubToken(context.Context, gen.RenameGithubTokenRequestObject) (gen.RenameGithubTokenResponseObject, error) {
	return nil, errTokensNotBuilt
}

func (h *handlers) DeleteGithubToken(context.Context, gen.DeleteGithubTokenRequestObject) (gen.DeleteGithubTokenResponseObject, error) {
	return nil, errTokensNotBuilt
}

func (h *handlers) CheckGithubToken(context.Context, gen.CheckGithubTokenRequestObject) (gen.CheckGithubTokenResponseObject, error) {
	return nil, errTokensNotBuilt
}
