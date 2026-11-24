package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// IdempotencyTTL is how long a merge result is kept for a repeated Idempotency-Key.
const IdempotencyTTL = 24 * time.Hour

// ActivityRow is one entry of the activity log.
type ActivityRow struct {
	ID          int64
	CreatedAt   time.Time
	Kind        string
	TicketKey   string
	PRID        string
	TriggeredBy string
	Result      string
	Summary     string
	Error       string
}

type activityJSON struct {
	Kind        string `json:"kind"`
	TicketKey   string `json:"ticket"`
	PRID        string `json:"pr"`
	TriggeredBy string `json:"by"`
	Result      string `json:"result"`
	Summary     string `json:"summary"`
	Error       string `json:"error"`
}

// RetryRow is a transition that failed and waits for a retry.
type RetryRow struct {
	ID           int64
	PRID         string
	TicketKey    string
	TransitionID string
	ToStatus     string
	Fields       map[string]string
	LastError    string
	CreatedAt    time.Time
}

type retryJSON struct {
	PRID         string            `json:"pr"`
	TicketKey    string            `json:"ticket"`
	TransitionID string            `json:"transition"`
	ToStatus     string            `json:"toStatus"`
	Fields       map[string]string `json:"fields"`
	LastError    string            `json:"error"`
}

// Merged is everything one merge writes, in one transaction. Encode receives the id of each new
// retry by ticket key and returns the result kept for the Idempotency-Key.
type Merged struct {
	Now      time.Time
	PRID     string
	Key      string
	Activity []ActivityRow
	Retries  []RetryRow
	// Firings are the fingerprints of the rules that hold for the tickets this merge handled, so the
	// sync engine does not move them again.
	Firings []Firing
	Encode  func(retryIDs map[string]int64) ([]byte, error)
}

