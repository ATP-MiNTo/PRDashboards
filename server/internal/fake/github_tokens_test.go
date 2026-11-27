//go:build e2e

package fake

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// twoOwners is a fake with the pull requests of acme and of oss/lib and the two tokens of
// different owners.
func twoOwners(t *testing.T) (*github, *httptest.Server) {
	t.Helper()
	g := newGitHub()
	g.st.Config.Review = true
	g.st.Config.ReadOnly = true
	srv := httptest.NewServer(g.routes())
	t.Cleanup(srv.Close)
	return g, srv
}

func call(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(b)
}

// repos lists the repositories and roles an open search returns for the token.
func repos(t *testing.T, token string) map[string]string {
	t.Helper()
	_, body := searchBodyFor(token, "is:pr is:open assignee:@me archived:false", githubConfig{Review: true, ReadOnly: true}, nil, time.Now())
	out := map[string]string{}
	for _, n := range body["data"].(map[string]any)["search"].(map[string]any)["nodes"].([]any) {
		repo := n.(map[string]any)["repository"].(map[string]any)
		out[repo["nameWithOwner"].(string)] = repo["viewerPermission"].(string)
	}
	return out
}

func TestTwoOwnerTokensSeeDifferentRepositories(t *testing.T) {
	all := repos(t, "ghp_classic_ok")
	if all["acme/web"] != "WRITE" || all["oss/lib"] != "READ" || len(all) < 5 {
		t.Fatalf("the unrestricted tokens see everything with the fixture's role: %v", all)
	}
	acme := repos(t, "github_pat_acme")
	if acme["acme/web"] != "WRITE" || acme["acme/review"] != "WRITE" || acme["oss/lib"] != "READ" || acme["acme-corp/billing"] != "" {
		t.Fatalf("%v", acme)
	}
	oss := repos(t, "github_pat_oss")
	if len(oss) != 1 || oss["oss/lib"] != "WRITE" {
		t.Fatalf("a token of another owner sees only what it was granted: %v", oss)
	}
}

func TestTwoOwnerTokensHaveTheirOwnLogin(t *testing.T) {
	_, srv := twoOwners(t)
	for token, want := range map[string]string{"github_pat_acme": "octocat", "github_pat_oss": "hubot", "ghp_classic_ok": "octocat"} {
		_, _, body := call(t, srv, http.MethodPost, "/graphql", token, `{"query":"query { viewer { login } }"}`)
		if !strings.Contains(body, `"login":"`+want+`"`) {
			t.Errorf("%s: %s", token, body)
		}
	}
	// The pull requests the viewer opened were opened by octocat: author:@me finds none for hubot.
	_, body := searchBodyFor("github_pat_oss", "is:pr is:open author:@me archived:false", githubConfig{ReadOnly: true}, nil, time.Now())
	if n := body["data"].(map[string]any)["search"].(map[string]any)["nodes"].([]any); len(n) != 0 {
		t.Fatalf("%v", n)
	}
}

func TestTwoOwnerTokensNodeLookups(t *testing.T) {
	cfg := githubConfig{Review: true, ReadOnly: true}
	id := "PR_acme_review_31"
	if n := nodeBodyFor("github_pat_oss", id, cfg, nil, time.Now())["data"].(map[string]any)["node"]; n != nil {
		t.Fatalf("a repository the token does not see is null: %v", n)
	}
	n := nodeBodyFor("github_pat_acme", id, cfg, nil, time.Now())["data"].(map[string]any)["node"].(map[string]any)
	if n["repository"].(map[string]any)["viewerPermission"] != "WRITE" {
		t.Fatalf("%v", n)
	}
	lib := nodeBodyFor("github_pat_oss", "PR_oss_lib_41", cfg, nil, time.Now())["data"].(map[string]any)["node"].(map[string]any)
	if lib["repository"].(map[string]any)["viewerPermission"] != "WRITE" {
		t.Fatalf("the role is the token's, not the fixture's: %v", lib)
	}
}

