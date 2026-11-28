package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

// refusal serves one fixed answer and returns the client that reads it.
func refusal(t *testing.T, status int, headers map[string]string, body string) *github.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return client(srv)
}

const ssoHeader = "required; url=https://github.com/orgs/acme/sso?authorization_request=A"

// forbiddenCases are the refusals GitHub sends, as a REST 403 body, and what each is.
var forbiddenCases = []struct {
	name    string
	headers map[string]string
	message string
	reason  string // "sso", "policy" or "" for a permission error
	org     string
	accept  []string
}{
	{"sso header", map[string]string{"X-GitHub-SSO": ssoHeader}, "Resource not accessible by personal access token", "sso", "acme", nil},
	{"saml message", nil, "Resource protected by organization SAML enforcement. The `acme` organization requires SSO.", "sso", "acme", nil},
	{"saml lower case", nil, "saml single sign-on required", "sso", "", nil},
	{"oauth app restrictions", nil, "The `acme` organization has enabled OAuth App access restrictions, meaning that data access to third-parties is limited.", "policy", "acme", nil},
	{"classic tokens forbidden", nil, "The `acme` organization forbids access via a personal access token (classic). Please use or request a fine-grained personal access token.", "policy", "acme", nil},
	{"token lifetime", nil, "`acme` forbids access via a fine-grained personal access tokens if the token's lifetime is greater than 366 days.", "policy", "acme", nil},
	{"lifetime wording", nil, "The personal access token lifetime is too long", "policy", "", nil},
	{"fine-grained forbidden", nil, "Fine-grained personal access tokens are forbidden for this organization", "policy", "", nil},
	{"fine-grained policy", nil, "A fine-grained personal access token policy applies", "policy", "", nil},
	{"ip allow list", nil, "Although you appear to have the correct authorization credentials, the `acme` organization has an IP allow list enabled, and 1.2.3.4 is not permitted to access this resource.", "policy", "acme", nil},
	{"pat permission", map[string]string{"X-Accepted-GitHub-Permissions": "contents=write"}, "Resource not accessible by personal access token", "", "", []string{"contents=write"}},
	{"integration permission", map[string]string{"X-Accepted-GitHub-Permissions": "pull_requests=write; contents=write"}, "Resource not accessible by integration", "", "", []string{"pull_requests=write", "contents=write"}},
	{"permission without header", nil, "Must have admin rights to Repository.", "", "", nil},
	{"empty message", nil, "", "", "", nil},
}

// check says whether err is the refusal the case describes.
func checkRefusal(t *testing.T, name string, err error, reason, org string, accept []string) {
	t.Helper()
	var blocked *github.OrgBlocked
	var perm *github.PermissionError
	switch {
	case reason != "":
		if !errors.As(err, &blocked) || blocked.Reason != reason || blocked.Org != org {
			t.Fatalf("%s: want OrgBlocked %s %q, got %#v", name, reason, org, err)
		}
		if (reason == "sso") != strings.HasPrefix(blocked.AuthorizeURL, "https://github.com/orgs/") && org != "" {
			t.Errorf("%s: authorize url %q", name, blocked.AuthorizeURL)
		}
		if reason == "policy" && (blocked.AuthorizeURL != "" || blocked.Message == "") {
			t.Errorf("%s: policy block %+v", name, blocked)
		}
	default:
		if !errors.As(err, &perm) || !slices.Equal(perm.Accepted, accept) {
			t.Fatalf("%s: want PermissionError %v, got %#v", name, accept, err)
		}
		if errors.As(err, &blocked) {
			t.Errorf("%s: a permission error must not be a block", name)
		}
	}
}

func TestMergeClassifiesForbidden(t *testing.T) {
	for _, tc := range forbiddenCases {
		c := refusal(t, 403, tc.headers, `{"message":`+quote(tc.message)+`}`)
		_, err := c.Merge(context.Background(), "tok", "acme/web", 7, "squash", "sha1")
		checkRefusal(t, tc.name, err, tc.reason, tc.org, tc.accept)
	}
}

