package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/mw"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

type stubMerger struct {
	req      merge.Request
	res      merge.Result
	err      error
	retryID  int64
	step     merge.Step
	retryErr error
}

func (m *stubMerger) Merge(_ context.Context, req merge.Request) (merge.Result, error) {
	m.req = req
	return m.res, m.err
}

func (m *stubMerger) Retry(_ context.Context, id int64) (merge.Step, error) {
	m.retryID = id
	return m.step, m.retryErr
}

type mergeRig struct {
	*snapRig
	m *stubMerger
}

func newMergeRig(t *testing.T, prs ...gen.SnapshotPr) *mergeRig {
	t.Helper()
	m := &stubMerger{}
	sp := newStubPoller(prs...)
	g := newRig(t, webFS(), func(c *api.Config) { c.Poller, c.Merger = sp, m })
	return &mergeRig{snapRig: &snapRig{rig: g, p: sp, l: g.login()}, m: m}
}

const uuid = "123e4567-e89b-42d3-a456-426614174000"

func (g *mergeRig) merge(body string, opts ...reqOpt) *httptest.ResponseRecorder {
	opts = append(g.l.write(), append([]reqOpt{header("Idempotency-Key", uuid)}, opts...)...)
	return g.do(http.MethodPost, "/api/prs/PR_1/merge", body, opts...)
}

const goodBody = `{"method":"squash","expectedHeadSha":"abc","deleteBranch":true,"tickets":[{"key":"INP-1","transitionId":"31","fields":{"resolution":"1"}},{"key":"INP-2","transitionId":null,"fields":{}}]}`

func TestMergeReturnsTheStepsAndPassesTheRequest(t *testing.T) {
	g := newMergeRig(t)
	g.m.res = merge.Result{PRID: "PR_1", Method: merge.Squash, SHA: "mergesha", Steps: []merge.Step{
		{Kind: merge.StepMerge, State: merge.StateOK},
		{Kind: merge.StepTransition, Ticket: "INP-1", State: merge.StateFailed, Message: "Jira answered 503", ToStatus: "Done", RetryID: 5},
	}}
	w := g.merge(goodBody)
	var out gen.MergeResult
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Sha != "mergesha" || len(out.Steps) != 2 ||
		out.Steps[0].Ticket != nil || *out.Steps[1].RetryId != 5 || *out.Steps[1].Message != "Jira answered 503" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	req := g.m.req
	if req.PRID != "PR_1" || req.IdempotencyKey != uuid || req.Method != merge.Squash || req.ExpectedHeadSHA != "abc" || !req.DeleteBranch ||
		len(req.Tickets) != 2 || req.Tickets[0].TransitionID != "31" || req.Tickets[0].Fields["resolution"] != "1" || req.Tickets[1].TransitionID != "" {
		t.Fatalf("%+v", req)
	}
}

