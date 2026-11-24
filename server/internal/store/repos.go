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
