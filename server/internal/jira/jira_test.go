package jira_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/backoff"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

type fixture struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

func load(t *testing.T, name string) fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) write(w http.ResponseWriter) {
	for k, v := range f.Headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.Status)
	_, _ = w.Write(f.Body)
}

type call struct {
	Method, Path, Query, Auth string
	Body                      map[string]any
}

type rig struct {
	c      *jira.Client
	clock  *testutil.Clock
	sleeps []time.Duration
	mu     sync.Mutex
	calls  []call
}

func (r *rig) seen() []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// newRig serves handler, or the named fixtures in order when handler is nil.
func newRig(t *testing.T, handler func(call, http.ResponseWriter) bool, names ...string) *rig {
	t.Helper()
	r := &rig{clock: testutil.NewClock()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c := call{Method: req.Method, Path: req.URL.Path, Query: req.URL.RawQuery, Auth: req.Header.Get("Authorization")}
		raw, _ := io.ReadAll(req.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &c.Body)
		}
		r.mu.Lock()
		r.calls = append(r.calls, c)
		n := len(r.calls)
		r.mu.Unlock()
		if handler != nil && handler(c, w) {
			return
		}
		load(t, names[min(n, len(names))-1]).write(w)
	}))
	t.Cleanup(srv.Close)
	r.c = &jira.Client{
		HTTP:   srv.Client(),
		Base:   srv.URL,
		Access: func(context.Context) (string, error) { return "tok", nil },
		Clock:  r.clock.Now,
		Backoff: backoff.Policy{Clock: r.clock.Now, Attempts: 3, Sleep: func(_ context.Context, d time.Duration) error {
			r.sleeps = append(r.sleeps, d)
			return nil
		}},
	}
	return r
}

const site = "cloud-1"

