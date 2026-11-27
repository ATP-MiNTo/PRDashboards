package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/review"
)

type stubReviewer struct {
	calls  []string
	req    review.Request
	result review.Result
	err    error
}

func (s *stubReviewer) do(action string, req review.Request) (review.Result, error) {
	s.calls, s.req = append(s.calls, action), req
	if s.err != nil {
		return review.Result{}, s.err
	}
	return review.Result{PRID: req.PRID, Action: action}, nil
}

func (s *stubReviewer) Approve(_ context.Context, r review.Request) (review.Result, error) {
	return s.do(review.ActionApprove, r)
}

func (s *stubReviewer) RequestChanges(_ context.Context, r review.Request) (review.Result, error) {
	return s.do(review.ActionRequestChanges, r)
}

func (s *stubReviewer) MarkReady(_ context.Context, r review.Request) (review.Result, error) {
	return s.do(review.ActionReady, r)
}

type reviewRig struct {
	*snapRig
	r *stubReviewer
}

func newReviewRig(t *testing.T) *reviewRig {
	t.Helper()
	r := &stubReviewer{}
	sp := newStubPoller()
	g := newRig(t, webFS(), func(c *api.Config) { c.Poller, c.Reviewer = sp, r })
	return &reviewRig{snapRig: &snapRig{rig: g, p: sp, l: g.login()}, r: r}
}

const sha = "0123456789abcdef0123456789abcdef01234567"

var reviewPaths = map[string]string{
	review.ActionApprove: "approve", review.ActionRequestChanges: "request-changes", review.ActionReady: "ready",
}

func (g *reviewRig) post(action, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	return g.do(http.MethodPost, "/api/prs/PR_1/"+reviewPaths[action], body, append(g.l.write(), opts...)...)
}

func bodyFor(action, extra string) string {
	b := `{"expectedHeadSha":"` + sha + `"`
	if action == review.ActionRequestChanges && extra == "" {
		extra = `"body":"please rename"`
	}
	if extra != "" {
		b += "," + extra
	}
	return b + "}"
}

func TestReviewEndpointsPassTheRequestAndAnswerTheResult(t *testing.T) {
	g := newReviewRig(t)
	for _, tc := range []struct{ action, extra, body string }{
		{review.ActionApprove, `"body":"LGTM"`, "LGTM"},
		{review.ActionApprove, ``, ""},
		{review.ActionRequestChanges, `"body":"please rename"`, "please rename"},
		{review.ActionReady, ``, ""},
	} {
		w := g.post(tc.action, bodyFor(tc.action, tc.extra))
		var out gen.ReviewResult
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.PrId != "PR_1" || string(out.Action) != tc.action {
			t.Fatalf("%s: %d %s", tc.action, w.Code, w.Body)
		}
		if g.r.req.PRID != "PR_1" || g.r.req.ExpectedHeadSHA != sha || g.r.req.Body != tc.body {
			t.Fatalf("%s: %+v", tc.action, g.r.req)
		}
	}
}

