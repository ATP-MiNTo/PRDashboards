package review_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
)

// withTokens makes the snapshot say the repository is read and written with the OSS token, whose
// account is hubot, while the primary token's account is octocat.
func withTokens(r *rig) {
	r.accounts.creds = map[string]auth.GitHubCredential{
		"bbbbbbbb": {ID: "bbbbbbbb", Name: "OSS", Login: "hubot", Token: "tok-oss"},
	}
	r.snap.tokenID = "bbbbbbbb"
}

func TestReviewActionsUseTheTokenOfTheRepository(t *testing.T) {
	r := newRig()
	withTokens(r)
	if _, err := r.svc.Approve(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if len(r.gh.reviews) != 1 || r.gh.reviews[0].token != "tok-oss" || !slices.Equal(r.gh.pullTokens, []string{"tok-oss"}) {
		t.Fatalf("%+v %v", r.gh.reviews, r.gh.pullTokens)
	}
	r.gh.pr.Draft = true
	if _, err := r.svc.MarkReady(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.gh.readyTokens, []string{"tok-oss"}) || !slices.Equal(r.gh.pullTokens, []string{"tok-oss", "tok-oss"}) {
		t.Fatalf("%v %v", r.gh.readyTokens, r.gh.pullTokens)
	}
}

func TestReviewWithoutATokenIdUsesThePrimaryToken(t *testing.T) {
	r := newRig()
	r.accounts.creds = map[string]auth.GitHubCredential{"bbbbbbbb": {ID: "bbbbbbbb", Token: "tok-oss"}}
	if _, err := r.svc.Approve(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if r.gh.reviews[0].token != "tok" {
		t.Fatalf("%+v", r.gh.reviews)
	}
}

func TestOwnPullRequestCheckUsesTheLoginOfTheChosenToken(t *testing.T) {
	// The pull request was opened by hubot, the account of the OSS token, not by the primary
	// account (octocat): reviewing it with the OSS token is refused before GitHub is asked.
	r := newRig()
	withTokens(r)
	r.gh.pr.Author = "Hubot"
	wantCode(t, func() error { _, err := r.svc.Approve(context.Background(), req()); return err }(), merge.CodeOwnPullRequest)
	if len(r.gh.reviews) != 0 {
		t.Fatalf("%+v", r.gh.reviews)
	}
	// The same pull request through the primary token (another account) may be reviewed.
	r2 := newRig()
	r2.gh.pr.Author = "hubot"
	if _, err := r2.svc.Approve(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	// And a pull request of the primary account is no obstacle for the OSS token.
	r3 := newRig()
	withTokens(r3)
	r3.gh.pr.Author = "octocat"
	if _, err := r3.svc.Approve(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
}

func TestReviewPermissionErrorNamesTheToken(t *testing.T) {
	r := newRig()
	withTokens(r)
	r.gh.reviewErr = &github.PermissionError{Message: "Resource not accessible", Accepted: []string{"pull_requests=write"}}
	var perm *github.PermissionError
	_, err := r.svc.Approve(context.Background(), req())
	if !errors.As(err, &perm) {
		t.Fatalf("%v", err)
	}
	want := `The token "OSS" can't write to acme/web: it needs the GitHub permission pull_requests: write. ` +
		`A fine-grained token works for one owner only, so a repository of another owner needs a token from that owner. Add one in Accounts.`
	if perm.Detail() != want {
		t.Fatalf("%q", perm.Detail())
	}
}
