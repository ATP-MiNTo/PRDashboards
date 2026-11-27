package review_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/review"
)

const prID = "PR_1"

type submit struct {
	repo        string
	number      int
	event, sha  string
	body, token string
}

type fakeGitHub struct {
	pr         github.PR
	prErr      error
	reviewErr  error
	readyErr   error
	pulls      int
	reviews    []submit
	readyCalls []string
}

func (f *fakeGitHub) PullRequest(context.Context, string, string) (github.PR, error) {
	f.pulls++
	return f.pr, f.prErr
}

func (f *fakeGitHub) SubmitReview(_ context.Context, token, repo string, number int, event, sha, body string) error {
	f.reviews = append(f.reviews, submit{repo, number, event, sha, body, token})
	return f.reviewErr
}

func (f *fakeGitHub) MarkReady(_ context.Context, _ string, id string) error {
	f.readyCalls = append(f.readyCalls, id)
	return f.readyErr
}

type fakeSnapshot struct{ known bool }

func (s fakeSnapshot) Linked(string) (merge.Linked, bool) { return merge.Linked{}, s.known }

type fakeAccounts struct {
	tokenErr error
	login    string
}

func (a fakeAccounts) GitHubToken(context.Context) (string, error) { return "tok", a.tokenErr }
func (a fakeAccounts) GitHubLogin(context.Context) string          { return a.login }

type rig struct {
	svc       *review.Service
	gh        *fakeGitHub
	snap      *fakeSnapshot
	accounts  *fakeAccounts
	refreshes atomic.Int32
}

func newRig() *rig {
	r := &rig{
		gh: &fakeGitHub{pr: github.PR{
			ID: prID, Number: 7, State: "OPEN", HeadSHA: "abc1234", BaseRef: "main", Author: "priya", Draft: false,
			Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN",
			Repo: github.Repo{ID: "R1", NameWithOwner: "acme/web"},
		}},
		snap:     &fakeSnapshot{known: true},
		accounts: &fakeAccounts{login: "octocat"},
	}
	r.svc = review.New(review.Config{GitHub: r.gh, Snapshot: r.snap, Accounts: r.accounts, Refresh: func() { r.refreshes.Add(1) }})
	return r
}

func req() review.Request { return review.Request{PRID: prID, ExpectedHeadSHA: "abc1234"} }

func wantCode(t *testing.T, err error, code string) *merge.Error {
	t.Helper()
	var e *merge.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("%v, want code %s", err, code)
	}
	return e
}

func TestApproveSubmitsTheReviewPinnedToTheExpectedHeadAndRefreshesOnce(t *testing.T) {
	r := newRig()
	in := req()
	in.Body = "LGTM"
	res, err := r.svc.Approve(context.Background(), in)
	if err != nil || res.PRID != prID || res.Action != review.ActionApprove {
		t.Fatalf("%+v %v", res, err)
	}
	want := submit{"acme/web", 7, github.ReviewApprove, "abc1234", "LGTM", "tok"}
	if len(r.gh.reviews) != 1 || r.gh.reviews[0] != want {
		t.Fatalf("%+v", r.gh.reviews)
	}
	if r.gh.pulls != 1 || r.refreshes.Load() != 1 || len(r.gh.readyCalls) != 0 {
		t.Fatalf("one node query and one refresh: pulls %d refreshes %d", r.gh.pulls, r.refreshes.Load())
	}
}

func TestRequestChangesNeedsATextAndSubmitsIt(t *testing.T) {
	r := newRig()
	for _, body := range []string{"", "   ", "\n\t "} {
		in := req()
		in.Body = body
		_, err := r.svc.RequestChanges(context.Background(), in)
		wantCode(t, err, merge.CodeBodyRequired)
	}
	if r.gh.pulls != 0 || len(r.gh.reviews) != 0 {
		t.Fatal("an empty text reached GitHub")
	}
	in := req()
	in.Body = "please rename"
	res, err := r.svc.RequestChanges(context.Background(), in)
	if err != nil || res.Action != review.ActionRequestChanges {
		t.Fatalf("%+v %v", res, err)
	}
	if len(r.gh.reviews) != 1 || r.gh.reviews[0].event != github.ReviewRequestChanges || r.gh.reviews[0].body != "please rename" {
		t.Fatalf("%+v", r.gh.reviews)
	}
}

