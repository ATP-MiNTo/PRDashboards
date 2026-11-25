// Package jira is the Jira Cloud REST client. It talks to api.atlassian.com/ex/jira/{cloudId} with
// the Atlassian access token and makes outbound calls only through the netguard client it is given.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
)

// DefaultBase is the gateway all Jira Cloud calls go through.
const DefaultBase = "https://api.atlassian.com"

const (
	// ChunkSize is the number of keys in one JQL (TECH_SPEC 10.3).
	ChunkSize = 100
	// AssignedLimit caps the ticket picker.
	AssignedLimit = 100
	// TransitionsTTL is how long the transitions of a ticket are cached.
	TransitionsTTL = 60 * time.Second
	// StatusesTTL is how long the status names of a site are cached.
	StatusesTTL = 10 * time.Minute

	// AssignedJQL lists the open tickets of the connected user.
	AssignedJQL = "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC"

	maxBody      = 8 << 20
	projectPage  = 50
	maxMissRetry = 5
	userAgent    = "prdash"
)

var (
	// ErrUnauthorized means Atlassian rejected the access token or the app's permissions.
	ErrUnauthorized = errors.New("jira rejected the access token")
	// ErrNotFound means the ticket does not exist or is not visible.
	ErrNotFound = errors.New("jira ticket not found")
	// ErrNoSite means Jira is connected but no site is selected.
	ErrNoSite = errors.New("no Jira site selected")
	// ErrBadInput means a key or a site id is not shaped like one.
	ErrBadInput = errors.New("invalid ticket key or site id")

	keyShape  = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)
	siteShape = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	// missingKey matches the validation error Jira gives for a key it does not know.
	missingKey = regexp.MustCompile(`(?i)issue with key '([^']+)' does not exist`)
)

// StatusError is an unexpected answer from Jira, including a rate limit that outlasted the retries.
type StatusError struct {
	Status     int
	RetryAfter time.Duration
	// Detail is the message Jira gave for a rejected request, shortened.
	Detail string
}

func (e *StatusError) Error() string { return "jira answered " + strconv.Itoa(e.Status) }

// Site is a Jira site: the cloud id used in API paths and the base URL used for links.
type Site struct {
	ID  string
	URL string
}

// Issue is the part of a ticket the dashboard shows.
type Issue struct {
	Key            string
	Summary        string
	Status         string
	StatusCategory string
	Project        string
	IssueType      string
}

// SearchResult is the outcome of a batched lookup. Missing lists keys Jira does not know or that
// the user cannot see.
type SearchResult struct {
	Issues   map[string]Issue
	Missing  []string
	Requests int
}

// Option is one allowed value of a transition field.
type Option struct {
	ID   string
	Name string
}

// Field is a field a transition asks for.
type Field struct {
	ID            string
	Name          string
	Required      bool
	Type          string
	AllowedValues []Option
}

// Transition is a move a ticket can make from its current status.
type Transition struct {
	ID         string
	Name       string
	ToStatus   string
	ToCategory string
	Fields     []Field
}

// Client calls Jira. Access returns a valid access token, refreshing it when needed.
type Client struct {
	HTTP    *http.Client
	Base    string
	Backoff backoff.Policy
	Access  func(ctx context.Context) (string, error)
	// Clock defaults to time.Now.
	Clock func() time.Time

	mu          sync.Mutex
	assigned    map[string]cachedIssues
	transitions map[string]cachedTransitions
	statuses    map[string]cachedStatuses
}

type cachedStatuses struct {
	at    time.Time
	names []string
}

type cachedIssues struct {
	at     time.Time
	issues []Issue
}

type cachedTransitions struct {
	at          time.Time
	transitions []Transition
}

func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return DefaultBase
}

func (c *Client) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

