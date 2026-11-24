package merge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/auth"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/events"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/github"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/jira"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

const (
	// TransitionParallelism is how many Jira transitions run at once (TECH_SPEC 7.7).
	TransitionParallelism = 4
	// MaxTickets is the most tickets one merge can move.
	MaxTickets = 20

	afterMergeTimeout = 2 * time.Minute
)

// Config holds the collaborators of a Service.
type Config struct {
	GitHub   GitHub
	Jira     Jira
	Store    Store
	Snapshot Snapshot
	Accounts Accounts
	// Refresh starts a refresh without waiting for it.
	Refresh   func()
	Publisher Publisher
	Log       *slog.Logger
	Clock     func() time.Time
}

type flight struct {
	done chan struct{}
	res  Result
	err  error
}

// Service carries out merges and retries of failed transitions (TECH_SPEC 7.7).
type Service struct {
	cfg Config

	mu      sync.Mutex
	flights map[string]*flight
}

// New creates a service.
func New(cfg Config) *Service {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Refresh == nil {
		cfg.Refresh = func() {}
	}
	return &Service{cfg: cfg, flights: map[string]*flight{}}
}

// Merge runs a confirmed merge. A repeated Idempotency-Key returns the stored result of the first
// call, and a call that arrives while the first one runs waits for it. A refusal is an *Error and
// nothing was merged. Once GitHub has merged, no failure undoes it: failed steps are in the result.
func (s *Service) Merge(ctx context.Context, req Request) (Result, error) {
	s.mu.Lock()
	if f, ok := s.flights[req.IdempotencyKey]; ok {
		s.mu.Unlock()
		select {
		case <-f.done:
			return f.res, f.err
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	f := &flight{done: make(chan struct{})}
	s.flights[req.IdempotencyKey] = f
	s.mu.Unlock()

	f.res, f.err = s.once(ctx, req)

	s.mu.Lock()
	delete(s.flights, req.IdempotencyKey)
	s.mu.Unlock()
	close(f.done)
	return f.res, f.err
}

func (s *Service) once(ctx context.Context, req Request) (Result, error) {
	prID, raw, ok, err := s.cfg.Store.Idempotent(ctx, req.IdempotencyKey, s.cfg.Clock())
	if err != nil {
		return Result{}, err
	}
	if ok {
		if prID != req.PRID {
			return Result{}, &Error{Code: CodeKeyReused, Detail: "the idempotency key was used for another pull request"}
		}
		var res Result
		return res, json.Unmarshal(raw, &res)
	}
	res, err := s.run(ctx, req)
	var refused *Error
	if errors.As(err, &refused) && refused.Fresh != nil {
		s.cfg.Refresh()
	}
	return res, err
}

func notConnected(err error) bool {
	return errors.Is(err, auth.ErrNotConnected) || errors.Is(err, auth.ErrNeedsReconnect) || errors.Is(err, jira.ErrNoSite) ||
		errors.Is(err, github.ErrBadCredentials) || errors.Is(err, jira.ErrUnauthorized)
}

func (s *Service) run(ctx context.Context, req Request) (Result, error) {
	view, ok := s.cfg.Snapshot.Linked(req.PRID)
	if !ok {
		return Result{}, &Error{Code: CodeNotFound, Detail: "unknown pull request"}
	}
	for _, t := range req.Tickets {
		if !contains(view.Tickets, t.Key) {
			return Result{}, &Error{Code: CodeTicketNotLinked, Detail: t.Key + " is not linked to this pull request"}
		}
	}
	token, err := s.cfg.Accounts.GitHubToken()
	if err != nil {
		if notConnected(err) {
			return Result{}, &Error{Code: CodeNotConnected, Detail: "GitHub is not connected"}
		}
		return Result{}, err
	}
	pr, err := s.cfg.GitHub.PullRequest(ctx, token, req.PRID)
	switch {
	case errors.Is(err, github.ErrPRNotFound):
		return Result{}, &Error{Code: CodeNotFound, Detail: "GitHub has no such pull request"}
	case notConnected(err):
		return Result{}, &Error{Code: CodeNotConnected, Detail: "GitHub is not connected"}
	case err != nil:
		return Result{}, err
	}
	if e := check(pr, req, s.cfg.Snapshot.Refs(pr.Repo.ID)); e != nil {
		return Result{}, e
	}
	if !(Allowed{Squash: pr.Repo.SquashAllowed, Merge: pr.Repo.MergeAllowed, Rebase: pr.Repo.RebaseAllowed}).Has(req.Method) {
		return Result{}, &Error{Code: CodeMethodNotAllowed, Detail: "the repository does not allow " + req.Method.Label()}
	}
	moves, err := s.prepare(ctx, req.Tickets)
	if err != nil {
		return Result{}, err
	}

	merged, err := s.cfg.GitHub.Merge(ctx, token, pr.Repo.NameWithOwner, pr.Number, string(req.Method), req.ExpectedHeadSHA)
	switch {
	case errors.Is(err, github.ErrHeadMoved):
		return Result{}, &Error{Code: CodeHeadMoved, Detail: "the branch changed while the merge was prepared", Fresh: freshOf(pr)}
	case errors.Is(err, github.ErrNotMergeable):
		return Result{}, &Error{Code: CodeNotMergeable, Detail: "GitHub refused the merge", Fresh: freshOf(pr)}
	case errors.Is(err, github.ErrPRNotFound):
		return Result{}, &Error{Code: CodeNotFound, Detail: "GitHub has no such pull request"}
	case notConnected(err):
		return Result{}, &Error{Code: CodeNotConnected, Detail: "GitHub is not connected"}
	case err != nil:
		return Result{}, err
	}

	// The merge happened. From here on nothing may cancel the rest, and nothing undoes it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), afterMergeTimeout)
	defer cancel()
	name := pr.Repo.NameWithOwner + "#" + strconv.Itoa(pr.Number)
	res := Result{PRID: req.PRID, Method: req.Method, SHA: merged.SHA, Steps: []Step{{Kind: StepMerge, State: StateOK}}}
	acts := []store.ActivityRow{{
		Kind: StepMerge, PRID: req.PRID, TriggeredBy: "manual", Result: StateOK,
		Summary: "Merged " + name + " with " + req.Method.Label(),
	}}
	if req.DeleteBranch {
		step, act := s.deleteBranch(ctx, token, pr, s.cfg.Snapshot.Refs(pr.Repo.ID))
		res.Steps = append(res.Steps, step)
		if act != nil {
			acts = append(acts, *act)
		}
	}
	steps, transitionActs, retries := s.transition(ctx, req.PRID, req.Tickets, moves)
	res.Steps = append(res.Steps, steps...)
	acts = append(acts, transitionActs...)

	keys := make([]string, len(req.Tickets))
	for i, t := range req.Tickets {
		keys[i] = t.Key
	}
	err = s.cfg.Store.RecordMerge(ctx, store.Merged{
		Now: s.cfg.Clock(), PRID: req.PRID, Key: req.IdempotencyKey, Activity: acts, Retries: retries,
		Tickets: keys, Fingerprint: req.PRID,
		Encode: func(ids map[string]int64) ([]byte, error) {
			for i := range res.Steps {
				if res.Steps[i].State == StateFailed && res.Steps[i].Kind == StepTransition {
					res.Steps[i].RetryID = ids[res.Steps[i].Ticket]
				}
			}
			return json.Marshal(res)
		},
	})
	if err != nil {
		s.cfg.Log.ErrorContext(ctx, "record merge", "pr", req.PRID, "err", err)
	}
	if s.cfg.Publisher != nil {
		_ = s.cfg.Publisher.Publish(events.Activity, map[string]string{"pr": req.PRID})
	}
	s.cfg.Refresh()
	return res, nil
}

func contains(list []string, key string) bool {
	for _, k := range list {
		if k == key {
			return true
		}
	}
	return false
}

func freshOf(pr github.PR) *Fresh {
	return &Fresh{
		State: pr.State, HeadSHA: pr.HeadSHA, BaseRef: pr.BaseRef, IsDraft: pr.Draft,
		Mergeable: pr.Mergeable, MergeState: pr.MergeStateStatus, ReviewDecision: pr.ReviewDecision,
	}
}

// check is step 2 of TECH_SPEC 7.7 against the pull request GitHub just reported: it is open, not
// a draft, its base is merged, the head is the commit the person saw, GitHub says it is mergeable
// and clean, and no required review is missing.
func check(pr github.PR, req Request, refs []Ref) *Error {
	refuse := func(code, detail string) *Error { return &Error{Code: code, Detail: detail, Fresh: freshOf(pr)} }
	switch {
	case pr.State != "" && pr.State != "OPEN":
		return refuse(CodeNotOpen, "the pull request is no longer open")
	case pr.Draft:
		return refuse(CodeDraft, "the pull request is a draft")
	}
	for _, r := range refs {
		if r.ID != pr.ID && r.Head == pr.BaseRef {
			return refuse(CodeStackBaseOpen, "merge after #"+strconv.Itoa(r.Number))
		}
	}
	switch {
	case pr.HeadSHA != req.ExpectedHeadSHA:
		return refuse(CodeHeadMoved, "the branch changed since the page loaded")
	case pr.Mergeable != "MERGEABLE":
		return refuse(CodeNotMergeable, "GitHub does not report the pull request as mergeable")
	case pr.MergeStateStatus != "CLEAN" && pr.MergeStateStatus != "HAS_HOOKS":
		return refuse(CodeNotMergeable, "the merge state is "+pr.MergeStateStatus)
	case pr.ReviewDecision == "REVIEW_REQUIRED" || pr.ReviewDecision == "CHANGES_REQUESTED":
		return refuse(CodeNotMergeable, "a required review is missing")
	}
	return nil
}

// move is a transition that passed validation.
type move struct {
	ticket string
	tr     jira.Transition
	fields map[string]any
	raw    map[string]string
	site   string
}

// prepare is step 3: every requested transition must be in the ticket's current transitions
// (cached for at most 60 s) and carry its required fields.
func (s *Service) prepare(ctx context.Context, tickets []Ticket) ([]move, error) {
	var asked []Ticket
	for _, t := range tickets {
		if t.TransitionID != "" {
			asked = append(asked, t)
		}
	}
	if len(asked) == 0 {
		return nil, nil
	}
	site, err := s.cfg.Accounts.JiraSite(ctx)
	if err != nil {
		if notConnected(err) {
			return nil, &Error{Code: CodeNotConnected, Detail: "Jira is not connected"}
		}
		return nil, err
	}
	type fetched struct {
		list []jira.Transition
		err  error
	}
	got := parallel(len(asked), func(i int) fetched {
		list, err := s.cfg.Jira.Transitions(ctx, site.ID, asked[i].Key)
		return fetched{list, err}
	})
	out := make([]move, 0, len(asked))
	for i, t := range asked {
		if err := got[i].err; err != nil {
			switch {
			case notConnected(err):
				return nil, &Error{Code: CodeNotConnected, Detail: "Jira is not connected"}
			case errors.Is(err, jira.ErrNotFound):
				return nil, &Error{Code: CodeTransitionUnavailable, Detail: t.Key + " was not found in Jira"}
			}
			return nil, err
		}
		tr, ok := findTransition(got[i].list, t.TransitionID)
		if !ok {
			return nil, &Error{Code: CodeTransitionUnavailable, Detail: t.Key + " cannot make that move now"}
		}
		fields, problem := encodeFields(tr, t.Fields)
		if problem != nil {
			return nil, &Error{Code: problem.code, Detail: t.Key + ": " + problem.detail}
		}
		out = append(out, move{ticket: t.Key, tr: tr, fields: fields, raw: t.Fields, site: site.ID})
	}
	return out, nil
}

func findTransition(list []jira.Transition, id string) (jira.Transition, bool) {
	for _, t := range list {
		if t.ID == id {
			return t, true
		}
	}
	return jira.Transition{}, false
}

// parallel runs fn for 0..n-1, at most TransitionParallelism at a time, and returns the results by
// index.
func parallel[T any](n int, fn func(i int) T) []T {
	out := make([]T, n)
	var g errgroup.Group
	g.SetLimit(TransitionParallelism)
	for i := range n {
		g.Go(func() error {
			out[i] = fn(i)
			return nil
		})
	}
	_ = g.Wait()
	return out
}

// deleteBranch is step 5: the head branch goes only when it lives in the base repository and no
// other open pull request uses it as its base. A failure is reported and not retried.
func (s *Service) deleteBranch(ctx context.Context, token string, pr github.PR, refs []Ref) (Step, *store.ActivityRow) {
	step := Step{Kind: StepDeleteBranch, State: StateSkipped}
	if pr.CrossRepo {
		step.Message = "Branch kept: it lives in a fork"
		return step, nil
	}
	for _, r := range refs {
		if r.ID != pr.ID && r.Base == pr.HeadRef {
			step.Message = "Branch kept: #" + strconv.Itoa(r.Number) + " uses it as its base"
			return step, nil
		}
	}
	act := store.ActivityRow{Kind: "branch_delete", PRID: pr.ID, TriggeredBy: "manual"}
	if err := s.cfg.GitHub.DeleteBranch(ctx, token, pr.Repo.NameWithOwner, pr.HeadRef); err != nil {
		s.cfg.Log.WarnContext(ctx, "delete branch", "pr", pr.ID, "err", err)
		step.State, step.Message = StateFailed, "The branch could not be deleted"
		act.Result, act.Summary, act.Error = StateFailed, "Delete branch "+pr.HeadRef, step.Message
		return step, &act
	}
	step.State = StateOK
	act.Result, act.Summary = StateOK, "Deleted branch "+pr.HeadRef
	return step, &act
}

type outcome struct {
	step  Step
	act   store.ActivityRow
	retry store.RetryRow
}

// transition is step 6: every ticket with a move is transitioned, at most 4 at a time. A failure
// never stops the others. Tickets left where they are get a skipped step and no activity.
func (s *Service) transition(ctx context.Context, prID string, tickets []Ticket, moves []move) ([]Step, []store.ActivityRow, []store.RetryRow) {
	done := parallel(len(moves), func(i int) outcome { return s.move(ctx, prID, moves[i], "manual") })
	byTicket := make(map[string]outcome, len(moves))
	for i, m := range moves {
		byTicket[m.ticket] = done[i]
	}
	var (
		steps   = make([]Step, 0, len(tickets))
		acts    []store.ActivityRow
		retries []store.RetryRow
	)
	for _, t := range tickets {
		o, moved := byTicket[t.Key]
		if !moved {
			steps = append(steps, Step{Kind: StepTransition, Ticket: t.Key, State: StateSkipped, Message: "Not moved"})
			continue
		}
		steps = append(steps, o.step)
		acts = append(acts, o.act)
		if o.step.State == StateFailed {
			retries = append(retries, o.retry)
		}
	}
	return steps, acts, retries
}

// move sends one transition and describes what happened.
func (s *Service) move(ctx context.Context, prID string, m move, by string) outcome {
	err := s.cfg.Jira.Transition(ctx, m.site, m.ticket, m.tr.ID, m.fields)
	o := outcome{
		step: Step{Kind: StepTransition, Ticket: m.ticket, State: StateOK, ToStatus: m.tr.ToStatus},
		act:  store.ActivityRow{Kind: StepTransition, TicketKey: m.ticket, PRID: prID, TriggeredBy: by, Result: StateOK, Summary: m.ticket + " moved to " + m.tr.ToStatus},
	}
	if err == nil {
		return o
	}
	s.cfg.Log.WarnContext(ctx, "jira transition failed", "ticket", m.ticket, "err", err)
	msg := describeJira(err)
	o.step.State, o.step.Message = StateFailed, msg
	o.act.Result, o.act.Error, o.act.Summary = StateFailed, msg, m.ticket+" could not be moved to "+m.tr.ToStatus
	o.retry = store.RetryRow{PRID: prID, TicketKey: m.ticket, TransitionID: m.tr.ID, ToStatus: m.tr.ToStatus, Fields: m.raw, LastError: msg}
	return o
}

// describeJira turns a failed Jira call into text for the person. It never includes a token.
func describeJira(err error) string {
	var se *jira.StatusError
	switch {
	case notConnected(err):
		return "Jira needs to be connected again"
	case errors.Is(err, jira.ErrNotFound):
		return "Jira does not know this ticket"
	case errors.As(err, &se) && se.Status == 400 && se.Detail != "":
		return "Jira rejected the move: " + se.Detail
	case errors.As(err, &se) && se.Status == 400:
		return "Jira rejected the move"
	case errors.As(err, &se):
		return fmt.Sprintf("Jira answered %d", se.Status)
	}
	return "Jira could not be reached"
}

// Retry repeats a failed transition. The transition must still be available for the ticket and
// its stored fields must still satisfy it. The outcome is logged, and a success removes the retry.
func (s *Service) Retry(ctx context.Context, id int64) (Step, error) {
	row, err := s.cfg.Store.Retry(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return Step{}, &Error{Code: CodeNotFound, Detail: "no such retry"}
	}
	if err != nil {
		return Step{}, err
	}
	site, err := s.cfg.Accounts.JiraSite(ctx)
	if err != nil {
		if notConnected(err) {
			return Step{}, &Error{Code: CodeNotConnected, Detail: "Jira is not connected"}
		}
		return Step{}, err
	}
	list, err := s.cfg.Jira.Transitions(ctx, site.ID, row.TicketKey)
	switch {
	case notConnected(err):
		return Step{}, &Error{Code: CodeNotConnected, Detail: "Jira is not connected"}
	case errors.Is(err, jira.ErrNotFound):
		return Step{}, &Error{Code: CodeTransitionUnavailable, Detail: row.TicketKey + " was not found in Jira"}
	case err != nil:
		return Step{}, err
	}
	tr, ok := findTransition(list, row.TransitionID)
	if !ok {
		return Step{}, &Error{Code: CodeTransitionUnavailable, Detail: row.TicketKey + " cannot make that move now"}
	}
	fields, problem := encodeFields(tr, row.Fields)
	if problem != nil {
		return Step{}, &Error{Code: problem.code, Detail: row.TicketKey + ": " + problem.detail}
	}
	o := s.move(ctx, row.PRID, move{ticket: row.TicketKey, tr: tr, fields: fields, site: site.ID}, "retry")
	o.act.Summary += " (retry)"
	if err := s.cfg.Store.FinishRetry(ctx, id, s.cfg.Clock(), o.act); err != nil {
		return Step{}, err
	}
	if o.step.State == StateFailed {
		o.step.RetryID = id
	}
	if s.cfg.Publisher != nil {
		_ = s.cfg.Publisher.Publish(events.Activity, map[string]string{"pr": row.PRID})
	}
	s.cfg.Refresh()
	return o.step, nil
}
