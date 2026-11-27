package merge_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
)

// withTokens gives the rig a second token and makes the snapshot say the repository is read and
// written with it.
func withTokens(r *rig) {
	r.accounts.creds = map[string]auth.GitHubCredential{
		"bbbbbbbb": {ID: "bbbbbbbb", Name: "OSS", Login: "hubot", Token: "tok-oss"},
	}
	l := r.snapshot.linked[prID]
	l.TokenID = "bbbbbbbb"
	r.snapshot.linked[prID] = l
}

func TestMergeUsesTheTokenOfTheRepositoryForEveryCall(t *testing.T) {
	r := newRig(t, "INP-1")
	withTokens(r)
	req := request(done("INP-1"))
	req.DeleteBranch = true
	if _, err := r.svc.Merge(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if want := []string{"pull:tok-oss", "merge:tok-oss", "delete:tok-oss"}; !slices.Equal(r.gh.tokens, want) {
		t.Fatalf("%v, want %v", r.gh.tokens, want)
	}
}

func TestMergeFallsBackToThePrimaryTokenWithoutATokenId(t *testing.T) {
	r := newRig(t, "INP-1")
	r.accounts.creds = map[string]auth.GitHubCredential{"bbbbbbbb": {ID: "bbbbbbbb", Name: "OSS", Token: "tok-oss"}}
	req := request(done("INP-1"))
	req.DeleteBranch = true
	if _, err := r.svc.Merge(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if want := []string{"pull:tok", "merge:tok", "delete:tok"}; !slices.Equal(r.gh.tokens, want) {
		t.Fatalf("%v, want %v", r.gh.tokens, want)
	}
}

func TestMergePermissionErrorNamesTheTokenThatWasUsed(t *testing.T) {
	r := newRig(t, "INP-1")
	withTokens(r)
	r.gh.mergeErr = &github.PermissionError{Message: "Resource not accessible", Accepted: []string{"contents=write"}}
	var perm *github.PermissionError
	if err := merge2(r, request(done("INP-1"))); !errors.As(err, &perm) {
		t.Fatalf("%v", err)
	}
	want := `The token "OSS" can't merge in acme/web: it needs the GitHub permission contents: write. ` +
		`A fine-grained token works for one owner only, so a repository of another owner needs a token from that owner. Add one in Accounts.`
	if perm.Detail() != want {
		t.Fatalf("%q", perm.Detail())
	}
	if strings.Contains(perm.Detail(), "tok-oss") {
		t.Fatal("the token value is in the detail")
	}
}

func TestMergeNeedsReconnectOfTheRepositoryTokenIsNotConnected(t *testing.T) {
	r := newRig(t, "INP-1")
	withTokens(r)
	r.accounts.tokenErr = auth.ErrNeedsReconnect
	wantError(t, merge2(r, request(done("INP-1"))), merge.CodeNotConnected)
	if len(r.gh.tokens) != 0 {
		t.Fatalf("%v", r.gh.tokens)
	}
}
