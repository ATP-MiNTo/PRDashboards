package linking_test

import (
	"reflect"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/linking"
)

func TestKeys(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"branch", "feat/INP-123-add-login-form", []string{"INP-123"}},
		{"two keys in order", "fix/INP-301-INP-302-auth-banners", []string{"INP-301", "INP-302"}},
		{"duplicates removed", "INP-1 and INP-1 again, then OPS-2", []string{"INP-1", "OPS-2"}},
		{"digits in project", "A1B2-77", []string{"A1B2-77"}},
		{"title with punctuation", "INP-410, OPS-53: Reconcile", []string{"INP-410", "OPS-53"}},
		{"lowercase ignored", "feat/inp-123", nil},
		{"single letter project ignored", "A-1", nil},
		{"no digits", "INP-x", nil},
		{"underscore joins words", "feat_INP-1", nil},
		{"empty", "", nil},
		{"shaped like a key", "UTF-8", []string{"UTF-8"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := linking.Keys(tc.in); !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Fatalf("Keys(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestProjectOfAndValidKey(t *testing.T) {
	if linking.ProjectOf("INP-12") != "INP" || linking.ProjectOf("nodash") != "nodash" {
		t.Fatal("ProjectOf")
	}
	for k, want := range map[string]bool{"INP-1": true, "A1-22": true, "": false, "inp-1": false, "INP-1 ": false, "x INP-1": false, "INP-1-2": false} {
		if linking.ValidKey(k) != want {
			t.Errorf("ValidKey(%q) != %v", k, want)
		}
	}
}

func TestResolve(t *testing.T) {
	projects := linking.NewProjects([]string{"INP", "OPS"})
	link := func(k string, s linking.Source) linking.Link { return linking.Link{Key: k, Source: s} }
	tests := []struct {
		name       string
		in         linking.Input
		projects   linking.Projects
		want       []linking.Link
		unverified bool
	}{
		{
			name:     "branch before title",
			in:       linking.Input{HeadRef: "feat/INP-1-x", Title: "OPS-2 and INP-3"},
			projects: projects,
			want:     []linking.Link{link("INP-1", linking.Branch), link("OPS-2", linking.Title), link("INP-3", linking.Title)},
		},
		{
			name:     "a key in branch and title keeps the branch source",
			in:       linking.Input{HeadRef: "feat/INP-1", Title: "INP-1 Add"},
			projects: projects,
			want:     []linking.Link{link("INP-1", linking.Branch)},
		},
		{
			name:     "manual links come last",
			in:       linking.Input{HeadRef: "feat/INP-1", Manual: []string{"OPS-9", "INP-1"}},
			projects: projects,
			want:     []linking.Link{link("INP-1", linking.Branch), link("OPS-9", linking.Manual)},
		},
		{
			name:     "removed keys are hidden, whatever their source",
			in:       linking.Input{HeadRef: "feat/INP-1", Title: "OPS-2", Manual: []string{"OPS-9"}, Removed: []string{"INP-1", "OPS-9"}},
			projects: projects,
			want:     []linking.Link{link("OPS-2", linking.Title)},
		},
		{
			name:     "a hidden key stays hidden when found again in the title",
			in:       linking.Input{HeadRef: "feat/INP-1", Title: "INP-1", Removed: []string{"INP-1"}},
			projects: projects,
		},
		{
			name:     "unknown projects are dropped",
			in:       linking.Input{HeadRef: "fix/UTF-8-decoding", Title: "INP-5"},
			projects: projects,
			want:     []linking.Link{link("INP-5", linking.Title)},
		},
		{
			name:     "a known but empty project list drops everything",
			in:       linking.Input{HeadRef: "feat/INP-1"},
			projects: linking.NewProjects(nil),
		},
		{
			name:       "unknown project list keeps every key and flags it",
			in:         linking.Input{HeadRef: "fix/UTF-8-decoding", Title: "INP-5"},
			projects:   linking.Projects{},
			want:       []linking.Link{link("UTF-8", linking.Branch), link("INP-5", linking.Title)},
			unverified: true,
		},
		{
			name:     "unknown project list and no key is not unverified",
			in:       linking.Input{HeadRef: "main"},
			projects: linking.Projects{},
		},
		{name: "nothing", in: linking.Input{}, projects: projects},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := linking.Resolve(tc.in, tc.projects)
			if len(got.Links) != len(tc.want) || (len(tc.want) > 0 && !reflect.DeepEqual(got.Links, tc.want)) {
				t.Fatalf("links %v, want %v", got.Links, tc.want)
			}
			if got.Unverified != tc.unverified {
				t.Fatalf("unverified %v, want %v", got.Unverified, tc.unverified)
			}
		})
	}
}

func TestProjects(t *testing.T) {
	var zero linking.Projects
	if zero.Known() || zero.Has("INP") {
		t.Fatal("zero value must be unknown")
	}
	p := linking.NewProjects([]string{"INP"})
	if !p.Known() || !p.Has("INP") || p.Has("OPS") {
		t.Fatal("known set")
	}
	if e := linking.NewProjects(nil); !e.Known() || e.Has("INP") {
		t.Fatal("empty set is known")
	}
}

func TestIndexAndAllKeys(t *testing.T) {
	prs := []linking.PRLinks{
		{PRID: "a", Links: []linking.Link{{Key: "INP-1"}, {Key: "INP-2"}}},
		{PRID: "b", Links: []linking.Link{{Key: "INP-1"}}},
		{PRID: "c"},
	}
	want := map[string][]string{"INP-1": {"a", "b"}, "INP-2": {"a"}}
	if got := linking.Index(prs); !reflect.DeepEqual(got, want) {
		t.Fatalf("index %v", got)
	}
	if got := linking.AllKeys(prs); !reflect.DeepEqual(got, []string{"INP-1", "INP-2"}) {
		t.Fatalf("keys %v", got)
	}
	if len(linking.Index(nil)) != 0 || linking.AllKeys(nil) != nil {
		t.Fatal("empty input")
	}
}