func TestMergeRequestValidation(t *testing.T) {
	g := newMergeRig(t)
	many := strings.Repeat(`{"key":"INP-1","transitionId":null,"fields":{}},`, 20) + `{"key":"INP-1","transitionId":null,"fields":{}}`
	for name, tc := range map[string]struct {
		body string
		opts []reqOpt
		want int
	}{
		"missing method":     {`{"expectedHeadSha":"a","deleteBranch":false,"tickets":[]}`, nil, 400},
		"unknown method":     {`{"method":"octopus","expectedHeadSha":"a","deleteBranch":false,"tickets":[]}`, nil, 400},
		"unknown field":      {`{"method":"squash","expectedHeadSha":"a","deleteBranch":false,"tickets":[],"x":1}`, nil, 400},
		"empty sha":          {`{"method":"squash","expectedHeadSha":"","deleteBranch":false,"tickets":[]}`, nil, 400},
		"21 tickets":         {`{"method":"squash","expectedHeadSha":"a","deleteBranch":false,"tickets":[` + many + `]}`, nil, 400},
		"bad ticket key":     {`{"method":"squash","expectedHeadSha":"a","deleteBranch":false,"tickets":[{"key":"x","transitionId":null,"fields":{}}]}`, nil, 400},
		"unknown ticket key": {`{"method":"squash","expectedHeadSha":"a","deleteBranch":false,"tickets":[{"key":"INP-1","transitionId":null,"fields":{},"y":1}]}`, nil, 400},
		"field not a string": {`{"method":"squash","expectedHeadSha":"a","deleteBranch":false,"tickets":[{"key":"INP-1","transitionId":"1","fields":{"a":1}}]}`, nil, 400},
		"duplicate ticket":   {`{"method":"squash","expectedHeadSha":"a","deleteBranch":false,"tickets":[{"key":"INP-1","transitionId":null,"fields":{}},{"key":"INP-1","transitionId":null,"fields":{}}]}`, nil, 422},
		"no idempotency key": {goodBody, []reqOpt{noHeader("Idempotency-Key")}, 400},
		"bad key":            {goodBody, []reqOpt{header("Idempotency-Key", "not-a-uuid")}, 400},
	} {
		if w := g.merge(tc.body, tc.opts...); w.Code != tc.want {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	wantProblem(t, g.do(http.MethodPost, "/api/prs/PR_1/merge", goodBody, cookie(g.l.cookie), header("Idempotency-Key", uuid)), http.StatusForbidden, "bad_csrf")
}

func TestMergeRefusalsAreProblems(t *testing.T) {
	g := newMergeRig(t)
	g.m.err = &merge.Error{Code: merge.CodeHeadMoved, Detail: "moved", Fresh: &merge.Fresh{State: "OPEN", HeadSHA: "new", BaseRef: "main", Mergeable: "MERGEABLE", MergeState: "CLEAN", ReviewDecision: "APPROVED"}}
	w := g.merge(goodBody)
	var p gen.MergeProblem
	if w.Code != http.StatusConflict || w.Header().Get("Content-Type") != "application/problem+json" || json.Unmarshal(w.Body.Bytes(), &p) != nil ||
		p.Code != "head_moved" || p.Pr == nil || p.Pr.HeadSha != "new" || *p.Pr.ReviewDecision != "APPROVED" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for _, code := range []string{merge.CodeNotMergeable, merge.CodeStackBaseOpen, merge.CodeDraft, merge.CodeNotOpen, merge.CodeTransitionUnavailable, merge.CodeNotConnected, merge.CodeMergeFailed, merge.CodeMergePending} {
		g.m.err = &merge.Error{Code: code}
		w := g.merge(goodBody)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"`+code+`"`) || !strings.Contains(w.Body.String(), `"pr":null`) {
			t.Errorf("%s: %d %s", code, w.Code, w.Body)
		}
	}
	g.m.err = &merge.Error{Code: merge.CodeMergeFailed, Detail: "GitHub did not merge: <b>checks</b> pending"}
	w = g.merge(goodBody)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"detail":"GitHub did not merge: \u003cb\u003echecks\u003c/b\u003e pending"`) {
		t.Errorf("merge_failed: %d %s", w.Code, w.Body)
	}
	g.m.err = &merge.Error{Code: merge.CodeNotFound}
	wantProblem(t, g.merge(goodBody), http.StatusNotFound, "not_found")
	for _, code := range []string{merge.CodeMethodNotAllowed, merge.CodeTicketNotLinked, merge.CodeMissingField, merge.CodeInvalidField, merge.CodeKeyReused} {
		g.m.err = &merge.Error{Code: code, Detail: "d"}
		wantProblem(t, g.merge(goodBody), http.StatusUnprocessableEntity, code)
	}
}

func TestMergeProviderFailures(t *testing.T) {
	g := newMergeRig(t)
	for err, want := range map[error][2]any{
		&github.RateLimitedError{}:      {http.StatusTooManyRequests, "rate_limited"},
		&github.OrgBlocked{Org: "acme"}: {http.StatusForbidden, "org_blocked"},
		&github.APIError{Status: 502}:   {http.StatusBadGateway, "upstream_error"},
		&jira.StatusError{Status: 500}:  {http.StatusBadGateway, "upstream_error"},
		errors.New("disk exploded"):     {http.StatusInternalServerError, "internal_error"},
	} {
		g.m.err = err
		wantProblem(t, g.merge(goodBody), want[0].(int), want[1].(string))
	}
}

func TestMergePermissionAndPolicyProblems(t *testing.T) {
	g := newMergeRig(t)
	cases := []struct {
		err    error
		code   string
		detail string
	}{
		{&github.PermissionError{Message: "Resource not accessible by personal access token", Accepted: []string{"contents=write"}},
			"missing_permission", "The token needs the GitHub permission contents: write"},
		{&github.PermissionError{Accepted: []string{"pull_requests=write", "contents=write"}},
			"missing_permission", "The token needs one of these GitHub permissions: pull_requests: write or contents: write"},
		{&github.PermissionError{Message: "Nope"}, "missing_permission", "The token lacks a GitHub permission this action needs: Nope"},
		{fmt.Errorf("wrapped: %w", &github.PermissionError{Accepted: []string{"contents=write"}}), "missing_permission", "contents: write"},
		{&github.OrgBlocked{Org: "acme", Reason: "policy", Message: "secret wording"}, "org_blocked", "The acme organization policy does not allow this token"},
		{&github.OrgBlocked{Reason: "policy"}, "org_blocked", "An organization policy does not allow this token"},
		{&github.OrgBlocked{Org: "acme", Reason: "sso"}, "org_blocked", "An organization has not authorized the token"},
	}
	for _, tc := range cases {
		g.m.err = tc.err
		w := g.merge(goodBody)
		wantProblem(t, w, http.StatusForbidden, tc.code)
		var p mw.Problem
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || !strings.Contains(p.Detail, tc.detail) || strings.Contains(p.Detail, "secret wording") {
			t.Errorf("detail %q, want %q", p.Detail, tc.detail)
		}
	}
}

func TestRetryEndpoints(t *testing.T) {
	g := newMergeRig(t)
	ctx := context.Background()
	if err := g.store.RecordMerge(ctx, store.Merged{
		Now: g.clock.Now(), PRID: "PR_1", Key: "k",
		Retries: []store.RetryRow{{PRID: "PR_1", TicketKey: "INP-1", TransitionID: "31", ToStatus: "Done", LastError: "boom"}, {TicketKey: "INP-2", TransitionID: "21", LastError: "x"}},
		Encode:  func(map[string]int64) ([]byte, error) { return []byte("{}"), nil },
	}); err != nil {
		t.Fatal(err)
	}
	w := g.do(http.MethodGet, "/api/retries", "", cookie(g.l.cookie))
	var list gen.Retries
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Retries) != 2 ||
		*list.Retries[0].PrId != "PR_1" || list.Retries[0].ToStatus != "Done" || list.Retries[1].PrId != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	id := list.Retries[0].Id

	g.m.step = merge.Step{Kind: merge.StepTransition, Ticket: "INP-1", State: merge.StateOK, ToStatus: "Done"}
	w = g.do(http.MethodPost, "/api/retries/1", "", g.l.write()...)
	var res gen.RetryResult
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &res) != nil || res.Step.State != "ok" || g.m.retryID != id {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for err, want := range map[error][2]any{
		&merge.Error{Code: merge.CodeNotFound}:              {404, "not_found"},
		&merge.Error{Code: merge.CodeTransitionUnavailable}: {409, "transition_unavailable"},
		&merge.Error{Code: merge.CodeNotConnected}:          {409, "not_connected"},
		&merge.Error{Code: merge.CodeMissingField}:          {422, "missing_field"},
		&jira.StatusError{Status: 500}:                      {502, "upstream_error"},
	} {
		g.m.retryErr = err
		wantProblem(t, g.do(http.MethodPost, "/api/retries/1", "", g.l.write()...), want[0].(int), want[1].(string))
	}

	if w := g.do(http.MethodDelete, "/api/retries/2", "", g.l.write()...); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	wantProblem(t, g.do(http.MethodDelete, "/api/retries/2", "", g.l.write()...), http.StatusNotFound, "not_found")
	if got, _ := g.store.Retries(ctx); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	if w := g.do(http.MethodPost, "/api/retries/0", "", g.l.write()...); w.Code != http.StatusBadRequest {
		t.Fatalf("%d", w.Code)
	}
}

func TestActivityIsPaginatedByCursor(t *testing.T) {
	g := newMergeRig(t)
	for i := range 5 {
		if err := g.store.RecordMerge(context.Background(), store.Merged{
			Now: g.clock.Now(), PRID: "PR_1", Key: string(rune('a' + i)),
			Activity: []store.ActivityRow{{Kind: "transition", TicketKey: "INP-1", PRID: "PR_1", TriggeredBy: "manual", Result: "failed", Summary: string(rune('a' + i)), Error: "e"}},
			Encode:   func(map[string]int64) ([]byte, error) { return []byte("{}"), nil },
		}); err != nil {
			t.Fatal(err)
		}
	}
	get := func(q string) gen.ActivityPage {
		w := g.do(http.MethodGet, "/api/activity"+q, "", cookie(g.l.cookie))
		var p gen.ActivityPage
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &p) != nil {
			t.Fatalf("%q: %d %s", q, w.Code, w.Body)
		}
		return p
	}
	p1 := get("?limit=2")
	if len(p1.Items) != 2 || p1.Items[0].Summary != "e" || p1.NextCursor == nil || *p1.NextCursor != p1.Items[1].Id || *p1.Items[0].Error != "e" || *p1.Items[0].TicketKey != "INP-1" {
		t.Fatalf("%+v", p1)
	}
	p2 := get("?limit=2&cursor=" + strconv.FormatInt(*p1.NextCursor, 10))
	if len(p2.Items) != 2 || p2.Items[0].Summary != "c" || p2.NextCursor == nil {
		t.Fatalf("%+v", p2)
	}
	p3 := get("?cursor=" + strconv.FormatInt(*p2.NextCursor, 10))
	if len(p3.Items) != 1 || p3.NextCursor != nil {
		t.Fatalf("%+v", p3)
	}
	for _, q := range []string{"?limit=0", "?limit=101", "?cursor=0", "?cursor=x"} {
		if w := g.do(http.MethodGet, "/api/activity"+q, "", cookie(g.l.cookie)); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, w.Code)
		}
	}
	if n := len(get("").Items); n != 5 {
		t.Fatalf("default page %d", n)
	}
}

