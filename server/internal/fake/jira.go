//go:build e2e

package fake

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
)

type fakeIssue struct{ key, summary, status, category, project, kind string }

var jiraIssues = []fakeIssue{
	{"INP-101", "Add login", "In Review", "indeterminate", "INP", "Story"},
	{"INP-102", "Single sign-on", "To Do", "new", "INP", "Story"},
	{"INP-103", "Account menu", "In Progress", "indeterminate", "INP", "Task"},
	{"INP-104", "Invoice export", "Done", "done", "INP", "Story"},
	{"INP-105", "Multi-repo feature", "In Review", "indeterminate", "INP", "Story"},
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

// issues is the fixture tickets with the status and category a ticket has after the moves made so far.
func (a *atlassian) issues() []fakeIssue {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]fakeIssue, len(jiraIssues))
	for i, issue := range jiraIssues {
		if status, moved := a.st.Statuses[issue.key]; moved {
			issue.status = status
			issue.category = transitionTargets[status]
		}
		out[i] = issue
	}
	return out
}

// jiraStatuses are the status names the fake site has: the ticket fixtures and the transition targets.
var jiraStatuses = []string{"To Do", "In Progress", "In Review", "Done", "Ready to Merge"}

// transitionTargets maps the status each fake transition leads to onto its category.
var transitionTargets = map[string]string{"Done": "done", "In Progress": "indeterminate", "Ready to Merge": "indeterminate"}

var transitionStatus = map[string]string{"31": "Done", "21": "In Progress", "41": "Ready to Merge"}

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
				for _, i := range a.issues() {
					if i.key == strings.TrimSpace(k) {
						issues = append(issues, i.json())
					}
				}
			}
		} else if strings.HasPrefix(body.JQL, "assignee = currentUser()") {
			calls.Assigned.Add(1)
			for _, i := range a.issues() {
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
	mux.HandleFunc("GET "+base+"/status", guard(func(w http.ResponseWriter, _ *http.Request) {
		calls.Statuses.Add(1)
		out := make([]map[string]string, len(jiraStatuses))
		for i, name := range jiraStatuses {
			out[i] = map[string]string{"name": name}
		}
		writeJSON(w, http.StatusOK, out)
	}))
	mux.HandleFunc("GET "+base+"/project/search", guard(func(w http.ResponseWriter, _ *http.Request) {
		calls.Projects.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"values": []map[string]string{{"key": "INP"}, {"key": "OPS"}}, "isLast": true})
	}))
	mux.HandleFunc("POST "+base+"/issue/{key}/transitions", guard(func(w http.ResponseWriter, r *http.Request) {
		calls.Moves.Add(1)
		var body struct {
			Transition struct {
				ID string `json:"id"`
			} `json:"transition"`
			Fields map[string]json.RawMessage `json:"fields"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"bad request"}})
			return
		}
		key := r.PathValue("key")
		fields, _ := json.Marshal(body.Fields)
		a.mu.Lock()
		mode := a.st.Config.TransitionMode
		a.st.Moves = append(a.st.Moves, key+":"+body.Transition.ID+":"+string(fields))
		a.mu.Unlock()
		status, known := transitionStatus[body.Transition.ID]
		exists := slices.ContainsFunc(jiraIssues, func(i fakeIssue) bool { return i.key == key })
		switch {
		case !exists:
			writeJSON(w, http.StatusNotFound, map[string]any{"errorMessages": []string{"Issue does not exist"}})
		case mode == "down":
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"errorMessages": []string{"Service unavailable"}})
		case mode == "reject":
			writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"Workflow validator failed"}})
		case !known:
			writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"Transition id '" + body.Transition.ID + "' is not valid for this issue."}})
		case body.Transition.ID == "31" && len(body.Fields["resolution"]) == 0:
			writeJSON(w, http.StatusBadRequest, map[string]any{"errors": map[string]string{"resolution": "Resolution is required."}})
		default:
			a.mu.Lock()
			if a.st.Statuses == nil {
				a.st.Statuses = map[string]string{}
			}
			a.st.Statuses[key] = status
			a.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	mux.HandleFunc("GET "+base+"/issue/{key}/transitions", guard(func(w http.ResponseWriter, r *http.Request) {
		calls.Transitions.Add(1)
		for _, i := range a.issues() {
			if i.key == r.PathValue("key") {
				writeJSON(w, http.StatusOK, map[string]any{"transitions": []map[string]any{
					{"id": "31", "name": "Done", "to": map[string]any{"name": "Done", "statusCategory": map[string]any{"key": "done"}},
						"fields": map[string]any{"resolution": map[string]any{
							"required": true, "name": "Resolution", "schema": map[string]any{"type": "resolution"},
							"allowedValues": []map[string]string{{"id": "10000", "name": "Done"}, {"id": "10001", "name": "Won't Do"}},
						}}},
					{"id": "21", "name": "In Progress", "to": map[string]any{"name": "In Progress", "statusCategory": map[string]any{"key": "indeterminate"}},
						"fields": map[string]any{}},
					{"id": "41", "name": "Ready to Merge", "to": map[string]any{"name": "Ready to Merge", "statusCategory": map[string]any{"key": "indeterminate"}},
						"fields": map[string]any{}},
				}})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"errorMessages": []string{"Issue does not exist"}})
	}))
}

type jiraCalls struct {
	Search, Assigned, Projects, Transitions, Statuses, Moves atomic.Int64
}

func (c *jiraCalls) snapshot() map[string]int64 {
	return map[string]int64{"search": c.Search.Load(), "assigned": c.Assigned.Load(), "projects": c.Projects.Load(), "transitions": c.Transitions.Load(), "statuses": c.Statuses.Load(), "moves": c.Moves.Load()}
}
