package auth

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
)

// Provider is an OAuth provider.
type Provider string

// Providers.
const (
	GitHub    Provider = "github"
	Atlassian Provider = "atlassian"
)

// ParseProvider converts a path segment to a Provider.
func ParseProvider(s string) (Provider, bool) {
	switch p := Provider(s); p {
	case GitHub, Atlassian:
		return p, true
	}
	return "", false
}

func (p Provider) clientItem() string {
	if p == GitHub {
		return secrets.GitHubClient
	}
	return secrets.AtlassianClient
}

func (p Provider) tokenItem() string {
	if p == GitHub {
		return secrets.GitHubToken
	}
	return secrets.AtlassianToken
}

// Errors of the OAuth service.
var (
	ErrNotConfigured     = errors.New("client credentials are not configured")
	ErrInvalidState      = errors.New("invalid or expired OAuth state")
	ErrExchange          = errors.New("token exchange failed")
	ErrNotConnected      = errors.New("not connected")
	ErrNeedsReconnect    = errors.New("the connection needs to be authorized again")
	ErrInsufficientScope = errors.New("token lacks required scopes")
)

// ClientCredentials are the OAuth app's client id and secret.
type ClientCredentials struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// GitHubToken is the item github.token.
type GitHubToken struct {
	Token   string `json:"token"`
	Method  string `json:"method"` // "oauth" or "pat"
	Login   string `json:"login"`
	Warning string `json:"warning,omitempty"`
}

// Site is a Jira site the Atlassian token can reach.
type Site struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// AtlassianToken is the item atlassian.token: access and refresh tokens, expiry and the sites.
type AtlassianToken struct {
	AccessToken    string    `json:"accessToken"`
	RefreshToken   string    `json:"refreshToken"`
	ExpiresAt      time.Time `json:"expiresAt"`
	Sites          []Site    `json:"sites"`
	NeedsReconnect bool      `json:"needsReconnect,omitempty"`
}

func loadJSON[T any](s secrets.Store, name string) (T, error) {
	var v T
	raw, err := s.Get(name)
	if errors.Is(err, secrets.ErrNotFound) {
		return v, ErrNotConnected
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(raw, &v)
	return v, err
}

func saveJSON(s secrets.Store, name string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Set(name, raw)
}

// validSite accepts only https Jira Cloud sites, because the UI links to them.
func validSite(s Site) bool {
	u, err := url.Parse(s.URL)
	if err != nil || s.ID == "" || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return strings.HasSuffix(host, ".atlassian.net") && len(host) > len(".atlassian.net")
}