func TestDeleteBranchClassifiesForbidden(t *testing.T) {
	for _, tc := range forbiddenCases {
		c := refusal(t, 403, tc.headers, `{"message":`+quote(tc.message)+`}`)
		err := c.DeleteBranch(context.Background(), "tok", "acme/web", "feat")
		checkRefusal(t, tc.name, err, tc.reason, tc.org, tc.accept)
	}
}

func TestSecondaryRateLimitIsNotAPermissionError(t *testing.T) {
	body := `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`
	c := refusal(t, 403, nil, body)
	var rl *github.RateLimitedError
	if _, err := c.Merge(context.Background(), "tok", "acme/web", 7, "squash", "s"); !errors.As(err, &rl) {
		t.Fatalf("merge: %v", err)
	}
	if err := c.DeleteBranch(context.Background(), "tok", "acme/web", "feat"); !errors.As(err, &rl) {
		t.Fatalf("delete: %v", err)
	}
	var perm *github.PermissionError
	if _, err := c.Merge(context.Background(), "tok", "acme/web", 7, "squash", "s"); errors.As(err, &perm) {
		t.Fatal("permission error")
	}
}

func TestPullRequestClassifiesForbidden(t *testing.T) {
	for _, tc := range forbiddenCases {
		body := `{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":` + quote(tc.message) + `}]}`
		c := refusal(t, 200, tc.headers, body)
		_, err := c.PullRequest(context.Background(), "tok", "PR_1")
		checkRefusal(t, tc.name, err, tc.reason, tc.org, tc.accept)
	}
}

func TestPullRequestRestShapedForbidden(t *testing.T) {
	c := refusal(t, 403, map[string]string{"X-Accepted-GitHub-Permissions": "pull_requests=read"}, `{"message":"Resource not accessible by personal access token"}`)
	_, err := c.PullRequest(context.Background(), "tok", "PR_1")
	checkRefusal(t, "rest shaped", err, "", "", []string{"pull_requests=read"})
}

func TestPullRequestWithNodeIgnoresPermissionErrors(t *testing.T) {
	body := `{"data":{"node":{"__typename":"PullRequest","id":"PR_1","number":3,"headRefOid":"abc","state":"OPEN"}},` +
		`"errors":[{"type":"FORBIDDEN","path":["node","reviewThreads"],"message":"Resource not accessible by personal access token"}]}`
	pr, err := refusal(t, 200, nil, body).PullRequest(context.Background(), "tok", "PR_1")
	if err != nil || pr.ID != "PR_1" || pr.HeadSHA != "abc" {
		t.Fatalf("%+v %v", pr, err)
	}
	// An SSO or policy refusal stays an error even when a node came back.
	sso := strings.Replace(body, "Resource not accessible by personal access token", "SAML enforcement for the `acme` organization", 1)
	_, err = refusal(t, 200, nil, sso).PullRequest(context.Background(), "tok", "PR_1")
	checkRefusal(t, "sso with node", err, "sso", "acme", nil)
}

func TestAcceptedPermissionsParsing(t *testing.T) {
	cases := []struct {
		header string
		want   []string
	}{
		{"contents=write", []string{"contents=write"}},
		{"pull_requests=write; contents=write", []string{"pull_requests=write", "contents=write"}},
		{"pull_requests=write, contents=admin", []string{"pull_requests=write", "contents=admin"}},
		{" contents=read ;; contents=read ", []string{"contents=read"}},
		{"contents=delete", nil},
		{"Contents=write", nil},
		{"contents=write<script>", nil},
		{"=write", nil},
		{"contents", nil},
		{strings.Repeat("a", 51) + "=write", nil},
		{"ok=read; <b>=write; x y=read; contents=write", []string{"ok=read", "contents=write"}},
		{"", nil},
	}
	for _, tc := range cases {
		c := refusal(t, 403, map[string]string{"X-Accepted-GitHub-Permissions": tc.header}, `{"message":"Resource not accessible by integration"}`)
		_, err := c.Merge(context.Background(), "tok", "acme/web", 7, "squash", "s")
		var perm *github.PermissionError
		if !errors.As(err, &perm) || !slices.Equal(perm.Accepted, tc.want) {
			t.Errorf("%q: got %#v, want %v", tc.header, err, tc.want)
		}
	}
}

