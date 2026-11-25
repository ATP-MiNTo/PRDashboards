package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	// ErrPRNotFound means the pull request does not exist or the token cannot see it.
	ErrPRNotFound = errors.New("github pull request not found")
	// ErrNotMergeable means GitHub refused the merge (405): checks, reviews, conflicts or the
	// method are not acceptable.
	ErrNotMergeable = errors.New("github refused the merge")
	// ErrHeadMoved means the head branch changed since the expected commit (409).
	ErrHeadMoved = errors.New("the head branch changed")

	nameShape = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// APIError is an unexpected answer of the REST API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("github answered %d: %s", e.Status, e.Message) }

type nodePage struct {
	Data struct {
		RateLimit struct {
			Limit     int `json:"limit"`
			Remaining int `json:"remaining"`
		} `json:"rateLimit"`
		Node *rawPR `json:"node"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// PullRequest re-fetches one pull request by node id with the same fields the search selects. It
// is the single-PR call made right before a merge. ErrPRNotFound means no such pull request. SAML
// and organization policy refusals are *OrgBlocked, also when a node came back. A FORBIDDEN error
// about a missing token permission is a *PermissionError only when the node is nil: with the node
// present the pull request is returned and the merge call reports the missing permission.
func (c *Client) PullRequest(ctx context.Context, token, id string) (PR, error) {
	body, status, h, err := c.graphql(ctx, token, pullRequestQuery, map[string]any{"id": id})
	if err != nil {
		return PR{}, err
	}
	var p nodePage
	if err := json.Unmarshal(body, &p); err != nil && status == http.StatusOK {
		return PR{}, fmt.Errorf("github pull request: bad response: %w", err)
	}
	for _, e := range p.Errors {
		if e.Type == "RATE_LIMITED" {
			return PR{}, &RateLimitedError{}
		}
	}
	if status == http.StatusForbidden && len(p.Errors) == 0 {
		// A REST-shaped refusal: {"message": "..."}.
		var m struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &m) == nil && m.Message != "" {
			p.Errors = []graphQLError{{Type: "FORBIDDEN", Message: m.Message}}
		}
	}
	if len(p.Errors) > 0 {
		c.logRefusal(ctx, "pull_request", status, h, "", p.Errors)
	}
	if blocked := blockedOf(h, p.Errors); len(blocked) > 0 {
		return PR{}, &blocked[0]
	}
	// A missing token permission is an error only when no node came back. When the node is there,
	// the permission error belongs to a field the merge does not need, and the merge call itself
	// reports a missing permission.
	if p.Data.Node == nil {
		for _, e := range p.Errors {
			if e.Type == "FORBIDDEN" {
				return PR{}, classifyForbidden(h, e.Message)
			}
		}
	}
	switch {
	case status != http.StatusOK:
		return PR{}, fmt.Errorf("github pull request: unexpected status %d", status)
	case len(p.Errors) > 0 && p.Data.Node == nil:
		for _, e := range p.Errors {
			if e.Type == "NOT_FOUND" {
				return PR{}, ErrPRNotFound
			}
		}
		return PR{}, fmt.Errorf("github pull request: %s", truncate(p.Errors[0].Type+" "+p.Errors[0].Message, 200))
	case p.Data.Node == nil || p.Data.Node.Typename != "PullRequest" || p.Data.Node.ID == "":
		return PR{}, ErrPRNotFound
	}
	return p.Data.Node.pr(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// MergeResult is GitHub's answer to a successful merge.
type MergeResult struct {
	SHA     string
	Message string
}

// repoPath validates "owner/name" and returns the REST path of the repository.
func repoPath(nameWithOwner string) (string, error) {
	owner, name, ok := strings.Cut(nameWithOwner, "/")
	if !ok || !orgName.MatchString(owner) || !nameShape.MatchString(name) || name == "." || name == ".." {
		return "", fmt.Errorf("invalid repository name %q", nameWithOwner)
	}
	return "/repos/" + owner + "/" + name, nil
}

func restMessage(body []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &m)
	return truncate(m.Message, 200)
}

// Merge merges a pull request with PUT /repos/{o}/{r}/pulls/{n}/merge. GitHub rejects the merge
// when sha is no longer the head commit, which is ErrHeadMoved. A 405 is ErrNotMergeable. Both wrap
// GitHub's message. A 403 is *OrgBlocked (SSO or policy) or *PermissionError (the token lacks a
// permission, usually Contents: write); a secondary rate limit is *RateLimitedError.
func (c *Client) Merge(ctx context.Context, token, nameWithOwner string, number int, method, sha string) (MergeResult, error) {
	path, err := repoPath(nameWithOwner)
	if err != nil {
		return MergeResult{}, err
	}
	payload, err := json.Marshal(map[string]string{"merge_method": method, "sha": sha})
	if err != nil {
		return MergeResult{}, err
	}
	body, status, h, err := c.send(ctx, token, http.MethodPut, fmt.Sprintf("%s%s/pulls/%d/merge", c.base(), path, number), payload)
	if err != nil {
		return MergeResult{}, err
	}
	switch status {
	case http.StatusOK:
		var out struct {
			SHA     string `json:"sha"`
			Merged  bool   `json:"merged"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &out); err != nil || !out.Merged {
			return MergeResult{}, &APIError{Status: status, Message: "the merge was not confirmed"}
		}
		return MergeResult{SHA: out.SHA, Message: out.Message}, nil
	case http.StatusMethodNotAllowed:
		return MergeResult{}, fmt.Errorf("%w: %s", ErrNotMergeable, restMessage(body))
	case http.StatusConflict:
		return MergeResult{}, fmt.Errorf("%w: %s", ErrHeadMoved, restMessage(body))
	case http.StatusNotFound:
		return MergeResult{}, ErrPRNotFound
	case http.StatusForbidden:
		return MergeResult{}, forbiddenError(h, body)
	}
	return MergeResult{}, &APIError{Status: status, Message: restMessage(body)}
}

// DeleteBranch deletes a branch with DELETE /repos/{o}/{r}/git/refs/heads/{branch}. A branch that
// is already gone is not an error. A 403 is classified as in Merge.
func (c *Client) DeleteBranch(ctx context.Context, token, nameWithOwner, branch string) error {
	path, err := repoPath(nameWithOwner)
	if err != nil {
		return err
	}
	if branch == "" || strings.Contains(branch, "..") {
		return fmt.Errorf("invalid branch name %q", branch)
	}
	parts := strings.Split(branch, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	body, status, h, err := c.send(ctx, token, http.MethodDelete, c.base()+path+"/git/refs/heads/"+strings.Join(parts, "/"), nil)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusNoContent, http.StatusNotFound:
		return nil
	case http.StatusForbidden:
		return forbiddenError(h, body)
	case http.StatusUnprocessableEntity:
		if strings.Contains(strings.ToLower(restMessage(body)), "does not exist") {
			return nil
		}
	}
	return &APIError{Status: status, Message: restMessage(body)}
}
