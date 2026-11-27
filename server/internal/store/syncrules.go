package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// RuleFilters are the optional filters of a sync rule.
type RuleFilters struct {
	Repo      string `json:"repo,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Project   string `json:"project,omitempty"`
	IssueType string `json:"issueType,omitempty"`
	Status    string `json:"status,omitempty"`
}

// SyncRule is a status sync rule (TECH_SPEC 7.3). Its position is its index in the list.
type SyncRule struct {
	ID      string
	Name    string
	Enabled bool
	Trigger string
	Hours   int
	Filters RuleFilters
	Target  string
	Mode    string
}

type triggerJSON struct {
	Type  string `json:"type"`
	Hours int    `json:"hours,omitempty"`
}

// Firing is the fingerprint a rule last fired with for a ticket.
type Firing struct {
	Rule        string `json:"rule"`
	Ticket      string `json:"ticket"`
	Fingerprint string `json:"fingerprint"`
}

// MergedPR is a merged pull request with the tickets linked to it.
type MergedPR struct {
	ID      string
	Number  int
	Repo    string
	Base    string
	Tickets []string
}

// SyncState is what one rule evaluation reads.
type SyncState struct {
	Rules  []SyncRule
	Fired  []Firing
	Merged []MergedPR
	// ReadySHA maps an open pull request to the head commit it had when it became ready.
	ReadySHA map[string]string
	// ApprovalsNeed maps a repository id to the approvals it requires (1 to 10).
	ApprovalsNeed map[string]int
}

// SyncWrite is what an evaluation writes next to the snapshot: the activity rows of the moves, the
// retries of failed ones and the fingerprints that were handled.
type SyncWrite struct {
	Activity []ActivityRow
	Retries  []RetryRow
	Firings  []Firing
}

// Empty reports whether there is nothing to write.
func (w SyncWrite) Empty() bool {
	return len(w.Activity) == 0 && len(w.Retries) == 0 && len(w.Firings) == 0
}

// Rules lists the sync rules in order.
func (s *Store) Rules(ctx context.Context) ([]SyncRule, error) {
	return readRules(ctx, s.r)
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func readRules(ctx context.Context, q queryer) ([]SyncRule, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, name, enabled, trigger, filters, target_status, mode FROM sync_rules ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []SyncRule{}
	for rows.Next() {
		var r SyncRule
		var trigger, filters string
		if err := rows.Scan(&r.ID, &r.Name, &r.Enabled, &trigger, &filters, &r.Target, &r.Mode); err != nil {
			return nil, err
		}
		var t triggerJSON
		if err := json.Unmarshal([]byte(trigger), &t); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(filters), &r.Filters); err != nil {
			return nil, err
		}
		r.Trigger, r.Hours = t.Type, t.Hours
		out = append(out, r)
	}
	return out, rows.Err()
}

type ruleJSON struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Enabled int         `json:"enabled"`
	Trigger triggerJSON `json:"trigger"`
	Filters RuleFilters `json:"filters"`
	Target  string      `json:"target"`
	Mode    string      `json:"mode"`
}

// PutRules replaces the whole ordered rule list in one transaction. The firings of the rules in
// reset are deleted (a new or edited rule starts fresh), rules that are gone lose theirs through
// the foreign key, and baseline firings are written for the rules the person chose not to apply to
// existing tickets.
func (s *Store) PutRules(ctx context.Context, list []SyncRule, reset []string, baseline []Firing, now time.Time) error {
	items := make([]ruleJSON, len(list))
	for i, r := range list {
		on := 0
		if r.Enabled {
			on = 1
		}
		items[i] = ruleJSON{r.ID, r.Name, on, triggerJSON{r.Trigger, r.Hours}, r.Filters, r.Target, r.Mode}
	}
	return s.Write(ctx, func(tx *sql.Tx) error {
		steps := []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM sync_rules WHERE id NOT IN (SELECT json_extract(value, '$.id') FROM json_each(?))`, []any{marshal(items)}},
			{`DELETE FROM rule_firings WHERE rule_id IN (SELECT value FROM json_each(?))`, []any{marshal(reset)}},
			{`INSERT INTO sync_rules (id, position, name, enabled, trigger, filters, target_status, mode)
			  SELECT json_extract(value, '$.id'), key, json_extract(value, '$.name'), json_extract(value, '$.enabled'),
			         json_extract(value, '$.trigger'), json_extract(value, '$.filters'), json_extract(value, '$.target'),
			         json_extract(value, '$.mode')
			  FROM json_each(?) WHERE true
			  ON CONFLICT (id) DO UPDATE SET position = excluded.position, name = excluded.name, enabled = excluded.enabled,
			    trigger = excluded.trigger, filters = excluded.filters, target_status = excluded.target_status, mode = excluded.mode`,
				[]any{marshal(items)}},
		}
		for _, st := range steps {
			if _, err := tx.ExecContext(ctx, st.sql, st.args...); err != nil {
				return err
			}
		}
		return insertFirings(ctx, tx, now, baseline)
	})
}