func TestSearchReturnsIssuesAndMissingKeys(t *testing.T) {
	r := newRig(t, nil, "search_two")
	res, err := r.c.Search(context.Background(), site, []string{"INP-1", "OPS-2", "INP-404", "INP-1", "bad key", "lower-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) != 2 || res.Requests != 1 {
		t.Fatalf("%+v", res)
	}
	want := jira.Issue{Key: "INP-1", Summary: "Add login", Status: "In Review", StatusCategory: "indeterminate", Project: "INP", IssueType: "Story"}
	if res.Issues["INP-1"] != want {
		t.Fatalf("issue %+v", res.Issues["INP-1"])
	}
	slices.Sort(res.Missing)
	if !slices.Equal(res.Missing, []string{"INP-404", "bad key", "lower-1"}) {
		t.Fatalf("missing %v", res.Missing)
	}
	c := r.seen()[0]
	if c.Method != "POST" || c.Path != "/ex/jira/cloud-1/rest/api/3/search/jql" || c.Auth != "Bearer tok" {
		t.Fatalf("%+v", c)
	}
	if c.Body["jql"] != "key in (INP-1,OPS-2,INP-404)" || c.Body["maxResults"] != float64(100) {
		t.Fatalf("body %v", c.Body)
	}
	if !slices.Equal(toStrings(c.Body["fields"]), []string{"summary", "status", "project", "issuetype"}) {
		t.Fatalf("fields %v", c.Body["fields"])
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestSearchSendsChunksOf100(t *testing.T) {
	r := newRig(t, func(c call, w http.ResponseWriter) bool {
		jql := c.Body["jql"].(string)
		keys := strings.Split(strings.TrimSuffix(strings.TrimPrefix(jql, "key in ("), ")"), ",")
		issues := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			issues = append(issues, map[string]any{"key": k, "fields": map[string]any{"summary": "s " + k}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issues": issues, "isLast": true})
		return true
	})
	keys := make([]string, 250)
	for i := range keys {
		keys[i] = fmt.Sprintf("INP-%d", i+1)
	}
	res, err := r.c.Search(context.Background(), site, keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) != 250 || res.Requests != 3 || len(res.Missing) != 0 {
		t.Fatalf("%d issues, %d requests, missing %v", len(res.Issues), res.Requests, res.Missing)
	}
	var sizes []int
	for _, c := range r.seen() {
		sizes = append(sizes, strings.Count(c.Body["jql"].(string), ",")+1)
	}
	if !slices.Equal(sizes, []int{100, 100, 50}) {
		t.Fatalf("chunk sizes %v", sizes)
	}
}

func TestSearchDropsKeysJiraRejectsAndAsksAgain(t *testing.T) {
	r := newRig(t, nil, "search_unknown_key", "search_two")
	res, err := r.c.Search(context.Background(), site, []string{"INP-1", "NOPE-9", "OPS-2"})
	if err != nil {
		t.Fatal(err)
	}
	calls := r.seen()
	if len(calls) != 2 || calls[1].Body["jql"] != "key in (INP-1,OPS-2)" || res.Requests != 2 {
		t.Fatalf("%+v", calls)
	}
	if !slices.Equal(res.Missing, []string{"NOPE-9"}) || len(res.Issues) != 2 {
		t.Fatalf("%+v", res)
	}
}

func TestSearchFailsOnAnErrorItCannotExplain(t *testing.T) {
	r := newRig(t, nil, "search_unparsable_400")
	_, err := r.c.Search(context.Background(), site, []string{"INP-1"})
	var se *jira.StatusError
	if !errors.As(err, &se) || se.Status != 400 {
		t.Fatalf("%v", err)
	}
}

func TestSearchFollowsNextPageToken(t *testing.T) {
	r := newRig(t, nil, "search_next_page_1", "search_next_page_2")
	res, err := r.c.Search(context.Background(), site, []string{"INP-1", "INP-2"})
	if err != nil || len(res.Issues) != 2 || res.Requests != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if r.seen()[1].Body["nextPageToken"] != "T1" {
		t.Fatalf("%v", r.seen()[1].Body)
	}
}

func TestSearchWithNothingToAskMakesNoRequest(t *testing.T) {
	r := newRig(t, nil, "search_two")
	res, err := r.c.Search(context.Background(), site, nil)
	if err != nil || res.Requests != 0 || len(r.seen()) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestProjectsFollowsPages(t *testing.T) {
	r := newRig(t, nil, "projects_page1", "projects_page2")
	got, err := r.c.Projects(context.Background(), site)
	if err != nil || !slices.Equal(got, []string{"INP", "OPS", "WEB"}) {
		t.Fatalf("%v %v", got, err)
	}
	calls := r.seen()
	if len(calls) != 2 || calls[0].Method != "GET" || calls[0].Path != "/ex/jira/cloud-1/rest/api/3/project/search" ||
		calls[0].Query != "maxResults=50&startAt=0" || calls[1].Query != "maxResults=50&startAt=2" {
		t.Fatalf("%+v", calls)
	}
}

func TestStatusesAreDistinctSortedAndCachedFor10Minutes(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, nil, "statuses")
	got, err := r.c.Statuses(ctx, site)
	if err != nil || !slices.Equal(got, []string{"Blocked", "done", "In Progress", "To Do"}) {
		t.Fatalf("case-insensitive duplicates and blanks are dropped, the rest sorted: %v %v", got, err)
	}
	calls := r.seen()
	if len(calls) != 1 || calls[0].Method != "GET" || calls[0].Path != "/ex/jira/cloud-1/rest/api/3/status" || calls[0].Auth != "Bearer tok" {
		t.Fatalf("%+v", calls)
	}
	got[0] = "changed"
	r.clock.Advance(jira.StatusesTTL - time.Second)
	again, err := r.c.Statuses(ctx, site)
	if err != nil || again[0] != "Blocked" || len(r.seen()) != 1 {
		t.Fatalf("a cache hit makes no call and returns a copy: %v %v %d", again, err, len(r.seen()))
	}
	if _, err := r.c.Statuses(ctx, "cloud-2"); err != nil || len(r.seen()) != 2 {
		t.Fatalf("the cache is per site: %v %d", err, len(r.seen()))
	}
	r.clock.Advance(2 * time.Second)
	if _, err := r.c.Statuses(ctx, site); err != nil || len(r.seen()) != 3 {
		t.Fatalf("expired after 10 minutes: %v %d", err, len(r.seen()))
	}
}

func TestStatusesErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := newRig(t, nil, "unauthorized").c.Statuses(ctx, site); !errors.Is(err, jira.ErrUnauthorized) {
		t.Fatalf("401: %v", err)
	}
	if _, err := newRig(t, nil, "statuses").c.Statuses(ctx, "bad site"); !errors.Is(err, jira.ErrBadInput) {
		t.Fatalf("site: %v", err)
	}
	r := newRig(t, func(_ call, w http.ResponseWriter) bool { _, _ = io.WriteString(w, "not json"); return true })
	if _, err := r.c.Statuses(ctx, site); err == nil {
		t.Fatal("bad body")
	}
	if _, err := r.c.Statuses(ctx, site); err == nil || len(r.seen()) != 2 {
		t.Fatalf("a failure is not cached: %d", len(r.seen()))
	}
}

func TestAssignedIsCachedForTheTTL(t *testing.T) {
	r := newRig(t, nil, "assigned")
	ctx := context.Background()
	got, err := r.c.Assigned(ctx, site, 5*time.Minute)
	if err != nil || len(got) != 2 || got[0].Key != "INP-7" {
		t.Fatalf("%v %v", got, err)
	}
	if jql := r.seen()[0].Body["jql"]; jql != "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC" || r.seen()[0].Body["maxResults"] != float64(100) {
		t.Fatalf("body %v", r.seen()[0].Body)
	}
	got[0].Key = "mutated"
	r.clock.Advance(4 * time.Minute)
	again, _ := r.c.Assigned(ctx, site, 5*time.Minute)
	if len(r.seen()) != 1 || again[0].Key != "INP-7" {
		t.Fatalf("cache hit expected, calls %d, %v", len(r.seen()), again)
	}
	r.clock.Advance(2 * time.Minute)
	if _, err := r.c.Assigned(ctx, site, 5*time.Minute); err != nil || len(r.seen()) != 2 {
		t.Fatalf("expired entry must be fetched again: %d %v", len(r.seen()), err)
	}
}

func TestTransitionsWithFieldsAreCachedFor60Seconds(t *testing.T) {
	r := newRig(t, nil, "transitions")
	ctx := context.Background()
	got, err := r.c.Transitions(ctx, site, "INP-1")
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	c := r.seen()[0]
	if c.Method != "GET" || c.Path != "/ex/jira/cloud-1/rest/api/3/issue/INP-1/transitions" || c.Query != "expand=transitions.fields" {
		t.Fatalf("%+v", c)
	}
	done := got[0]
	if done.ID != "31" || done.ToStatus != "Done" || done.ToCategory != "done" || len(done.Fields) != 2 {
		t.Fatalf("%+v", done)
	}
	res := done.Fields[1]
	if res.ID != "resolution" || !res.Required || res.Type != "resolution" || len(res.AllowedValues) != 2 || res.AllowedValues[1].Name != "Won't Do" {
		t.Fatalf("%+v", res)
	}
	if done.Fields[0].ID != "customfield_10010" || done.Fields[0].AllowedValues[0].Name != "2026.11" {
		t.Fatalf("%+v", done.Fields[0])
	}
	if got[1].Fields == nil || len(got[1].Fields) != 0 {
		t.Fatalf("fields of a plain transition: %#v", got[1].Fields)
	}

	r.clock.Advance(59 * time.Second)
	_, _ = r.c.Transitions(ctx, site, "INP-1")
	if len(r.seen()) != 1 {
		t.Fatal("served from cache within 60 s")
	}
	_, _ = r.c.Transitions(ctx, site, "OPS-2")
	if len(r.seen()) != 2 {
		t.Fatal("the cache is per ticket")
	}
	r.clock.Advance(2 * time.Second)
	_, _ = r.c.Transitions(ctx, site, "INP-1")
	if len(r.seen()) != 3 {
		t.Fatal("fetched again after 60 s")
	}
}

func TestBackoffHonoursRetryAfterOn429And503(t *testing.T) {
	r := newRig(t, nil, "rate_limited", "search_two")
	if _, err := r.c.Search(context.Background(), site, []string{"INP-1"}); err != nil {
		t.Fatal(err)
	}
	if len(r.seen()) != 2 || len(r.sleeps) != 1 || r.sleeps[0] != 7*time.Second {
		t.Fatalf("calls %d sleeps %v", len(r.seen()), r.sleeps)
	}

	r = newRig(t, nil, "unavailable", "unavailable", "unavailable")
	_, err := r.c.Search(context.Background(), site, []string{"INP-1"})
	var se *jira.StatusError
	if !errors.As(err, &se) || se.Status != 503 || len(r.seen()) != 3 || len(r.sleeps) != 2 {
		t.Fatalf("%v calls %d sleeps %v", err, len(r.seen()), r.sleeps)
	}
	for _, d := range r.sleeps {
		if d < time.Second || d > backoff.DefaultMax {
			t.Fatalf("exponential delay out of range: %v", d)
		}
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, nil, "unauthorized")
	if _, err := r.c.Projects(ctx, site); !errors.Is(err, jira.ErrUnauthorized) {
		t.Fatalf("401: %v", err)
	}
	r = newRig(t, nil, "not_found")
	if _, err := r.c.Transitions(ctx, site, "INP-1"); !errors.Is(err, jira.ErrNotFound) {
		t.Fatalf("404: %v", err)
	}
	r = newRig(t, nil, "search_two")
	for _, bad := range []string{"", "x/../y", "a b", strings.Repeat("a", 65)} {
		if _, err := r.c.Projects(ctx, bad); !errors.Is(err, jira.ErrBadInput) {
			t.Errorf("site %q: %v", bad, err)
		}
	}
	for _, bad := range []string{"inp-1", "INP-1/../x", "INP-1?x=1", ""} {
		if _, err := r.c.Transitions(ctx, site, bad); !errors.Is(err, jira.ErrBadInput) {
			t.Errorf("key %q: %v", bad, err)
		}
	}
	if len(r.seen()) != 0 {
		t.Fatal("malformed input must not reach Jira")
	}
	r.c.Access = func(context.Context) (string, error) { return "", errors.New("no token") }
	if _, err := r.c.Projects(ctx, site); err == nil || len(r.seen()) != 0 {
		t.Fatalf("token error must stop the call: %v", err)
	}
	r = newRig(t, func(_ call, w http.ResponseWriter) bool { w.WriteHeader(500); return true })
	if _, err := r.c.Assigned(ctx, site, time.Minute); err == nil {
		t.Fatal("500 is an error")
	}
	var se *jira.StatusError
	if _, err := r.c.Projects(ctx, site); !errors.As(err, &se) || se.Error() != "jira answered 500" {
		t.Fatalf("%v", err)
	}
}

func TestBadBodies(t *testing.T) {
	r := newRig(t, func(_ call, w http.ResponseWriter) bool { _, _ = io.WriteString(w, "not json"); return true })
	if _, err := r.c.Projects(context.Background(), site); err == nil {
		t.Fatal("bad projects body")
	}
	if _, err := r.c.Transitions(context.Background(), site, "INP-1"); err == nil {
		t.Fatal("bad transitions body")
	}
}

func TestTransitionPostsTheMoveAndDropsTheCache(t *testing.T) {
	status := http.StatusNoContent
	var reply string
	r := newRig(t, func(c call, w http.ResponseWriter) bool {
		if c.Method == http.MethodPost {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, reply)
			return true
		}
		_, _ = io.WriteString(w, `{"transitions":[]}`)
		return true
	})
	ctx := context.Background()
	const site = "cloud-1"
	_, _ = r.c.Transitions(ctx, site, "INP-1")
	if err := r.c.Transition(ctx, site, "INP-1", "31", map[string]any{"resolution": map[string]string{"id": "10000"}}); err != nil {
		t.Fatal(err)
	}
	_, _ = r.c.Transitions(ctx, site, "INP-1")
	calls := r.seen()
	if len(calls) != 3 || calls[1].Method != http.MethodPost || calls[1].Path != "/ex/jira/cloud-1/rest/api/3/issue/INP-1/transitions" {
		t.Fatalf("%+v", calls)
	}
	body := calls[1].Body
	if tr, _ := body["transition"].(map[string]any); tr["id"] != "31" || body["fields"] == nil {
		t.Fatalf("%+v", body)
	}

	if err := r.c.Transition(ctx, site, "INP-1", "21", nil); err != nil {
		t.Fatal(err)
	}
	if _, has := r.seen()[3].Body["fields"]; has {
		t.Fatal("empty fields are not sent")
	}

	status, reply = http.StatusBadRequest, `{"errorMessages":["Transition failed"],"errors":{"resolution":"Resolution is required"}}`
	var se *jira.StatusError
	if err := r.c.Transition(ctx, site, "INP-1", "31", nil); !errors.As(err, &se) || se.Status != 400 || se.Detail != "Transition failed; resolution: Resolution is required" {
		t.Fatalf("%v", err)
	}
	reply = `{"errorMessages":["` + strings.Repeat("x", 300) + `"]}`
	if err := r.c.Transition(ctx, site, "INP-1", "31", nil); !errors.As(err, &se) || len(se.Detail) != 203 {
		t.Fatalf("%v", err)
	}
	status = http.StatusUnauthorized
	if err := r.c.Transition(ctx, site, "INP-1", "31", nil); !errors.Is(err, jira.ErrUnauthorized) {
		t.Fatalf("%v", err)
	}
	status = http.StatusNotFound
	if err := r.c.Transition(ctx, site, "INP-1", "31", nil); !errors.Is(err, jira.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if err := r.c.Transition(ctx, site, "lower-1", "31", nil); !errors.Is(err, jira.ErrBadInput) {
		t.Fatalf("%v", err)
	}
}
