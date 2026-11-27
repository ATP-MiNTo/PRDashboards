package api_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/events"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/poller"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/rules"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

type stubPoller struct {
	mu          sync.Mutex
	pub         *poller.Published
	triggers    int
	relinks     int
	reschedules int
	relinkErr   error

	reevaluations int
	previewed     []rules.Rule
	batches       int
	baselined     []rules.Rule
	preview       poller.RulePreview
	baseline      []store.Firing
	projects      []string
}

func newStubPoller(prs ...gen.SnapshotPr) *stubPoller {
	snap := gen.Snapshot{Version: 7, Prs: prs}
	raw, _ := json.Marshal(snap)
	return &stubPoller{pub: &poller.Published{Snapshot: snap, JSON: raw}}
}

func (s *stubPoller) Current() *poller.Published { return s.pub }
func (s *stubPoller) PR(id string) (gen.SnapshotPr, bool) {
	for _, p := range s.pub.Snapshot.Prs {
		if p.Id == id {
			return p, true
		}
	}
	return gen.SnapshotPr{}, false
}
func (s *stubPoller) Trigger() gen.SyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggers++
	return gen.SyncStatus{Running: true}
}
func (s *stubPoller) Relink(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relinks++
	return s.relinkErr
}
func (s *stubPoller) Reevaluate(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reevaluations++
	return s.relinkErr
}

func (s *stubPoller) Preview(_ context.Context, r rules.Rule) (poller.RulePreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.previewed = append(s.previewed, r)
	return s.preview, nil
}

func (s *stubPoller) PreviewAll(_ context.Context, rs []rules.Rule) ([]poller.RulePreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.previewed = append(s.previewed, rs...)
	s.batches++
	out := make([]poller.RulePreview, len(rs))
	for i := range rs {
		out[i] = s.preview
	}
	return out, nil
}

func (s *stubPoller) Baseline(_ context.Context, rs []rules.Rule) ([]store.Firing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baselined = append(s.baselined, rs...)
	return s.baseline, nil
}

func (s *stubPoller) Reschedule() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reschedules++
}
func (s *stubPoller) ProjectKeys() []string { return s.projects }
func (s *stubPoller) counts() (triggers, relinks, reschedules int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.triggers, s.relinks, s.reschedules
}

type stubJira struct {
	issues      []jira.Issue
	transitions []jira.Transition
	err         error
	statuses    []string
	statusErr   error
	gotTTL      time.Duration
	gotSite     string
}

func (j *stubJira) Assigned(_ context.Context, site string, ttl time.Duration) ([]jira.Issue, error) {
	j.gotSite, j.gotTTL = site, ttl
	return j.issues, j.err
}

func (j *stubJira) Statuses(_ context.Context, site string) ([]string, error) {
	j.gotSite = site
	return j.statuses, j.statusErr
}

func (j *stubJira) Transitions(_ context.Context, site, _ string) ([]jira.Transition, error) {
	j.gotSite = site
	return j.transitions, j.err
}

type stubAccounts struct {
	site jira.Site
	err  error
}

func (a stubAccounts) GitHubToken(context.Context) (string, error) { return "tok", nil }
func (a stubAccounts) JiraSite(context.Context) (jira.Site, error) { return a.site, a.err }

type snapRig struct {
	*rig
	p *stubPoller
	j *stubJira
	a *stubAccounts
	l login
}

func newSnapRig(t *testing.T, prs ...gen.SnapshotPr) *snapRig {
	t.Helper()
	sp, sj, sa := newStubPoller(prs...), &stubJira{}, &stubAccounts{site: jira.Site{ID: "cloud-1", URL: "https://acme.atlassian.net"}}
	g := newRig(t, webFS(), func(c *api.Config) {
		c.Poller, c.Jira, c.Events = sp, sj, events.NewHub(events.Options{Heartbeat: 10 * time.Millisecond})
		c.Accounts = sa
	})
	return &snapRig{rig: g, p: sp, j: sj, a: sa, l: g.login()}
}

func prRow(id string, tickets ...gen.SnapshotTicket) gen.SnapshotPr {
	return gen.SnapshotPr{Id: id, Number: 1, Tickets: tickets}
}

