package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/rules"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

func ruleJSON(id, trigger string, hours int, extra string) string {
	return `{"id":"` + id + `","name":"` + id + `","enabled":true,"trigger":"` + trigger + `","hours":` + itoa(hours) +
		`,"filters":{"repo":"","branch":"` + extra + `","project":"","issueType":"","status":""},"target":"Done","mode":"all"}`
}

func putRules(baseline bool, rs ...string) string {
	b := "false"
	if baseline {
		b = "true"
	}
	return `{"rules":[` + strings.Join(rs, ",") + `],"baseline":` + b + `}`
}

func TestRulesGetPutAndReevaluate(t *testing.T) {
	g := newMergeRig(t)
	var list gen.Rules
	w := g.do(http.MethodGet, "/api/rules", "", cookie(g.l.cookie))
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Rules) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	put := func(body string) *http.Response {
		return g.do(http.MethodPut, "/api/rules", body, g.l.write()...).Result()
	}
	if r := put(putRules(false, ruleJSON("a", "merged", 0, "main"), ruleJSON("b", "commit_stale", 48, ""))); r.StatusCode != 200 {
		t.Fatalf("%d", r.StatusCode)
	}
	if g.p.reevaluations != 1 || len(g.p.baselined) != 0 {
		t.Fatalf("reevaluations %d baselined %d", g.p.reevaluations, len(g.p.baselined))
	}
	w = g.do(http.MethodGet, "/api/rules", "", cookie(g.l.cookie))
	if json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Rules) != 2 || list.Rules[0].Id != "a" || list.Rules[1].Hours != 48 || list.Rules[0].Filters.Branch != "main" {
		t.Fatalf("%s", w.Body)
	}

	// A non commit_stale rule keeps no hours, and a rename or a reorder is not a change.
	if r := put(putRules(true, ruleJSON("b", "commit_stale", 48, ""), ruleJSON("a", "merged", 5, "main"))); r.StatusCode != 200 {
		t.Fatalf("%d", r.StatusCode)
	}
	if len(g.p.baselined) != 0 {
		t.Fatalf("an unchanged rule needs no baseline: %+v", g.p.baselined)
	}
	stored, _ := g.store.Rules(t.Context())
	if stored[0].ID != "b" || stored[1].Hours != 0 {
		t.Fatalf("%+v", stored)
	}

	g.p.baseline = []store.Firing{{Rule: "c", Ticket: "INP-1", Fingerprint: "x"}}
	if r := put(putRules(true, ruleJSON("b", "commit_stale", 48, ""), ruleJSON("a", "merged", 0, "main"), ruleJSON("c", "ci_failing", 0, ""))); r.StatusCode != 200 {
		t.Fatalf("%d", r.StatusCode)
	}
	if len(g.p.baselined) != 1 || g.p.baselined[0].ID != "c" || !g.p.baselined[0].Enabled {
		t.Fatalf("only the new rule is baselined: %+v", g.p.baselined)
	}
	st, _ := g.store.SyncState(t.Context(), g.clock.Now(), nil)
	if len(st.Fired) != 1 || st.Fired[0].Rule != "c" {
		t.Fatalf("baseline not stored: %+v", st.Fired)
	}

	g.p.relinkErr = errors.New("boom")
	wantProblem(t, g.do(http.MethodPut, "/api/rules", putRules(false), g.l.write()...), 500, "internal_error")
}

