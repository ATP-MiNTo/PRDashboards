// Package stacks finds stacked pull requests (TECH_SPEC 7.2). It is pure: no network, no database,
// no clock.
package stacks

import "strconv"

// PR is what stack detection needs of a pull request.
type PR struct {
	ID      string
	Repo    string
	Number  int
	HeadRef string
	BaseRef string
}

// Info places a pull request in a stack.
type Info struct {
	// RootID is the pull request at the base of the stack.
	RootID string
	// Position is 1-based, in depth-first order from the root.
	Position int
	Size     int
	// ParentID and ParentNumber name the pull request this one is stacked on, empty and 0 for the
	// root.
	ParentID     string
	ParentNumber int
}

// MergeBlock is the reason a child cannot merge yet, or "" for the root.
func (i Info) MergeBlock() string {
	if i.ParentNumber == 0 {
		return ""
	}
	return "Merge after #" + strconv.Itoa(i.ParentNumber)
}

// Detect returns the stack position of every pull request that is in a stack, keyed by id. A pull
// request is stacked on another when, in the same repo, its base branch is the head branch of
// another pull request of the list. It is one pass over a map keyed by repo and head branch.
//
// When two pull requests share a head branch, the first in the list is the parent. A cycle never
// forms a stack, because it has no root.
func Detect(prs []PR) map[string]Info {
	byHead := make(map[string]int, len(prs))
	for i, p := range prs {
		k := key(p.Repo, p.HeadRef)
		if _, taken := byHead[k]; !taken {
			byHead[k] = i
		}
	}
	parent := make([]int, len(prs))
	children := make([][]int, len(prs))
	for i, p := range prs {
		parent[i] = -1
		if j, ok := byHead[key(p.Repo, p.BaseRef)]; ok && j != i {
			parent[i] = j
			children[j] = append(children[j], i)
		}
	}

	out := map[string]Info{}
	for i, p := range prs {
		if parent[i] != -1 || len(children[i]) == 0 {
			continue
		}
		var members []int
		var walk func(int)
		walk = func(n int) {
			members = append(members, n)
			for _, c := range children[n] {
				walk(c)
			}
		}
		walk(i)
		for pos, m := range members {
			info := Info{RootID: p.ID, Position: pos + 1, Size: len(members)}
			if parent[m] != -1 {
				info.ParentID, info.ParentNumber = prs[parent[m]].ID, prs[parent[m]].Number
			}
			out[prs[m].ID] = info
		}
	}
	return out
}

func key(repo, branch string) string { return repo + "|" + branch }