func TestSnapshotServesTheEncodedBytesWithAnETag(t *testing.T) {
	g := newSnapRig(t, prRow("PR_1"))
	w := g.do(http.MethodGet, "/api/snapshot", "", cookie(g.l.cookie))
	if w.Code != http.StatusOK || w.Header().Get("ETag") != `"7"` || w.Header().Get("Content-Type") != "application/json" || w.Body.String() != string(g.p.pub.JSON) {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body)
	}
	for _, inm := range []string{`"7"`, `W/"7"`, `"1", "7"`, `*`} {
		w := g.do(http.MethodGet, "/api/snapshot", "", cookie(g.l.cookie), header("If-None-Match", inm))
		if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
			t.Errorf("If-None-Match %q: %d", inm, w.Code)
		}
	}
	if w := g.do(http.MethodGet, "/api/snapshot", "", cookie(g.l.cookie), header("If-None-Match", `"6"`)); w.Code != http.StatusOK {
		t.Fatalf("a stale ETag gets the snapshot: %d", w.Code)
	}
	wantProblem(t, g.do(http.MethodGet, "/api/snapshot", ""), http.StatusUnauthorized, "unauthenticated")
}

func TestSnapshotServesTheStoredGzipWhenAccepted(t *testing.T) {
	g := newSnapRig(t, prRow("PR_1"))
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(g.p.pub.JSON)
	_ = zw.Close()
	g.p.pub.Gzip = buf.Bytes()
	get := func(opts ...reqOpt) *httptest.ResponseRecorder {
		return g.do(http.MethodGet, "/api/snapshot", "", append([]reqOpt{cookie(g.l.cookie)}, opts...)...)
	}
	for _, ae := range []string{"gzip", "br, gzip;q=0.5", "GZIP", "*", "deflate, gzip, br"} {
		w := get(header("Accept-Encoding", ae))
		if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("ETag") != `"7"` ||
			w.Header().Get("Vary") != "Accept-Encoding" || !bytes.Equal(w.Body.Bytes(), buf.Bytes()) {
			t.Fatalf("%q: %d %v", ae, w.Code, w.Header())
		}
		zr, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		if raw, _ := io.ReadAll(zr); !bytes.Equal(raw, g.p.pub.JSON) {
			t.Fatalf("%q: the gzip body is not the snapshot", ae)
		}
	}
	for _, ae := range []string{"", "identity", "gzip;q=0", "br", "*;q=0"} {
		var opts []reqOpt
		if ae != "" {
			opts = append(opts, header("Accept-Encoding", ae))
		}
		w := get(opts...)
		if w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "Accept-Encoding" || w.Body.String() != string(g.p.pub.JSON) {
			t.Fatalf("%q: %d %v", ae, w.Code, w.Header())
		}
	}
	w := get(header("Accept-Encoding", "gzip"), header("If-None-Match", `"7"`))
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 || w.Header().Get("ETag") != `"7"` || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("304 with gzip: %d %v", w.Code, w.Header())
	}
	g.p.pub.Gzip = nil
	if w := get(header("Accept-Encoding", "gzip")); w.Header().Get("Content-Encoding") != "" || w.Body.String() != string(g.p.pub.JSON) {
		t.Fatal("without stored gzip the raw JSON is served")
	}
}

