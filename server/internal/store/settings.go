package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Settings are the global settings. Defaults apply to keys that were never written.
type Settings struct {
	GitHubSec              int
	JiraSec                int
	Theme                  string
	DefaultMethod          string
	DeleteBranchAfterMerge bool
	JiraSiteID             *string
}

// DefaultSettings returns the settings of a fresh install.
func DefaultSettings() Settings {
	return Settings{GitHubSec: 60, JiraSec: 300, Theme: "auto", DefaultMethod: "squash"}
}

const (
	keyGitHubSec     = "polling.githubSec"
	keyJiraSec       = "polling.jiraSec"
	keyTheme         = "theme"
	keyDefaultMethod = "defaultMethodForNewRepos"
	keyDeleteBranch  = "deleteBranchAfterMerge"
	keyJiraSite      = "jiraSiteId"
)

// GetSettings reads every settings row in one query.
func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	out := DefaultSettings()
	rows, err := s.r.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return out, err
		}
		var dst any
		switch k {
		case keyGitHubSec:
			dst = &out.GitHubSec
		case keyJiraSec:
			dst = &out.JiraSec
		case keyTheme:
			dst = &out.Theme
		case keyDefaultMethod:
			dst = &out.DefaultMethod
		case keyDeleteBranch:
			dst = &out.DeleteBranchAfterMerge
		case keyJiraSite:
			dst = &out.JiraSiteID
		default:
			continue
		}
		if err := json.Unmarshal([]byte(v), dst); err != nil {
			return out, fmt.Errorf("setting %s: %w", k, err)
		}
	}
	return out, rows.Err()
}

// PutSettings replaces every setting in one transaction.
func (s *Store) PutSettings(ctx context.Context, v Settings) error {
	pairs := []struct {
		key string
		val any
	}{
		{keyGitHubSec, v.GitHubSec},
		{keyJiraSec, v.JiraSec},
		{keyTheme, v.Theme},
		{keyDefaultMethod, v.DefaultMethod},
		{keyDeleteBranch, v.DeleteBranchAfterMerge},
		{keyJiraSite, v.JiraSiteID},
	}
	return s.Write(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, p := range pairs {
			b, err := json.Marshal(p.val)
			if err != nil {
				return err
			}
			if _, err := stmt.ExecContext(ctx, p.key, string(b)); err != nil {
				return err
			}
		}
		return nil
	})
}
