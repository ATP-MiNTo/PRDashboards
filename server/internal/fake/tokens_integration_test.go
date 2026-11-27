//go:build e2e

package fake

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	ghc "github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/review"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/testutil"
)

// tokenAccounts is the adapter of cmd/prdash: the poller, the merge and the review ask the real
// auth service for credentials.
type tokenAccounts struct{ o *auth.OAuth }

func (a tokenAccounts) GitHubTokens(context.Context) ([]auth.GitHubCredential, error) {
	list, err := a.o.GitHubTokens()
	if err != nil {
		return nil, err
	}
	out := make([]auth.GitHubCredential, len(list))
	for i, t := range list {
		out[i] = auth.GitHubCredential{ID: t.ID, Name: t.Name, Login: t.Login, Method: t.Method}
	}
	return out, nil
}

func (a tokenAccounts) GitHubToken(ctx context.Context, id string) (auth.GitHubCredential, error) {
	cred, err := a.o.GitHubAccess(ctx, id)
	if errors.Is(err, auth.ErrTokenNotFound) {
		return a.o.GitHubAccess(ctx, "")
	}
	return cred, err
}

func (a tokenAccounts) GitHubRejected(_ context.Context, id string) { _ = a.o.MarkGitHubRejected(id) }

func (a tokenAccounts) JiraSite(context.Context) (jira.Site, error) {
	return jira.Site{}, auth.ErrNotConnected
}

// stack is the real server side (auth, poller, merge, review) against the fake GitHub.
type stack struct {
	t     *testing.T
	g     *github
	o     *auth.OAuth
	p     *poller.Poller
	m     *merge.Service
	r     *review.Service
	clock *testutil.Clock
	store *store.Store
	vault *secrets.Memory
}

func newStack(t *testing.T) *stack {
	t.Helper()
	g := newGitHub()
	g.st.Config.ReadOnly, g.st.Config.Review = true, true
	srv := httptest.NewServer(g.routes())
	t.Cleanup(srv.Close)
	clock := testutil.NewClock()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := &ghc.Client{HTTP: srv.Client(), APIBase: srv.URL}
	vault := secrets.NewMemory()
	o := auth.NewOAuth(auth.Config{Secrets: vault, States: db, HTTP: srv.Client(), GitHub: client, Clock: clock.Now, Rand: &testutil.Rand{}, Port: 4317})
	acc := tokenAccounts{o}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := poller.New(poller.Config{Store: db, GitHub: client, Accounts: acc, Log: log, Clock: clock.Now})
	s := &stack{t: t, g: g, o: o, p: p, clock: clock, store: db, vault: vault}
	s.m = merge.New(merge.Config{GitHub: client, Store: db, Snapshot: p, Accounts: acc, Log: log, Clock: clock.Now})
	s.r = review.New(review.Config{GitHub: client, Snapshot: p, Accounts: acc, Log: log})
	return s
}

func (s *stack) add(token, name string) auth.GitHubToken {
	s.t.Helper()
	tok, err := s.o.AddPAT(context.Background(), token, name)
	if err != nil {
		s.t.Fatal(err)
	}
	return tok
}

func (s *stack) sync() gen.Snapshot {
	s.t.Helper()
	s.clock.Advance(time.Minute)
	if err := s.p.Sync(context.Background()); err != nil {
		s.t.Fatal(err)
	}
	return s.p.Current().Snapshot
}

func (s *stack) calls(op string) []fakeCall {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	var out []fakeCall
	for _, c := range s.g.st.Calls {
		if c.Op == op {
			out = append(out, c)
		}
	}
	return out
}

func (s *stack) reset() {
	s.g.mu.Lock()
	s.g.st.Calls = nil
	s.g.mu.Unlock()
}

func prOf(t *testing.T, snap gen.Snapshot, repo string, number int) gen.SnapshotPr {
	t.Helper()
	for _, p := range snap.Prs {
		if p.Repo.NameWithOwner == repo && p.Number == number {
			return p
		}
	}
	t.Fatalf("%s#%d is not in the snapshot", repo, number)
	return gen.SnapshotPr{}
}