func TestSyncNowAcceptsAndJoins(t *testing.T) {
	g := newSnapRig(t)
	w := g.do(http.MethodPost, "/api/sync", "", g.l.write()...)
	var st gen.SyncStatus
	if w.Code != http.StatusAccepted || json.Unmarshal(w.Body.Bytes(), &st) != nil || !st.Running {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if n, _, _ := g.p.counts(); n != 1 {
		t.Fatalf("triggers %d", n)
	}
	wantProblem(t, g.do(http.MethodPost, "/api/sync", "", cookie(g.l.cookie)), http.StatusForbidden, "bad_csrf")
}

func TestPutPrTicketsStoresTheDecisionAndRelinks(t *testing.T) {
	branch := gen.SnapshotTicket{Key: "INP-1", Source: gen.Branch}
	title := gen.SnapshotTicket{Key: "INP-2", Source: gen.Title}
	g := newSnapRig(t, prRow("PR_1", branch, title))
	if err := g.store.SaveRefresh(context.Background(), store.Refresh{
		Now: g.clock.Now(), DefaultMethod: "merge", Version: 1, Snapshot: []byte("x"),
		Repos: []store.RepoRow{{ID: "R1", NameWithOwner: "acme/web"}},
		PRs:   []store.PRRow{{ID: "PR_1", RepoID: "R1", Number: 1, Title: "t", HeadBranch: "h", BaseBranch: "main", HeadSHA: "s"}},
	}); err != nil {
		t.Fatal(err)
	}
	put := func(id, body string) *httptest.ResponseRecorder {
		return g.do(http.MethodPut, "/api/prs/"+id+"/tickets", body, g.l.write()...)
	}
	if w := put("PR_1", `{"manual":["OPS-9"],"removed":["INP-2"]}`); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, relinks, _ := g.p.counts(); relinks != 1 {
		t.Fatalf("relinks %d", relinks)
	}
	links, err := g.store.UserLinks(context.Background())
	if err != nil || len(links["PR_1"].Manual) != 1 || links["PR_1"].Manual[0] != "OPS-9" || len(links["PR_1"].Removed) != 1 || links["PR_1"].Removed[0] != "INP-2" {
		t.Fatalf("%+v %v", links, err)
	}
	if w := put("PR_1", `{"manual":[],"removed":[]}`); w.Code != http.StatusNoContent {
		t.Fatalf("clearing: %d", w.Code)
	}
	if links, _ := g.store.UserLinks(context.Background()); len(links) != 0 {
		t.Fatalf("cleared: %+v", links)
	}

	g.p.relinkErr = errors.New("boom")
	wantProblem(t, put("PR_1", `{"manual":[],"removed":[]}`), http.StatusInternalServerError, "internal_error")
	g.p.relinkErr = nil

	wantProblem(t, put("PR_404", `{"manual":[],"removed":[]}`), http.StatusNotFound, "not_found")
	rows := func(n int) string {
		keys := make([]string, n)
		for i := range keys {
			keys[i] = `"OPS-` + string(rune('1'+i%9)) + strings.Repeat("0", i/9) + `"`
		}
		return strings.Join(keys, ",")
	}
	for name, body := range map[string]string{
		"overlap":   `{"manual":["OPS-9"],"removed":["OPS-9"]}`,
		"duplicate": `{"manual":["OPS-9","OPS-9"],"removed":[]}`,
		"too many":  `{"manual":[` + rows(21) + `],"removed":[]}`,
	} {
		w := put("PR_1", body)
		if w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	for _, body := range []string{`{"manual":["lower-1"],"removed":[]}`, `{"manual":["OPS-1"]}`, `{"manual":[],"removed":[],"x":1}`, `{"manual":["OPS-1; DROP"],"removed":[]}`} {
		if w := put("PR_1", body); w.Code != http.StatusBadRequest && w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
}

func TestPutPrTicketsForAPRTheStoreDoesNotKnow(t *testing.T) {
	g := newSnapRig(t, prRow("PR_1"))
	wantProblem(t, g.do(http.MethodPut, "/api/prs/PR_1/tickets", `{"manual":[],"removed":[]}`, g.l.write()...), http.StatusNotFound, "not_found")
}

func TestAssignedTicketsUseTheSelectedSiteAndIntervalCache(t *testing.T) {
	g := newSnapRig(t)
	g.j.issues = []jira.Issue{{Key: "INP-1", Summary: "Add login", Status: "In Review", StatusCategory: "indeterminate", Project: "INP", IssueType: "Story"}}
	w := g.do(http.MethodGet, "/api/tickets/assigned", "", cookie(g.l.cookie))
	var out gen.AssignedTickets
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Tickets) != 1 || out.Tickets[0].Key != "INP-1" ||
		out.Tickets[0].StatusCategory != gen.Indeterminate || out.Tickets[0].IssueType != "Story" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if g.j.gotSite != "cloud-1" || g.j.gotTTL != 300*time.Second {
		t.Fatalf("site %q ttl %v", g.j.gotSite, g.j.gotTTL)
	}

	g.a.err = auth.ErrNotConnected
	wantProblem(t, g.do(http.MethodGet, "/api/tickets/assigned", "", cookie(g.l.cookie)), http.StatusConflict, "not_connected")
	g.a.err = poller.ErrNoSite
	wantProblem(t, g.do(http.MethodGet, "/api/tickets/assigned", "", cookie(g.l.cookie)), http.StatusConflict, "not_connected")
	g.a.err = errors.New("keychain locked")
	wantProblem(t, g.do(http.MethodGet, "/api/tickets/assigned", "", cookie(g.l.cookie)), http.StatusInternalServerError, "internal_error")
	g.a.err = nil
	g.j.err = &jira.StatusError{Status: 503}
	wantProblem(t, g.do(http.MethodGet, "/api/tickets/assigned", "", cookie(g.l.cookie)), http.StatusBadGateway, "upstream_error")
	g.j.err = jira.ErrUnauthorized
	wantProblem(t, g.do(http.MethodGet, "/api/tickets/assigned", "", cookie(g.l.cookie)), http.StatusConflict, "not_connected")
}

func TestTransitionsAreMappedAndErrorsBecomeProblems(t *testing.T) {
	g := newSnapRig(t)
	g.j.transitions = []jira.Transition{{
		ID: "31", Name: "Done", ToStatus: "Done", ToCategory: "done",
		Fields: []jira.Field{{ID: "resolution", Name: "Resolution", Required: true, Type: "resolution", AllowedValues: []jira.Option{{ID: "1", Name: "Fixed"}}}},
	}, {ID: "21", Name: "Start", ToStatus: "In Progress", ToCategory: "indeterminate"}}
	w := g.do(http.MethodGet, "/api/tickets/INP-1/transitions", "", cookie(g.l.cookie))
	var out gen.Transitions
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Key != "INP-1" || len(out.Transitions) != 2 ||
		out.Transitions[0].Fields[0].AllowedValues[0].Name != "Fixed" || !out.Transitions[0].Fields[0].Required || len(out.Transitions[1].Fields) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	g.j.err = jira.ErrNotFound
	wantProblem(t, g.do(http.MethodGet, "/api/tickets/INP-1/transitions", "", cookie(g.l.cookie)), http.StatusNotFound, "not_found")
	g.j.err = errors.New("down")
	wantProblem(t, g.do(http.MethodGet, "/api/tickets/INP-1/transitions", "", cookie(g.l.cookie)), http.StatusBadGateway, "upstream_error")
	g.a.err = auth.ErrNeedsReconnect
	wantProblem(t, g.do(http.MethodGet, "/api/tickets/INP-1/transitions", "", cookie(g.l.cookie)), http.StatusConflict, "not_connected")
	if w := g.do(http.MethodGet, "/api/tickets/not-a-key/transitions", "", cookie(g.l.cookie)); w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Fatalf("bad key: %d", w.Code)
	}
}

func TestConnectionAndSettingChangesWakeThePoller(t *testing.T) {
	g := newSnapRig(t)
	put := func(site string) {
		body := `{"polling":{"githubSec":120,"jiraSec":900},"theme":"auto","defaultMethodForNewRepos":"squash","deleteBranchAfterMerge":true,"jiraSiteId":` + site + `,"excludedStatuses":["Done","QA"]}`
		if w := g.do(http.MethodPut, "/api/settings", body, g.l.write()...); w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	put("null")
	if tr, _, rs := g.p.counts(); tr != 0 || rs != 1 {
		t.Fatalf("a settings change reschedules only: triggers %d reschedules %d", tr, rs)
	}
	if g.p.reevaluations != 1 {
		t.Fatalf("changing the excluded statuses evaluates the rules again: %d", g.p.reevaluations)
	}
	put("null")
	if g.p.reevaluations != 1 {
		t.Fatalf("saving the same excluded statuses does not: %d", g.p.reevaluations)
	}
	if w := g.do(http.MethodPut, "/api/github/pat", `{"token":"ghp_pat"}`, g.l.write()...); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if tr, _, _ := g.p.counts(); tr != 1 {
		t.Fatalf("connecting refreshes: %d", tr)
	}
	if w := g.do(http.MethodDelete, "/api/accounts/github", "", g.l.write()...); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if tr, _, _ := g.p.counts(); tr != 2 {
		t.Fatalf("disconnecting refreshes: %d", tr)
	}
}

func TestEventsNeedASession(t *testing.T) {
	g := newSnapRig(t)
	wantProblem(t, g.do(http.MethodGet, "/events", ""), http.StatusUnauthorized, "unauthenticated")
	wantProblem(t, g.do(http.MethodGet, "/events", "", cookie("forged")), http.StatusUnauthorized, "unauthenticated")
	wantProblem(t, g.do(http.MethodGet, "/events", "", withHost("evil.example:4317")), http.StatusMisdirectedRequest, "misdirected_host")
	wantProblem(t, g.do(http.MethodGet, "/events", "", cookie(g.l.cookie), header("Sec-Fetch-Site", "cross-site")), http.StatusForbidden, "cross_site")
}

func TestEventsStreamWithAHeartbeat(t *testing.T) {
	g := newSnapRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodGet, "http://"+host+"/events", nil).WithContext(ctx)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Cookie", "prd_session="+g.l.cookie)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { g.h.ServeHTTP(w, r); close(done) }()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done
	body := w.Body.String()
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/event-stream" || w.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(body, ": heartbeat") {
		t.Fatalf("%d %v %q", w.Code, w.Header(), body)
	}
}