func TestRulesAcceptTheDefaultMode(t *testing.T) {
	g := newMergeRig(t)
	follow := strings.Replace(ruleJSON("a", "merged", 0, ""), `"mode":"all"`, `"mode":"default"`, 1)
	w := g.do(http.MethodPut, "/api/rules", putRules(false, follow, ruleJSON("b", "merged", 0, "")), g.l.write()...)
	var list gen.Rules
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.Rules[0].Mode != gen.RuleModeDefault || list.Rules[1].Mode != gen.RuleModeAll {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	stored, _ := g.store.Rules(t.Context())
	if stored[0].Mode != "default" || stored[1].Mode != "all" {
		t.Fatalf("%+v", stored)
	}
	w = g.do(http.MethodPost, "/api/rules/preview", `{"rule":`+follow+`}`, g.l.write()...)
	if w.Code != 200 || len(g.p.previewed) != 1 || g.p.previewed[0].Mode != rules.Default {
		t.Fatalf("%d %s %+v", w.Code, w.Body, g.p.previewed)
	}
}

func TestRulesPutValidation(t *testing.T) {
	g := newMergeRig(t)
	put := func(body string) *http.Response {
		return g.do(http.MethodPut, "/api/rules", body, g.l.write()...).Result()
	}
	many := make([]string, 101)
	for i := range many {
		many[i] = ruleJSON("r"+itoa(i), "merged", 0, "")
	}
	badSchema := map[string]string{
		"101 rules":    putRules(false, many...),
		"unknown":      strings.Replace(putRules(false, ruleJSON("a", "merged", 0, "")), `"mode":"all"`, `"mode":"all","x":1`, 1),
		"bad trigger":  putRules(false, ruleJSON("a", "pushed", 0, "")),
		"bad mode":     strings.Replace(putRules(false, ruleJSON("a", "merged", 0, "")), `"mode":"all"`, `"mode":"some"`, 1),
		"empty mode":   strings.Replace(putRules(false, ruleJSON("a", "merged", 0, "")), `"mode":"all"`, `"mode":""`, 1),
		"bad glob":     putRules(false, ruleJSON("a", "merged", 0, "a b")),
		"bad id":       putRules(false, ruleJSON("a b", "merged", 0, "")),
		"721 hours":    putRules(false, ruleJSON("a", "commit_stale", 721, "")),
		"no baseline":  `{"rules":[]}`,
		"empty target": strings.Replace(putRules(false, ruleJSON("a", "merged", 0, "")), `"target":"Done"`, `"target":""`, 1),
	}
	for name, body := range badSchema {
		if r := put(body); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", name, r.StatusCode)
		}
	}
	for name, body := range map[string]string{
		"stale without hours": putRules(false, ruleJSON("a", "commit_stale", 0, "")),
		"duplicate id":        putRules(false, ruleJSON("a", "merged", 0, ""), ruleJSON("a", "merged", 0, "")),
		"blank target":        strings.Replace(putRules(false, ruleJSON("a", "merged", 0, "")), `"target":"Done"`, `"target":"  "`, 1),
	} {
		wantProblem(t, g.do(http.MethodPut, "/api/rules", body, g.l.write()...), 422, "invalid_rule")
		_ = name
	}
	exactly100 := make([]string, 100)
	for i := range exactly100 {
		exactly100[i] = ruleJSON("r"+itoa(i), "merged", 0, "")
	}
	if r := put(putRules(false, exactly100...)); r.StatusCode != 200 {
		t.Fatalf("100 rules: %d", r.StatusCode)
	}
}

func TestRulesPreview(t *testing.T) {
	g := newMergeRig(t)
	g.p.preview = poller.RulePreview{
		Moves:   []poller.PreviewMove{{Ticket: "INP-1", Status: "In Review"}, {Ticket: "INP-2", Status: "Done", InTarget: true}},
		Waiting: []rules.Waiting{{Ticket: "INP-3", Total: 3, WaitingOn: []rules.Ref{{ID: "PR_1", Number: 34, Repo: "acme/fe"}}}},
	}
	w := g.do(http.MethodPost, "/api/rules/preview", `{"rule":`+ruleJSON("a", "merged", 0, "")+`}`, g.l.write()...)
	var out gen.RulePreview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Moves) != 2 || !out.Moves[1].AlreadyInTarget ||
		out.Waiting[0].WaitingOn[0].Repo != "acme/fe" || out.Waiting[0].Total != 3 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(g.p.previewed) != 1 || !g.p.previewed[0].Enabled || g.p.previewed[0].Trigger != rules.Merged {
		t.Fatalf("%+v", g.p.previewed)
	}
	wantProblem(t, g.do(http.MethodPost, "/api/rules/preview", `{"rule":`+ruleJSON("a", "commit_stale", 0, "")+`}`, g.l.write()...), 422, "invalid_rule")
	if w := g.do(http.MethodPost, "/api/rules/preview", `{"rule":{}}`, g.l.write()...); w.Code != http.StatusBadRequest {
		t.Fatalf("%d", w.Code)
	}
}