func TestMarkReadyMarksADraft(t *testing.T) {
	r := newRig()
	r.gh.pr.Draft = true
	res, err := r.svc.MarkReady(context.Background(), req())
	if err != nil || res.Action != review.ActionReady {
		t.Fatalf("%+v %v", res, err)
	}
	if len(r.gh.readyCalls) != 1 || r.gh.readyCalls[0] != prID || len(r.gh.reviews) != 0 || r.refreshes.Load() != 1 {
		t.Fatalf("%+v refreshes %d", r.gh.readyCalls, r.refreshes.Load())
	}
}

func TestRecheckRefusalsCarryTheFreshStateAndWriteNothing(t *testing.T) {
	tests := []struct {
		name   string
		action string
		mutate func(r *rig)
		code   string
	}{
		{"closed meanwhile", review.ActionApprove, func(r *rig) { r.gh.pr.State = "MERGED" }, merge.CodeNotOpen},
		{"closed meanwhile, changes", review.ActionRequestChanges, func(r *rig) { r.gh.pr.State = "CLOSED" }, merge.CodeNotOpen},
		{"closed meanwhile, ready", review.ActionReady, func(r *rig) { r.gh.pr.State = "CLOSED"; r.gh.pr.Draft = true }, merge.CodeNotOpen},
		{"head moved", review.ActionApprove, func(r *rig) { r.gh.pr.HeadSHA = "def5678" }, merge.CodeHeadMoved},
		{"head moved, changes", review.ActionRequestChanges, func(r *rig) { r.gh.pr.HeadSHA = "def5678" }, merge.CodeHeadMoved},
		{"head moved, ready", review.ActionReady, func(r *rig) { r.gh.pr.HeadSHA = "def5678"; r.gh.pr.Draft = true }, merge.CodeHeadMoved},
		{"no longer a draft", review.ActionReady, func(r *rig) { r.gh.pr.Draft = false }, merge.CodeNotDraft},
		{"own pull request", review.ActionApprove, func(r *rig) { r.gh.pr.Author = "OctoCat" }, merge.CodeOwnPullRequest},
		{"own pull request, changes", review.ActionRequestChanges, func(r *rig) { r.gh.pr.Author = "octocat" }, merge.CodeOwnPullRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig()
			tt.mutate(r)
			in := req()
			in.Body = "text"
			var err error
			switch tt.action {
			case review.ActionApprove:
				_, err = r.svc.Approve(context.Background(), in)
			case review.ActionRequestChanges:
				_, err = r.svc.RequestChanges(context.Background(), in)
			default:
				_, err = r.svc.MarkReady(context.Background(), in)
			}
			e := wantCode(t, err, tt.code)
			if e.Fresh == nil || e.Fresh.HeadSHA != r.gh.pr.HeadSHA || e.Fresh.State != r.gh.pr.State {
				t.Fatalf("the fresh state is missing: %+v", e.Fresh)
			}
			if len(r.gh.reviews) != 0 || len(r.gh.readyCalls) != 0 {
				t.Fatal("a refused action wrote to GitHub")
			}
			if r.refreshes.Load() != 1 {
				t.Fatalf("a refusal that shows a newer state refreshes the snapshot: %d", r.refreshes.Load())
			}
		})
	}
}

func TestReadyIgnoresWhoOpenedThePullRequest(t *testing.T) {
	r := newRig()
	r.gh.pr.Draft, r.gh.pr.Author = true, "octocat"
	if _, err := r.svc.MarkReady(context.Background(), req()); err != nil {
		t.Fatalf("an author may mark their own draft ready: %v", err)
	}
}

func TestUnknownLoginLeavesTheOwnPullRequestCheckToGitHub(t *testing.T) {
	r := newRig()
	r.accounts.login = ""
	r.gh.pr.Author = "octocat"
	r.gh.reviewErr = github.ErrOwnPullRequest
	_, err := r.svc.Approve(context.Background(), req())
	e := wantCode(t, err, merge.CodeOwnPullRequest)
	if e.Fresh == nil || len(r.gh.reviews) != 1 {
		t.Fatalf("GitHub was asked and refused: %+v %d", e, len(r.gh.reviews))
	}
}

