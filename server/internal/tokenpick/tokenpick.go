// Package tokenpick decides which GitHub token a repository is read and written with, when several
// tokens can see it, and merges the search results of the tokens into one list. It is pure: no
// network, no database, no clock.
package tokenpick

import (
	"slices"
	"strings"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
)

// Token names a credential. It never holds the token value.
type Token struct {
	ID    string
	Name  string
	Login string
}

// Rank orders the viewer permissions: ADMIN, MAINTAIN, WRITE, TRIAGE, READ, and 0 for a role
// GitHub did not say or one this version does not know.
func Rank(permission string) int {
	switch permission {
	case "ADMIN":
		return 5
	case "MAINTAIN":
		return 4
	case "WRITE":
		return 3
	case "TRIAGE":
		return 2
	case "READ":
		return 1
	}
	return 0
}

// Choose returns the position of the best permission in the list: the highest rank, the first one
// on a tie, so the earlier token (the primary one first) wins. It returns -1 for an empty list.
func Choose(permissions []string) int {
	best := -1
	for i, p := range permissions {
		if best < 0 || Rank(p) > Rank(permissions[best]) {
			best = i
		}
	}
	return best
}

// Found is what the open pull request searches of one token returned.
type Found struct {
	Token  Token
	Result github.SearchResult
}

// Merged is the union of the searches of several tokens.
type Merged struct {
	// Result holds the pull requests once each, the other fields combined: Capped and Incomplete
	// when any token's search was, the errors of all of them, the blocked organizations that no
	// token got through, the highest total and the sum of the requests. Its RateLimit is Lowest.
	Result github.SearchResult
	// Repos maps a repository id to the token it is read and written with.
	Repos map[string]Token
	// Lowest is the budget with the least remaining, which the refresh interval and backoff follow.
	Lowest github.RateLimit
	// Tightest is the budget with the highest used share, which the dashboard shows.
	Tightest github.RateLimit
}

var roleOrder = []string{github.RoleAssigned, github.RoleAuthored, github.RoleReviewer}

type copyOf struct {
	from int
	pr   github.PR
}

// Merge combines the results of the tokens, given in list order (the primary token first).
//
// A pull request is identified by its node id and listed once, in the position where the first
// token listed it, with the roles of every token that found it. For each repository the token is
// chosen among those whose searches returned that repository: the highest viewer permission, the
// earlier token on a tie. The pull request's fields come from that token's copy; a pull request
// the chosen token did not return comes from the best token that did. Every pull request of the
// repository then carries the chosen token's viewer permission.
func Merge(found []Found) Merged {
	var out Merged
	out.Repos = map[string]Token{}
	rls := make([]github.RateLimit, 0, len(found))

	// The candidates of each repository, in token order, with the permission each token has.
	type candidate struct {
		from       int
		permission string
	}
	cands := map[string][]candidate{}
	for i, f := range found {
		seen := map[string]bool{}
		for _, pr := range f.Result.PRs {
			if !seen[pr.Repo.ID] {
				seen[pr.Repo.ID] = true
				cands[pr.Repo.ID] = append(cands[pr.Repo.ID], candidate{i, pr.Repo.ViewerPermission})
			}
		}
		rls = append(rls, f.Result.RateLimit)
	}
	chosen := make(map[string]candidate, len(cands))
	for repo, cs := range cands {
		perms := make([]string, len(cs))
		for i, c := range cs {
			perms[i] = c.permission
		}
		c := cs[Choose(perms)]
		chosen[repo] = c
		out.Repos[repo] = found[c.from].Token
	}

	var order []string
	copies := map[string][]copyOf{}
	for i, f := range found {
		for _, pr := range f.Result.PRs {
			if _, ok := copies[pr.ID]; !ok {
				order = append(order, pr.ID)
			}
			copies[pr.ID] = append(copies[pr.ID], copyOf{i, pr})
		}
	}
	for _, id := range order {
		cs := copies[id]
		repo := cs[0].pr.Repo.ID
		var pick copyOf
		if at := slices.IndexFunc(cs, func(c copyOf) bool { return c.from == chosen[repo].from }); at >= 0 {
			pick = cs[at]
		} else {
			perms := make([]string, len(cs))
			for i, c := range cs {
				perms[i] = c.pr.Repo.ViewerPermission
			}
			pick = cs[Choose(perms)]
		}
		pr := pick.pr
		pr.Repo.ViewerPermission = chosen[repo].permission
		pr.Roles = nil
		for _, role := range roleOrder {
			if slices.ContainsFunc(cs, func(c copyOf) bool { return slices.Contains(c.pr.Roles, role) }) {
				pr.Roles = append(pr.Roles, role)
			}
		}
		out.Result.PRs = append(out.Result.PRs, pr)
	}

	for _, f := range found {
		r := f.Result
		out.Result.Total = max(out.Result.Total, r.Total)
		out.Result.Capped = out.Result.Capped || r.Capped
		out.Result.Incomplete = out.Result.Incomplete || r.Incomplete
		out.Result.Requests += r.Requests
		for _, e := range r.Errors {
			if !slices.Contains(out.Result.Errors, e) {
				out.Result.Errors = append(out.Result.Errors, e)
			}
		}
	}
	out.Result.Total = max(out.Result.Total, len(out.Result.PRs))
	out.Result.Blocked = blocked(found, out.Result.PRs)
	out.Lowest, out.Tightest = Lowest(rls), Tightest(rls)
	out.Result.RateLimit = out.Lowest
	return out
}

// blocked lists the organizations a token was refused for, once each. An organization whose pull
// requests came back from another token is not blocked for the dashboard, so it is left out.
func blocked(found []Found, prs []github.PR) []github.OrgBlocked {
	var out []github.OrgBlocked
	for _, f := range found {
		for _, b := range f.Result.Blocked {
			if b.Org != "" && slices.ContainsFunc(prs, func(pr github.PR) bool { return ownerOf(pr.Repo.NameWithOwner, b.Org) }) {
				continue
			}
			if !slices.ContainsFunc(out, func(o github.OrgBlocked) bool { return o.Org == b.Org && o.Reason == b.Reason }) {
				out = append(out, b)
			}
		}
	}
	return out
}

func ownerOf(nameWithOwner, org string) bool {
	owner, _, _ := strings.Cut(nameWithOwner, "/")
	return strings.EqualFold(owner, org)
}

// Lowest returns the budget with the least remaining, the first on a tie, and the summed cost.
// Entries without a limit (GitHub reported none) are ignored; with none left it is the zero value.
func Lowest(rls []github.RateLimit) github.RateLimit {
	return pickLimit(rls, func(a, b github.RateLimit) bool { return a.Remaining < b.Remaining })
}

// Tightest returns the budget with the highest share used, the first on a tie, and the summed
// cost. Entries without a limit are ignored.
func Tightest(rls []github.RateLimit) github.RateLimit {
	return pickLimit(rls, func(a, b github.RateLimit) bool {
		// used(a)/limit(a) > used(b)/limit(b), without division.
		return (a.Limit-a.Remaining)*b.Limit > (b.Limit-b.Remaining)*a.Limit
	})
}

func pickLimit(rls []github.RateLimit, better func(a, b github.RateLimit) bool) github.RateLimit {
	var out github.RateLimit
	cost := 0
	for _, r := range rls {
		cost += r.Cost
		if r.Limit > 0 && (out.Limit == 0 || better(r, out)) {
			out = r
		}
	}
	out.Cost = cost
	if out.Limit == 0 {
		return github.RateLimit{Cost: cost}
	}
	return out
}
