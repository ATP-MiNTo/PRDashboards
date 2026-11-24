package stacks_test

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/stacks"
)

func pr(id string, n int, repo, head, base string) stacks.PR {
	return stacks.PR{ID: id, Number: n, Repo: repo, HeadRef: head, BaseRef: base}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		prs  []stacks.PR
		want map[string]stacks.Info
	}{
		{name: "no pull requests", want: map[string]stacks.Info{}},
		{
			name: "independent pull requests are not stacks",
			prs:  []stacks.PR{pr("a", 1, "r", "f1", "main"), pr("b", 2, "r", "f2", "main")},
			want: map[string]stacks.Info{},
		},
		{
			name: "a chain, in any input order",
			prs: []stacks.PR{
				pr("c", 3, "r", "f3", "f2"),
				pr("a", 1, "r", "f1", "main"),
				pr("b", 2, "r", "f2", "f1"),
			},
			want: map[string]stacks.Info{
				"a": {RootID: "a", Position: 1, Size: 3},
				"b": {RootID: "a", Position: 2, Size: 3, ParentID: "a", ParentNumber: 1},
				"c": {RootID: "a", Position: 3, Size: 3, ParentID: "b", ParentNumber: 2},
			},
		},
		{
			name: "a branching stack is walked depth first",
			prs: []stacks.PR{
				pr("a", 1, "r", "f1", "main"),
				pr("b", 2, "r", "f2", "f1"),
				pr("c", 3, "r", "f3", "f1"),
				pr("d", 4, "r", "f4", "f2"),
			},
			want: map[string]stacks.Info{
				"a": {RootID: "a", Position: 1, Size: 4},
				"b": {RootID: "a", Position: 2, Size: 4, ParentID: "a", ParentNumber: 1},
				"d": {RootID: "a", Position: 3, Size: 4, ParentID: "b", ParentNumber: 2},
				"c": {RootID: "a", Position: 4, Size: 4, ParentID: "a", ParentNumber: 1},
			},
		},
		{
			name: "the same branch name in another repo is not a parent",
			prs:  []stacks.PR{pr("a", 1, "r1", "f1", "main"), pr("b", 2, "r2", "f2", "f1")},
			want: map[string]stacks.Info{},
		},
		{
			name: "two stacks in one repo",
			prs: []stacks.PR{
				pr("a", 1, "r", "f1", "main"), pr("b", 2, "r", "f2", "f1"),
				pr("x", 7, "r", "g1", "main"), pr("y", 8, "r", "g2", "g1"),
			},
			want: map[string]stacks.Info{
				"a": {RootID: "a", Position: 1, Size: 2},
				"b": {RootID: "a", Position: 2, Size: 2, ParentID: "a", ParentNumber: 1},
				"x": {RootID: "x", Position: 1, Size: 2},
				"y": {RootID: "x", Position: 2, Size: 2, ParentID: "x", ParentNumber: 7},
			},
		},
		{
			name: "a pull request whose base is its own head is not its own parent",
			prs:  []stacks.PR{pr("a", 1, "r", "f1", "f1")},
			want: map[string]stacks.Info{},
		},
		{
			name: "a cycle has no root and forms no stack",
			prs:  []stacks.PR{pr("a", 1, "r", "f1", "f2"), pr("b", 2, "r", "f2", "f1")},
			want: map[string]stacks.Info{},
		},
		{
			name: "the first of two pull requests with one head branch is the parent",
			prs: []stacks.PR{
				pr("a", 1, "r", "f1", "main"),
				pr("a2", 2, "r", "f1", "release"),
				pr("b", 3, "r", "f2", "f1"),
			},
			want: map[string]stacks.Info{
				"a": {RootID: "a", Position: 1, Size: 2},
				"b": {RootID: "a", Position: 2, Size: 2, ParentID: "a", ParentNumber: 1},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stacks.Detect(tc.prs); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestMergeBlock(t *testing.T) {
	if got := (stacks.Info{ParentNumber: 12}).MergeBlock(); got != "Merge after #12" {
		t.Fatalf("child: %q", got)
	}
	if got := (stacks.Info{}).MergeBlock(); got != "" {
		t.Fatalf("root: %q", got)
	}
}

func TestDetectIsLinear(t *testing.T) {
	const n = 20000
	prs := make([]stacks.PR, 0, n)
	prs = append(prs, pr("p0", 0, "r", "b0", "main"))
	for i := 1; i < n; i++ {
		prs = append(prs, pr("p"+strconv.Itoa(i), i, "r", "b"+strconv.Itoa(i), "b"+strconv.Itoa(i-1)))
	}
	got := stacks.Detect(prs)
	if len(got) != n || got["p"+strconv.Itoa(n-1)].Position != n {
		t.Fatalf("%d members, last position %d", len(got), got["p"+strconv.Itoa(n-1)].Position)
	}
}
