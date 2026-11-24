package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"
)

// Session is a persisted browser session. Only the SHA-256 of the session id is stored.
type Session struct {
	IDHash       [sha256.Size]byte
	CreatedAt    time.Time
	LastIssuedAt time.Time
	ExpiresAt    time.Time
}

// InsertSession stores a new session and drops expired ones in the same transaction.
func (s *Store) InsertSession(ctx context.Context, v Session) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, unix(v.CreatedAt)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO sessions (id_hash, created_at, last_issued_at, expires_at) VALUES (?, ?, ?, ?)`,
			v.IDHash[:], unix(v.CreatedAt), unix(v.LastIssuedAt), unix(v.ExpiresAt))
		return err
	})
}

// SlideSession records a new issue time and expiry for a session.
func (s *Store) SlideSession(ctx context.Context, idHash [sha256.Size]byte, lastIssued, expires time.Time) error {
	_, err := s.w.ExecContext(ctx, `UPDATE sessions SET last_issued_at = ?, expires_at = ? WHERE id_hash = ?`,
		unix(lastIssued), unix(expires), idHash[:])
	return err
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(ctx context.Context, idHash [sha256.Size]byte) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, idHash[:])
	return err
}

// DeleteAllSessions removes every session.
func (s *Store) DeleteAllSessions(ctx context.Context) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM sessions`)
	return err
}

// LoadSessions returns the sessions that have not expired at now.
func (s *Store) LoadSessions(ctx context.Context, now time.Time) ([]Session, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT id_hash, created_at, last_issued_at, expires_at FROM sessions WHERE expires_at > ?`, unix(now))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		var (
			v       Session
			h       []byte
			c, l, e int64
		)
		if err := rows.Scan(&h, &c, &l, &e); err != nil {
			return nil, err
		}
		if len(h) != sha256.Size {
			continue
		}
		copy(v.IDHash[:], h)
		v.CreatedAt, v.LastIssuedAt, v.ExpiresAt = fromUnix(c), fromUnix(l), fromUnix(e)
		out = append(out, v)
	}
	return out, rows.Err()
}

// OAuthState is a pending OAuth authorization. Only the SHA-256 of the state value is stored.
type OAuthState struct {
	Hash       [sha256.Size]byte
	Provider   string
	SessionKey string
	Verifier   string
	ExpiresAt  time.Time
}

// InsertOAuthState stores a pending authorization and drops expired ones in the same transaction.
func (s *Store) InsertOAuthState(ctx context.Context, v OAuthState, now time.Time) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_states WHERE expires_at <= ?`, unix(now)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO oauth_states (state_hash, provider, session_key, verifier, expires_at) VALUES (?, ?, ?, ?, ?)`,
			v.Hash[:], v.Provider, v.SessionKey, v.Verifier, unix(v.ExpiresAt))
		return err
	})
}

// ConsumeOAuthState deletes the state and returns it. A second call for the same hash returns
// ErrNotFound, so every state can be used once. Expiry is checked by the caller.
func (s *Store) ConsumeOAuthState(ctx context.Context, hash [sha256.Size]byte) (OAuthState, error) {
	v := OAuthState{Hash: hash}
	var exp int64
	err := s.w.QueryRowContext(ctx,
		`DELETE FROM oauth_states WHERE state_hash = ? RETURNING provider, session_key, verifier, expires_at`,
		hash[:]).Scan(&v.Provider, &v.SessionKey, &v.Verifier, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthState{}, ErrNotFound
	}
	if err != nil {
		return OAuthState{}, err
	}
	v.ExpiresAt = fromUnix(exp)
	return v, nil
}
