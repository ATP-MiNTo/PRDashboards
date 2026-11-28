package auth_test

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
)

var hex8 = regexp.MustCompile(`^[0-9a-f]{8}$`)

func list(t *testing.T, r *oauthRig) []auth.GitHubToken {
	t.Helper()
	l, err := r.o.GitHubTokens()
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func names(l []auth.GitHubToken) []string {
	out := make([]string, len(l))
	for i, t := range l {
		out[i] = t.Name
	}
	return out
}

func TestLegacyTokenMovesIntoTheListAsThePrimaryEntry(t *testing.T) {
	expires := time.Date(2026, 11, 24, 17, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, raw, wantName string
		fineGrained         bool
	}{
		{"app", `{"token":"ghu_1","method":"app","login":"octocat","refreshToken":"ghr_1","expiresAt":"2026-11-24T17:00:00Z","needsReconnect":true}`, "GitHub App (octocat)", false},
		{"oauth", `{"token":"gho_legacy","method":"oauth","login":"octocat"}`, "OAuth (octocat)", false},
		{"classic", `{"token":"ghp_pat","method":"pat","login":"octocat"}`, "Token (octocat)", false},
		{"fine-grained", `{"token":"github_pat_x","method":"pat","login":"octocat","warning":"` + auth.FineGrainedWarning + `"}`, "Token (octocat)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newOAuthRig(t)
			if err := r.sec.Set(secrets.GitHubToken, []byte(tc.raw)); err != nil {
				t.Fatal(err)
			}
			if err := r.o.MigrateGitHub(); err != nil {
				t.Fatal(err)
			}
			l := list(t, r)
			if len(l) != 1 {
				t.Fatalf("%+v", l)
			}
			got := l[0]
			if !hex8.MatchString(got.ID) || got.Name != tc.wantName || got.FineGrained != tc.fineGrained {
				t.Fatalf("%+v", got)
			}
			if tc.name == "app" && (got.RefreshToken != "ghr_1" || !got.ExpiresAt.Equal(expires) || !got.NeedsReconnect || got.Method != "app" || got.Token != "ghu_1") {
				t.Fatalf("the refresh state is kept: %+v", got)
			}
			if _, err := r.sec.Get(secrets.GitHubToken); !errors.Is(err, secrets.ErrNotFound) {
				t.Fatalf("legacy item kept: %v", err)
			}
			if again := list(t, r); again[0].ID != got.ID {
				t.Fatal("the id changes between reads")
			}
		})
	}
}

func TestLegacyTokenIsMovedByTheFirstReadToo(t *testing.T) {
	r := newOAuthRig(t)
	_ = r.sec.Set(secrets.GitHubToken, []byte(`{"token":"ghp_pat","method":"pat","login":"octocat"}`))
	acc, ok, err := r.o.GitHubAccount()
	if err != nil || !ok || acc.Token != "ghp_pat" || acc.ID == "" {
		t.Fatalf("%+v %v %v", acc, ok, err)
	}
	if _, err := r.sec.Get(secrets.GitHubTokens); err != nil {
		t.Fatal("list not stored")
	}
}