func TestReviewRequestValidation(t *testing.T) {
	g := newReviewRig(t)
	long := strings.Repeat("a", 65537)
	for name, tc := range map[string]struct {
		action, body string
		want         int
	}{
		"approve without head":          {review.ActionApprove, `{}`, 400},
		"approve bad head":              {review.ActionApprove, `{"expectedHeadSha":"xyz"}`, 400},
		"approve head with a slash":     {review.ActionApprove, `{"expectedHeadSha":"abc1234/../x"}`, 400},
		"approve unknown field":         {review.ActionApprove, bodyFor(review.ActionApprove, `"x":1`), 400},
		"approve body not a string":     {review.ActionApprove, bodyFor(review.ActionApprove, `"body":1`), 400},
		"approve body too long":         {review.ActionApprove, bodyFor(review.ActionApprove, `"body":"`+long+`"`), 413}, // the 64 KiB request cap comes first
		"request changes without body":  {review.ActionRequestChanges, `{"expectedHeadSha":"` + sha + `"}`, 400},
		"request changes empty body":    {review.ActionRequestChanges, `{"expectedHeadSha":"` + sha + `","body":""}`, 400},
		"request changes null body":     {review.ActionRequestChanges, `{"expectedHeadSha":"` + sha + `","body":null}`, 400},
		"request changes body too long": {review.ActionRequestChanges, bodyFor(review.ActionRequestChanges, `"body":"`+long+`"`), 413},
		"ready without head":            {review.ActionReady, `{}`, 400},
		"ready unknown field":           {review.ActionReady, bodyFor(review.ActionReady, `"body":"x"`), 400},
		"not an object":                 {review.ActionApprove, `[]`, 400},
	} {
		if w := g.post(tc.action, tc.body); w.Code != tc.want {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if len(g.r.calls) != 0 {
		t.Fatalf("an invalid request reached the service: %v", g.r.calls)
	}
	// The largest text the schema allows passes (the 64 KiB request cap is checked first).
	if w := g.post(review.ActionApprove, bodyFor(review.ActionApprove, `"body":"`+strings.Repeat("a", 65000)+`"`)); w.Code != 200 {
		t.Errorf("large body: %d", w.Code)
	}
}

// TestReviewEndpointsAreGuardedLikeTheMerge runs the checks every write passes: session, CSRF,
// Origin, Sec-Fetch-Site, Host, content type and the body size.
func TestReviewEndpointsAreGuardedLikeTheMerge(t *testing.T) {
	g := newReviewRig(t)
	for action, path := range reviewPaths {
		url := "/api/prs/PR_1/" + path
		body := bodyFor(action, "")
		wantProblem(t, g.do(http.MethodPost, url, body), http.StatusUnauthorized, "unauthenticated")
		wantProblem(t, g.do(http.MethodPost, url, body, cookie(g.l.cookie)), http.StatusForbidden, "bad_csrf")
		wantProblem(t, g.do(http.MethodPost, url, body, cookie(g.l.cookie), csrf("wrong")), http.StatusForbidden, "bad_csrf")
		wantProblem(t, g.do(http.MethodPost, url, body, append(g.l.write(), header("Origin", "https://evil.example"))...), http.StatusForbidden, "bad_origin")
		wantProblem(t, g.do(http.MethodPost, url, body, append(g.l.write(), header("Sec-Fetch-Site", "cross-site"))...), http.StatusForbidden, "cross_site")
		wantProblem(t, g.do(http.MethodPost, url, body, append(g.l.write(), withHost("evil.example:4317"))...), http.StatusMisdirectedRequest, "misdirected_host")
		wantProblem(t, g.do(http.MethodPost, url, body, append(g.l.write(), header("Content-Type", "text/plain"))...), http.StatusUnsupportedMediaType, "unsupported_media_type")
		wantProblem(t, g.do(http.MethodPost, url, strings.Repeat(" ", 70000)+body, g.l.write()...), http.StatusRequestEntityTooLarge, "body_too_large")
		wantProblem(t, g.do(http.MethodPost, url, `{"expectedHeadSha":"`+sha+`"}{}`, g.l.write()...), http.StatusBadRequest, "invalid_body")
		wantProblem(t, g.do(http.MethodGet, url, "", cookie(g.l.cookie)), http.StatusNotFound, "not_found")
		wantProblem(t, g.do(http.MethodPost, "/api/prs/bad%20id/"+path, body, g.l.write()...), http.StatusBadRequest, "invalid_body")
	}
	if len(g.r.calls) != 0 {
		t.Fatalf("a refused request reached the service: %v", g.r.calls)
	}
}

func TestReviewRefusalsAreConflictsWithTheFreshState(t *testing.T) {
	g := newReviewRig(t)
	fresh := &merge.Fresh{State: "OPEN", HeadSHA: "new", BaseRef: "main", IsDraft: true, Mergeable: "MERGEABLE", MergeState: "DRAFT"}
	for _, code := range []string{merge.CodeHeadMoved, merge.CodeNotOpen, merge.CodeNotDraft, merge.CodeOwnPullRequest, merge.CodeNotConnected} {
		for action := range reviewPaths {
			g.r.err = &merge.Error{Code: code, Detail: "d <b>", Fresh: fresh}
			w := g.post(action, bodyFor(action, ""))
			var p gen.MergeProblem
			if w.Code != http.StatusConflict || w.Header().Get("Content-Type") != "application/problem+json" || json.Unmarshal(w.Body.Bytes(), &p) != nil ||
				p.Code != code || p.Pr == nil || p.Pr.HeadSha != "new" || !p.Pr.IsDraft || *p.Detail != "d <b>" {
				t.Fatalf("%s %s: %d %s", action, code, w.Code, w.Body)
			}
		}
	}
	g.r.err = &merge.Error{Code: merge.CodeNotConnected}
	if w := g.post(review.ActionApprove, bodyFor(review.ActionApprove, "")); !strings.Contains(w.Body.String(), `"pr":null`) {
		t.Fatalf("%s", w.Body)
	}
}

func TestReviewOtherRefusals(t *testing.T) {
	g := newReviewRig(t)
	g.r.err = &merge.Error{Code: merge.CodeNotFound, Detail: "unknown pull request"}
	wantProblem(t, g.post(review.ActionApprove, bodyFor(review.ActionApprove, "")), http.StatusNotFound, "not_found")
	for _, code := range []string{merge.CodeBodyRequired, merge.CodeReviewRejected} {
		g.r.err = &merge.Error{Code: code, Detail: "d"}
		wantProblem(t, g.post(review.ActionRequestChanges, bodyFor(review.ActionRequestChanges, "")), http.StatusUnprocessableEntity, code)
	}
}

func TestReviewProviderFailuresNameTheMissingPermission(t *testing.T) {
	g := newReviewRig(t)
	cases := []struct {
		err    error
		status int
		code   string
		detail string
	}{
		{&github.PermissionError{Message: "Resource not accessible by personal access token", Accepted: []string{"pull_requests=write"}},
			http.StatusForbidden, "missing_permission", "The token needs the GitHub permission pull_requests: write"},
		{&github.PermissionError{Message: "Nope"}, http.StatusForbidden, "missing_permission", "The token lacks a GitHub permission this action needs: Nope"},
		{&github.OrgBlocked{Org: "acme", Reason: "sso"}, http.StatusForbidden, "org_blocked", "An organization has not authorized the token"},
		{&github.OrgBlocked{Org: "acme", Reason: "policy", Message: "secret wording"}, http.StatusForbidden, "org_blocked", "The acme organization policy does not allow this token"},
		{&github.RateLimitedError{}, http.StatusTooManyRequests, "rate_limited", ""},
		{&github.APIError{Status: 502}, http.StatusBadGateway, "upstream_error", ""},
		{errors.New("disk exploded"), http.StatusInternalServerError, "internal_error", ""},
	}
	for _, tc := range cases {
		for action := range reviewPaths {
			g.r.err = tc.err
			w := g.post(action, bodyFor(action, ""))
			wantProblem(t, w, tc.status, tc.code)
			if !strings.Contains(w.Body.String(), tc.detail) || strings.Contains(w.Body.String(), "secret wording") {
				t.Errorf("%s: %s, want detail %q", action, w.Body, tc.detail)
			}
		}
	}
}