func seedRepo(t *testing.T, g *mergeRig) {
	t.Helper()
	if err := g.store.SaveRefresh(context.Background(), store.Refresh{
		Now: g.clock.Now(), DefaultMethod: "squash", Version: 1, Snapshot: []byte("x"),
		Repos: []store.RepoRow{{ID: "R1", NameWithOwner: "acme/web"}},
		PRs:   []store.PRRow{{ID: "PR_1", RepoID: "R1", Number: 1, Title: "t", HeadBranch: "h", BaseBranch: "main", HeadSHA: "s"}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReposListGetAndPut(t *testing.T) {
	g := newMergeRig(t)
	seedRepo(t, g)
	var list gen.Repos
	w := g.do(http.MethodGet, "/api/repos", "", cookie(g.l.cookie))
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Repos) != 1 || !list.Repos[0].IsNew || list.Repos[0].OpenPrs != 1 ||
		list.Repos[0].DefaultMethod != "squash" || len(list.Repos[0].BranchRules) != 2 || list.Repos[0].BranchRules[0].Pattern != "main" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	put := func(id, body string) *httptest.ResponseRecorder {
		return g.do(http.MethodPut, "/api/repos/"+id, body, g.l.write()...)
	}
	w = put("R1", `{"defaultMethod":"rebase","branchRules":[{"pattern":"release/*","method":"merge"}],"clearNew":true}`)
	var one gen.RepoSettings
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &one) != nil || one.IsNew || one.DefaultMethod != "rebase" || len(one.BranchRules) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, relinks, _ := g.p.counts(); relinks != 1 {
		t.Fatalf("a save rebuilds the snapshot: %d", relinks)
	}
	if triggers, _, _ := g.p.counts(); triggers != 0 {
		t.Fatal("saving settings must not start an outside refresh")
	}
	if one.ApprovalsRequired != 0 {
		t.Fatalf("a repo that never set it needs no approval: %d", one.ApprovalsRequired)
	}
	w = g.do(http.MethodGet, "/api/repos/R1", "", cookie(g.l.cookie))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rebase"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	wantProblem(t, g.do(http.MethodGet, "/api/repos/R404", "", cookie(g.l.cookie)), 404, "not_found")
	wantProblem(t, put("R404", `{"defaultMethod":"merge","branchRules":[],"clearNew":false}`), 404, "not_found")

	g.p.relinkErr = errors.New("boom")
	wantProblem(t, put("R1", `{"defaultMethod":"merge","branchRules":[],"clearNew":false}`), 500, "internal_error")
	g.p.relinkErr = nil

	rules := func(n int) string {
		return strings.TrimSuffix(strings.Repeat(`{"pattern":"a","method":"merge"},`, n), ",")
	}
	for name, body := range map[string]string{
		"51 rules":         `{"defaultMethod":"merge","branchRules":[` + rules(51) + `],"clearNew":false}`,
		"empty pattern":    `{"defaultMethod":"merge","branchRules":[{"pattern":"","method":"merge"}],"clearNew":false}`,
		"201 chars":        `{"defaultMethod":"merge","branchRules":[{"pattern":"` + strings.Repeat("a", 201) + `","method":"merge"}],"clearNew":false}`,
		"space in pattern": `{"defaultMethod":"merge","branchRules":[{"pattern":"a b","method":"merge"}],"clearNew":false}`,
		"regex characters": `{"defaultMethod":"merge","branchRules":[{"pattern":"a(b)","method":"merge"}],"clearNew":false}`,
		"bad method":       `{"defaultMethod":"octopus","branchRules":[],"clearNew":false}`,
		"missing clearNew": `{"defaultMethod":"merge","branchRules":[]}`,
		"unknown field":    `{"defaultMethod":"merge","branchRules":[],"clearNew":false,"x":1}`,
	} {
		if w := put("R1", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if w := put("R1", `{"defaultMethod":"merge","branchRules":[`+rules(50)+`],"clearNew":false}`); w.Code != 200 {
		t.Fatalf("50 rules: %d", w.Code)
	}
	if w := put("R1", `{"defaultMethod":"merge","branchRules":[{"pattern":"A._/*-9","method":"merge"}],"clearNew":false}`); w.Code != 200 {
		t.Fatalf("allowed characters: %d %s", w.Code, w.Body)
	}
}

func TestPutReposSavesEveryRepoWithOneRelink(t *testing.T) {
	g := newMergeRig(t)
	seedRepo(t, g)
	put := func(body string) *httptest.ResponseRecorder {
		return g.do(http.MethodPut, "/api/repos", body, g.l.write()...)
	}
	w := put(`{"repos":[{"id":"R1","update":{"defaultMethod":"rebase","branchRules":[{"pattern":"release/*","method":"merge"}],"clearNew":true}}]}`)
	var list gen.Repos
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Repos) != 1 || list.Repos[0].IsNew ||
		list.Repos[0].DefaultMethod != "rebase" || len(list.Repos[0].BranchRules) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, relinks, _ := g.p.counts(); relinks != 1 {
		t.Fatalf("one rebuild for the whole save: %d", relinks)
	}
	wantProblem(t, put(`{"repos":[{"id":"R404","update":{"defaultMethod":"merge","branchRules":[],"clearNew":false}}]}`), 404, "not_found")
	if _, relinks, _ := g.p.counts(); relinks != 1 {
		t.Fatalf("a refused save rebuilds nothing: %d", relinks)
	}
	g.p.relinkErr = errors.New("boom")
	wantProblem(t, put(`{"repos":[{"id":"R1","update":{"defaultMethod":"merge","branchRules":[],"clearNew":false}}]}`), 500, "internal_error")
	g.p.relinkErr = nil

	item := `{"id":"R1","update":{"defaultMethod":"merge","branchRules":[],"clearNew":false}}`
	for name, body := range map[string]string{
		"empty list":  `{"repos":[]}`,
		"501 repos":   `{"repos":[` + strings.TrimSuffix(strings.Repeat(item+",", 501), ",") + `]}`,
		"no update":   `{"repos":[{"id":"R1"}]}`,
		"bad pattern": `{"repos":[{"id":"R1","update":{"defaultMethod":"merge","branchRules":[{"pattern":"","method":"merge"}],"clearNew":false}}]}`,
		"extra field": `{"repos":[` + item + `],"x":1}`,
	} {
		if w := put(body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	wantProblem(t, g.do(http.MethodPut, "/api/repos", `{"repos":[`+item+`]}`, cookie(g.l.cookie)), http.StatusForbidden, "bad_csrf")
}

func TestRepoApprovalsRequired(t *testing.T) {
	g := newMergeRig(t)
	seedRepo(t, g)
	put := func(body string) *httptest.ResponseRecorder {
		return g.do(http.MethodPut, "/api/repos/R1", body, g.l.write()...)
	}
	read := func(w *httptest.ResponseRecorder) gen.RepoSettings {
		t.Helper()
		var r gen.RepoSettings
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		return r
	}
	if r := read(put(`{"defaultMethod":"merge","branchRules":[],"clearNew":false,"approvalsRequired":2}`)); r.ApprovalsRequired != 2 {
		t.Fatalf("%+v", r)
	}
	if _, relinks, _ := g.p.counts(); relinks != 1 {
		t.Fatalf("a change of the setting rebuilds the snapshot so the counts follow: %d", relinks)
	}
	// Left out, the stored value stays.
	if r := read(put(`{"defaultMethod":"squash","branchRules":[],"clearNew":false}`)); r.ApprovalsRequired != 2 || r.DefaultMethod != "squash" {
		t.Fatalf("%+v", r)
	}
	if r := read(g.do(http.MethodGet, "/api/repos/R1", "", cookie(g.l.cookie))); r.ApprovalsRequired != 2 {
		t.Fatalf("%+v", r)
	}
	// Several repos at once.
	w := g.do(http.MethodPut, "/api/repos", `{"repos":[{"id":"R1","update":{"defaultMethod":"merge","branchRules":[],"clearNew":false,"approvalsRequired":10}}]}`, g.l.write()...)
	var list gen.Repos
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.Repos[0].ApprovalsRequired != 10 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for name, v := range map[string]string{"11": "11", "negative": "-1", "string": `"2"`, "fraction": "1.5", "null": "null"} {
		if w := put(`{"defaultMethod":"merge","branchRules":[],"clearNew":false,"approvalsRequired":` + v + `}`); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if r := read(g.do(http.MethodGet, "/api/repos/R1", "", cookie(g.l.cookie))); r.ApprovalsRequired != 10 {
		t.Fatalf("a rejected update changed it: %+v", r)
	}
	// 0 is a value of its own, not "left out": it sets the setting to nothing required.
	if r := read(put(`{"defaultMethod":"merge","branchRules":[],"clearNew":false,"approvalsRequired":0}`)); r.ApprovalsRequired != 0 {
		t.Fatalf("0 is allowed and stored: %+v", r)
	}
	if r := read(put(`{"defaultMethod":"merge","branchRules":[],"clearNew":false}`)); r.ApprovalsRequired != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestRepoMethodInheritanceAndDeleteBranchChoice(t *testing.T) {
	g := newMergeRig(t)
	seedRepo(t, g)
	put := func(body string) *httptest.ResponseRecorder {
		return g.do(http.MethodPut, "/api/repos/R1", body, g.l.write()...)
	}
	read := func(w *httptest.ResponseRecorder) gen.RepoSettings {
		t.Helper()
		var r gen.RepoSettings
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		return r
	}
	if r := read(g.do(http.MethodGet, "/api/repos/R1", "", cookie(g.l.cookie))); !r.InheritMethod || r.DeleteBranch != gen.DeleteBranchChoiceDefault {
		t.Fatalf("a new repo follows the defaults: %+v", r)
	}
	r := read(put(`{"defaultMethod":"rebase","branchRules":[],"clearNew":false,"inheritMethod":false,"deleteBranch":"no"}`))
	if r.InheritMethod || r.DeleteBranch != gen.DeleteBranchChoiceNo || r.DefaultMethod != "rebase" {
		t.Fatalf("%+v", r)
	}
	// The delete-branch choice left out stays; the inheritance left out is false, like the method it goes with.
	r = read(put(`{"defaultMethod":"merge","branchRules":[],"clearNew":false}`))
	if r.InheritMethod || r.DeleteBranch != gen.DeleteBranchChoiceNo {
		t.Fatalf("%+v", r)
	}
	r = read(put(`{"defaultMethod":"merge","branchRules":[],"clearNew":false,"inheritMethod":true,"deleteBranch":"yes"}`))
	if !r.InheritMethod || r.DeleteBranch != gen.DeleteBranchChoiceYes {
		t.Fatalf("%+v", r)
	}
	if _, relinks, _ := g.p.counts(); relinks != 3 {
		t.Fatalf("each save rebuilds the snapshot: %d", relinks)
	}
	for name, v := range map[string]string{
		"unknown choice": `"maybe"`, "boolean": `true`, "null": `null`, "empty": `""`,
	} {
		if w := put(`{"defaultMethod":"merge","branchRules":[],"clearNew":false,"deleteBranch":` + v + `}`); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if w := put(`{"defaultMethod":"merge","branchRules":[],"clearNew":false,"inheritMethod":"yes"}`); w.Code != http.StatusBadRequest {
		t.Errorf("inheritMethod string: %d %s", w.Code, w.Body)
	}
	w := g.do(http.MethodPut, "/api/repos", `{"repos":[{"id":"R1","update":{"defaultMethod":"squash","branchRules":[],"clearNew":false,"inheritMethod":false,"deleteBranch":"default"}}]}`, g.l.write()...)
	var list gen.Repos
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.Repos[0].InheritMethod || list.Repos[0].DeleteBranch != gen.DeleteBranchChoiceDefault {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