func (c *Client) call(ctx context.Context, method, site, path string, query url.Values, body any) (int, []byte, error) {
	if !siteShape.MatchString(site) {
		return 0, nil, ErrBadInput
	}
	token, err := c.Access(ctx)
	if err != nil {
		return 0, nil, err
	}
	var payload []byte
	if body != nil {
		if payload, err = json.Marshal(body); err != nil {
			return 0, nil, err
		}
	}
	target := c.base() + "/ex/jira/" + site + "/rest/api/3" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	resp, err := c.Backoff.Do(ctx, func(ctx context.Context) (*http.Response, error) {
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return c.HTTP.Do(req)
	})
	if err != nil {
		return 0, nil, fmt.Errorf("jira %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return 0, nil, fmt.Errorf("jira %s: %w", path, err)
	}
	if backoff.Retryable(resp) {
		wait, _ := backoff.Hint(resp.Header, c.now())
		return resp.StatusCode, raw, &StatusError{Status: resp.StatusCode, RetryAfter: wait}
	}
	return resp.StatusCode, raw, nil
}

func statusErr(status int) error {
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	}
	return &StatusError{Status: status}
}

type issueJSON struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
	} `json:"fields"`
}

func (i issueJSON) issue() Issue {
	return Issue{
		Key: i.Key, Summary: i.Fields.Summary, Status: i.Fields.Status.Name,
		StatusCategory: i.Fields.Status.StatusCategory.Key, Project: i.Fields.Project.Key, IssueType: i.Fields.IssueType.Name,
	}
}

type searchJSON struct {
	Issues        []issueJSON `json:"issues"`
	NextPageToken string      `json:"nextPageToken"`
	IsLast        bool        `json:"isLast"`
	ErrorMessages []string    `json:"errorMessages"`
}

var searchFields = []string{"summary", "status", "project", "issuetype"}

// jql runs one JQL through POST /search/jql and returns its issues. A 400 with error messages is
// returned as the messages, so the caller can drop keys Jira says do not exist.
func (c *Client) jql(ctx context.Context, site, jql string, limit int, requests *int) ([]Issue, []string, error) {
	var out []Issue
	token := ""
	for {
		body := map[string]any{"jql": jql, "fields": searchFields, "maxResults": limit}
		if token != "" {
			body["nextPageToken"] = token
		}
		*requests++
		status, raw, err := c.call(ctx, http.MethodPost, site, "/search/jql", nil, body)
		if err != nil {
			return nil, nil, err
		}
		var page searchJSON
		_ = json.Unmarshal(raw, &page)
		if status == http.StatusBadRequest && len(page.ErrorMessages) > 0 {
			return nil, page.ErrorMessages, nil
		}
		if err := statusErr(status); err != nil {
			return nil, nil, err
		}
		for _, i := range page.Issues {
			out = append(out, i.issue())
		}
		if page.IsLast || page.NextPageToken == "" || len(out) >= limit {
			return out, nil, nil
		}
		token = page.NextPageToken
	}
}

