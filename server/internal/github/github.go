// Package github is the GitHub API client. Milestone 3 has the viewer check and the mapping of
// blocked organizations. It makes outbound calls only through the netguard client it is given.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
)

// DefaultAPIBase is the GraphQL host.
const DefaultAPIBase = "https://api.github.com"

const (
	maxBody   = 1 << 20
	userAgent = "prdash"
	// apiVersion is sent with every request, so GitHub keeps the response shapes this client parses.
	apiVersion = "2022-11-28"
)

// ErrBadCredentials means GitHub rejected the token (401).
var ErrBadCredentials = errors.New("github rejected the token")

// Client talks to GitHub through HTTP, which must be a netguard client.
type Client struct {
	HTTP    *http.Client
	APIBase string
	Backoff backoff.Policy
	// Clock defaults to time.Now.
	Clock func() time.Time
}

func (c *Client) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

func (c *Client) base() string {
	if c.APIBase != "" {
		return c.APIBase
	}
	return DefaultAPIBase
}

// Viewer is the authenticated user and the token's scopes.
type Viewer struct {
	Login string
	// Classic is true when GitHub sent X-OAuth-Scopes, which classic tokens and OAuth apps get.
	// Fine-grained tokens have no scopes header.
	Classic bool
	Scopes  []string
}

// HasScope reports whether the token grants scope, directly or through a broader one.
func (v Viewer) HasScope(scope string) bool {
	implied := map[string][]string{"read:org": {"write:org", "admin:org"}}
	for _, s := range v.Scopes {
		if s == scope {
			return true
		}
		for _, broader := range implied[scope] {
			if s == broader {
				return true
			}
		}
	}
	return false
}

type graphQLResponse struct {
	Data struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// Viewer runs `viewer { login }` with token and reads the scopes header. A blocked organization is
// returned as *OrgBlocked, and a rejected token as ErrBadCredentials.
func (c *Client) Viewer(ctx context.Context, token string) (Viewer, error) {
	resp, err := c.Backoff.Do(ctx, func(ctx context.Context) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/graphql",
			strings.NewReader(`{"query":"query{viewer{login}}"}`))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		return c.HTTP.Do(req)
	})
	if err != nil {
		return Viewer{}, fmt.Errorf("github viewer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Viewer{}, fmt.Errorf("github viewer: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return Viewer{}, ErrBadCredentials
	}
	var parsed graphQLResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&parsed); err != nil {
			return Viewer{}, fmt.Errorf("github viewer: bad response: %w", err)
		}
		if parsed.Data.Viewer.Login != "" {
			return viewerFrom(parsed.Data.Viewer.Login, resp.Header), nil
		}
	}
	if blocked := Classify(resp.StatusCode, resp.Header, body); blocked != nil {
		return Viewer{}, blocked
	}
	if resp.StatusCode != http.StatusOK {
		return Viewer{}, fmt.Errorf("github viewer: unexpected status %d", resp.StatusCode)
	}
	return Viewer{}, errors.New("github viewer: response has no login")
}

func viewerFrom(login string, h http.Header) Viewer {
	v := Viewer{Login: login}
	raw, ok := h["X-Oauth-Scopes"]
	if !ok {
		return v
	}
	v.Classic = true
	for _, part := range strings.Split(strings.Join(raw, ","), ",") {
		if s := strings.TrimSpace(part); s != "" {
			v.Scopes = append(v.Scopes, s)
		}
	}
	return v
}

// OrgBlocked says an organization refused the token, usually because SAML single sign-on has not
// been authorized for it. AuthorizeURL is empty when GitHub did not say which organization.
type OrgBlocked struct {
	Org          string
	AuthorizeURL string
}

func (e *OrgBlocked) Error() string {
	if e.Org == "" {
		return "github: an organization blocked access to the token"
	}
	return "github: organization " + e.Org + " blocked access to the token"
}

type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

var (
	orgName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	quotedName = regexp.MustCompile("`([A-Za-z0-9][A-Za-z0-9-]{0,38})`")
)

// Classify maps an API answer to an OrgBlocked, or nil when the answer is not an organization
// block. It looks at the X-GitHub-SSO header ("required; url=...") and at GraphQL errors of type
// FORBIDDEN, which GitHub uses for SAML enforcement and token policies.
func Classify(status int, h http.Header, body []byte) *OrgBlocked {
	if v := h.Get("X-GitHub-SSO"); strings.HasPrefix(v, "required") {
		if org, authorize, ok := parseSSOHeader(v); ok {
			return &OrgBlocked{Org: org, AuthorizeURL: authorize}
		}
		return &OrgBlocked{}
	}
	var parsed struct {
		Errors []graphQLError `json:"errors"`
	}
	if (status != http.StatusOK && status != http.StatusForbidden) || json.Unmarshal(body, &parsed) != nil {
		return nil
	}
	for _, e := range parsed.Errors {
		if e.Type != "FORBIDDEN" {
			continue
		}
		blocked := &OrgBlocked{}
		if m := quotedName.FindStringSubmatch(e.Message); m != nil {
			blocked.Org = m[1]
			blocked.AuthorizeURL = "https://github.com/orgs/" + m[1] + "/sso"
		}
		return blocked
	}
	return nil
}

// parseSSOHeader reads `required; url=https://github.com/orgs/acme/sso?authorization_request=...`.
// The URL is accepted only when it is https on github.com under /orgs/<org>/sso, because the UI
// links to it.
func parseSSOHeader(v string) (org, authorize string, ok bool) {
	_, rest, found := strings.Cut(v, "url=")
	if !found {
		return "", "", false
	}
	raw, _, _ := strings.Cut(rest, ";")
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "orgs" || parts[2] != "sso" || !orgName.MatchString(parts[1]) {
		return "", "", false
	}
	return parts[1], u.String(), true
}