func TestPermissionErrorMessageIsCut(t *testing.T) {
	c := refusal(t, 403, nil, `{"message":"`+strings.Repeat("x", 500)+`"}`)
	_, err := c.Merge(context.Background(), "tok", "acme/web", 7, "squash", "s")
	var perm *github.PermissionError
	if !errors.As(err, &perm) || len(perm.Message) > 203 || perm.Error() == "" {
		t.Fatalf("%#v", err)
	}
}

const oneOwner = "A fine-grained token works for one owner only, so a repository of another owner needs a token from that owner. Add one in Accounts."

func TestWithRepoNamesTheRepositoryOnlyOnPermissionErrors(t *testing.T) {
	perm := &github.PermissionError{Accepted: []string{"contents=write"}}
	wrapped := fmt.Errorf("wrapped: %w", perm)
	if got := github.WithRepo(wrapped, "acme/web", "merge in"); got != wrapped || perm.Repo != "acme/web" || perm.Action != "merge in" {
		t.Fatalf("%#v", perm)
	}
	other := errors.New("boom")
	if github.WithRepo(other, "acme/web", "") != other || github.WithRepo(nil, "acme/web", "") != nil {
		t.Fatal("other errors pass through")
	}
}

func TestWithTokenNamesTheTokenOnlyOnPermissionErrors(t *testing.T) {
	perm := &github.PermissionError{Message: "x"}
	wrapped := fmt.Errorf("merge: %w", perm)
	if got := github.WithToken(wrapped, "Work"); got != wrapped || perm.Token != "Work" {
		t.Fatalf("%v %+v", got, perm)
	}
	other := errors.New("boom")
	if github.WithToken(other, "Work") != other || github.WithToken(nil, "Work") != nil {
		t.Fatal("other errors pass through")
	}
}

func TestPermissionErrorDetail(t *testing.T) {
	cases := []struct {
		perm github.PermissionError
		want string
	}{
		{github.PermissionError{Accepted: []string{"contents=write"}}, "The token needs the GitHub permission contents: write"},
		{github.PermissionError{Accepted: []string{"pull_requests=write", "contents=write"}}, "The token needs one of these GitHub permissions: pull_requests: write or contents: write"},
		{github.PermissionError{Message: "Nope"}, "The token lacks a GitHub permission this action needs: Nope"},
		{github.PermissionError{Accepted: []string{"pull_requests=write"}, Repo: "ATP-MiNTo/PRDashboards"},
			"This token can't write to ATP-MiNTo/PRDashboards: it needs the GitHub permission pull_requests: write. " + oneOwner},
		{github.PermissionError{Accepted: []string{"contents=write"}, Repo: "acme/web", Action: "merge in"},
			"This token can't merge in acme/web: it needs the GitHub permission contents: write. " + oneOwner},
		{github.PermissionError{Accepted: []string{"pull_requests=write", "contents=write"}, Repo: "acme/web"},
			"This token can't write to acme/web: it needs one of these GitHub permissions: pull_requests: write or contents: write. " + oneOwner},
		{github.PermissionError{Message: "Resource not accessible.", Repo: "acme/web"},
			"This token can't write to acme/web (GitHub says: Resource not accessible). " + oneOwner},
		{github.PermissionError{Repo: "acme/web"}, "This token can't write to acme/web. " + oneOwner},
		// A named token reads as such, with and without a repository.
		{github.PermissionError{Accepted: []string{"pull_requests=write"}, Repo: "ATP-MiNTo/PRDashboards", Token: "Work"},
			`The token "Work" can't write to ATP-MiNTo/PRDashboards: it needs the GitHub permission pull_requests: write. ` + oneOwner},
		{github.PermissionError{Accepted: []string{"contents=write"}, Repo: "acme/web", Action: "merge in", Token: "OSS (hubot)"},
			`The token "OSS (hubot)" can't merge in acme/web: it needs the GitHub permission contents: write. ` + oneOwner},
		{github.PermissionError{Accepted: []string{"contents=write"}, Token: "Work"}, `The token "Work" needs the GitHub permission contents: write`},
		{github.PermissionError{Message: "Nope", Token: "Work"}, `The token "Work" lacks a GitHub permission this action needs: Nope`},
	}
	for _, tc := range cases {
		if got := tc.perm.Detail(); got != tc.want {
			t.Errorf("%q, want %q", got, tc.want)
		}
	}
}

