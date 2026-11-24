package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func (o *OAuth) completeGitHub(ctx context.Context, cred ClientCredentials, code, verifier string) error {
	status, body, err := o.postForm(ctx, o.cfg.Endpoints.GitHubToken, url.Values{
		"client_id":     {cred.ClientID},
		"client_secret": {cred.ClientSecret},
		"code":          {code},
		"redirect_uri":  {o.CallbackURL(GitHub)},
		"code_verifier": {verifier},
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrExchange, err)
	}
	var tok githubTokenResponse
	if status != http.StatusOK || json.Unmarshal(body, &tok) != nil || tok.Error != "" || tok.AccessToken == "" {
		return fmt.Errorf("%w: github answered %d %s", ErrExchange, status, tok.Error)
	}
	viewer, err := o.cfg.GitHub.Viewer(ctx, tok.AccessToken)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrExchange, err)
	}
	return saveJSON(o.cfg.Secrets, GitHub.tokenItem(), GitHubToken{Token: tok.AccessToken, Method: "oauth", Login: viewer.Login})
}

// FineGrainedWarning is shown for fine-grained personal access tokens.
const FineGrainedWarning = "Fine-grained tokens only see the repositories they were granted."

// ScopeError lists the scopes a classic token lacks.
type ScopeError struct{ Missing []string }

func (e *ScopeError) Error() string {
	return "token lacks required scopes: " + strings.Join(e.Missing, ", ")
}

// Is makes errors.Is(err, ErrInsufficientScope) work.
func (e *ScopeError) Is(target error) bool { return target == ErrInsufficientScope }

// SetPAT validates a personal access token with `viewer { login }` and its scopes header, then
// stores it. Classic tokens need repo and read:org. Fine-grained tokens have no scopes header and
// are accepted with a warning.
func (o *OAuth) SetPAT(ctx context.Context, token string) (GitHubToken, error) {
	viewer, err := o.cfg.GitHub.Viewer(ctx, token)
	if err != nil {
		return GitHubToken{}, err
	}
	rec := GitHubToken{Token: token, Method: "pat", Login: viewer.Login}
	if viewer.Classic {
		var missing []string
		for _, s := range []string{"repo", "read:org"} {
			if !viewer.HasScope(s) {
				missing = append(missing, s)
			}
		}
		if len(missing) > 0 {
			return GitHubToken{}, &ScopeError{Missing: missing}
		}
	} else {
		rec.Warning = FineGrainedWarning
	}
	if err := saveJSON(o.cfg.Secrets, GitHub.tokenItem(), rec); err != nil {
		return GitHubToken{}, err
	}
	return rec, nil
}

// GitHubAccount reports the stored GitHub connection. Nothing is sent to GitHub.
func (o *OAuth) GitHubAccount() (GitHubToken, bool, error) {
	t, err := loadJSON[GitHubToken](o.cfg.Secrets, GitHub.tokenItem())
	if errors.Is(err, ErrNotConnected) {
		return GitHubToken{}, false, nil
	}
	return t, err == nil, err
}

// CheckGitHub asks GitHub who the stored token belongs to and updates the stored login.
func (o *OAuth) CheckGitHub(ctx context.Context) (GitHubToken, error) {
	t, err := loadJSON[GitHubToken](o.cfg.Secrets, GitHub.tokenItem())
	if err != nil {
		return t, err
	}
	viewer, err := o.cfg.GitHub.Viewer(ctx, t.Token)
	if err != nil {
		return t, err
	}
	if viewer.Login != t.Login {
		t.Login = viewer.Login
		if err := saveJSON(o.cfg.Secrets, GitHub.tokenItem(), t); err != nil {
			return t, err
		}
	}
	return t, nil
}
