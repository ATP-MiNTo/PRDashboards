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
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
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
	// Log receives a WARN line for every refusal by GitHub: the operation, status, GitHub's message
	// (cut to 200 characters), the GraphQL error type and path and the accepted permissions header.
	// Never the token, the request or the full response. Nil logs nothing.
	Log *slog.Logger
}

// logRefusal logs one refusal. msg and errs come from GitHub's answer; the body is never logged.
func (c *Client) logRefusal(ctx context.Context, op string, status int, h http.Header, msg string, errs []graphQLError) {
	if c.Log == nil {
		return
	}
	args := []any{"op", op, "status", status, "message", truncate(msg, 200)}
	if v := h.Get("X-Accepted-GitHub-Permissions"); v != "" {
		args = append(args, "accepted_permissions", truncate(v, 200))
	}
	if len(errs) == 0 {
		c.Log.WarnContext(ctx, "github refused a call", args...)
		return
	}
	for _, e := range errs {
		c.Log.WarnContext(ctx, "github refused a call", append(slices.Clone(args),
			"error_type", e.Type, "error_path", truncate(fmt.Sprint(e.Path), 200), "error_message", truncate(e.Message, 200))...)
	}
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

// Reasons an organization refused a token.
const (
	// ReasonSSO means SAML single sign-on has not been authorized for the token.
	ReasonSSO = "sso"
	// ReasonPolicy means an organization policy refuses the token: OAuth App access restrictions,
	// a ban on classic or fine-grained tokens, a token lifetime limit or an IP allow list.
	ReasonPolicy = "policy"
)

// OrgBlocked says an organization refused the token, because SAML single sign-on has not been
// authorized for it (ReasonSSO) or because an organization policy forbids it (ReasonPolicy).
// AuthorizeURL is set only for SSO, and only when GitHub said which organization. Org is empty
// when GitHub did not say which one. Message is GitHub's own explanation, cut to 200 characters,
// and is set for ReasonPolicy only.
type OrgBlocked struct {
	Org          string
	AuthorizeURL string
	Reason       string
	Message      string
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
	Path    []any  `json:"path"`
}

var (
	orgName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	quotedName = regexp.MustCompile("`([A-Za-z0-9][A-Za-z0-9-]{0,38})`")
)

// Classify maps an API answer to an OrgBlocked, or nil when the answer is not an organization
// block. It looks at the X-GitHub-SSO header ("required; url=...") and at GraphQL errors of type
// FORBIDDEN that are SAML or organization policy refusals. A FORBIDDEN error about a missing
// token permission is not an organization block (see PermissionError).
func Classify(status int, h http.Header, body []byte) *OrgBlocked {
	if b := ssoHeader(h); b != nil {
		return b
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
		if b, ok := classifyForbidden(h, e.Message).(*OrgBlocked); ok {
			return b
		}
	}
	return nil
}

// ssoHeader reads X-GitHub-SSO, or returns nil when the answer is not an SSO refusal.
func ssoHeader(h http.Header) *OrgBlocked {
	v := h.Get("X-GitHub-SSO")
	if !strings.HasPrefix(v, "required") {
		return nil
	}
	if org, authorize, ok := parseSSOHeader(v); ok {
		return &OrgBlocked{Org: org, AuthorizeURL: authorize, Reason: ReasonSSO}
	}
	return &OrgBlocked{Reason: ReasonSSO}
}

// policyMatchers are the wordings GitHub uses when an organization policy refuses a token. A
// message matches a row when it contains every fragment of the row (lower case). Examples:
// "The `acme` organization has enabled OAuth App access restrictions", "The `acme` organization
// forbids access via a personal access token (classic)", "`acme` forbids access via a
// fine-grained personal access tokens if the token's lifetime is greater than 366 days" and "the
// `acme` organization has an IP allow list enabled".
var policyMatchers = [][]string{
	{"oauth app access restrictions"},
	{"forbids access via"},
	{"ip allow list"},
	{"personal access token", "lifetime"},
	{"fine-grained personal access token", "forbidden"},
	{"fine-grained personal access token", "policy"},
}

func matchesPolicy(lower string) bool {
	return slices.ContainsFunc(policyMatchers, func(row []string) bool {
		return !slices.ContainsFunc(row, func(fragment string) bool { return !strings.Contains(lower, fragment) })
	})
}

// classifyForbidden decides why GitHub refused (a GraphQL FORBIDDEN error or a REST 403) and
// returns *OrgBlocked for SAML SSO and organization policies, or *PermissionError for anything
// else. An SSO header always means SSO. The message is GitHub's text.
func classifyForbidden(h http.Header, message string) error {
	if b := ssoHeader(h); b != nil {
		return b
	}
	lower := strings.ToLower(message)
	reason := ReasonSSO
	switch {
	case strings.Contains(lower, "saml"):
	case matchesPolicy(lower):
		reason = ReasonPolicy
	default:
		return &PermissionError{Message: truncate(message, 200), Accepted: acceptedPermissions(h)}
	}
	b := &OrgBlocked{Reason: reason}
	if m := quotedName.FindStringSubmatch(message); m != nil {
		b.Org = m[1]
		if reason == ReasonSSO {
			b.AuthorizeURL = "https://github.com/orgs/" + m[1] + "/sso"
		}
	}
	if reason == ReasonPolicy {
		b.Message = truncate(message, 200)
	}
	return b
}

// PermissionError says GitHub refused an action because the token lacks a permission, for
// example "Resource not accessible by personal access token" when a fine-grained token has
// Contents read-only and the action needs Contents write. It is not an organization block.
type PermissionError struct {
	// Message is GitHub's explanation, cut to 200 characters.
	Message string
	// Accepted lists the permissions GitHub said would allow the call, from the
	// X-Accepted-GitHub-Permissions header, such as "contents=write". Only entries of the shape
	// name=read|write|admin are kept. It is empty when GitHub sent none.
	Accepted []string
}

func (e *PermissionError) Error() string {
	return "github: the token lacks a permission: " + e.Message
}

// Detail is a sentence for the person: which permission to add, built from the validated
// Accepted entries ("contents=write" reads "contents: write"), or GitHub's message when it named
// none.
func (e *PermissionError) Detail() string {
	if len(e.Accepted) == 0 {
		return "The token lacks a GitHub permission this action needs: " + e.Message
	}
	names := make([]string, len(e.Accepted))
	for i, a := range e.Accepted {
		names[i] = strings.Replace(a, "=", ": ", 1)
	}
	if len(names) == 1 {
		return "The token needs the GitHub permission " + names[0]
	}
	return "The token needs one of these GitHub permissions: " + strings.Join(names, " or ")
}

var permissionShape = regexp.MustCompile(`^[a-z_]{1,50}=(read|write|admin)$`)

// acceptedPermissions parses X-Accepted-GitHub-Permissions ("contents=write" or
// "pull_requests=write; contents=write"), keeping valid entries once each, in order.
func acceptedPermissions(h http.Header) []string {
	var out []string
	for _, v := range h.Values("X-Accepted-GitHub-Permissions") {
		for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ',' }) {
			p := strings.TrimSpace(part)
			if permissionShape.MatchString(p) && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// forbiddenError maps a REST 403 to *OrgBlocked or *PermissionError.
func forbiddenError(h http.Header, body []byte) error {
	var m struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &m)
	return classifyForbidden(h, m.Message)
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
