package store

import (
	"context"
	"database/sql"
)

// RepoConfig is a repository the dashboard has seen, with its merge settings and the number of its
// pull requests stored as open.
type RepoConfig struct {
	ID            string
	NameWithOwner string
	IsNew         bool
	DefaultMethod string
	Rules         []BranchRule
	OpenPRs       int
}

// Repos lists every repo with its ordered branch rules, in two queries.
func (s *Store) Repos(ctx context.Context) ([]RepoConfig, error) {
	return s.repos(ctx, "")
}

// Repo returns one repo, or ErrNotFound.
func (s *Store) Repo(ctx context.Context, id string) (RepoConfig, error) {
	out, err := s.repos(ctx, id)
	if err != nil {
		return RepoConfig{}, err
	}
	if len(out) == 0 {
		return RepoConfig{}, ErrNotFound
	}
	return out[0], nil
}

func (s *Store) repos(ctx context.Context, id string) ([]RepoConfig, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT r.id, r.name_with_owner, r.is_new, r.default_method,
		        (SELECT count(*) FROM prs p WHERE p.repo_id = r.id AND p.state = 'open')
		 FROM repos r WHERE ? = '' OR r.id = ? ORDER BY r.name_with_owner COLLATE NOCASE, r.id`, id, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []RepoConfig{}
	at := map[string]int{}
	for rows.Next() {
		var c RepoConfig
		if err := rows.Scan(&c.ID, &c.NameWithOwner, &c.IsNew, &c.DefaultMethod, &c.OpenPRs); err != nil {
			return nil, err
		}
		c.Rules = []BranchRule{}
		at[c.ID] = len(out)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ruleRows, err := s.r.QueryContext(ctx,
		`SELECT repo_id, pattern, method FROM repo_branch_rules WHERE ? = '' OR repo_id = ? ORDER BY repo_id, position`, id, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ruleRows.Close() }()
	for ruleRows.Next() {
		var repo string
		var rule BranchRule
		if err := ruleRows.Scan(&repo, &rule.Pattern, &rule.Method); err != nil {
			return nil, err
		}
		if i, ok := at[repo]; ok {
			out[i].Rules = append(out[i].Rules, rule)
		}
	}
	return out, ruleRows.Err()
}

// PutRepo sets the default method and replaces the whole rule list in one transaction. clearNew
// also marks the repo as seen. It returns ErrNotFound for an unknown repo.
func (s *Store) PutRepo(ctx context.Context, id, method string, rules []BranchRule, clearNew bool) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE repos SET default_method = ?, is_new = CASE WHEN ? THEN 0 ELSE is_new END WHERE id = ?`, method, clearNew, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM repo_branch_rules WHERE repo_id = ?`, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO repo_branch_rules (repo_id, position, pattern, method)
			 SELECT ?, key, json_extract(value, '$.pattern'), json_extract(value, '$.method') FROM json_each(?)`,
			id, marshal(rules))
		return err
	})
}

// RepoUpdate is one repository's new default method and whole rule list, for PutRepos.
type RepoUpdate struct {
	ID       string
	Method   string
	Rules    []BranchRule
	ClearNew bool
}

type repoUpdateJSON struct {
	ID       string       `json:"id"`
	Method   string       `json:"method"`
	ClearNew int          `json:"clearNew"`
	Rules    []BranchRule `json:"rules"`
}

// PutRepos applies every update in one transaction with set-based statements, so there is no loop
// against the database. An id that appears twice takes its last update. An unknown id makes the
// whole call return ErrNotFound with nothing written.
func (s *Store) PutRepos(ctx context.Context, updates []RepoUpdate) error {
	last := make(map[string]int, len(updates))
	for i, u := range updates {
		last[u.ID] = i
	}
	items := make([]repoUpdateJSON, 0, len(last))
	for i, u := range updates {
		if last[u.ID] != i {
			continue
		}
		cn := 0
		if u.ClearNew {
			cn = 1
		}
		rules := u.Rules
		if rules == nil {
			rules = []BranchRule{}
		}
		items = append(items, repoUpdateJSON{u.ID, u.Method, cn, rules})
	}
	payload := marshal(items)
	return s.Write(ctx, func(tx *sql.Tx) error {
		var unknown int
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM json_each(?) WHERE json_extract(value, '$.id') NOT IN (SELECT id FROM repos)`,
			payload).Scan(&unknown); err != nil {
			return err
		}
		if unknown > 0 {
			return ErrNotFound
		}
		steps := []string{
			`UPDATE repos SET default_method = u.method, is_new = CASE WHEN u.clear THEN 0 ELSE repos.is_new END
			 FROM (SELECT json_extract(value, '$.id') AS id, json_extract(value, '$.method') AS method,
			              json_extract(value, '$.clearNew') AS clear FROM json_each(?)) u
			 WHERE repos.id = u.id`,
			`DELETE FROM repo_branch_rules WHERE repo_id IN (SELECT json_extract(value, '$.id') FROM json_each(?))`,
			`INSERT INTO repo_branch_rules (repo_id, position, pattern, method)
			 SELECT json_extract(u.value, '$.id'), r.key, json_extract(r.value, '$.pattern'), json_extract(r.value, '$.method')
			 FROM json_each(?) u, json_each(u.value, '$.rules') r`,
		}
		for _, q := range steps {
			if _, err := tx.ExecContext(ctx, q, payload); err != nil {
				return err
			}
		}
		return nil
	})
}
