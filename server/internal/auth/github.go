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

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
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
	return o.saveSignIn(GitHubToken{
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

// AddPAT validates a personal access token with `viewer { login }` and its scopes header, then
// stores it as a new entry of the list. Classic tokens need repo and read:org. Fine-grained tokens
// have no scopes header and are accepted with a warning. name is the person's name for it, or
// empty for Token (login), made unique. It refuses an 11th token (ErrTokenLimit), a token that is
// already stored (*DuplicateError) and a name that is invalid or taken, before it calls GitHub.
func (o *OAuth) AddPAT(ctx context.Context, token, name string) (GitHubToken, error) {
	if name != "" {
		var err error
		if name, err = ValidName(name); err != nil {
			return GitHubToken{}, err
		}
	}
	list, err := o.tokens()
	if err != nil {
		return GitHubToken{}, err
	}
	if err := admit(list, token, name); err != nil {
		return GitHubToken{}, err
	}
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
		rec.FineGrained = true
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	if list, err = o.tokensLocked(); err != nil {
		return GitHubToken{}, err
	}
	// The list may have changed while GitHub answered.
	if err := admit(list, token, name); err != nil {
		return GitHubToken{}, err
	}
	if rec.ID, err = o.newID(list); err != nil {
		return GitHubToken{}, err
	}
	rec.Name = name
	if rec.Name == "" {
		rec.Name = defaultName(rec.Method, rec.Login, list)
	}
	if err := o.saveTokens(append(list, rec)); err != nil {
		return GitHubToken{}, err
	}
	return rec, nil
}

// GitHubAccount reports the primary stored GitHub token (the first of the list). Nothing is sent
// to GitHub.
func (o *OAuth) GitHubAccount() (GitHubToken, bool, error) {
	list, err := o.tokens()
	if err != nil || len(list) == 0 {
		return GitHubToken{}, false, err
	}
	return list[0], true, nil
}

// GitHubCredential is a GitHub token ready to use. Token is empty when the credential cannot be
// used (the error says why), so a caller can still name it.
type GitHubCredential struct {
	ID     string
	Name   string
	Login  string
	Method string
	Token  string
}

func credentialOf(t GitHubToken) GitHubCredential {
	return GitHubCredential{ID: t.ID, Name: t.Name, Login: t.Login, Method: t.Method, Token: t.Token}
}

// GitHubAccess returns the GitHub token with the given id that is valid for at least RefreshSkew,
// refreshing a GitHub App user token when needed. An empty id means the primary token. A missing
// id is ErrTokenNotFound, and no token at all is ErrNotConnected. Tokens without an expiry
// (personal access tokens, tokens of the former OAuth App, GitHub Apps with expiry turned off) are
// returned as stored. Callers that find the token expired queue on tokenMu and re-read the stored
// token, so one of them spends the refresh token. The rotated refresh token is stored before the
// new access token is returned. A refresh that GitHub rejects marks that token as needing a
// reconnect, the others are not touched. Other failures keep the stored token.
func (o *OAuth) GitHubAccess(ctx context.Context, id string) (GitHubCredential, error) {
	list, err := o.tokens()
	if err != nil {
		return GitHubCredential{}, err
	}
	tok, _, err := findToken(list, id)
	if err != nil {
		return GitHubCredential{}, err
	}
	if usable, err := o.githubUsable(tok); usable || err != nil {
		return credentialIf(tok, err), err
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	if list, err = o.tokensLocked(); err != nil {
		return GitHubCredential{}, err
	}
	tok, at, err := findToken(list, tok.ID)
	if err != nil {
		return GitHubCredential{}, err
	}
	if usable, err := o.githubUsable(tok); usable || err != nil {
		return credentialIf(tok, err), err
	}
	access, err := o.refreshGitHub(ctx, list, at)
	if err != nil {
		return credentialIf(tok, err), err
	}
	tok.Token = access
	return credentialOf(tok), nil
}

// credentialIf is the credential of t, without the token when err is set.
func credentialIf(t GitHubToken, err error) GitHubCredential {
	c := credentialOf(t)
	if err != nil {
		c.Token = ""
	}
	return c
}

// githubUsable says whether the token can be used as stored. A personal access token that GitHub
// rejected is flagged for the person but still tried, so a token that works again (a lifted SSO
// block, say) recovers without being added again.
func (o *OAuth) githubUsable(tok GitHubToken) (bool, error) {
	if tok.NeedsReconnect && tok.Method != MethodPAT {
		return false, ErrNeedsReconnect
	}
	return tok.ExpiresAt.IsZero() || o.cfg.Clock().Add(RefreshSkew).Before(tok.ExpiresAt), nil
}

// refreshGitHub refreshes list[at], an App token, and stores the list. It must run with tokenMu
// held. It returns the new access token.
func (o *OAuth) refreshGitHub(ctx context.Context, list []GitHubToken, at int) (string, error) {
	tok := list[at]
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
		list[at].NeedsReconnect = true
		if err := o.saveTokens(list); err != nil {
			return "", err
		}
		return "", ErrNeedsReconnect
	}
	if status != http.StatusOK || out.Error != "" || out.AccessToken == "" {
		return "", fmt.Errorf("refresh github token: answered %d %s", status, out.Error)
	}
	list[at].Token = out.AccessToken
	if out.RefreshToken != "" {
		list[at].RefreshToken = out.RefreshToken
	}
	list[at].ExpiresAt = o.expiry(out.ExpiresIn)
	if err := o.saveTokens(list); err != nil {
		return "", fmt.Errorf("store rotated github token: %w", err)
	}
	return out.AccessToken, nil
}

// CheckGitHub checks every stored token (see CheckGitHubToken) and returns the primary one. All
// tokens are checked even when one fails, so each keeps a current state; the first error is
// returned. ErrNotConnected means there is no token.
func (o *OAuth) CheckGitHub(ctx context.Context) (GitHubToken, error) {
	list, err := o.tokens()
	if err != nil {
		return GitHubToken{}, err
	}
	if len(list) == 0 {
		return GitHubToken{}, ErrNotConnected
	}
	var first error
	for _, t := range list {
		if _, err := o.CheckGitHubToken(ctx, t.ID); err != nil && first == nil {
			first = err
		}
	}
	primary, _, _ := o.GitHubAccount()
	return primary, first
}

// CheckGitHubToken asks GitHub who one stored token belongs to and updates the stored login. It
// refreshes the token first when it is due. A rejected token is flagged as needing a reconnect
// (see MarkGitHubRejected) and the error is github.ErrBadCredentials; a token that works clears
// the flag of a personal access token.
func (o *OAuth) CheckGitHubToken(ctx context.Context, id string) (GitHubToken, error) {
	cred, err := o.GitHubAccess(ctx, id)
	if err != nil {
		t, _ := o.githubToken(cred.ID)
		return t, err
	}
	viewer, err := o.cfg.GitHub.Viewer(ctx, cred.Token)
	if err != nil {
		if errors.Is(err, github.ErrBadCredentials) {
			_ = o.MarkGitHubRejected(cred.ID)
		}
		t, _ := o.githubToken(cred.ID)
		return t, err
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	list, err := o.tokensLocked()
	if err != nil {
		return GitHubToken{}, err
	}
	t, at, err := findToken(list, cred.ID)
	if err != nil {
		return GitHubToken{}, err
	}
	if viewer.Login != t.Login || (t.NeedsReconnect && t.Method == MethodPAT) {
		list[at].Login = viewer.Login
		if t.Method == MethodPAT {
			list[at].NeedsReconnect = false
		}
		if err := o.saveTokens(list); err != nil {
			return t, err
		}
		t = list[at]
	}
	return t, nil
}

// MarkGitHubRejected records that GitHub rejected the token with the given id, so the person sees
// that it needs a sign-in again (or, for a personal access token, a new one). The other tokens
// are untouched. An unknown id is not an error.
func (o *OAuth) MarkGitHubRejected(id string) error {
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	list, err := o.tokensLocked()
	if err != nil {
		return err
	}
	_, at, err := findToken(list, id)
	if err != nil || list[at].NeedsReconnect {
		return nil
	}
	list[at].NeedsReconnect = true
	return o.saveTokens(list)
}
