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
	authored := titles(t, "is:pr is:open author:@me -assignee:@me archived:false")
	const both, authoredOnly, assignedOnly = "INP-102 Single sign-on", "WIP schema cleanup", "INP-101 Add login"
	if !assigned[both] || authored[both] {
		t.Errorf("a pull request that is assigned and authored is only in the assigned search: %v / %v", assigned, authored)
	}
	if all := titles(t, "is:pr is:open author:@me archived:false"); !all[both] {
		t.Errorf("without the exclusion the author search finds it too: %v", all)
	}
	_, body := searchBody("is:pr is:open assignee:@me archived:false", githubConfig{}, nil, time.Now())
	if body["data"].(map[string]any)["viewer"].(map[string]string)["login"] != "octocat" {
		t.Error("the search answer names the viewer")
	}
	if assigned[authoredOnly] || !authored[authoredOnly] {
		t.Errorf("an authored-only pull request is only in the author search: %v / %v", assigned, authored)
	}
	if !assigned[assignedOnly] || authored[assignedOnly] {
		t.Errorf("an assigned-only pull request is only in the assignee search: %v / %v", assigned, authored)
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