func TestTwoOwnerTokensWrites(t *testing.T) {
	g, srv := twoOwners(t)
	merge := func(token, repo string, n int, sha string) int {
		code, _, _ := call(t, srv, http.MethodPut, "/repos/"+repo+"/pulls/"+itoa(n)+"/merge", token, `{"merge_method":"squash","sha":"`+sha+`"}`)
		return code
	}
	lib, _ := findFixture("oss/lib", 41, githubConfig{ReadOnly: true})
	web, _ := findFixture("acme/web", 101, githubConfig{})

	if got := merge("github_pat_acme", "oss/lib", 41, lib.headSHA(false)); got != http.StatusForbidden {
		t.Fatalf("a read-only role is refused: %d", got)
	}
	code, h, _ := call(t, srv, http.MethodPut, "/repos/oss/lib/pulls/41/merge", "github_pat_acme", `{"merge_method":"squash","sha":"x"}`)
	if code != http.StatusForbidden || h.Get("X-Accepted-GitHub-Permissions") != "contents=write" {
		t.Fatalf("%d %v", code, h)
	}
	if got := merge("github_pat_oss", "acme/web", 101, web.headSHA(false)); got != http.StatusNotFound {
		t.Fatalf("a repository of another owner is not found: %d", got)
	}
	if got := merge("github_pat_oss", "oss/lib", 41, lib.headSHA(false)); got != http.StatusOK {
		t.Fatalf("the token of the owner merges: %d", got)
	}
	if got := merge("github_pat_acme", "acme/web", 101, web.headSHA(false)); got != http.StatusOK {
		t.Fatalf("%d", got)
	}
	// The unrestricted tokens keep working as before, role or not.
	lib2, _ := findFixture("oss/lib", 41, githubConfig{ReadOnly: true})
	g.mu.Lock()
	g.st.Merged = nil
	g.mu.Unlock()
	if got := merge("ghp_classic_ok", "oss/lib", 41, lib2.headSHA(false)); got != http.StatusOK {
		t.Fatalf("%d", got)
	}

	// Reviews and the ready mark follow the same rules.
	rcode, rh, _ := call(t, srv, http.MethodPost, "/repos/oss/lib/pulls/41/reviews", "github_pat_acme", `{"event":"APPROVE","commit_id":"x"}`)
	if rcode != http.StatusForbidden || rh.Get("X-Accepted-GitHub-Permissions") != "pull_requests=write" {
		t.Fatalf("%d %v", rcode, rh)
	}
	if rcode, _, _ = call(t, srv, http.MethodPost, "/repos/acme/review/pulls/31/reviews", "github_pat_oss", `{"event":"APPROVE","commit_id":"x"}`); rcode != http.StatusNotFound {
		t.Fatalf("%d", rcode)
	}
	rev, _ := findFixture("acme/review", 31, githubConfig{Review: true})
	if rcode, _, _ = call(t, srv, http.MethodPost, "/repos/acme/review/pulls/31/reviews", "github_pat_acme", `{"event":"APPROVE","commit_id":"`+rev.headSHA(false)+`"}`); rcode != http.StatusOK {
		t.Fatalf("%d", rcode)
	}
	// octocat opened #33; hubot did not, so only octocat is refused. hubot cannot see it at all.
	own, _ := findFixture("acme/review", 33, githubConfig{Review: true})
	if rcode, _, _ = call(t, srv, http.MethodPost, "/repos/acme/review/pulls/33/reviews", "github_pat_acme", `{"event":"APPROVE","commit_id":"`+own.headSHA(false)+`"}`); rcode != http.StatusUnprocessableEntity {
		t.Fatalf("%d", rcode)
	}

	// The delete follows the role too.
	if code, _, _ = call(t, srv, http.MethodDelete, "/repos/oss/lib/git/refs/heads/feat/ro-41", "github_pat_acme", ""); code != http.StatusForbidden {
		t.Fatalf("%d", code)
	}
	if code, _, _ = call(t, srv, http.MethodDelete, "/repos/oss/lib/git/refs/heads/feat/ro-41", "github_pat_oss", ""); code != http.StatusNoContent {
		t.Fatalf("%d", code)
	}

	// The calls are recorded with their tokens.
	st, _ := json.Marshal(g.st)
	var out struct{ Calls []fakeCall }
	_ = json.Unmarshal(st, &out)
	want := fakeCall{Op: "merge", Token: "github_pat_oss", Repo: "oss/lib"}
	if !slices.Contains(out.Calls, want) {
		t.Fatalf("%+v", out.Calls)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestPATsOfTheOwnersAreAcceptedAsFineGrainedTokens(t *testing.T) {
	_, srv := twoOwners(t)
	for _, token := range []string{"github_pat_acme", "github_pat_oss"} {
		code, h, _ := call(t, srv, http.MethodPost, "/graphql", token, `{"query":"query { viewer { login } }"}`)
		if code != http.StatusOK || h.Get("X-OAuth-Scopes") != "" {
			t.Fatalf("%s: %d %v", token, code, h)
		}
	}
	if code, _, _ := call(t, srv, http.MethodPost, "/graphql", "github_pat_unknown", `{"query":"query { viewer { login } }"}`); code != http.StatusUnauthorized {
		t.Fatalf("%d", code)
	}
}