func TestJiraOptionsReturnStatusesAndProjectsAndDegradeToProjects(t *testing.T) {
	g := newSnapRig(t)
	g.p.projects = []string{"INP", "OPS"}
	g.j.statuses = []string{"Done", "To Do"}
	w := g.do(http.MethodGet, "/api/jira/options", "", cookie(g.l.cookie))
	var out gen.JiraOptions
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil ||
		!slices.Equal(out.Statuses, []string{"Done", "To Do"}) || !slices.Equal(out.Projects, []string{"INP", "OPS"}) || g.j.gotSite != "cloud-1" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	g.j.statuses, g.p.projects = nil, nil
	if w := g.do(http.MethodGet, "/api/jira/options", "", cookie(g.l.cookie)); !strings.Contains(w.Body.String(), `"statuses":[]`) || !strings.Contains(w.Body.String(), `"projects":[]`) {
		t.Fatalf("empty lists are arrays: %s", w.Body)
	}

	g.p.projects = []string{"INP"}
	g.j.statusErr = &jira.StatusError{Status: 503}
	w = g.do(http.MethodGet, "/api/jira/options", "", cookie(g.l.cookie))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"statuses":[]`) || !strings.Contains(w.Body.String(), `"projects":["INP"]`) {
		t.Fatalf("a failing statuses call still gives the projects: %d %s", w.Code, w.Body)
	}
	g.j.statusErr = jira.ErrUnauthorized
	wantProblem(t, g.do(http.MethodGet, "/api/jira/options", "", cookie(g.l.cookie)), http.StatusConflict, "not_connected")
	g.j.statusErr = nil
	g.a.err = auth.ErrNotConnected
	wantProblem(t, g.do(http.MethodGet, "/api/jira/options", "", cookie(g.l.cookie)), http.StatusConflict, "not_connected")
	g.a.err = nil
	if triggers, _, _ := g.p.counts(); triggers != 0 {
		t.Fatal("reading the options must not refresh")
	}
}

func TestChangingTheDefaultMethodResolvesThePullRequestsAgain(t *testing.T) {
	g := newSnapRig(t)
	put := func(method string) {
		body := `{"polling":{"githubSec":120,"jiraSec":900},"theme":"auto","defaultMethodForNewRepos":"` + method + `","deleteBranchAfterMerge":true,"jiraSiteId":null,"excludedStatuses":["Done"]}`
		if w := g.do(http.MethodPut, "/api/settings", body, g.l.write()...); w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	put("squash")
	if _, relinks, _ := g.p.counts(); relinks != 0 {
		t.Fatalf("the same default changes nothing: %d", relinks)
	}
	put("rebase")
	if tr, relinks, _ := g.p.counts(); relinks != 1 || tr != 0 {
		t.Fatalf("repositories that follow the default take it from cached data: relinks %d triggers %d", relinks, tr)
	}
}
