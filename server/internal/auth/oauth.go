package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

const (
	// StateTTL is how long a started authorization stays valid.
	StateTTL = 10 * time.Minute
	// RefreshSkew is how long before expiry an access token counts as expired.
	RefreshSkew = time.Minute

	atlassianScope = "read:jira-work write:jira-work read:jira-user offline_access"
	maxResponse    = 1 << 20
	userAgent      = "prdash"
)

// Endpoints are the provider URLs. The e2e build points them at the fake servers.
type Endpoints struct {
	GitHubAuthorize    string
	GitHubToken        string
	AtlassianAuthorize string
	AtlassianToken     string
	AtlassianResources string
}

// DefaultEndpoints are the real provider URLs.
func DefaultEndpoints() Endpoints {
	return Endpoints{ //nolint:gosec // public endpoint URL
		GitHubAuthorize:    "https://github.com/login/oauth/authorize",
		GitHubToken:        "https://github.com/login/oauth/access_token",
		AtlassianAuthorize: "https://auth.atlassian.com/authorize",
		AtlassianToken:     "https://auth.atlassian.com/oauth/token",
		AtlassianResources: "https://api.atlassian.com/oauth/token/accessible-resources",
	}
}

// StateStore keeps pending authorizations.
type StateStore interface {
	InsertOAuthState(ctx context.Context, v store.OAuthState, now time.Time) error
	ConsumeOAuthState(ctx context.Context, hash [sha256.Size]byte) (store.OAuthState, error)
}

// Config holds what OAuth needs. HTTP must be a netguard client.
type Config struct {
	Secrets   secrets.Store
	States    StateStore
	HTTP      *http.Client
	GitHub    *github.Client
	Endpoints Endpoints
	Backoff   backoff.Policy
	Clock     func() time.Time
	Rand      io.Reader
	// Port is the loopback port of the callback URLs.
	Port int
}

// OAuth runs the GitHub and Atlassian authorization flows, keeps the tokens in the secrets store
// and refreshes the expiring ones.
type OAuth struct {
	cfg Config

	// tokenMu serializes every read-modify-write of atlassian.token and github.token. A refresh holds it, so one
	// refresh is in flight at a time and the rotated refresh token is stored before anyone
	// uses the new access token.
	tokenMu sync.Mutex

	// verifiers holds the PKCE verifier of each pending authorization in memory only, keyed by
	// the hash of its state and spent once.
	verifierMu sync.Mutex
	verifiers  map[[sha256.Size]byte]pendingVerifier
}

type pendingVerifier struct {
	value   string
	expires time.Time
}

// NewOAuth returns the OAuth service.
func NewOAuth(cfg Config) *OAuth {
	if cfg.Endpoints == (Endpoints{}) {
		cfg.Endpoints = DefaultEndpoints()
	}
	return &OAuth{cfg: cfg, verifiers: make(map[[sha256.Size]byte]pendingVerifier)}
}

// putVerifier remembers a verifier until expires and drops the ones already expired.
func (o *OAuth) putVerifier(key [sha256.Size]byte, value string, now, expires time.Time) {
	o.verifierMu.Lock()
	defer o.verifierMu.Unlock()
	for k, v := range o.verifiers {
		if !now.Before(v.expires) {
			delete(o.verifiers, k)
		}
	}
	o.verifiers[key] = pendingVerifier{value: value, expires: expires}
}

// takeVerifier returns and forgets the verifier stored under key, when it is still valid.
func (o *OAuth) takeVerifier(key [sha256.Size]byte, now time.Time) (string, bool) {
	o.verifierMu.Lock()
	defer o.verifierMu.Unlock()
	v, ok := o.verifiers[key]
	delete(o.verifiers, key)
	return v.value, ok && now.Before(v.expires)
}

// CallbackURL is the redirect URI to register with the provider.
func (o *OAuth) CallbackURL(p Provider) string {
	return fmt.Sprintf("http://127.0.0.1:%d/oauth/%s/callback", o.cfg.Port, p)
}

// SetClient stores the client credentials. The GitHub App slug, when given, must be valid.
func (o *OAuth) SetClient(p Provider, c ClientCredentials) error {
	if p != GitHub {
		c.AppSlug = ""
	}
	if c.AppSlug != "" && !ValidSlug(c.AppSlug) {
		return ErrInvalidSlug
	}
	return saveJSON(o.cfg.Secrets, p.clientItem(), c)
}

// ClientConfigured reports whether client credentials are stored for p.
func (o *OAuth) ClientConfigured(p Provider) bool {
	_, err := o.client(p)
	return err == nil
}

// InstallURL is the link where an org owner picks the repositories for the GitHub App. It is built
// only from a validated slug and is empty when no slug is stored.
func (o *OAuth) InstallURL() string {
	c, err := o.client(GitHub)
	if err != nil || !ValidSlug(c.AppSlug) {
		return ""
	}
	return "https://github.com/apps/" + c.AppSlug + "/installations/new"
}

