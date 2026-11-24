package merge_test

import (
	"slices"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
)

var all = merge.Allowed{Squash: true, Merge: true, Rebase: true}

func TestResolve(t *testing.T) {
	rules := []merge.Rule{
		{Pattern: "main", Method: merge.Merge},
		{Pattern: "release/*", Method: merge.Rebase},
		{Pattern: "release/hotfix", Method: merge.Squash},
	}
	tests := []struct {
		name    string
		repo    merge.Repo
		base    string
		allowed merge.Allowed
		method  merge.Method
		dflt    merge.Method
		source  string
		warning string
		list    []merge.Method
	}{
		{"first matching rule wins", merge.Repo{Default: merge.Squash, Rules: rules}, "release/hotfix", all, merge.Rebase, merge.Rebase, "rule: release/*", "", []merge.Method{merge.Squash, merge.Merge, merge.Rebase}},
		{"exact rule", merge.Repo{Default: merge.Squash, Rules: rules}, "main", all, merge.Merge, merge.Merge, "rule: main", "", nil},
		{"no rule uses the repo default", merge.Repo{Default: merge.Rebase, Rules: rules}, "develop", all, merge.Rebase, merge.Rebase, "repo default", "", nil},
		{"no rules at all", merge.Repo{Default: merge.Squash}, "main", all, merge.Squash, merge.Squash, "repo default", "", nil},
		{"new repo default", merge.Repo{Default: merge.Squash, IsNew: true, Rules: rules}, "develop", all, merge.Squash, merge.Squash, "new repo default", "", nil},
		{"a rule beats the new repo default", merge.Repo{Default: merge.Squash, IsNew: true, Rules: rules}, "main", all, merge.Merge, merge.Merge, "rule: main", "", nil},
		{"squash disallowed falls back to merge commit", merge.Repo{Default: merge.Squash}, "develop", merge.Allowed{Merge: true, Rebase: true}, merge.Merge, merge.Squash, "repo default",
			"Repo disallows squash merge, using merge commit", []merge.Method{merge.Merge, merge.Rebase}},
		{"merge commit disallowed falls back to squash", merge.Repo{Default: merge.Merge}, "develop", merge.Allowed{Squash: true, Rebase: true}, merge.Squash, merge.Merge, "repo default",
			"Repo disallows merge commit, using squash merge", nil},
		{"only rebase left", merge.Repo{Default: merge.Squash}, "develop", merge.Allowed{Rebase: true}, merge.Rebase, merge.Squash, "repo default",
			"Repo disallows squash merge, using rebase merge", []merge.Method{merge.Rebase}},
		{"a rule's method can fall back too", merge.Repo{Default: merge.Squash, Rules: rules}, "release/1", merge.Allowed{Squash: true}, merge.Squash, merge.Rebase, "rule: release/*",
			"Repo disallows rebase merge, using squash merge", nil},
		{"nothing allowed keeps the configured method", merge.Repo{Default: merge.Squash}, "develop", merge.Allowed{}, merge.Squash, merge.Squash, "repo default",
			"Repo allows no merge method", []merge.Method{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := merge.Resolve(tt.repo, tt.base, tt.allowed)
			if got.Method != tt.method || got.Default != tt.dflt || got.Source != tt.source || got.Warning != tt.warning {
				t.Fatalf("got %+v", got)
			}
			if tt.list != nil && !slices.Equal(got.Allowed, tt.list) {
				t.Fatalf("allowed %v, want %v", got.Allowed, tt.list)
			}
		})
	}
}

func TestPermitsOnlyAllowedOverrides(t *testing.T) {
	res := merge.Resolve(merge.Repo{Default: merge.Squash}, "x", merge.Allowed{Squash: true, Rebase: true})
	for m, want := range map[merge.Method]bool{merge.Squash: true, merge.Rebase: true, merge.Merge: false, "bogus": false} {
		if res.Permits(m) != want {
			t.Errorf("Permits(%s) = %v", m, !want)
		}
	}
}

func TestMethodHelpers(t *testing.T) {
	for m, label := range map[merge.Method]string{merge.Squash: "squash merge", merge.Merge: "merge commit", merge.Rebase: "rebase merge"} {
		if m.Label() != label || !m.Valid() {
			t.Errorf("%s: %q %v", m, m.Label(), m.Valid())
		}
	}
	if merge.Method("octopus").Valid() || (merge.Allowed{Squash: true}).Has("octopus") {
		t.Fatal("unknown method accepted")
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"main", "main", true},
		{"main", "main2", false},
		{"main", "xmain", false},
		{"release/*", "release/1.2", true},
		{"release/*", "release/1.2/hotfix", true},
		{"release/*", "release/", true},
		{"release/*", "release", false},
		{"*", "anything/at/all", true},
		{"*", "", true},
		{"*-hotfix", "v2-hotfix", true},
		{"*-hotfix", "v2-hotfix-2", false},
		{"feat/*/ui", "feat/a/ui", true},
		{"feat/*/ui", "feat/a/b/ui", true},
		{"feat/*/ui", "feat/ui", false},
		{"a*a", "a", false},
		{"a*a", "aa", true},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxcyyb", false},
		{"a*b*c", "abc", true},
		{"a**c", "ac", true},
		{"v1.*", "v1x2", false},
	}
	for _, tt := range tests {
		if got := merge.Match(tt.pattern, tt.name); got != tt.want {
			t.Errorf("Match(%q, %q) = %v", tt.pattern, tt.name, got)
		}
	}
}
