package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type atlassianTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
}

func (o *OAuth) completeAtlassian(ctx context.Context, cred ClientCredentials, code string) error {
	status, body, err := o.postJSON(ctx, o.cfg.Endpoints.AtlassianToken, map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     cred.ClientID,
		"client_secret": cred.ClientSecret,
		"code":          code,
		"redirect_uri":  o.CallbackURL(Atlassian),
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrExchange, err)
	}
	var tok atlassianTokenResponse
	if status != http.StatusOK || json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" || tok.RefreshToken == "" {
		return fmt.Errorf("%w: atlassian answered %d %s", ErrExchange, status, tok.Error)
	}
	sites, err := o.accessibleSites(ctx, tok.AccessToken)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrExchange, err)
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	return saveJSON(o.cfg.Secrets, Atlassian.tokenItem(), AtlassianToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    o.cfg.Clock().Add(time.Duration(tok.ExpiresIn) * time.Second),
		Sites:        sites,
	})
}

// AtlassianAccount returns the stored Atlassian connection. Nothing is sent to Atlassian.
func (o *OAuth) AtlassianAccount() (AtlassianToken, bool, error) {
	t, err := loadJSON[AtlassianToken](o.cfg.Secrets, Atlassian.tokenItem())
	if errors.Is(err, ErrNotConnected) {
		return AtlassianToken{}, false, nil
	}
	return t, err == nil, err
}

// AtlassianAccess returns an access token that is valid for at least RefreshSkew, refreshing it
// when needed. Callers that find the token expired queue on one mutex, and each re-reads the
// stored token after getting it, so exactly one of them spends the refresh token. The rotated
// refresh token is stored before the new access token is returned. A refresh that Atlassian
// rejects marks the connection as needing a reconnect.
func (o *OAuth) AtlassianAccess(ctx context.Context) (string, error) {
	tok, err := loadJSON[AtlassianToken](o.cfg.Secrets, Atlassian.tokenItem())
	if err != nil {
		return "", err
	}
	if usable, err := o.usable(tok); usable || err != nil {
		return tok.AccessToken, err
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	if tok, err = loadJSON[AtlassianToken](o.cfg.Secrets, Atlassian.tokenItem()); err != nil {
		return "", err
	}
	if usable, err := o.usable(tok); usable || err != nil {
		return tok.AccessToken, err
	}
	return o.refresh(ctx, tok)
}

func (o *OAuth) usable(tok AtlassianToken) (bool, error) {
	if tok.NeedsReconnect {
		return false, ErrNeedsReconnect
	}
	return o.cfg.Clock().Add(RefreshSkew).Before(tok.ExpiresAt), nil
}

// refresh must run with tokenMu held.
func (o *OAuth) refresh(ctx context.Context, tok AtlassianToken) (string, error) {
	cred, err := o.client(Atlassian)
	if err != nil {
		return "", err
	}
	status, body, err := o.postJSON(ctx, o.cfg.Endpoints.AtlassianToken, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     cred.ClientID,
		"client_secret": cred.ClientSecret,
		"refresh_token": tok.RefreshToken,
	})
	if err != nil {
		return "", fmt.Errorf("refresh atlassian token: %w", err)
	}
	var out atlassianTokenResponse
	_ = json.Unmarshal(body, &out)
	if rejected(status, out.Error) {
		tok.NeedsReconnect = true
		if err := saveJSON(o.cfg.Secrets, Atlassian.tokenItem(), tok); err != nil {
			return "", err
		}
		return "", ErrNeedsReconnect
	}
	if status != http.StatusOK || out.AccessToken == "" {
		return "", fmt.Errorf("refresh atlassian token: answered %d", status)
	}
	tok.AccessToken = out.AccessToken
	if out.RefreshToken != "" {
		tok.RefreshToken = out.RefreshToken
	}
	tok.ExpiresAt = o.cfg.Clock().Add(time.Duration(out.ExpiresIn) * time.Second)
	if err := saveJSON(o.cfg.Secrets, Atlassian.tokenItem(), tok); err != nil {
		return "", fmt.Errorf("store rotated atlassian token: %w", err)
	}
	return tok.AccessToken, nil
}

// rejected tells a refresh token that Atlassian will never accept again (400, 401, or 403 with
// invalid_grant) from a transient failure.
func rejected(status int, code string) bool {
	return status == http.StatusBadRequest || status == http.StatusUnauthorized ||
		(status == http.StatusForbidden && code == "invalid_grant")
}

// CheckAtlassian refreshes the access token when needed and reloads the site list.
func (o *OAuth) CheckAtlassian(ctx context.Context) (AtlassianToken, error) {
	access, err := o.AtlassianAccess(ctx)
	if err != nil {
		tok, _, _ := o.AtlassianAccount()
		return tok, err
	}
	sites, err := o.accessibleSites(ctx, access)
	if err != nil {
		return AtlassianToken{}, err
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	tok, err := loadJSON[AtlassianToken](o.cfg.Secrets, Atlassian.tokenItem())
	if err != nil {
		return tok, err
	}
	tok.Sites = sites
	return tok, saveJSON(o.cfg.Secrets, Atlassian.tokenItem(), tok)
}
