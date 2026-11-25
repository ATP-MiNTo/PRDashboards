package poller

import (
	"context"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/api/gen"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/events"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/merge"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/rules"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

const (
	// mergedWindow is how far back a merged pull request is considered for a ticket that has no
	// open pull request any more.
	mergedWindow = 30 * 24 * time.Hour
	// transitionParallelism is the number of Jira transitions in flight at once.
	transitionParallelism = 4
	// ActivityNeedsInput is the activity result of a rule that cannot move a ticket on its own.
	ActivityNeedsInput = "needs_input"
)

// ruleOutcome is what the rule evaluation of a refresh produced.
type ruleOutcome struct {
	write   store.SyncWrite
	waiting map[string]gen.TicketWaiting
	// plans maps "prID|ticket" to what merging that pull request does for the ticket.
	plans map[string]gen.TicketMergePlan
}

func toRules(in []store.SyncRule) []rules.Rule {
	out := make([]rules.Rule, len(in))
	for i, r := range in {
		out[i] = rules.Rule{
			ID: r.ID, Name: r.Name, Enabled: r.Enabled, Trigger: rules.Trigger(r.Trigger), Hours: r.Hours,
			Filters: rules.Filters(r.Filters), Target: r.Target, Mode: rules.Mode(r.Mode),
		}
	}
	return out
}

func hasEnabled(rs []store.SyncRule) bool {
	return slices.ContainsFunc(rs, func(r store.SyncRule) bool { return r.Enabled })
}

// mergedKeys are the tickets of merged pull requests, which Jira must be asked about as well.
func mergedKeys(st store.SyncState) []string {
	var out []string
	for _, m := range st.Merged {
		out = append(out, m.Tickets...)
	}
	return out
}

func resolvedMerged(rs []store.Resolution) []string {
	var out []string
	for _, r := range rs {
		if r.State == "merged" {
			out = append(out, r.ID)
		}
	}
	return out
}

// latestReview is the id of the most recent review, the larger id on a tie.
func latestReview(rs []github.Review) string {
	best := -1
	for i, r := range rs {
		if best < 0 || newer(r, rs[best]) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return rs[best].ID
}

func newer(a, b github.Review) bool {
	switch {
	case a.SubmittedAt == nil || b.SubmittedAt == nil:
		return a.SubmittedAt != nil || b.SubmittedAt == nil && a.ID > b.ID
	case !a.SubmittedAt.Equal(*b.SubmittedAt):
		return a.SubmittedAt.After(*b.SubmittedAt)
	}
	return a.ID > b.ID
}

// ticketsOf joins the open pull requests of the snapshot and the merged ones of the database into
// the engine's view, one entry per ticket. Callers hold p.mu.
func (p *Poller) ticketsOf(st store.SyncState, ls []linked) []rules.Ticket {
	merged := make(map[string]struct{}, len(st.Merged))
	for _, m := range st.Merged {
		merged[m.ID] = struct{}{}
	}
	jiraOK := p.jira.state == "ok"
	at := map[string]int{}
	var out []rules.Ticket
	add := func(key string, pr rules.PR) {
		i, ok := at[key]
		if !ok {
			i = len(out)
			at[key] = i
			t := rules.Ticket{Key: key, Project: key[:strings.LastIndexByte(key, '-')]}
			if issue, found := p.jira.issues[key]; found && jiraOK {
				t.Project, t.IssueType, t.Status, t.Available = issue.Project, issue.IssueType, issue.Status, true
			}
			out = append(out, t)
		}
		out[i].PRs = append(out[i].PRs, pr)
	}
	for _, l := range ls {
		if _, done := merged[l.pr.ID]; done || len(l.res.Links) == 0 {
			continue
		}
		pr := l.pr
		ready := st.ReadySHA[pr.ID]
		if ready == "" && !pr.Draft {
			ready = pr.HeadSHA
		}
		rp := rules.PR{
			ID: pr.ID, Number: pr.Number, Repo: pr.Repo.NameWithOwner, Base: pr.BaseRef, State: rules.StateOpen,
			Draft: pr.Draft, HeadSHA: pr.HeadSHA, ReadySHA: ready, ReviewDecision: pr.ReviewDecision,
			LatestReview: latestReview(pr.Reviews), ReviewRequests: pr.ReviewRequests, CommittedAt: pr.CommittedAt, CI: string(pr.CI.State),
		}
		for _, link := range l.res.Links {
			add(link.Key, rp)
		}
	}
	for _, m := range st.Merged {
		rp := rules.PR{ID: m.ID, Number: m.Number, Repo: m.Repo, Base: m.Base, State: rules.StateMerged}
		for _, key := range m.Tickets {
			add(key, rp)
		}
	}
	return out
}

func fired(st store.SyncState) map[rules.Key]string {
	out := make(map[rules.Key]string, len(st.Fired))
	for _, f := range st.Fired {
		out[rules.Key{Rule: f.Rule, Ticket: f.Ticket}] = f.Fingerprint
	}
	return out
}

// runRules evaluates the rules after the data of a refresh is in, moves the tickets that fire and
// returns what to store with the snapshot (TECH_SPEC 7.3, 7.4).
func (p *Poller) runRules(ctx context.Context, now time.Time, st store.SyncState, ls []linked, site jira.Site, exclude []string) ruleOutcome {
	var out ruleOutcome
	if !hasEnabled(st.Rules) {
		return out
	}
	rs := toRules(st.Rules)
	p.mu.Lock()
	tickets := p.ticketsOf(st, ls)
	p.mu.Unlock()
	res := rules.Evaluate(rules.Input{Now: now, Rules: rs, Tickets: tickets, Fired: fired(st), Exclude: exclude})
	out.waiting = waitingOf(res.Waiting, rs)
	out.plans = plansOf(now, rs, tickets, exclude)
	if len(res.Firings) == 0 || site.ID == "" {
		return out
	}
	out.write = p.fire(ctx, site, res.Firings, rs)
	return out
}

func ruleLabel(r rules.Rule) string {
	if r.Name != "" {
		return r.Name
	}
	return strings.ReplaceAll(string(r.Trigger), "_", " ")
}

func waitingOf(ws []rules.Waiting, rs []rules.Rule) map[string]gen.TicketWaiting {
	if len(ws) == 0 {
		return nil
	}
	names := make(map[string]string, len(rs))
	for _, r := range rs {
		names[r.ID] = ruleLabel(r)
	}
	out := make(map[string]gen.TicketWaiting, len(ws))
	for _, w := range ws {
		if _, ok := out[w.Ticket]; ok {
			continue // the first rule in order is the one shown
		}
		out[w.Ticket] = gen.TicketWaiting{Rule: names[w.Rule], Total: w.Total, WaitingOn: prRefs(w.WaitingOn)}
	}
	return out
}

// plansOf works out, for every open pull request of every ticket, what the merged rules do when it
// is merged. Tickets without a merged rule have no plan.
func plansOf(now time.Time, rs []rules.Rule, tickets []rules.Ticket, exclude []string) map[string]gen.TicketMergePlan {
	if !slices.ContainsFunc(rs, func(r rules.Rule) bool { return r.Enabled && r.Trigger == rules.Merged }) {
		return nil
	}
	names := make(map[string]string, len(rs))
	for _, r := range rs {
		names[r.ID] = ruleLabel(r)
	}
	out := map[string]gen.TicketMergePlan{}
	for _, t := range tickets {
		for _, p := range t.PRs {
			if p.State != rules.StateOpen {
				continue
			}
			if plan, ok := rules.AfterMerge(rs, t, p.ID, now, exclude); ok {
				out[p.ID+"|"+t.Key] = gen.TicketMergePlan{Rule: names[plan.Rule], Target: plan.Target, Total: plan.Total, WaitingOn: prRefs(plan.WaitingOn)}
			}
		}
	}
	return out
}

func prRefs(in []rules.Ref) []gen.PrRef {
	out := make([]gen.PrRef, len(in))
	for i, r := range in {
		out[i] = gen.PrRef{PrId: r.ID, Number: r.Number, Repo: r.Repo}
	}
	return out
}

// job is one firing that needs Jira.
type job struct {
	f    rules.Firing
	rule rules.Rule
}

// result of one firing. A skipped one is not recorded, so the next refresh tries again.
type result struct {
	skip  bool
	act   store.ActivityRow
	retry *store.RetryRow
	moved *jira.Transition
}

// fire records the firings of tickets that already are in their target status, and for the others
// fetches the transitions of those tickets only, then moves them, at most 4 in parallel.
func (p *Poller) fire(ctx context.Context, site jira.Site, firings []rules.Firing, rs []rules.Rule) store.SyncWrite {
	byID := make(map[string]rules.Rule, len(rs))
	for _, r := range rs {
		byID[r.ID] = r
	}
	var w store.SyncWrite
	var jobs []job
	for _, f := range firings {
		if f.InTarget {
			w.Firings = append(w.Firings, store.Firing{Rule: f.Rule, Ticket: f.Ticket, Fingerprint: f.Fingerprint})
			continue
		}
		jobs = append(jobs, job{f: f, rule: byID[f.Rule]})
	}
	done := make([]result, len(jobs))
	var g errgroup.Group
	g.SetLimit(transitionParallelism)
	for i, j := range jobs {
		g.Go(func() error {
			done[i] = p.move(ctx, site, j)
			return nil
		})
	}
	_ = g.Wait()
	for i, j := range jobs {
		d := done[i]
		if d.skip {
			continue
		}
		w.Firings = append(w.Firings, store.Firing{Rule: j.f.Rule, Ticket: j.f.Ticket, Fingerprint: j.f.Fingerprint})
		w.Activity = append(w.Activity, d.act)
		if d.retry != nil {
			w.Retries = append(w.Retries, *d.retry)
		}
		if d.moved != nil {
			p.mu.Lock()
			if issue, ok := p.jira.issues[j.f.Ticket]; ok {
				issue.Status, issue.StatusCategory = d.moved.ToStatus, d.moved.ToCategory
				p.jira.issues[j.f.Ticket] = issue
			}
			p.mu.Unlock()
		}
	}
	return w
}

// move carries out one firing: it picks the transition whose target status is the rule's target.
func (p *Poller) move(ctx context.Context, site jira.Site, j job) result {
	label := ruleLabel(j.rule)
	act := store.ActivityRow{Kind: "transition", TicketKey: j.f.Ticket, TriggeredBy: "rule:" + j.f.Rule}
	list, err := p.cfg.Jira.Transitions(ctx, site.ID, j.f.Ticket)
	if err != nil {
		p.cfg.Log.WarnContext(ctx, "rule transitions lookup failed", "ticket", j.f.Ticket, "err", err)
		return result{skip: true}
	}
	i := slices.IndexFunc(list, func(t jira.Transition) bool { return strings.EqualFold(t.ToStatus, j.f.Target) })
	if i < 0 {
		act.Result = "skipped"
		act.Summary = j.f.Ticket + " was not moved to " + j.f.Target + " by rule " + label
		act.Error = "Jira offers no transition to " + j.f.Target + " from here"
		return result{act: act}
	}
	tr := list[i]
	if slices.ContainsFunc(tr.Fields, func(f jira.Field) bool { return f.Required }) {
		act.Result = ActivityNeedsInput
		act.Summary = j.f.Ticket + " needs input to move to " + tr.ToStatus + " (rule " + label + ")"
		act.Error = "The transition asks for required fields, so move the ticket from the merge panel or in Jira"
		return result{act: act}
	}
	if err := p.cfg.Jira.Transition(ctx, site.ID, j.f.Ticket, tr.ID, nil); err != nil {
		p.cfg.Log.WarnContext(ctx, "rule transition failed", "ticket", j.f.Ticket, "err", err)
		msg := merge.DescribeJira(err)
		act.Result, act.Error = "failed", msg
		act.Summary = j.f.Ticket + " could not be moved to " + tr.ToStatus + " (rule " + label + ")"
		return result{act: act, retry: &store.RetryRow{TicketKey: j.f.Ticket, TransitionID: tr.ID, ToStatus: tr.ToStatus, LastError: msg}}
	}
	act.Result = "ok"
	act.Summary = j.f.Ticket + " moved to " + tr.ToStatus + " (rule " + label + ")"
	return result{act: act, moved: &tr}
}

func (p *Poller) publishActivity(w store.SyncWrite) {
	if p.cfg.Publisher != nil && len(w.Activity) > 0 {
		_ = p.cfg.Publisher.Publish(events.Activity, map[string]string{"by": "rules"})
	}
}

// Reevaluate evaluates the rules again from the data already fetched, for example after the rules
// changed. It asks Jira only about tickets it has not seen.
func (p *Poller) Reevaluate(ctx context.Context) error { return p.Relink(ctx) }

// PreviewMove is a ticket a rule would move now.
type PreviewMove struct {
	Ticket   string
	Status   string
	InTarget bool
}

// RulePreview is what a draft rule would do against the cached data (TECH_SPEC 7.4).
type RulePreview struct {
	Moves   []PreviewMove
	Waiting []rules.Waiting
}

func (p *Poller) cachedTickets(ctx context.Context) ([]rules.Ticket, error) {
	st, err := p.cfg.Store.SyncState(ctx, p.cfg.Clock().Add(-mergedWindow), nil)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ticketsOf(st, p.ls), nil
}

// Preview evaluates one rule against the cached data as if it had never fired. It makes no call.
func (p *Poller) Preview(ctx context.Context, r rules.Rule) (RulePreview, error) {
	tickets, err := p.cachedTickets(ctx)
	if err != nil {
		return RulePreview{}, err
	}
	set, err := p.cfg.Store.GetSettings(ctx)
	if err != nil {
		return RulePreview{}, err
	}
	pv := rules.PreviewRule(r, p.cfg.Clock(), tickets, set.ExcludedStatuses)
	status := make(map[string]string, len(tickets))
	for _, t := range tickets {
		status[t.Key] = t.Status
	}
	out := RulePreview{Moves: make([]PreviewMove, len(pv.Moves)), Waiting: pv.Waiting}
	for i, m := range pv.Moves {
		out.Moves[i] = PreviewMove{Ticket: m.Ticket, Status: status[m.Ticket], InTarget: m.InTarget}
	}
	return out, nil
}

// Baseline is the set of fingerprints that hold now for the given rules, to record when the person
// chooses not to move existing tickets.
func (p *Poller) Baseline(ctx context.Context, rs []rules.Rule) ([]store.Firing, error) {
	tickets, err := p.cachedTickets(ctx)
	if err != nil {
		return nil, err
	}
	set, err := p.cfg.Store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	res := rules.Evaluate(rules.Input{Now: p.cfg.Clock(), Rules: rs, Tickets: tickets, Exclude: set.ExcludedStatuses})
	out := make([]store.Firing, len(res.Holds))
	for i, h := range res.Holds {
		out[i] = store.Firing{Rule: h.Rule, Ticket: h.Ticket, Fingerprint: h.Fingerprint}
	}
	return out, nil
}

// MergedFirings are the fingerprints of the enabled merged rules that hold for the tickets of a pull
// request that was just merged. The merge records them, so the sync engine does not move tickets the
// person chose to leave (TECH_SPEC 7.7 step 7).
func (p *Poller) MergedFirings(ctx context.Context, prID string, tickets []string) ([]store.Firing, error) {
	st, err := p.cfg.Store.SyncState(ctx, p.cfg.Clock().Add(-mergedWindow), []string{prID})
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	view := p.ticketsOf(st, p.ls)
	p.mu.Unlock()
	set, err := p.cfg.Store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	rs := toRules(st.Rules)
	res := rules.Evaluate(rules.Input{Now: p.cfg.Clock(), Rules: rs, Tickets: view, Exclude: set.ExcludedStatuses})
	var out []store.Firing
	for _, h := range res.Holds {
		if h.Trigger == rules.Merged && slices.Contains(tickets, h.Ticket) {
			out = append(out, store.Firing{Rule: h.Rule, Ticket: h.Ticket, Fingerprint: h.Fingerprint})
		}
	}
	return out, nil
}