func TestUnknownPullRequestAndConnectionProblems(t *testing.T) {
	r := newRig()
	r.snap.known = false
	_, err := r.svc.Approve(context.Background(), req())
	wantCode(t, err, merge.CodeNotFound)
	if r.gh.pulls != 0 {
		t.Fatal("GitHub was asked about a pull request the dashboard does not list")
	}

	r = newRig()
	r.accounts.tokenErr = auth.ErrNotConnected
	_, err = r.svc.Approve(context.Background(), req())
	wantCode(t, err, merge.CodeNotConnected)

	r = newRig()
	r.accounts.tokenErr = errors.New("vault locked")
	if _, err = r.svc.Approve(context.Background(), req()); err == nil || errors.As(err, new(*merge.Error)) {
		t.Fatalf("%v", err)
	}

	r = newRig()
	r.gh.prErr = github.ErrPRNotFound
	_, err = r.svc.Approve(context.Background(), req())
	wantCode(t, err, merge.CodeNotFound)

	r = newRig()
	r.gh.prErr = github.ErrBadCredentials
	_, err = r.svc.Approve(context.Background(), req())
	wantCode(t, err, merge.CodeNotConnected)

	r = newRig()
	r.gh.prErr = &github.RateLimitedError{}
	_, err = r.svc.Approve(context.Background(), req())
	var rl *github.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("%v", err)
	}
}

func TestGitHubAnswersAreMapped(t *testing.T) {
	perm := &github.PermissionError{Message: "Resource not accessible", Accepted: []string{"pull_requests=write"}}
	tests := []struct {
		name   string
		action string
		set    func(f *fakeGitHub)
		code   string // "" means the error comes through unchanged, and passes is checked
		passes func(error) bool
	}{
		{"head moved on GitHub's side", review.ActionApprove, func(f *fakeGitHub) { f.reviewErr = github.ErrHeadMoved }, merge.CodeHeadMoved, nil},
		{"own pull request", review.ActionApprove, func(f *fakeGitHub) { f.reviewErr = github.ErrOwnPullRequest }, merge.CodeOwnPullRequest, nil},
		{"not found on write", review.ActionRequestChanges, func(f *fakeGitHub) { f.reviewErr = github.ErrPRNotFound }, merge.CodeNotFound, nil},
		{"rejected", review.ActionApprove, func(f *fakeGitHub) { f.reviewErr = &github.ReviewRejectedError{Message: "Review body can't be blank"} }, merge.CodeReviewRejected, nil},
		{"not a draft on GitHub's side", review.ActionReady, func(f *fakeGitHub) { f.pr.Draft = true; f.readyErr = github.ErrNotDraft }, merge.CodeNotDraft, nil},
		{"token rejected", review.ActionApprove, func(f *fakeGitHub) { f.reviewErr = github.ErrBadCredentials }, merge.CodeNotConnected, nil},
		{"permission", review.ActionApprove, func(f *fakeGitHub) { f.reviewErr = perm }, "", func(e error) bool { return errors.Is(e, perm) }},
		{"permission on ready", review.ActionReady, func(f *fakeGitHub) { f.pr.Draft = true; f.readyErr = perm }, "", func(e error) bool { return errors.Is(e, perm) }},
		{"organization", review.ActionRequestChanges, func(f *fakeGitHub) { f.reviewErr = &github.OrgBlocked{Org: "acme"} }, "", func(e error) bool { var b *github.OrgBlocked; return errors.As(e, &b) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig()
			tt.set(r.gh)
			in := req()
			in.Body = "x"
			var err error
			switch tt.action {
			case review.ActionApprove:
				_, err = r.svc.Approve(context.Background(), in)
			case review.ActionRequestChanges:
				_, err = r.svc.RequestChanges(context.Background(), in)
			default:
				_, err = r.svc.MarkReady(context.Background(), in)
			}
			if tt.code != "" {
				wantCode(t, err, tt.code)
			} else if !tt.passes(err) {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestAFailedWriteDoesNotRefreshTheSnapshotUnlessTheStateChanged(t *testing.T) {
	r := newRig()
	r.gh.reviewErr = &github.PermissionError{}
	_, _ = r.svc.Approve(context.Background(), req())
	if r.refreshes.Load() != 0 {
		t.Fatalf("%d", r.refreshes.Load())
	}
}

func TestNewDefaults(t *testing.T) {
	// No logger and no refresh hook: the service still works.
	r := newRig()
	svc := review.New(review.Config{GitHub: r.gh, Snapshot: r.snap, Accounts: r.accounts})
	if _, err := svc.Approve(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	r.gh.reviewErr = errors.New("boom")
	if _, err := svc.Approve(context.Background(), req()); err == nil {
		t.Fatal("an unexpected error was swallowed")
	}
}