// Search looks tickets up by key, ChunkSize keys per request. Keys that Jira does not know are
// reported in Missing and do not fail the lookup: when Jira rejects a chunk because of unknown
// keys, those keys are taken out and the chunk is asked again.
func (c *Client) Search(ctx context.Context, site string, keys []string) (SearchResult, error) {
	res := SearchResult{Issues: map[string]Issue{}}
	valid := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if keyShape.MatchString(k) {
			valid = append(valid, k)
		} else {
			res.Missing = append(res.Missing, k)
		}
	}
	for chunk := range slices.Chunk(valid, ChunkSize) {
		if err := c.searchChunk(ctx, site, chunk, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (c *Client) searchChunk(ctx context.Context, site string, chunk []string, res *SearchResult) error {
	asked := slices.Clone(chunk)
	for range maxMissRetry {
		if len(asked) == 0 {
			return nil
		}
		issues, messages, err := c.jql(ctx, site, "key in ("+strings.Join(asked, ",")+")", ChunkSize, &res.Requests)
		if err != nil {
			return err
		}
		if messages != nil {
			gone := unknownKeys(messages)
			next := slices.DeleteFunc(asked, func(k string) bool { return slices.Contains(gone, k) })
			if len(next) == len(asked) {
				return &StatusError{Status: http.StatusBadRequest}
			}
			res.Missing = append(res.Missing, gone...)
			asked = next
			continue
		}
		for _, i := range issues {
			res.Issues[i.Key] = i
		}
		for _, k := range asked {
			if _, ok := res.Issues[k]; !ok {
				res.Missing = append(res.Missing, k)
			}
		}
		return nil
	}
	return &StatusError{Status: http.StatusBadRequest}
}

func unknownKeys(messages []string) []string {
	var out []string
	for _, m := range messages {
		for _, sub := range missingKey.FindAllStringSubmatch(m, -1) {
			if keyShape.MatchString(sub[1]) {
				out = append(out, sub[1])
			}
		}
	}
	return out
}

// Projects lists the project keys of the site with GET /project/search, following its pages.
func (c *Client) Projects(ctx context.Context, site string) ([]string, error) {
	var out []string
	for start := 0; ; {
		q := url.Values{"maxResults": {strconv.Itoa(projectPage)}, "startAt": {strconv.Itoa(start)}}
		status, raw, err := c.call(ctx, http.MethodGet, site, "/project/search", q, nil)
		if err != nil {
			return nil, err
		}
		if err := statusErr(status); err != nil {
			return nil, err
		}
		var page struct {
			Values []struct {
				Key string `json:"key"`
			} `json:"values"`
			IsLast bool `json:"isLast"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("jira projects: bad response: %w", err)
		}
		for _, v := range page.Values {
			out = append(out, v.Key)
		}
		if page.IsLast || len(page.Values) == 0 {
			return out, nil
		}
		start += len(page.Values)
	}
}

// Assigned lists the open tickets assigned to the user, newest update first, at most
// AssignedLimit. The answer is cached for ttl.
func (c *Client) Assigned(ctx context.Context, site string, ttl time.Duration) ([]Issue, error) {
	c.mu.Lock()
	hit, ok := c.assigned[site]
	c.mu.Unlock()
	if ok && c.now().Sub(hit.at) < ttl {
		return slices.Clone(hit.issues), nil
	}
	var requests int
	issues, messages, err := c.jql(ctx, site, AssignedJQL, AssignedLimit, &requests)
	if err != nil {
		return nil, err
	}
	if messages != nil {
		return nil, &StatusError{Status: http.StatusBadRequest}
	}
	c.mu.Lock()
	if c.assigned == nil {
		c.assigned = map[string]cachedIssues{}
	}
	c.assigned[site] = cachedIssues{at: c.now(), issues: issues}
	c.mu.Unlock()
	return slices.Clone(issues), nil
}

type transitionsJSON struct {
	Transitions []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		To   struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"to"`
		Fields map[string]struct {
			Required bool   `json:"required"`
			Name     string `json:"name"`
			Schema   struct {
				Type string `json:"type"`
			} `json:"schema"`
			AllowedValues []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"allowedValues"`
		} `json:"fields"`
	} `json:"transitions"`
}

// Transitions returns the moves a ticket can make, with the fields each one needs, from
// GET /issue/{key}/transitions?expand=transitions.fields. The answer is cached per ticket for
// TransitionsTTL.
func (c *Client) Transitions(ctx context.Context, site, key string) ([]Transition, error) {
	if !keyShape.MatchString(key) {
		return nil, ErrBadInput
	}
	cacheKey := site + "|" + key
	c.mu.Lock()
	hit, ok := c.transitions[cacheKey]
	c.mu.Unlock()
	if ok && c.now().Sub(hit.at) < TransitionsTTL {
		return slices.Clone(hit.transitions), nil
	}
	status, raw, err := c.call(ctx, http.MethodGet, site, "/issue/"+key+"/transitions", url.Values{"expand": {"transitions.fields"}}, nil)
	if err != nil {
		return nil, err
	}
	if err := statusErr(status); err != nil {
		return nil, err
	}
	var parsed transitionsJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("jira transitions: bad response: %w", err)
	}
	out := make([]Transition, 0, len(parsed.Transitions))
	for _, t := range parsed.Transitions {
		tr := Transition{ID: t.ID, Name: t.Name, ToStatus: t.To.Name, ToCategory: t.To.StatusCategory.Key, Fields: []Field{}}
		for id, f := range t.Fields {
			field := Field{ID: id, Name: f.Name, Required: f.Required, Type: f.Schema.Type, AllowedValues: []Option{}}
			for _, v := range f.AllowedValues {
				field.AllowedValues = append(field.AllowedValues, Option{ID: v.ID, Name: cmpName(v.Name, v.Value)})
			}
			tr.Fields = append(tr.Fields, field)
		}
		slices.SortFunc(tr.Fields, func(a, b Field) int { return strings.Compare(a.ID, b.ID) })
		out = append(out, tr)
	}
	c.mu.Lock()
	if c.transitions == nil {
		c.transitions = map[string]cachedTransitions{}
	}
	c.transitions[cacheKey] = cachedTransitions{at: c.now(), transitions: out}
	c.mu.Unlock()
	return slices.Clone(out), nil
}

// Statuses lists the distinct status names of the site, sorted, from one GET /status. Names that
// differ only in case count once. The answer is cached per site for StatusesTTL.
func (c *Client) Statuses(ctx context.Context, site string) ([]string, error) {
	c.mu.Lock()
	hit, ok := c.statuses[site]
	c.mu.Unlock()
	if ok && c.now().Sub(hit.at) < StatusesTTL {
		return slices.Clone(hit.names), nil
	}
	status, raw, err := c.call(ctx, http.MethodGet, site, "/status", nil, nil)
	if err != nil {
		return nil, err
	}
	if err := statusErr(status); err != nil {
		return nil, err
	}
	var parsed []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("jira statuses: bad response: %w", err)
	}
	seen := make(map[string]struct{}, len(parsed))
	out := make([]string, 0, len(parsed))
	for _, s := range parsed {
		name := strings.TrimSpace(s.Name)
		folded := strings.ToLower(name)
		if _, dup := seen[folded]; name == "" || dup {
			continue
		}
		seen[folded] = struct{}{}
		out = append(out, name)
	}
	slices.SortFunc(out, func(a, b string) int {
		if d := strings.Compare(strings.ToLower(a), strings.ToLower(b)); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	c.mu.Lock()
	if c.statuses == nil {
		c.statuses = map[string]cachedStatuses{}
	}
	c.statuses[site] = cachedStatuses{at: c.now(), names: out}
	c.mu.Unlock()
	return slices.Clone(out), nil
}

func cmpName(name, value string) string {
	if name != "" {
		return name
	}
	return value
}

// Transition moves a ticket with POST /issue/{key}/transitions. fields holds the values Jira
// expects per field id (already shaped, for example {"id": "10000"} for a resolution). Jira's
// reason for rejecting the request is in StatusError.Detail. The cached transitions of the ticket
// are dropped, because its status changed.
func (c *Client) Transition(ctx context.Context, site, key, id string, fields map[string]any) error {
	if !keyShape.MatchString(key) {
		return ErrBadInput
	}
	body := map[string]any{"transition": map[string]string{"id": id}}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	status, raw, err := c.call(ctx, http.MethodPost, site, "/issue/"+key+"/transitions", nil, body)
	if err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.transitions, site+"|"+key)
	c.mu.Unlock()
	if status == http.StatusNoContent || status == http.StatusOK {
		return nil
	}
	if status == http.StatusBadRequest {
		return &StatusError{Status: status, Detail: rejection(raw)}
	}
	return statusErr(status)
}

// rejection extracts Jira's own explanation from a 400 answer.
func rejection(raw []byte) string {
	var e struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	_ = json.Unmarshal(raw, &e)
	parts := slices.Clone(e.ErrorMessages)
	for _, k := range slices.Sorted(maps.Keys(e.Errors)) {
		parts = append(parts, k+": "+e.Errors[k])
	}
	out := strings.Join(parts, "; ")
	if len(out) > 200 {
		out = out[:200] + "..."
	}
	return out
}
