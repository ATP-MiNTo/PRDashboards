//go:build e2e

package fake

import (
	"testing"
	"time"
)

func titles(t *testing.T, query string) map[string]bool {
	t.Helper()
	_, body := searchBody(query, githubConfig{}, nil, time.Now())
	out := map[string]bool{}
	for _, n := range body["data"].(map[string]any)["search"].(map[string]any)["nodes"].([]any) {
		out[n.(map[string]any)["title"].(string)] = true
	}
	return out
}

func TestSearchHonoursAssigneeAndAuthor(t *testing.T) {
	assigned := titles(t, "is:pr is:open assignee:@me archived:false")
	authored := titles(t, "is:pr is:open author:@me archived:false")
	const both, authoredOnly, assignedOnly = "INP-102 Single sign-on", "WIP schema cleanup", "INP-101 Add login"
	if !assigned[both] || !authored[both] {
		t.Errorf("a pull request that is assigned and authored is in both searches: %v / %v", assigned, authored)
	}
	if assigned[authoredOnly] || !authored[authoredOnly] {
		t.Errorf("an authored-only pull request is only in the author search: %v / %v", assigned, authored)
	}
	if !assigned[assignedOnly] || authored[assignedOnly] {
		t.Errorf("an assigned-only pull request is only in the assignee search: %v / %v", assigned, authored)
	}
}

func TestSearchHonoursReviewRequested(t *testing.T) {
	review := titles(t, "is:pr is:open review-requested:@me archived:false")
	assigned := titles(t, "is:pr is:open assignee:@me archived:false")
	authored := titles(t, "is:pr is:open author:@me archived:false")
	reviewOnly := []string{"Tighten rate limiter", "Fix focus ring on tabs"}
	for _, title := range reviewOnly {
		if !review[title] || assigned[title] || authored[title] {
			t.Errorf("%q is only in the review requested search: %v / %v / %v", title, review, assigned, authored)
		}
	}
	if len(review) != len(reviewOnly) {
		t.Errorf("only the review-only pull requests match review-requested:@me: %v", review)
	}
	for _, title := range []string{"INP-101 Add login", "INP-102 Single sign-on", "WIP schema cleanup"} {
		if review[title] {
			t.Errorf("%q must not be in the review requested search", title)
		}
	}
}

func TestNodesCarryAuthorUpdateTimeAndSize(t *testing.T) {
	now := time.Now()
	_, body := searchBody("is:pr is:open assignee:@me archived:false", githubConfig{}, nil, now)
	nodes := body["data"].(map[string]any)["search"].(map[string]any)["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatal("no nodes")
	}
	for _, n := range nodes {
		m := n.(map[string]any)
		author := m["author"].(map[string]any)["login"]
		if author == "" || m["updatedAt"] == nil || m["additions"] == nil || m["deletions"] == nil {
			t.Errorf("%v lacks author, updatedAt or size: %v", m["title"], m)
		}
		if m["title"] == "INP-102 Single sign-on" && author != "octocat" {
			t.Errorf("a pull request the viewer opened is by the viewer, got %v", author)
		}
		if m["title"] == "INP-101 Add login" && (m["additions"] != 214 || m["deletions"] != 38) {
			t.Errorf("fixture size %v +%v -%v", m["title"], m["additions"], m["deletions"])
		}
	}
}
