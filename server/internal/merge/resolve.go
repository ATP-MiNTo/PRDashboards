// Package merge decides how a pull request is merged and carries the merge out. This file is the
// pure method resolver of TECH_SPEC 7.6: no network, database or clock.
package merge

import (
	"slices"
	"strings"
)

// Method is a GitHub merge method.
type Method string

// Merge methods.
const (
	Squash Method = "squash"
	Merge  Method = "merge"
	Rebase Method = "rebase"
)

// Methods lists every method in the order the UI offers them.
var Methods = []Method{Squash, Merge, Rebase}

// fallbackOrder is the order tried when the configured method is not allowed.
var fallbackOrder = []Method{Merge, Squash, Rebase}

// Label is the name of a method in a sentence.
func (m Method) Label() string {
	switch m {
	case Squash:
		return "squash merge"
	case Rebase:
		return "rebase merge"
	}
	return "merge commit"
}

// Valid reports whether m is one of the three methods.
func (m Method) Valid() bool { return slices.Contains(Methods, m) }

// Rule sends pull requests whose base branch matches Pattern to Method.
type Rule struct {
	Pattern string
	Method  Method
}

// Allowed says which methods the repository accepts.
type Allowed struct{ Squash, Merge, Rebase bool }

// Has reports whether m is allowed.
func (a Allowed) Has(m Method) bool {
	switch m {
	case Squash:
		return a.Squash
	case Merge:
		return a.Merge
	case Rebase:
		return a.Rebase
	}
	return false
}

// List returns the allowed methods in UI order.
func (a Allowed) List() []Method {
	out := make([]Method, 0, len(Methods))
	for _, m := range Methods {
		if a.Has(m) {
			out = append(out, m)
		}
	}
	return out
}

// Repo is the merge configuration of one repository. IsNew marks a repo whose settings the person
// has not looked at yet, so its default still comes from the global default for new repos.
type Repo struct {
	Default Method
	IsNew   bool
	Rules   []Rule
	// Approvals is how many approvals a pull request needs before it may merge; outside 1 to 10 it
	// means the default of 1 (NeedOf).
	Approvals int
}

// Resolution is the method to use for one pull request.
type Resolution struct {
	// Method is what the merge uses unless the person picks another allowed one.
	Method Method
	// Default is the configured method, before the fallback for a disallowed one.
	Default Method
	// Source says where Default came from: "rule: <pattern>", "repo default" or "new repo default".
	Source  string
	Allowed []Method
	// Warning is set when the configured method is not allowed and Method is a fallback.
	Warning string
}

// Resolve applies TECH_SPEC 7.6: the first branch rule that matches base, else the repo default,
// then the fallback to the first allowed method in the order merge commit, squash, rebase.
func Resolve(repo Repo, base string, allowed Allowed) Resolution {
	res := Resolution{Default: repo.Default, Source: "repo default", Allowed: allowed.List()}
	if repo.IsNew {
		res.Source = "new repo default"
	}
	for _, r := range repo.Rules {
		if Match(r.Pattern, base) {
			res.Default, res.Source = r.Method, "rule: "+r.Pattern
			break
		}
	}
	res.Method = res.Default
	if allowed.Has(res.Default) {
		return res
	}
	for _, m := range fallbackOrder {
		if allowed.Has(m) {
			res.Method = m
			res.Warning = "Repo disallows " + res.Default.Label() + ", using " + m.Label()
			return res
		}
	}
	res.Warning = "Repo allows no merge method"
	return res
}

// Permits reports whether a per-pull-request override can be used: only an allowed method can.
func (r Resolution) Permits(m Method) bool { return slices.Contains(r.Allowed, m) }

// Match reports whether name matches pattern. A pattern without `*` must equal name. Each `*`
// matches any run of characters, slashes included, so `release/*` matches `release/1.2/hotfix`.
func Match(pattern, name string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	parts := strings.Split(pattern, "*")
	rest, ok := strings.CutPrefix(name, parts[0])
	if !ok {
		return false
	}
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, p)
		if i < 0 {
			return false
		}
		rest = rest[i+len(p):]
	}
	return len(rest) >= len(last) && strings.HasSuffix(rest, last)
}