func quote(s string) string {
	b := new(strings.Builder)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func TestPullRequestNestedForbiddenFieldDoesNotBlock(t *testing.T) {
	body := `{"data":{"node":{"__typename":"PullRequest","id":"PR_1","number":3,"headRefOid":"abc","state":"OPEN"}},` +
		`"errors":[{"type":"FORBIDDEN","path":["node","reviewRequests","nodes",0,"requestedReviewer"],"message":"Resource not accessible by personal access token"}]}`
	pr, err := refusal(t, 200, nil, body).PullRequest(context.Background(), "tok", "PR_1")
	if err != nil || pr.ID != "PR_1" || pr.HeadSHA != "abc" {
		t.Fatalf("%+v %v", pr, err)
	}
}

func TestRefusalsAreLoggedWithoutSecrets(t *testing.T) {
	var buf strings.Builder
	c := refusal(t, 403, map[string]string{"X-Accepted-GitHub-Permissions": "contents=write"}, `{"message":"Resource not accessible by personal access token"}`)
	c.Log = slog.New(slog.NewTextHandler(&buf, nil))
	_, _ = c.Merge(context.Background(), "secret-token", "acme/web", 7, "squash", "sha1")
	body := `{"data":{"node":{"__typename":"PullRequest","id":"PR_1","headRefOid":"abc"}},"errors":[{"type":"FORBIDDEN","path":["node","reviewRequests"],"message":"Resource not accessible by integration"}]}`
	c2 := refusal(t, 200, nil, body)
	c2.Log = c.Log
	_, _ = c2.PullRequest(context.Background(), "secret-token", "PR_1")
	out := buf.String()
	for _, want := range []string{"PUT /repos/acme/web/pulls/7/merge", "status=403", "accepted_permissions=\"contents=write\"", "Resource not accessible by personal access token", "error_type=FORBIDDEN", "error_path=\"[node reviewRequests]\""} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "secret-token") || strings.Contains(out, "sha1") {
		t.Errorf("secret in log: %s", out)
	}
}

// hiddenChecks is what GitHub answers when a fine-grained token cannot read check runs: the rollup
// is there, its three context nodes are null and each has a FORBIDDEN error with explicit flags.
func hiddenChecks() string {
	var errs []string
	for i := range 3 {
		errs = append(errs, `{"type":"FORBIDDEN","path":["node","commits","nodes",0,"commit","statusCheckRollup","contexts","nodes",`+
			string(rune('0'+i))+`],"extensions":{"saml_failure":false,"ip_allow_list_failure":false},`+
			`"locations":[{"line":1,"column":160}],"message":"Resource not accessible by personal access token"}`)
	}
	return `"errors":[` + strings.Join(errs, ",") + `]`
}

func TestPullRequestWithHiddenCheckRuns(t *testing.T) {
	body := `{"data":{"node":{"__typename":"PullRequest","id":"PR_1","number":3,"headRefOid":"abc","state":"OPEN",` +
		`"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS","contexts":{"totalCount":3,"nodes":[null,null,null]}}}}]}}},` + hiddenChecks() + `}`
	pr, err := refusal(t, 200, nil, body).PullRequest(context.Background(), "tok", "PR_1")
	if err != nil || pr.ID != "PR_1" || pr.HeadSHA != "abc" {
		t.Fatalf("%+v %v", pr, err)
	}
	if pr.CI.State != github.CIPassing || pr.CI.Total != 3 || len(pr.CI.Checks) != 0 {
		t.Fatalf("ci %+v", pr.CI)
	}
}

