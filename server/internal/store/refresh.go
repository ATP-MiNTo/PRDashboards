package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// RepoRow is a repository seen in a refresh.
type RepoRow struct {
	ID            string `json:"id"`
	NameWithOwner string `json:"name"`
}

// PRRow is the part of an open pull request that is kept in the prs table.
type PRRow struct {
	ID         string `json:"id"`
	RepoID     string `json:"repo"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	HeadBranch string `json:"head"`
	BaseBranch string `json:"base"`
	HeadSHA    string `json:"sha"`
	Draft      bool   `json:"draft"`
}

// LinkRow is an automatically found ticket link. Manual links and hidden keys are written by
// SetLinks only.
type LinkRow struct {
	PRID     string `json:"pr"`
	Key      string `json:"key"`
	Source   string `json:"source"`
	Position int    `json:"position"`
}

// Resolution closes a pull request that left the open list.
type Resolution struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	MergedAt *int64 `json:"mergedAt"`
}

// Refresh is everything one poller refresh writes.
type Refresh struct {
	Now           time.Time
	DefaultMethod string
	Repos         []RepoRow
	PRs           []PRRow
	Links         []LinkRow
	Resolved      []Resolution
	Version       int64
	Snapshot      []byte
	// Sync is the result of the rule evaluation of this refresh, written in the same transaction.
	Sync SyncWrite
}

// BranchRule sends the pull requests that target a matching base branch to a merge method.
type BranchRule struct {
	Pattern string `json:"pattern"`
	Method  string `json:"method"`
}

// SeedRules are the branch rules a repo gets when it is first seen (TECH_SPEC 7.6).
var SeedRules = []BranchRule{{Pattern: "main", Method: "merge"}, {Pattern: "production", Method: "merge"}}

func marshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // the row types are plain structs, so this cannot happen
	}
	if string(b) == "null" {
		return "[]" // json_each over null yields one NULL row
	}
	return string(b)
}

func ids[T any](rows []T, id func(T) string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = id(r)
	}
	return out
}

// SaveRefresh writes repos, pull requests, ticket links, resolutions and the snapshot in one
// transaction, with one statement per table. Repos seen for the first time get is_new, the global
// default method and the seeded branch rules.
func (s *Store) SaveRefresh(ctx context.Context, r Refresh) error {
	now := unix(r.Now)
	return s.Write(ctx, func(tx *sql.Tx) error {
		repoIDs := marshal(ids(r.Repos, func(x RepoRow) string { return x.ID }))
		fresh, err := newIDs(ctx, tx, repoIDs)
		if err != nil {
			return err
		}
		prIDs := marshal(ids(r.PRs, func(x PRRow) string { return x.ID }))
		steps := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO repos (id, name_with_owner, first_seen_at, default_method, is_new)
			  SELECT json_extract(value, '$.id'), json_extract(value, '$.name'), ?, ?, 1 FROM json_each(?) WHERE true
			  ON CONFLICT (id) DO UPDATE SET name_with_owner = excluded.name_with_owner`,
				[]any{now, r.DefaultMethod, marshal(r.Repos)}},
			{`INSERT INTO repo_branch_rules (repo_id, position, pattern, method)
			  SELECT j.value, s.key, json_extract(s.value, '$.pattern'), json_extract(s.value, '$.method')
			  FROM json_each(?) j CROSS JOIN json_each(?) s`,
				[]any{marshal(fresh), marshal(SeedRules)}},
			{`INSERT INTO prs (id, repo_id, number, title, head_branch, base_branch, head_sha, state, merged_at, last_seen_at, ready_sha)
			  SELECT json_extract(value, '$.id'), json_extract(value, '$.repo'), json_extract(value, '$.number'),
			         json_extract(value, '$.title'), json_extract(value, '$.head'), json_extract(value, '$.base'),
			         json_extract(value, '$.sha'), 'open', NULL, ?,
			         CASE WHEN json_extract(value, '$.draft') THEN NULL ELSE json_extract(value, '$.sha') END
			  FROM json_each(?) WHERE true
			  ON CONFLICT (id) DO UPDATE SET repo_id = excluded.repo_id, number = excluded.number, title = excluded.title,
			    head_branch = excluded.head_branch, base_branch = excluded.base_branch, head_sha = excluded.head_sha,
			    state = 'open', merged_at = NULL, last_seen_at = excluded.last_seen_at,
			    ready_sha = CASE WHEN excluded.ready_sha IS NULL THEN NULL
			                     WHEN prs.state = 'open' AND prs.ready_sha IS NOT NULL THEN prs.ready_sha
			                     ELSE excluded.ready_sha END`,
				[]any{now, marshal(r.PRs)}},
			{`UPDATE prs SET state = j.state, merged_at = j.merged_at
			  FROM (SELECT json_extract(value, '$.id') AS id, json_extract(value, '$.state') AS state,
			               json_extract(value, '$.mergedAt') AS merged_at FROM json_each(?)) j
			  WHERE prs.id = j.id`,
				[]any{marshal(r.Resolved)}},
			{`DELETE FROM ticket_links WHERE removed = 0 AND source <> 'manual' AND pr_id IN (SELECT value FROM json_each(?))`,
				[]any{prIDs}},
			{`INSERT INTO ticket_links (pr_id, ticket_key, source, removed, position)
			  SELECT json_extract(value, '$.pr'), json_extract(value, '$.key'), json_extract(value, '$.source'), 0,
			         json_extract(value, '$.position')
			  FROM json_each(?) WHERE true ON CONFLICT (pr_id, ticket_key) DO NOTHING`,
				[]any{marshal(r.Links)}},
			{`INSERT INTO snapshot (id, version, data, updated_at) VALUES (1, ?, ?, ?)
			  ON CONFLICT (id) DO UPDATE SET version = excluded.version, data = excluded.data, updated_at = excluded.updated_at`,
				[]any{r.Version, r.Snapshot, now}},
		}
		for _, st := range steps {
			if _, err := tx.ExecContext(ctx, st.sql, st.args...); err != nil {
				return err
			}
		}
		return writeSync(ctx, tx, r.Now, r.Sync)
	})
}