func tokenOf(r gen.SnapshotRepo) string {
	if r.TokenName == nil {
		return "<none>"
	}
	return *r.TokenName
}

func TestTwoOwnersOneDashboard(t *testing.T) {
	s := newStack(t)
	work := s.add("github_pat_acme", "Work")
	oss := s.add("github_pat_oss", "OSS")
	if !work.FineGrained || !oss.FineGrained {
		t.Fatalf("%+v %+v", work, oss)
	}

	// Poll: three searches per token, one token after the other, merged by pull request.
	snap := s.sync()
	if snap.Sources.Github.State != gen.SourceStateOk {
		t.Fatalf("%+v", snap.Sources.Github)
	}
	var searchTokens []string
	for _, c := range s.calls("search") {
		searchTokens = append(searchTokens, c.Token)
	}
	if want := []string{"github_pat_acme", "github_pat_acme", "github_pat_acme", "github_pat_oss", "github_pat_oss", "github_pat_oss"}; !slices.Equal(searchTokens, want) {
		t.Fatalf("searches %v", searchTokens)
	}
	web, lib := prOf(t, snap, "acme/web", 104), prOf(t, snap, "oss/lib", 41)
	if tokenOf(web.Repo) != "Work" || web.Repo.TokenId == nil || *web.Repo.TokenId != work.ID || *web.Repo.ViewerPermission != gen.WRITE {
		t.Fatalf("%+v", web.Repo)
	}
	// Both tokens see oss/lib: Work as READ, OSS as WRITE. OSS wins, and its role is the one shown.
	if tokenOf(lib.Repo) != "OSS" || *lib.Repo.TokenId != oss.ID || *lib.Repo.ViewerPermission != gen.WRITE {
		t.Fatalf("%+v", lib.Repo)
	}

	// Writes go with the token of the repository.
	s.reset()
	if _, err := s.m.Merge(context.Background(), merge.Request{
		PRID: lib.Id, IdempotencyKey: "00000000-0000-4000-8000-000000000001", Method: merge.Squash, ExpectedHeadSHA: lib.HeadSha,
	}); err != nil {
		t.Fatalf("merge in oss/lib: %v", err)
	}
	if got := s.calls("merge"); len(got) != 1 || got[0] != (fakeCall{Op: "merge", Token: "github_pat_oss", Repo: "oss/lib"}) {
		t.Fatalf("%+v", got)
	}
	if got := s.calls("node"); len(got) != 1 || got[0].Token != "github_pat_oss" {
		t.Fatalf("the re-check uses the same token: %+v", got)
	}
	s.reset()
	rev := prOf(t, snap, "acme/review", 31)
	if _, err := s.r.Approve(context.Background(), review.Request{PRID: rev.Id, ExpectedHeadSHA: rev.HeadSha}); err != nil {
		t.Fatalf("approve in acme/review: %v", err)
	}
	if got := s.calls("review"); len(got) != 1 || got[0].Token != "github_pat_acme" {
		t.Fatalf("%+v", got)
	}

	// The own pull request check uses the login of the token of the repository. #33 was opened by
	// octocat, the account of Work, so it is refused up front.
	own := prOf(t, snap, "acme/review", 33)
	s.reset()
	_, err := s.r.Approve(context.Background(), review.Request{PRID: own.Id, ExpectedHeadSHA: own.HeadSha})
	var refused *merge.Error
	if !errors.As(err, &refused) || refused.Code != merge.CodeOwnPullRequest || len(s.calls("review")) != 0 {
		t.Fatalf("%v", err)
	}
}

