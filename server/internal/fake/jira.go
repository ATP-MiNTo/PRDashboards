//go:build e2e

package fake

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
)

type fakeIssue struct{ key, summary, status, category, project, kind string }

var jiraIssues = []fakeIssue{
	{"INP-101", "Add login", "In Review", "indeterminate", "INP", "Story"},
	{"INP-102", "Single sign-on", "To Do", "new", "INP", "Story"},
	{"INP-103", "Account menu", "In Progress", "indeterminate", "INP", "Task"},
	{"INP-104", "Invoice export", "Done", "done", "INP", "Story"},
	{"OPS-12", "Rotate signing keys", "In Progress", "indeterminate", "OPS", "Task"},
	{"OPS-20", "Review firewall rules", "To Do", "new", "OPS", "Task"},
}

func (i fakeIssue) json() map[string]any {
	return map[string]any{"id": i.key, "key": i.key, "fields": map[string]any{
		"summary": i.summary,
		"status":  map[string]any{"name": i.status, "statusCategory": map[string]any{"key": i.category}},
		"project": map[string]any{"key": i.project}, "issuetype": map[string]any{"name": i.kind},
	}}
}

var keyList = regexp.MustCompile(`^key in \((.*)\)$`)

// jiraRoutes serves the slice of the Jira REST API the app uses, behind the same gateway path as
// api.atlassian.com/ex/jira/{cloudId}. The counters let a test see how many calls were made.
func (a *atlassian) jiraRoutes(mux *http.ServeMux, calls *jiraCalls) {
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !a.access.has(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Unauthorized"})
				return
			}
			if r.PathValue("cloud") != "cloud-1" && r.PathValue("cloud") != "cloud-2" {
				http.NotFound(w, r)
				return
			}
			h(w, r)
		}
	}
	const base = "/ex/jira/{cloud}/rest/api/3"
	mux.HandleFunc("POST "+base+"/search/jql", guard(func(w http.ResponseWriter, r *http.Request) {
		calls.Search.Add(1)
		var body struct {
			JQL string `json:"jql"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"bad request"}})
			return
		}
		issues := []any{}
		if m := keyList.FindStringSubmatch(body.JQL); m != nil {
			for _, k := range strings.Split(m[1], ",") {
				for _, i := range jiraIssues {
					if i.key == strings.TrimSpace(k) {
						issues = append(issues, i.json())
					}
				}
			}
		} else if strings.HasPrefix(body.JQL, "assignee = currentUser()") {
			calls.Assigned.Add(1)
			for _, i := range jiraIssues {
				if i.category != "done" {
					issues = append(issues, i.json())
				}
			}
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"unsupported query"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"issues": issues, "isLast": true})
	}))
	mux.HandleFunc("GET "+base+"/project/search", guard(func(w http.ResponseWriter, _ *http.Request) {
		calls.Projects.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"values": []map[string]string{{"key": "INP"}, {"key": "OPS"}}, "isLast": true})
	}))
	mux.HandleFunc("GET "+base+"/issue/{key}/transitions", guard(func(w http.ResponseWriter, r *http.Request) {
		calls.Transitions.Add(1)
		for _, i := range jiraIssues {
			if i.key == r.PathValue("key") {
				writeJSON(w, http.StatusOK, map[string]any{"transitions": []map[string]any{
					{"id": "31", "name": "Done", "to": map[string]any{"name": "Done", "statusCategory": map[string]any{"key": "done"}},
						"fields": map[string]any{"resolution": map[string]any{
							"required": true, "name": "Resolution", "schema": map[string]any{"type": "resolution"},
							"allowedValues": []map[string]string{{"id": "10000", "name": "Done"}, {"id": "10001", "name": "Won't Do"}},
						}}},
					{"id": "21", "name": "In Progress", "to": map[string]any{"name": "In Progress", "statusCategory": map[string]any{"key": "indeterminate"}},
						"fields": map[string]any{}},
				}})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"errorMessages": []string{"Issue does not exist"}})
	}))
}

type jiraCalls struct {
	Search, Assigned, Projects, Transitions atomic.Int64
}

func (c *jiraCalls) snapshot() map[string]int64 {
	return map[string]int64{"search": c.Search.Load(), "assigned": c.Assigned.Load(), "projects": c.Projects.Load(), "transitions": c.Transitions.Load()}
}