func newIDs(ctx context.Context, tx *sql.Tx, idsJSON string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT value FROM json_each(?) WHERE value NOT IN (SELECT id FROM repos)`, idsJSON)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// StoredSnapshot is the row of the snapshot table.
type StoredSnapshot struct {
	Version   int64
	Data      []byte
	UpdatedAt time.Time
}

// LoadSnapshot returns the last saved snapshot, or ErrNotFound.
func (s *Store) LoadSnapshot(ctx context.Context) (StoredSnapshot, error) {
	var out StoredSnapshot
	var updated int64
	err := s.r.QueryRowContext(ctx, `SELECT version, data, updated_at FROM snapshot WHERE id = 1`).Scan(&out.Version, &out.Data, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	out.UpdatedAt = fromUnix(updated)
	return out, err
}

// SaveSnapshot replaces the stored snapshot only.
func (s *Store) SaveSnapshot(ctx context.Context, version int64, data []byte, now time.Time) error {
	_, err := s.w.ExecContext(ctx,
		`INSERT INTO snapshot (id, version, data, updated_at) VALUES (1, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET version = excluded.version, data = excluded.data, updated_at = excluded.updated_at`,
		version, data, unix(now))
	return err
}

// OpenPRs maps the id of every pull request stored as open to when it was last seen.
func (s *Store) OpenPRs(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id, last_seen_at FROM prs WHERE state = 'open'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var seen int64
		if err := rows.Scan(&id, &seen); err != nil {
			return nil, err
		}
		out[id] = fromUnix(seen)
	}
	return out, rows.Err()
}

// UserLinks are what the person decided for one pull request: tickets added by hand, and
// automatically found keys to hide.
type UserLinks struct {
	Manual  []string
	Removed []string
}

// UserLinks reads every manual link and hidden key in one query.
func (s *Store) UserLinks(ctx context.Context) (map[string]UserLinks, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT pr_id, ticket_key, removed FROM ticket_links WHERE source = 'manual' OR removed = 1 ORDER BY pr_id, position, ticket_key`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]UserLinks{}
	for rows.Next() {
		var pr, key string
		var removed bool
		if err := rows.Scan(&pr, &key, &removed); err != nil {
			return nil, err
		}
		l := out[pr]
		if removed {
			l.Removed = append(l.Removed, key)
		} else {
			l.Manual = append(l.Manual, key)
		}
		out[pr] = l
	}
	return out, rows.Err()
}

// HiddenKey is an automatically found key the person removed, with the source it was found in.
type HiddenKey struct {
	Key    string
	Source string
}

// SetUserLinks replaces the manual links and hidden keys of a pull request in one transaction.
// It returns ErrNotFound when the pull request is not stored.
func (s *Store) SetUserLinks(ctx context.Context, prID string, manual []string, hidden []HiddenKey) error {
	type row struct {
		Key      string `json:"key"`
		Source   string `json:"source"`
		Removed  int    `json:"removed"`
		Position int    `json:"position"`
	}
	rows := make([]row, 0, len(manual)+len(hidden))
	for i, k := range manual {
		rows = append(rows, row{Key: k, Source: "manual", Position: i})
	}
	for i, h := range hidden {
		rows = append(rows, row{Key: h.Key, Source: h.Source, Removed: 1, Position: i})
	}
	return s.Write(ctx, func(tx *sql.Tx) error {
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM prs WHERE id = ?`, prID).Scan(&found); err != nil {
			return err
		}
		if found == 0 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM ticket_links WHERE pr_id = ? AND (source = 'manual' OR removed = 1)`, prID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO ticket_links (pr_id, ticket_key, source, removed, position)
			 SELECT ?, json_extract(value, '$.key'), json_extract(value, '$.source'), json_extract(value, '$.removed'),
			        json_extract(value, '$.position')
			 FROM json_each(?) WHERE true ON CONFLICT (pr_id, ticket_key) DO UPDATE SET source = excluded.source,
			   removed = excluded.removed, position = excluded.position`,
			prID, marshal(rows))
		return err
	})
}