func TestAddPATNames(t *testing.T) {
	r := newOAuthRig(t)
	a, err := r.o.AddPAT(ctx, "ghp_pat", "")
	if err != nil || a.Name != "Token (octocat)" || !hex8.MatchString(a.ID) || a.Method != "pat" || a.FineGrained {
		t.Fatalf("%+v %v", a, err)
	}
	r.prov.githubScopes.Store("")
	b, err := r.o.AddPAT(ctx, "github_pat_x", "")
	if err != nil || b.Name != "Token (octocat) 2" || !b.FineGrained || b.Warning != auth.FineGrainedWarning {
		t.Fatalf("a default name is made unique: %+v %v", b, err)
	}
	c, err := r.o.AddPAT(ctx, "ghp_extra1", "  Work  ")
	if err != nil || c.Name != "Work" {
		t.Fatalf("a given name is trimmed: %+v %v", c, err)
	}
	if got := names(list(t, r)); fmt.Sprint(got) != "[Token (octocat) Token (octocat) 2 Work]" {
		t.Fatalf("%v in this order", got)
	}
	if a.ID == b.ID || b.ID == c.ID {
		t.Fatal("ids repeat")
	}
	if _, err := r.o.AddPAT(ctx, "ghp_extra2", "WORK"); !errors.Is(err, auth.ErrTokenNameTaken) {
		t.Fatalf("names are unique ignoring case: %v", err)
	}
	for name, bad := range map[string]string{"blank": "   ", "long": strings.Repeat("a", 41), "control": "a\nb", "tab": "a\tb"} {
		if _, err := r.o.AddPAT(ctx, "ghp_extra3", bad); !errors.Is(err, auth.ErrTokenName) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := r.o.AddPAT(ctx, "ghp_extra3", strings.Repeat("é", 40)); err != nil {
		t.Fatalf("40 characters, not bytes: %v", err)
	}
}

func TestDefaultNameFitsTheLimit(t *testing.T) {
	r := newOAuthRig(t)
	r.prov.longLogin = strings.Repeat("x", 39)
	a, err := r.o.AddPAT(ctx, "ghp_pat", "")
	if err != nil || len([]rune(a.Name)) > auth.MaxTokenName || !strings.HasPrefix(a.Name, "Token (xxx") {
		t.Fatalf("%q %v", a.Name, err)
	}
	b, err := r.o.AddPAT(ctx, "ghp_extra1", "")
	if err != nil || len([]rune(b.Name)) > auth.MaxTokenName || b.Name == a.Name || !strings.HasSuffix(b.Name, " 2") {
		t.Fatalf("%q %v", b.Name, err)
	}
}

func TestAddPATDuplicateAndLimit(t *testing.T) {
	r := newOAuthRig(t)
	if _, err := r.o.AddPAT(ctx, "ghp_pat", "Main"); err != nil {
		t.Fatal(err)
	}
	_, err := r.o.AddPAT(ctx, "ghp_pat", "Other")
	var dup *auth.DuplicateError
	if !errors.Is(err, auth.ErrTokenDuplicate) || !errors.As(err, &dup) || dup.Name != "Main" {
		t.Fatalf("the same token string twice: %v", err)
	}
	// The same login, a different token (a second fine-grained token for another owner) is fine.
	if _, err := r.o.AddPAT(ctx, "ghp_extra1", ""); err != nil {
		t.Fatalf("same login, other token: %v", err)
	}
	for i := 2; i <= 9; i++ {
		if _, err := r.o.AddPAT(ctx, fmt.Sprintf("ghp_extra%d", i), ""); err != nil {
			t.Fatalf("token %d: %v", i+1, err)
		}
	}
	if n := len(list(t, r)); n != auth.MaxGitHubTokens {
		t.Fatalf("%d tokens", n)
	}
	calls := r.prov.viewerCalls.Load()
	if _, err := r.o.AddPAT(ctx, "ghp_extra10", ""); !errors.Is(err, auth.ErrTokenLimit) {
		t.Fatalf("the 11th: %v", err)
	}
	if r.prov.viewerCalls.Load() != calls {
		t.Fatal("the limit is checked before GitHub is asked")
	}
	if n := len(list(t, r)); n != auth.MaxGitHubTokens {
		t.Fatalf("%d tokens", n)
	}
}

func TestRenameAndRemoveGitHubToken(t *testing.T) {
	r := newOAuthRig(t)
	a, _ := r.o.AddPAT(ctx, "ghp_pat", "One")
	b, _ := r.o.AddPAT(ctx, "ghp_extra1", "Two")

	got, err := r.o.RenameGitHubToken(b.ID, " Three ")
	if err != nil || got.Name != "Three" || got.ID != b.ID {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := r.o.RenameGitHubToken(b.ID, "one"); !errors.Is(err, auth.ErrTokenNameTaken) {
		t.Fatalf("%v", err)
	}
	if got, err := r.o.RenameGitHubToken(a.ID, "ONE"); err != nil || got.Name != "ONE" {
		t.Fatalf("a token may change the case of its own name: %+v %v", got, err)
	}
	if _, err := r.o.RenameGitHubToken(a.ID, ""); !errors.Is(err, auth.ErrTokenName) {
		t.Fatalf("%v", err)
	}
	for _, id := range []string{"deadbeef", ""} {
		if _, err := r.o.RenameGitHubToken(id, "x"); !errors.Is(err, auth.ErrTokenNotFound) {
			t.Fatalf("%q: %v", id, err)
		}
		if _, err := r.o.RemoveGitHubToken(id); !errors.Is(err, auth.ErrTokenNotFound) {
			t.Fatalf("%q: %v", id, err)
		}
	}

	left, err := r.o.RemoveGitHubToken(a.ID)
	if err != nil || left != 1 {
		t.Fatalf("%d %v", left, err)
	}
	if l := list(t, r); len(l) != 1 || l[0].ID != b.ID {
		t.Fatalf("%+v", l)
	}
	if acc, ok, _ := r.o.GitHubAccount(); !ok || acc.ID != b.ID {
		t.Fatalf("the next token becomes the primary one: %+v", acc)
	}
	if left, err = r.o.RemoveGitHubToken(b.ID); err != nil || left != 0 {
		t.Fatalf("%d %v", left, err)
	}
	if _, err := r.sec.Get(secrets.GitHubTokens); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("removing the last token disconnects: %v", err)
	}
	if _, ok, _ := r.o.GitHubAccount(); ok {
		t.Fatal("still connected")
	}
	if _, err := r.o.GitHubAccess(ctx, ""); !errors.Is(err, auth.ErrNotConnected) {
		t.Fatalf("%v", err)
	}
}

func TestDisconnectRemovesEveryToken(t *testing.T) {
	r := newOAuthRig(t)
	_, _ = r.o.AddPAT(ctx, "ghp_pat", "")
	_, _ = r.o.AddPAT(ctx, "ghp_extra1", "")
	_ = r.sec.Set(secrets.GitHubToken, []byte(`{"token":"stale","method":"pat","login":"x"}`))
	if err := r.o.Disconnect(auth.GitHub); err != nil {
		t.Fatal(err)
	}
	if n := len(list(t, r)); n != 0 {
		t.Fatalf("%d tokens, and a stale legacy copy must not come back", n)
	}
}

func TestSignInReplacesTheAppEntryInPlace(t *testing.T) {
	r := newOAuthRig(t)
	pat, _ := r.o.AddPAT(ctx, "ghp_pat", "Work")
	connectGitHub(t, r)
	l := list(t, r)
	if len(l) != 2 || l[0].ID != pat.ID || l[1].Method != "app" || l[1].Name != "GitHub App (octocat)" {
		t.Fatalf("the sign-in is added after the tokens: %+v", l)
	}
	app := l[1]
	if _, err := r.o.RenameGitHubToken(app.ID, "Me"); err != nil {
		t.Fatal(err)
	}
	r.prov.githubTokenAnswer.Store(`{"access_token":"ghu_2","token_type":"bearer","refresh_token":"ghr_9","expires_in":28800}`)
	connectGitHub(t, r)
	l = list(t, r)
	if len(l) != 2 || l[1].ID != app.ID || l[1].Name != "Me" || l[1].Token != "ghu_2" || l[1].RefreshToken != "ghr_9" || l[1].NeedsReconnect {
		t.Fatalf("a new sign-in replaces the App entry and keeps its id and name: %+v", l)
	}

	// The default name follows the login of the new sign-in.
	r2 := newOAuthRig(t)
	connectGitHub(t, r2)
	r2.prov.githubTokenAnswer.Store(`{"access_token":"ghp_hubot","token_type":"bearer"}`)
	connectGitHub(t, r2)
	if l := list(t, r2); len(l) != 1 || l[0].Name != "GitHub App (hubot)" {
		t.Fatalf("%+v", l)
	}
}

func TestSignInAtTheLimitIsRefusedUnlessItReplaces(t *testing.T) {
	r := newOAuthRig(t)
	for i := range auth.MaxGitHubTokens {
		if _, err := r.o.AddPAT(ctx, fmt.Sprintf("ghp_extra%d", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	_, state := r.start(t, auth.GitHub, "s")
	if err := r.o.Complete(ctx, auth.GitHub, "c", state, "s"); !errors.Is(err, auth.ErrTokenLimit) {
		t.Fatalf("%v", err)
	}
	if n := len(list(t, r)); n != auth.MaxGitHubTokens {
		t.Fatalf("%d", n)
	}
}

func TestEachTokenRefreshesOnItsOwn(t *testing.T) {
	r := newOAuthRig(t)
	pat, _ := r.o.AddPAT(ctx, "ghp_pat", "Work")
	connectGitHub(t, r)
	app := list(t, r)[1]

	r.clock.Advance(9 * time.Hour)
	got, err := r.o.GitHubAccess(ctx, app.ID)
	if err != nil || got.Token != "ghu_2" || got.Name != app.Name || got.Login != "octocat" {
		t.Fatalf("%+v %v", got, err)
	}
	if l := list(t, r); l[0].Token != "ghp_pat" || l[1].Token != "ghu_2" || l[1].RefreshToken != "ghr_2" {
		t.Fatalf("only the App entry rotated: %+v", l)
	}
	if got, err := r.o.GitHubAccess(ctx, pat.ID); err != nil || got.Token != "ghp_pat" || got.Name != "Work" {
		t.Fatalf("%+v %v", got, err)
	}
	if r.prov.ghRefreshCalls.Load() != 1 {
		t.Fatalf("%d refresh calls", r.prov.ghRefreshCalls.Load())
	}

	// A refresh GitHub rejects flags the App entry and leaves the personal access token alone.
	r.prov.ghRefreshAnswer.Store(`{"error":"bad_refresh_token"}`)
	r.clock.Advance(9 * time.Hour)
	got, err = r.o.GitHubAccess(ctx, app.ID)
	if !errors.Is(err, auth.ErrNeedsReconnect) || got.Token != "" || got.Name != app.Name {
		t.Fatalf("the credential is named, without its token: %+v %v", got, err)
	}
	l := list(t, r)
	if l[0].NeedsReconnect || !l[1].NeedsReconnect {
		t.Fatalf("%+v", l)
	}
	if got, err := r.o.GitHubAccess(ctx, pat.ID); err != nil || got.Token != "ghp_pat" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := r.o.GitHubAccess(ctx, "deadbeef"); !errors.Is(err, auth.ErrTokenNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestRejectedPersonalTokenIsFlaggedButStaysUsable(t *testing.T) {
	r := newOAuthRig(t)
	pat, _ := r.o.AddPAT(ctx, "ghp_pat", "Work")
	other, _ := r.o.AddPAT(ctx, "ghp_extra1", "Other")
	if err := r.o.MarkGitHubRejected(pat.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.o.MarkGitHubRejected("deadbeef"); err != nil {
		t.Fatalf("an unknown id is not an error: %v", err)
	}
	l := list(t, r)
	if !l[0].NeedsReconnect || l[1].NeedsReconnect {
		t.Fatalf("%+v", l)
	}
	if got, err := r.o.GitHubAccess(ctx, pat.ID); err != nil || got.Token != "ghp_pat" {
		t.Fatalf("a flagged token is still tried: %+v %v", got, err)
	}
	// A check that works clears the flag.
	if _, err := r.o.CheckGitHubToken(ctx, pat.ID); err != nil {
		t.Fatal(err)
	}
	if list(t, r)[0].NeedsReconnect {
		t.Fatal("flag kept after a good check")
	}
	// A check that GitHub rejects sets it, on that token only.
	setGitHub2(t, r, other.ID, "revoked")
	if _, err := r.o.CheckGitHubToken(ctx, other.ID); !errors.Is(err, github.ErrBadCredentials) {
		t.Fatalf("%v", err)
	}
	if l := list(t, r); l[0].NeedsReconnect || !l[1].NeedsReconnect {
		t.Fatalf("%+v", l)
	}
}

// setGitHub2 replaces the token string of an entry.
func setGitHub2(t *testing.T, r *oauthRig, id, token string) {
	t.Helper()
	l := list(t, r)
	for i := range l {
		if l[i].ID == id {
			l[i].Token = token
		}
	}
	raw, _ := jsonMarshal(l)
	if err := r.sec.Set(secrets.GitHubTokens, raw); err != nil {
		t.Fatal(err)
	}
}

func TestCheckGitHubChecksEveryToken(t *testing.T) {
	r := newOAuthRig(t)
	a, _ := r.o.AddPAT(ctx, "ghp_pat", "A")
	b, _ := r.o.AddPAT(ctx, "ghp_extra1", "B")
	setGitHub2(t, r, a.ID, "revoked")
	primary, err := r.o.CheckGitHub(ctx)
	if !errors.Is(err, github.ErrBadCredentials) || primary.ID != a.ID {
		t.Fatalf("the first error is returned: %+v %v", primary, err)
	}
	l := list(t, r)
	if !l[0].NeedsReconnect || l[1].NeedsReconnect || l[1].ID != b.ID {
		t.Fatalf("both were checked, only the bad one is flagged: %+v", l)
	}
}