func (o *OAuth) client(p Provider) (ClientCredentials, error) {
	c, err := loadJSON[ClientCredentials](o.cfg.Secrets, p.clientItem())
	if errors.Is(err, ErrNotConnected) || (err == nil && (c.ClientID == "" || c.ClientSecret == "")) {
		return c, ErrNotConfigured
	}
	return c, err
}

func (o *OAuth) random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(o.cfg.Rand, b); err != nil {
		return "", err
	}
	return b64.EncodeToString(b), nil
}

// Start begins an authorization for the session and returns the provider URL to redirect to. The
// state is 256 random bits, stored only as a hash, bound to the session and valid for 10 minutes.
// Both providers get a PKCE S256 challenge; the verifier stays in memory and is never written to
// the database. A GitHub App takes its permissions from its settings,
// so no scope is sent.
func (o *OAuth) Start(ctx context.Context, p Provider, sessionKey string) (string, error) {
	cred, err := o.client(p)
	if err != nil {
		return "", err
	}
	state, err := o.random(32)
	if err != nil {
		return "", err
	}
	now := o.cfg.Clock()
	row := store.OAuthState{
		Hash: sha256.Sum256([]byte(state)), Provider: string(p), SessionKey: sessionKey, ExpiresAt: now.Add(StateTTL),
	}
	q := url.Values{}
	q.Set("client_id", cred.ClientID)
	q.Set("redirect_uri", o.CallbackURL(p))
	q.Set("state", state)
	verifier, err := o.random(32)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	q.Set("code_challenge", b64.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	var base string
	switch p {
	case GitHub:
		base = o.cfg.Endpoints.GitHubAuthorize
	case Atlassian:
		q.Set("audience", "api.atlassian.com")
		q.Set("scope", atlassianScope)
		q.Set("prompt", "consent")
		q.Set("response_type", "code")
		base = o.cfg.Endpoints.AtlassianAuthorize
	}
	if err := o.cfg.States.InsertOAuthState(ctx, row, now); err != nil {
		return "", fmt.Errorf("store oauth state: %w", err)
	}
	o.putVerifier(row.Hash, verifier, now, row.ExpiresAt)
	return base + "?" + strings.ReplaceAll(q.Encode(), "+", "%20"), nil
}

// Complete finishes an authorization. The state is spent first, whatever the outcome, and must
// belong to the same provider and session and be unexpired. Then the code is exchanged and the
// tokens are stored.
func (o *OAuth) Complete(ctx context.Context, p Provider, code, state, sessionKey string) error {
	hash := sha256.Sum256([]byte(state))
	row, err := o.cfg.States.ConsumeOAuthState(ctx, hash)
	verifier, haveVerifier := o.takeVerifier(hash, o.cfg.Clock())
	if errors.Is(err, store.ErrNotFound) {
		return ErrInvalidState
	}
	if err != nil {
		return err
	}
	if !haveVerifier || row.Provider != string(p) || !o.cfg.Clock().Before(row.ExpiresAt) ||
		subtle.ConstantTimeCompare([]byte(row.SessionKey), []byte(sessionKey)) != 1 {
		return ErrInvalidState
	}
	if code == "" {
		return fmt.Errorf("%w: empty code", ErrExchange)
	}
	cred, err := o.client(p)
	if err != nil {
		return err
	}
	if p == GitHub {
		return o.completeGitHub(ctx, cred, code, verifier)
	}
	return o.completeAtlassian(ctx, cred, code, verifier)
}

// Disconnect removes the stored tokens of p. The client credentials stay.
func (o *OAuth) Disconnect(p Provider) error {
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	if err := o.cfg.Secrets.Delete(p.tokenItem()); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return err
	}
	return nil
}

// send runs req and returns the status and the body, read up to 1 MiB.
func (o *OAuth) send(req *http.Request) (int, []byte, error) {
	req.Header.Set("User-Agent", userAgent)
	resp, err := o.cfg.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	return resp.StatusCode, body, err
}

func (o *OAuth) postForm(ctx context.Context, endpoint string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return o.send(req)
}

func (o *OAuth) postJSON(ctx context.Context, endpoint string, payload any) (int, []byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return o.send(req)
}

// accessibleSites lists the Jira sites the access token can reach.
func (o *OAuth) accessibleSites(ctx context.Context, access string) ([]Site, error) {
	resp, err := o.cfg.Backoff.Do(ctx, func(ctx context.Context) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.cfg.Endpoints.AtlassianResources, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		return o.cfg.HTTP.Do(req)
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("accessible-resources answered %d", resp.StatusCode)
	}
	var raw []struct {
		Site
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("accessible-resources: %w", err)
	}
	sites := make([]Site, 0, len(raw))
	for _, r := range raw {
		if validSite(r.Site) && (len(r.Scopes) == 0 || hasScope(r.Scopes, "read:jira-work")) {
			sites = append(sites, r.Site)
		}
	}
	return sites, nil
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}
