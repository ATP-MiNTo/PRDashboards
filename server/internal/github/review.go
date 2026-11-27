package github

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

//go:embed review.graphql
var markReadyMutation string

// Review events GitHub accepts on POST .../pulls/{n}/reviews that this client sends.
const (
	ReviewApprove        = "APPROVE"
	ReviewRequestChanges = "REQUEST_CHANGES"
)

// MaxReviewBody is GitHub's limit for the text of a review, in characters.
const MaxReviewBody = 65536

var (
	// ErrOwnPullRequest means GitHub refused a review because the reviewer opened the pull request.
	ErrOwnPullRequest = errors.New("github: you cannot review your own pull request")
	// ErrNotDraft means the pull request is not a draft (any more), so it cannot be marked ready.
	ErrNotDraft = errors.New("github: the pull request is not a draft")
)

// ReviewRejectedError is GitHub saying no to a review or a ready mark for a reason that is neither
// a permission nor the head moving. Message is GitHub's text cut to 200 characters; it is not
// secret and the web shows it as plain text.
type ReviewRejectedError struct {
	Message string
}

func (e *ReviewRejectedError) Error() string { return "github rejected the review: " + e.Message }

// reviewPermission is what a review and a ready mark need on a fine-grained token or GitHub App.
const reviewPermission = "pull_requests=write"

// needsReviewPermission names pull_requests: write on a *PermissionError that came without the
// X-Accepted-GitHub-Permissions header (GraphQL never sends it), so the person is told which
// permission to add. Other errors pass through.
func needsReviewPermission(err error) error {
	var perm *PermissionError
	if errors.As(err, &perm) && len(perm.Accepted) == 0 {
		perm.Accepted = []string{reviewPermission}
	}
	return err
}

// restDetail is the explanation of a REST error: its message and the strings of its errors list,
// joined and cut to 200 characters.
func restDetail(body []byte) string {
	var m struct {
		Message string `json:"message"`
		Errors  []any  `json:"errors"`
	}
	_ = json.Unmarshal(body, &m)
	parts := []string{m.Message}
	for _, e := range m.Errors {
		switch v := e.(type) {
		case string:
			parts = append(parts, v)
		case map[string]any:
			if msg, ok := v["message"].(string); ok {
				parts = append(parts, msg)
			}
		}
	}
	return truncate(strings.TrimSpace(strings.Join(parts, ": ")), 200)
}

// SubmitReview submits a review with POST /repos/{o}/{r}/pulls/{n}/reviews. event is ReviewApprove
// or ReviewRequestChanges; sha pins the review to that head commit (GitHub refuses a commit that is
// not part of the pull request, which is ErrHeadMoved); body may be empty for an approval. A 403 is
// *OrgBlocked or *PermissionError (normally pull_requests: write), a 404 ErrPRNotFound, a 422 about
// the author's own pull request ErrOwnPullRequest, and any other 422 a *ReviewRejectedError.
func (c *Client) SubmitReview(ctx context.Context, token, nameWithOwner string, number int, event, sha, body string) error {
	path, err := repoPath(nameWithOwner)
	if err != nil {
		return err
	}
	if event != ReviewApprove && event != ReviewRequestChanges {
		return fmt.Errorf("invalid review event %q", event)
	}
	if !commitSHA.MatchString(sha) {
		return fmt.Errorf("invalid commit %q", sha)
	}
	if len([]rune(body)) > MaxReviewBody {
		return fmt.Errorf("the review text is over %d characters", MaxReviewBody)
	}
	payload := map[string]string{"commit_id": sha, "event": event}
	if body != "" {
		payload["body"] = body
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	answer, status, h, err := c.send(ctx, token, http.MethodPost, fmt.Sprintf("%s%s/pulls/%d/reviews", c.base(), path, number), raw)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusNotFound:
		return ErrPRNotFound
	case http.StatusForbidden:
		return needsReviewPermission(forbiddenError(h, answer))
	case http.StatusUnprocessableEntity:
		detail := restDetail(answer)
		lower := strings.ToLower(detail)
		switch {
		case strings.Contains(lower, "your own pull request"):
			return ErrOwnPullRequest
		case strings.Contains(lower, "commit_id") || strings.Contains(lower, "commit sha"):
			return fmt.Errorf("%w: %s", ErrHeadMoved, detail)
		}
		return &ReviewRejectedError{Message: detail}
	}
	return &APIError{Status: status, Message: restMessage(answer)}
}

type markReadyPage struct {
	Data struct {
		Mark *struct {
			PullRequest *struct {
				ID      string `json:"id"`
				IsDraft bool   `json:"isDraft"`
			} `json:"pullRequest"`
		} `json:"markPullRequestReadyForReview"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// MarkReady runs markPullRequestReadyForReview for the pull request with node id id. Success is
// GitHub answering with the pull request no longer a draft. SAML and organization policy refusals
// are *OrgBlocked, a missing permission is *PermissionError (pull_requests: write), NOT_FOUND is
// ErrPRNotFound, and a pull request that is not a draft is ErrNotDraft. Anything else GitHub
// refuses is a *ReviewRejectedError.
func (c *Client) MarkReady(ctx context.Context, token, id string) error {
	body, status, h, err := c.graphql(ctx, token, markReadyMutation, map[string]any{"id": id})
	if err != nil {
		return err
	}
	var p markReadyPage
	if err := json.Unmarshal(body, &p); err != nil && status == http.StatusOK {
		return fmt.Errorf("github mark ready: bad response: %w", err)
	}
	for _, e := range p.Errors {
		if e.Type == "RATE_LIMITED" {
			return &RateLimitedError{}
		}
	}
	if status == http.StatusForbidden && len(p.Errors) == 0 {
		var m struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &m) == nil && m.Message != "" {
			p.Errors = []graphQLError{{Type: "FORBIDDEN", Message: m.Message}}
		}
	}
	if len(p.Errors) > 0 {
		c.logRefusal(ctx, "mark_ready", status, h, "", p.Errors)
	}
	if blocked := blockedOf(h, p.Errors); len(blocked) > 0 {
		return &blocked[0]
	}
	for _, e := range p.Errors {
		switch {
		case e.Type == "FORBIDDEN":
			return needsReviewPermission(classifyForbidden(h, e.Message, e.Extensions))
		case e.Type == "NOT_FOUND":
			return ErrPRNotFound
		}
	}
	switch {
	case status != http.StatusOK:
		return &APIError{Status: status, Message: restMessage(body)}
	case len(p.Errors) > 0:
		msg := truncate(p.Errors[0].Message, 200)
		if strings.Contains(strings.ToLower(msg), "not a draft") || strings.Contains(strings.ToLower(msg), "not in draft") {
			return ErrNotDraft
		}
		return &ReviewRejectedError{Message: msg}
	case p.Data.Mark == nil || p.Data.Mark.PullRequest == nil || p.Data.Mark.PullRequest.IsDraft:
		return &ReviewRejectedError{Message: "GitHub did not mark the pull request ready"}
	}
	return nil
}