func insertActivity(ctx context.Context, tx *sql.Tx, now time.Time, rows []ActivityRow) error {
	if len(rows) == 0 {
		return nil
	}
	items := make([]activityJSON, len(rows))
	for i, r := range rows {
		items[i] = activityJSON{r.Kind, r.TicketKey, r.PRID, r.TriggeredBy, r.Result, r.Summary, r.Error}
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO activity (created_at, kind, ticket_key, pr_id, triggered_by, result, summary, error)
		 SELECT ?, json_extract(value, '$.kind'), nullif(json_extract(value, '$.ticket'), ''), nullif(json_extract(value, '$.pr'), ''),
		        json_extract(value, '$.by'), json_extract(value, '$.result'), json_extract(value, '$.summary'),
		        nullif(json_extract(value, '$.error'), '')
		 FROM json_each(?) ORDER BY key`, unix(now), marshal(items))
	return err
}

func insertRetries(ctx context.Context, tx *sql.Tx, now time.Time, retries []RetryRow) (map[string]int64, error) {
	ids := map[string]int64{}
	if len(retries) == 0 {
		return ids, nil
	}
	items := make([]retryJSON, len(retries))
	for i, r := range retries {
		if r.Fields == nil {
			r.Fields = map[string]string{}
		}
		items[i] = retryJSON{r.PRID, r.TicketKey, r.TransitionID, r.ToStatus, r.Fields, r.LastError}
	}
	rows, err := tx.QueryContext(ctx,
		`INSERT INTO pending_retries (pr_id, ticket_key, transition_id, to_status, fields, last_error, created_at)
		 SELECT json_extract(value, '$.pr'), json_extract(value, '$.ticket'), json_extract(value, '$.transition'),
		        json_extract(value, '$.toStatus'), json_extract(value, '$.fields'), json_extract(value, '$.error'), ?
		 FROM json_each(?) ORDER BY key RETURNING id, ticket_key`, unix(now), marshal(items))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids[key] = id
	}
	return ids, errors.Join(rows.Err(), rows.Close())
}

// RecordMerge writes the activity rows, the pending retries, the merged rule firings and the
// idempotency record of a merge in one transaction, and removes expired idempotency records.
func (s *Store) RecordMerge(ctx context.Context, m Merged) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		if err := insertActivity(ctx, tx, m.Now, m.Activity); err != nil {
			return err
		}
		ids, err := insertRetries(ctx, tx, m.Now, m.Retries)
		if err != nil {
			return err
		}
		if err := insertFirings(ctx, tx, m.Now, m.Firings); err != nil {
			return err
		}
		result, err := m.Encode(ids)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM idempotency WHERE created_at < ?`, unix(m.Now.Add(-IdempotencyTTL))); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO idempotency (key, pr_id, result_json, created_at) VALUES (?, ?, ?, ?)
			 ON CONFLICT (key) DO UPDATE SET result_json = excluded.result_json, created_at = excluded.created_at`,
			m.Key, m.PRID, string(result), unix(m.Now))
		return err
	})
}

// Idempotent returns the pull request and the result stored for key, when it is younger than
// IdempotencyTTL.
func (s *Store) Idempotent(ctx context.Context, key string, now time.Time) (prID string, result []byte, ok bool, err error) {
	var raw string
	err = s.r.QueryRowContext(ctx, `SELECT pr_id, result_json FROM idempotency WHERE key = ? AND created_at >= ?`,
		key, unix(now.Add(-IdempotencyTTL))).Scan(&prID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false, nil
	}
	return prID, []byte(raw), err == nil, err
}

// Retries lists the pending retries, oldest first.
func (s *Store) Retries(ctx context.Context) ([]RetryRow, error) {
	return s.retries(ctx, 0)
}

// Retry returns one pending retry, or ErrNotFound.
func (s *Store) Retry(ctx context.Context, id int64) (RetryRow, error) {
	rows, err := s.retries(ctx, id)
	if err != nil {
		return RetryRow{}, err
	}
	if len(rows) == 0 {
		return RetryRow{}, ErrNotFound
	}
	return rows[0], nil
}

func (s *Store) retries(ctx context.Context, id int64) ([]RetryRow, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT id, coalesce(pr_id, ''), ticket_key, transition_id, to_status, fields, last_error, created_at
		 FROM pending_retries WHERE ? = 0 OR id = ? ORDER BY id`, id, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []RetryRow{}
	for rows.Next() {
		var r RetryRow
		var fields string
		var created int64
		if err := rows.Scan(&r.ID, &r.PRID, &r.TicketKey, &r.TransitionID, &r.ToStatus, &fields, &r.LastError, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(fields), &r.Fields); err != nil {
			return nil, err
		}
		r.CreatedAt = fromUnix(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRetry dismisses a pending retry. It returns ErrNotFound when there is none.
func (s *Store) DeleteRetry(ctx context.Context, id int64) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM pending_retries WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// FinishRetry records the outcome of a retry in one transaction: a success removes the pending
// retry, a failure keeps it with the new error. Either way one activity row is written.
func (s *Store) FinishRetry(ctx context.Context, id int64, now time.Time, act ActivityRow) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		var res sql.Result
		var err error
		if act.Result == "ok" {
			res, err = tx.ExecContext(ctx, `DELETE FROM pending_retries WHERE id = ?`, id)
		} else {
			res, err = tx.ExecContext(ctx, `UPDATE pending_retries SET last_error = ? WHERE id = ?`, act.Error, id)
		}
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrNotFound
		}
		return insertActivity(ctx, tx, now, []ActivityRow{act})
	})
}

// Activity returns up to limit rows with an id below before (0 for the newest), newest first.
func (s *Store) Activity(ctx context.Context, before int64, limit int) ([]ActivityRow, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT id, created_at, kind, coalesce(ticket_key, ''), coalesce(pr_id, ''), triggered_by, result, summary, coalesce(error, '')
		 FROM activity WHERE ? = 0 OR id < ? ORDER BY id DESC LIMIT ?`, before, before, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []ActivityRow{}
	for rows.Next() {
		var a ActivityRow
		var created int64
		if err := rows.Scan(&a.ID, &created, &a.Kind, &a.TicketKey, &a.PRID, &a.TriggeredBy, &a.Result, &a.Summary, &a.Error); err != nil {
			return nil, err
		}
		a.CreatedAt = fromUnix(created)
		out = append(out, a)
	}
	return out, rows.Err()
}
