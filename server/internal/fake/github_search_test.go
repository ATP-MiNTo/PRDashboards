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
