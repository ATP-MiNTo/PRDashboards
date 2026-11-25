// Package linking resolves which Jira tickets a pull request belongs to (TECH_SPEC 7.1). It is pure:
// no network, no database, no clock.
package linking

import (
	"regexp"
	"slices"
	"strings"
)

var keyPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)

// Source says where a link came from.
type Source string

// Link sources.
const (
	Branch Source = "branch"
	Title  Source = "title"
	Manual Source = "manual"
)

// Link is one ticket of a pull request.
type Link struct {
	Key    string
	Source Source
}

// Input is what is known about one pull request. Manual holds the keys the person added and
// Removed the keys the person hid.
type Input struct {
	HeadRef string
	Title   string
	Manual  []string
	Removed []string
}

// Projects is the set of Jira project keys of the connected site. The zero value means the list
// is unknown, for example when Jira could not be reached.
type Projects struct {
	set map[string]struct{}
}

// NewProjects returns a known set. An empty list is a known, empty set.
func NewProjects(keys []string) Projects {
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}
	return Projects{set: set}
}

// Known reports whether the project list is available.
func (p Projects) Known() bool { return p.set != nil }

// Keys returns the project keys in sorted order. It is nil when the list is unknown.
func (p Projects) Keys() []string {
	if p.set == nil {
		return nil
	}
	out := make([]string, 0, len(p.set))
	for k := range p.set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Has reports whether the project exists. It is false when the list is unknown.
func (p Projects) Has(project string) bool {
	_, ok := p.set[project]
	return ok
}

// Result is the ordered ticket list of a pull request. Unverified is true when the project list
// was unknown, so the keys were kept without checking them.
type Result struct {
	Links      []Link
	Unverified bool
}

// Keys returns the ticket keys in text, in order and without duplicates.
func Keys(text string) []string {
	found := keyPattern.FindAllString(text, -1)
	out := found[:0]
	seen := make(map[string]struct{}, len(found))
	for _, k := range found {
		if _, dup := seen[k]; !dup {
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

// ProjectOf returns the project part of a ticket key.
func ProjectOf(key string) string {
	project, _, _ := strings.Cut(key, "-")
	return project
}

// ValidKey reports whether s is exactly one ticket key.
func ValidKey(s string) bool { return keyPattern.FindString(s) == s && s != "" }

// Resolve lists the tickets of a pull request: keys of the head branch, then of the title, then
// the manual links. Duplicates keep their first source. Hidden keys are dropped, and so are keys
// of projects that do not exist on the site.
func Resolve(in Input, projects Projects) Result {
	hidden := make(map[string]struct{}, len(in.Removed))
	for _, k := range in.Removed {
		hidden[k] = struct{}{}
	}
	var res Result
	seen := map[string]struct{}{}
	add := func(key string, src Source) {
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		if _, gone := hidden[key]; gone {
			return
		}
		if projects.Known() && !projects.Has(ProjectOf(key)) {
			return
		}
		res.Links = append(res.Links, Link{Key: key, Source: src})
	}
	for _, k := range Keys(in.HeadRef) {
		add(k, Branch)
	}
	for _, k := range Keys(in.Title) {
		add(k, Title)
	}
	for _, k := range in.Manual {
		add(k, Manual)
	}
	res.Unverified = !projects.Known() && len(res.Links) > 0
	return res
}

// PRLinks is the resolved tickets of one pull request.
type PRLinks struct {
	PRID  string
	Links []Link
}

// Index inverts the links: for every ticket, the ids of its pull requests in input order. A ticket
// may have many pull requests.
func Index(prs []PRLinks) map[string][]string {
	out := map[string][]string{}
	for _, p := range prs {
		for _, l := range p.Links {
			out[l.Key] = append(out[l.Key], p.PRID)
		}
	}
	return out
}

// AllKeys lists every distinct ticket key of the given pull requests, in first-seen order.
func AllKeys(prs []PRLinks) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, p := range prs {
		for _, l := range p.Links {
			if _, dup := seen[l.Key]; !dup {
				seen[l.Key] = struct{}{}
				out = append(out, l.Key)
			}
		}
	}
	return out
}