// searchWithErrors runs SearchOpen against pages that carry the given errors array.
func searchWithErrors(t *testing.T, errs string) (github.SearchResult, error) {
	t.Helper()
	c := serveBodies(t, func(string) fixtureBody {
		var extra map[string]any
		_ = json.Unmarshal([]byte(`{"errors":`+errs+`}`), &extra)
		return page([]string{"A"}, 1, 4900, 1, extra)
	})
	return c.SearchOpen(context.Background(), "tok")
}

func TestSearchWithHiddenCheckRunsHasNoBanner(t *testing.T) {
	c := serveBodies(t, func(string) fixtureBody {
		var extra map[string]any
		_ = json.Unmarshal([]byte(`{`+hiddenChecks()+`}`), &extra)
		return page([]string{"A"}, 1, 4900, 1, extra)
	})
	res, err := c.SearchOpen(context.Background(), "tok")
	if err != nil || len(res.PRs) != 1 || len(res.Blocked) != 0 || len(res.Errors) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSearchErrorsAroundHiddenChecks(t *testing.T) {
	flags := `"extensions":{"saml_failure":false,"ip_allow_list_failure":false}`
	const msg = `"message":"Resource not accessible by personal access token"`
	check := `"path":["search","nodes",0,"commits","nodes",0,"commit","statusCheckRollup","contexts","nodes",1]`
	cases := []struct {
		name        string
		errs        string
		wantErrors  int // three searches (assigned, authored, review requested) each report the error once
		wantBlocked int
	}{
		{"hidden checks only", `[{"type":"FORBIDDEN",` + check + `,` + flags + `,` + msg + `}]`, 0, 0},
		{"permission error off the check path stays", `[{"type":"FORBIDDEN","path":["search","nodes",1],` + flags + `,` + msg + `}]`, 3, 0},
		{"hidden check next to a real error", `[{"type":"FORBIDDEN",` + check + `,` + flags + `,` + msg + `},{"type":"INTERNAL","message":"boom"}]`, 3, 0},
		{"saml flag on a check path is SSO", `[{"type":"FORBIDDEN",` + check + `,"extensions":{"saml_failure":true,"ip_allow_list_failure":false},"message":"Resource protected by organization SAML enforcement. You must grant your token access to \"acme\""}]`, 0, 1},
	}
	for _, tc := range cases {
		res, err := searchWithErrors(t, tc.errs)
		if err != nil || len(res.PRs) != 1 || len(res.Errors) != tc.wantErrors || len(res.Blocked) != tc.wantBlocked {
			t.Errorf("%s: %+v %v", tc.name, res, err)
		}
	}
}

func TestForbiddenExtensionsDecide(t *testing.T) {
	cases := []struct {
		name   string
		ext    string
		msg    string
		reason string
	}{
		{"saml flag", `{"saml_failure":true,"ip_allow_list_failure":false}`, "Resource not accessible by personal access token", "sso"},
		{"ip flag", `{"saml_failure":false,"ip_allow_list_failure":true}`, "whatever", "policy"},
		{"both false ignores wording", `{"saml_failure":false,"ip_allow_list_failure":false}`, "SAML enforcement; IP allow list; forbids access via", ""},
		{"no flags falls back to wording", `{}`, "SAML enforcement", "sso"},
		{"one flag only falls back to wording", `{"saml_failure":false}`, "SAML enforcement", "sso"},
	}
	for _, tc := range cases {
		body := `{"data":{"node":null},"errors":[{"type":"FORBIDDEN","extensions":` + tc.ext + `,"message":` + quote(tc.msg) + `}]}`
		_, err := refusal(t, 200, nil, body).PullRequest(context.Background(), "tok", "PR_1")
		checkRefusal(t, tc.name, err, tc.reason, "", nil)
	}
}