func TestOneRepositoryOfAnotherOwnerNeedsATokenOfThatOwner(t *testing.T) {
	s := newStack(t)
	s.add("github_pat_acme", "Work")
	snap := s.sync()
	lib := prOf(t, snap, "oss/lib", 41)
	if tokenOf(lib.Repo) != "Work" || *lib.Repo.ViewerPermission != gen.READ {
		t.Fatalf("with one token the repository is read with it, as a reader: %+v", lib.Repo)
	}
	_, err := s.m.Merge(context.Background(), merge.Request{
		PRID: lib.Id, IdempotencyKey: "00000000-0000-4000-8000-000000000002", Method: merge.Squash, ExpectedHeadSHA: lib.HeadSha,
	})
	var perm *ghc.PermissionError
	if !errors.As(err, &perm) {
		t.Fatalf("%v", err)
	}
	for _, want := range []string{`The token "Work" can't merge in oss/lib`, "contents: write", "needs a token from that owner. Add one in Accounts."} {
		if !strings.Contains(perm.Detail(), want) {
			t.Errorf("%q lacks %q", perm.Detail(), want)
		}
	}
	// Adding the token of the owner fixes it at the next refresh.
	s.add("github_pat_oss", "OSS")
	lib = prOf(t, s.sync(), "oss/lib", 41)
	if tokenOf(lib.Repo) != "OSS" {
		t.Fatalf("%+v", lib.Repo)
	}
	if _, err := s.m.Merge(context.Background(), merge.Request{
		PRID: lib.Id, IdempotencyKey: "00000000-0000-4000-8000-000000000003", Method: merge.Squash, ExpectedHeadSHA: lib.HeadSha,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestARejectedTokenFailsTheRefreshAndIsMarked(t *testing.T) {
	s := newStack(t)
	s.add("github_pat_acme", "Work")
	oss := s.add("github_pat_oss", "OSS")
	if st := s.sync().Sources.Github.State; st != gen.SourceStateOk {
		t.Fatal(st)
	}
	// The vault holds a token GitHub does not know any more.
	list, _ := s.o.GitHubTokens()
	for i := range list {
		if list[i].ID == oss.ID {
			list[i].Token = "github_pat_revoked"
		}
	}
	revoke(t, s, list)
	src := s.sync().Sources.Github
	if src.State != gen.SourceStateDisconnected || src.Error == nil || *src.Error != `GitHub token "OSS" was rejected` {
		t.Fatalf("%+v", src)
	}
	list, _ = s.o.GitHubTokens()
	if list[0].NeedsReconnect || !list[1].NeedsReconnect {
		t.Fatalf("only the rejected token is marked: %+v", list)
	}
}

// revoke stores the list as it is now.
func revoke(t *testing.T, s *stack, list []auth.GitHubToken) {
	t.Helper()
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.vault.Set(secrets.GitHubTokens, raw); err != nil {
		t.Fatal(err)
	}
}

func TestRemovingTheTokenOfAnOwnerDowngradesItsRepositories(t *testing.T) {
	s := newStack(t)
	s.add("github_pat_acme", "Work")
	oss := s.add("github_pat_oss", "OSS")
	lib := prOf(t, s.sync(), "oss/lib", 41)
	if tokenOf(lib.Repo) != "OSS" {
		t.Fatalf("%+v", lib.Repo)
	}
	if _, err := s.o.RemoveGitHubToken(oss.ID); err != nil {
		t.Fatal(err)
	}
	// Before the next refresh the snapshot still names the removed token: the write falls back to
	// the primary token, which is a reader there.
	_, err := s.r.Approve(context.Background(), review.Request{PRID: lib.Id, ExpectedHeadSHA: lib.HeadSha})
	var perm *ghc.PermissionError
	if !errors.As(err, &perm) || !strings.Contains(perm.Detail(), `"Work"`) || !strings.Contains(perm.Detail(), "oss/lib") {
		t.Fatalf("%v", err)
	}
	lib = prOf(t, s.sync(), "oss/lib", 41)
	if tokenOf(lib.Repo) != "Work" || *lib.Repo.ViewerPermission != gen.READ {
		t.Fatalf("%+v", lib.Repo)
	}
}
