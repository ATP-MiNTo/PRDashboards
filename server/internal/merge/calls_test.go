package merge_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

// TestOutboundCallsOfOneMerge runs the real clients against recording servers: GitHub sees one node
// query, one merge and one branch delete, and Jira sees transitions only for the ticket that was
// not cached plus one POST per ticket.
func TestOutboundCallsOfOneMerge(t *testing.T) {
	var graphql, puts, deletes, transitionGets, transitionPosts atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			graphql.Add(1)
			var body struct{ Query string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(body.Query, "node(id: $id)") || !strings.Contains(body.Query, "fragment PullRequestFields") {
				t.Errorf("unexpected query %q", body.Query)
			}
			_, _ = io.WriteString(w, `{"data":{"node":{"__typename":"PullRequest","id":"PR_1","number":7,"state":"OPEN","isCrossRepository":false,
				"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN","reviewDecision":"APPROVED","headRefName":"feat/x","headRefOid":"sha1","baseRefName":"main",
				"repository":{"id":"R1","nameWithOwner":"acme/web","squashMergeAllowed":true,"mergeCommitAllowed":true,"rebaseMergeAllowed":true}}}}`)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/acme/web/pulls/7/merge":
			puts.Add(1)
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["merge_method"] != "squash" || body["sha"] != "sha1" {
				t.Errorf("merge body %v", body)
			}
			_, _ = io.WriteString(w, `{"sha":"abc","merged":true,"message":"Pull Request successfully merged"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/acme/web/git/refs/heads/feat/x":
			deletes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected GitHub call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	jr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/transitions"):
			transitionGets.Add(1)
			_, _ = io.WriteString(w, `{"transitions":[{"id":"21","name":"Start","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},"fields":{}}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transitions"):
			transitionPosts.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Jira call %s %s", r.Method, r.URL.Path)
		}
	}))
	defer jr.Close()

	clock := testutil.NewClock()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	jc := &jira.Client{HTTP: jr.Client(), Base: jr.URL, Clock: clock.Now,
		Access: func(context.Context) (string, error) { return "tok", nil }}
	if _, err := jc.Transitions(context.Background(), "cloud-1", "INP-1"); err != nil { // cached by the panel
		t.Fatal(err)
	}
	transitionGets.Store(0)

	snap := &fakeSnapshot{
		linked: map[string]merge.Linked{prID: {RepoID: "R1", NameWithOwner: "acme/web", Number: 7, Tickets: []string{"INP-1", "INP-2", "INP-3"}}},
		refs:   map[string][]merge.Ref{"R1": {{ID: prID, Number: 7, Head: "feat/x", Base: "main"}}},
	}
	svc := merge.New(merge.Config{
		GitHub: &github.Client{HTTP: gh.Client(), APIBase: gh.URL}, Jira: jc, Store: db, Snapshot: snap,
		Accounts: fakeAccounts{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Clock: clock.Now,
	})
	req := request(
		merge.Ticket{Key: "INP-1", TransitionID: "21"}, merge.Ticket{Key: "INP-2", TransitionID: "21"}, merge.Ticket{Key: "INP-3", TransitionID: "21"})
	req.DeleteBranch = true
	res, err := svc.Merge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.SHA != "abc" || len(res.Steps) != 5 {
		t.Fatalf("%+v", res)
	}
	if graphql.Load() != 1 || puts.Load() != 1 || deletes.Load() != 1 {
		t.Fatalf("GitHub: %d node queries, %d merges, %d deletes", graphql.Load(), puts.Load(), deletes.Load())
	}
	if transitionGets.Load() != 2 || transitionPosts.Load() != 3 {
		t.Fatalf("Jira: %d transition lists, %d transition posts", transitionGets.Load(), transitionPosts.Load())
	}
}
