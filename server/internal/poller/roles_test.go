package poller_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

func rolesByID(s gen.Snapshot) map[string][]gen.SnapshotPrRoles {
	out := map[string][]gen.SnapshotPrRoles{}
	for _, p := range s.Prs {
		out[p.Id] = p.Roles
	}
	return out
}

func TestSnapshotCarriesTheRolesOfEachPR(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/a", "main", "a")
	a.Roles = []string{github.RoleAssigned}
	b := pr("PR_b", 2, "acme/web", "feat/b", "main", "b")
	b.Roles = []string{github.RoleAuthored}
	c := pr("PR_c", 3, "acme/web", "feat/c", "main", "c")
	c.Roles = []string{github.RoleAssigned, github.RoleAuthored}
	d := pr("PR_d", 4, "acme/web", "feat/d", "main", "d")
	d.Roles = []string{github.RoleReviewer}
	e := pr("PR_e", 5, "acme/web", "feat/e", "main", "e")
	e.Roles = []string{github.RoleReviewer, github.RoleAuthored}
	r := newRig(t, a, b, c, d, e)
	r.sync()
	want := map[string][]gen.SnapshotPrRoles{
		"PR_a": {gen.Assigned}, "PR_b": {gen.Authored}, "PR_c": {gen.Assigned, gen.Authored},
		"PR_d": {gen.Reviewer}, "PR_e": {gen.Authored, gen.Reviewer},
	}
	for id, roles := range want {
		if got := rolesByID(r.snapshot())[id]; !slices.Equal(got, roles) {
			t.Errorf("%s: got %v, want %v", id, got, roles)
		}
	}
}

func TestPRWithoutRolesCountsForAssignedAndAuthoredNotReviewer(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/a", "main", "a"))
	r.sync()
	if got := rolesByID(r.snapshot())["PR_a"]; !slices.Equal(got, []gen.SnapshotPrRoles{gen.Assigned, gen.Authored}) {
		t.Fatalf("missing roles fall back to assigned and authored: %v", got)
	}
}

func TestStoredSnapshotWithoutRolesLoadsWithBoth(t *testing.T) {
	r := newRig(t, pr("PR_a", 1, "acme/web", "feat/a", "main", "a"))
	r.sync()
	var doc map[string]any
	if err := json.Unmarshal(r.p.Current().JSON, &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range doc["prs"].([]any) {
		delete(p.(map[string]any), "roles")
	}
	old, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(old)
	_ = zw.Close()
	if err := r.store.SaveSnapshot(context.Background(), 9, buf.Bytes(), r.clock.Now()); err != nil {
		t.Fatal(err)
	}

	fresh := r.newPoller(nil, nil)
	if err := fresh.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := fresh.Current()
	if roles := rolesByID(got.Snapshot)["PR_a"]; !slices.Equal(roles, []gen.SnapshotPrRoles{gen.Assigned, gen.Authored}) {
		t.Fatalf("roles after load: %v", roles)
	}
	var served struct {
		Prs []struct {
			Roles []string `json:"roles"`
		} `json:"prs"`
	}
	if err := json.Unmarshal(got.JSON, &served); err != nil || len(served.Prs) != 1 || len(served.Prs[0].Roles) != 2 {
		t.Fatalf("the served JSON carries the roles too: %v %+v", err, served)
	}
}

func TestRolesSurviveRestart(t *testing.T) {
	a := pr("PR_a", 1, "acme/web", "feat/a", "main", "a")
	a.Roles = []string{github.RoleAuthored}
	r := newRig(t, a)
	r.sync()
	fresh := r.newPoller(nil, nil)
	if err := fresh.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rolesByID(fresh.Current().Snapshot)["PR_a"]; !slices.Equal(got, []gen.SnapshotPrRoles{gen.Authored}) {
		t.Fatalf("got %v", got)
	}
}