// insertFirings records fingerprints. A firing for a rule that no longer exists is dropped.
func insertFirings(ctx context.Context, tx *sql.Tx, now time.Time, firings []Firing) error {
	if len(firings) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO rule_firings (rule_id, ticket_key, fingerprint, fired_at)
		 SELECT r.id, json_extract(f.value, '$.ticket'), json_extract(f.value, '$.fingerprint'), ?
		 FROM json_each(?) f JOIN sync_rules r ON r.id = json_extract(f.value, '$.rule') WHERE true
		 ON CONFLICT (rule_id, ticket_key) DO UPDATE SET fingerprint = excluded.fingerprint, fired_at = excluded.fired_at`,
		unix(now), marshal(firings))
	return err
}

// writeSync writes the activity, the retries and the firings of an evaluation inside tx.
func writeSync(ctx context.Context, tx *sql.Tx, now time.Time, w SyncWrite) error {
	if err := insertActivity(ctx, tx, now, w.Activity); err != nil {
		return err
	}
	if _, err := insertRetries(ctx, tx, now, w.Retries); err != nil {
		return err
	}
	return insertFirings(ctx, tx, now, w.Firings)
}

// SaveSync stores the snapshot together with the result of an evaluation in one transaction.
func (s *Store) SaveSync(ctx context.Context, version int64, data []byte, now time.Time, w SyncWrite) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		if err := writeSync(ctx, tx, now, w); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO snapshot (id, version, data, updated_at) VALUES (1, ?, ?, ?)
			 ON CONFLICT (id) DO UPDATE SET version = excluded.version, data = excluded.data, updated_at = excluded.updated_at`,
			version, data, unix(now))
		return err
	})
}

// SyncState reads what an evaluation needs. Merged lists the pull requests merged since the given
// time, those linked to a ticket that also has an open pull request, and the ones in justMerged
// (resolved by the refresh that is running).
func (s *Store) SyncState(ctx context.Context, since time.Time, justMerged []string) (SyncState, error) {
	var st SyncState
	rules, err := readRules(ctx, s.r)
	if err != nil {
		return st, err
	}
	st.Rules = rules
	rows, err := s.r.QueryContext(ctx, `SELECT rule_id, ticket_key, fingerprint FROM rule_firings`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var f Firing
		if err := rows.Scan(&f.Rule, &f.Ticket, &f.Fingerprint); err != nil {
			_ = rows.Close()
			return st, err
		}
		st.Fired = append(st.Fired, f)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return st, err
	}
	_ = rows.Close()

	rows, err = s.r.QueryContext(ctx,
		`SELECT p.id, p.number, r.name_with_owner, p.base_branch, l.ticket_key
		 FROM prs p JOIN repos r ON r.id = p.repo_id JOIN ticket_links l ON l.pr_id = p.id AND l.removed = 0
		 WHERE (p.state = 'merged' AND (p.merged_at >= ? OR l.ticket_key IN
		          (SELECT o.ticket_key FROM ticket_links o JOIN prs op ON op.id = o.pr_id WHERE op.state = 'open' AND o.removed = 0)))
		    OR p.id IN (SELECT value FROM json_each(?))
		 ORDER BY p.id, l.position, l.ticket_key`, unix(since), marshal(justMerged))
	if err != nil {
		return st, err
	}
	at := map[string]int{}
	for rows.Next() {
		var m MergedPR
		var key string
		if err := rows.Scan(&m.ID, &m.Number, &m.Repo, &m.Base, &key); err != nil {
			_ = rows.Close()
			return st, err
		}
		i, ok := at[m.ID]
		if !ok {
			i = len(st.Merged)
			at[m.ID] = i
			st.Merged = append(st.Merged, m)
		}
		st.Merged[i].Tickets = append(st.Merged[i].Tickets, key)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return st, err
	}

	st.ReadySHA = map[string]string{}
	rows, err = s.r.QueryContext(ctx, `SELECT id, ready_sha FROM prs WHERE state = 'open' AND ready_sha IS NOT NULL`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var id, sha string
		if err := rows.Scan(&id, &sha); err != nil {
			_ = rows.Close()
			return st, err
		}
		st.ReadySHA[id] = sha
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return st, err
	}

	st.ApprovalsNeed = map[string]int{}
	rows, err = s.r.QueryContext(ctx, `SELECT id, approvals_required FROM repos`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var id string
		var need int
		if err := rows.Scan(&id, &need); err != nil {
			_ = rows.Close()
			return st, err
		}
		st.ApprovalsNeed[id] = need
	}
	return st, errors.Join(rows.Err(), rows.Close())
}
