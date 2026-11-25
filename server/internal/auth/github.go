package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type githubTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// expiry is the expiry time of a token answer, or the zero time when the token does not expire.
func (o *OAuth) expiry(expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return o.cfg.Clock().Add(time.Duration(expiresIn) * time.Second)
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
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	return saveJSON(o.cfg.Secrets, GitHub.tokenItem(), GitHubToken{
		Token: tok.AccessToken, Method: MethodApp, Login: viewer.Login,
		RefreshToken: tok.RefreshToken, ExpiresAt: o.expiry(tok.ExpiresIn),
	})
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
	rec := GitHubToken{Token: token, Method: MethodPAT, Login: viewer.Login}
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
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
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

// GitHubAccess returns a GitHub token that is valid for at least RefreshSkew, refreshing a GitHub
// App user token when needed. Tokens without an expiry (personal access tokens, tokens of the
// former OAuth App, GitHub Apps with expiry turned off) are returned as stored. Callers that find
// the token expired queue on tokenMu and re-read the stored token, so one of them spends the
// refresh token. The rotated refresh token is stored before the new access token is returned. A
// refresh that GitHub rejects marks the connection as needing a reconnect. Other failures keep the
// stored token.
func (o *OAuth) GitHubAccess(ctx context.Context) (string, error) {
	tok, err := loadJSON[GitHubToken](o.cfg.Secrets, GitHub.tokenItem())
	if err != nil {
		return "", err
	}
	if usable, err := o.githubUsable(tok); usable || err != nil {
		return tok.Token, err
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	if tok, err = loadJSON[GitHubToken](o.cfg.Secrets, GitHub.tokenItem()); err != nil {
		return "", err
	}
	if usable, err := o.githubUsable(tok); usable || err != nil {
		return tok.Token, err
	}
	return o.refreshGitHub(ctx, tok)
}

func (o *OAuth) githubUsable(tok GitHubToken) (bool, error) {
	if tok.NeedsReconnect {
		return false, ErrNeedsReconnect
	}
	return tok.ExpiresAt.IsZero() || o.cfg.Clock().Add(RefreshSkew).Before(tok.ExpiresAt), nil
}

// refreshGitHub must run with tokenMu held.
func (o *OAuth) refreshGitHub(ctx context.Context, tok GitHubToken) (string, error) {
	cred, err := o.client(GitHub)
	if err != nil {
		return "", err
	}
	status, body, err := o.postForm(ctx, o.cfg.Endpoints.GitHubToken, url.Values{
		"client_id":     {cred.ClientID},
		"client_secret": {cred.ClientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
	})
	if err != nil {
		return "", fmt.Errorf("refresh github token: %w", err)
	}
	var out githubTokenResponse
	_ = json.Unmarshal(body, &out)
	// GitHub reports a bad refresh token with status 200 and an error in the body.
	if out.Error == "bad_refresh_token" || status == http.StatusBadRequest || status == http.StatusUnauthorized {
		tok.NeedsReconnect = true
		if err := saveJSON(o.cfg.Secrets, GitHub.tokenItem(), tok); err != nil {
			return "", err
		}
		return "", ErrNeedsReconnect
	}
	if status != http.StatusOK || out.Error != "" || out.AccessToken == "" {
		return "", fmt.Errorf("refresh github token: answered %d %s", status, out.Error)
	}
	tok.Token = out.AccessToken
	if out.RefreshToken != "" {
		tok.RefreshToken = out.RefreshToken
	}
	tok.ExpiresAt = o.expiry(out.ExpiresIn)
	if err := saveJSON(o.cfg.Secrets, GitHub.tokenItem(), tok); err != nil {
		return "", fmt.Errorf("store rotated github token: %w", err)
	}
	return tok.Token, nil
}

// CheckGitHub asks GitHub who the stored token belongs to and updates the stored login. It
// refreshes the token first when it is due.
func (o *OAuth) CheckGitHub(ctx context.Context) (GitHubToken, error) {
	access, err := o.GitHubAccess(ctx)
	if err != nil {
		t, _, _ := o.GitHubAccount()
		return t, err
	}
	viewer, err := o.cfg.GitHub.Viewer(ctx, access)
	if err != nil {
		t, _, _ := o.GitHubAccount()
		return t, err
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	t, err := loadJSON[GitHubToken](o.cfg.Secrets, GitHub.tokenItem())
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
