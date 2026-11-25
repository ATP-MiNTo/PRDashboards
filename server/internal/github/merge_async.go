package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	// asyncAPIVersion is the REST version the asynchronous merge endpoints are documented with.
	asyncAPIVersion = "2026-03-10"
	// asyncPollFirst and asyncPollMax bound the wait between two polls: it starts at the first and
	// doubles up to the max.
	asyncPollFirst = time.Second
	asyncPollMax   = 5 * time.Second
	// asyncPollCap is how long one merge waits for GitHub in total.
	asyncPollCap = 90 * time.Second
)

var (
	// ErrEnqueued means GitHub put the pull request in a merge queue. It is not merged yet.
	ErrEnqueued = errors.New("the pull request was added to the merge queue")
	// ErrMergePending means GitHub was still merging when the client stopped waiting.
	ErrMergePending = errors.New("GitHub is still merging")

	requestID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	commitSHA = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
)

// MergeFailedError means GitHub accepted the asynchronous merge and then failed it, for example
// because a branch protection rule was not met. Reason is GitHub's text, cut to 200 characters.
type MergeFailedError struct {
	Reason string
}

func (e *MergeFailedError) Error() string { return "github did not merge: " + e.Reason }

// asyncBody holds the fields of the asynchronous merge answers this client reads. The exact shape
// is not documented in full, so every field is optional and several places are tried.
type asyncBody struct {
	Status         string `json:"status"`
	UUID           string `json:"uuid"`
	ID             string `json:"id"`
	Message        string `json:"message"`
	Reason         string `json:"reason"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	SHA            string `json:"sha"`
	Details        struct {
		Status         string `json:"status"`
		UUID           string `json:"uuid"`
		Message        string `json:"message"`
		Reason         string `json:"reason"`
		MergeCommitSHA string `json:"merge_commit_sha"`
	} `json:"details"`
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseAsync(body []byte) asyncBody {
	var b asyncBody
	_ = json.Unmarshal(body, &b)
	b.Status = strings.ToLower(firstNonEmpty(b.Status, b.Details.Status))
	return b
}

func (b asyncBody) uuid() string {
	id := firstNonEmpty(b.Details.UUID, b.UUID, b.ID)
	if !requestID.MatchString(id) {
		return ""
	}
	return id
}

func (b asyncBody) reason() string {
	r := firstNonEmpty(b.Message, b.Details.Message, b.Details.Reason, b.Reason)
	if r == "" {
		r = "no reason was given"
	}
	return truncate(r, 200)
}

func (b asyncBody) sha() string {
	s := firstNonEmpty(b.Details.MergeCommitSHA, b.MergeCommitSHA, b.SHA)
	if !commitSHA.MatchString(s) {
		return ""
	}
	return s
}

// outcome maps a terminal status to the result. done is false while GitHub is still working or
// the status is not one this client knows.
func (b asyncBody) outcome() (res MergeResult, done bool, err error) {
	switch b.Status {
	case "merged":
		return MergeResult{SHA: b.sha()}, true, nil
	case "enqueued", "queued":
		return MergeResult{}, true, ErrEnqueued
	case "failed":
		return MergeResult{}, true, &MergeFailedError{Reason: b.reason()}
	}
	return MergeResult{}, false, nil
}

// MergeAsync merges a pull request with GitHub's asynchronous merge API, the only one that merges
// a stacked pull request (and every unmerged pull request below it). It submits with
// PUT /repos/{o}/{r}/pulls/{n}/merge-async and polls GET .../merge-async/{uuid}, waiting 1 s at
// first and up to 5 s, for at most 90 s. The wait uses Backoff.Wait, so a test can skip it.
//
// The result is a MergeResult when GitHub merged, ErrEnqueued when the pull request went into a
// merge queue, *MergeFailedError when GitHub failed the merge, and ErrMergePending when it was
// still working at the cap. 400 and 422 are ErrNotMergeable, 404 is ErrPRNotFound, and a 403 is
// classified as in Merge.
func (c *Client) MergeAsync(ctx context.Context, token, nameWithOwner string, number int, method, sha string) (MergeResult, error) {
	path, err := repoPath(nameWithOwner)
	if err != nil {
		return MergeResult{}, err
	}
	payload, err := json.Marshal(map[string]string{"merge_method": method, "sha": sha})
	if err != nil {
		return MergeResult{}, err
	}
	target := fmt.Sprintf("%s%s/pulls/%d/merge-async", c.base(), path, number)
	body, status, h, err := c.sendVersion(ctx, token, asyncAPIVersion, http.MethodPut, target, payload)
	if err != nil {
		return MergeResult{}, err
	}
	b := parseAsync(body)
	switch status {
	case http.StatusOK:
		if res, done, err := b.outcome(); done {
			return res, err
		}
		if id := b.uuid(); id != "" && b.Status == "pending" {
			return c.pollMerge(ctx, token, target, id)
		}
		return MergeResult{}, &APIError{Status: status, Message: "the merge was not confirmed"}
	case http.StatusAccepted:
		if id := b.uuid(); id != "" {
			return c.pollMerge(ctx, token, target, id)
		}
		return MergeResult{}, &APIError{Status: status, Message: "the merge request has no id"}
	case http.StatusConflict:
		// A request for this pull request already exists and the body names it.
		if id := b.uuid(); id != "" {
			return c.pollMerge(ctx, token, target, id)
		}
		return MergeResult{}, fmt.Errorf("%w: %s", ErrHeadMoved, restMessage(body))
	}
	return MergeResult{}, asyncRefusal(status, h, body)
}

// asyncRefusal maps a refusal of either asynchronous call.
func asyncRefusal(status int, h http.Header, body []byte) error {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s", ErrNotMergeable, restMessage(body))
	case http.StatusNotFound:
		return ErrPRNotFound
	case http.StatusForbidden:
		return forbiddenError(h, body)
	}
	return &APIError{Status: status, Message: restMessage(body)}
}

// pollMerge asks for the state of request id until it ends, the cap passes or ctx ends.
func (c *Client) pollMerge(ctx context.Context, token, submitURL, id string) (MergeResult, error) {
	target := submitURL + "/" + id
	deadline := c.now().Add(asyncPollCap)
	var waited time.Duration
	wait := asyncPollFirst
	for {
		if err := c.Backoff.Wait(ctx, wait); err != nil {
			return MergeResult{}, err
		}
		waited += wait
		wait = min(wait*2, asyncPollMax)
		body, status, h, err := c.sendVersion(ctx, token, asyncAPIVersion, http.MethodGet, target, nil)
		if err != nil {
			return MergeResult{}, err
		}
		switch {
		case status == http.StatusOK:
			if res, done, err := parseAsync(body).outcome(); done {
				return res, err
			}
		case status >= 500:
			// GitHub may be busy; the merge can still be going. Keep asking until the cap.
		default:
			return MergeResult{}, asyncRefusal(status, h, body)
		}
		if waited >= asyncPollCap || c.now().After(deadline) {
			return MergeResult{}, ErrMergePending
		}
	}
}
