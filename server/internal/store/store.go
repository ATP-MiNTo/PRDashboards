// Package store is the only code that touches SQLite.
//
// Writes go through one *sql.DB limited to a single connection, so SQLite never sees two
// concurrent writers and a write transaction is held by exactly one goroutine at a time. Reads use
// a separate read-only pool, which WAL mode lets run beside the writer.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// Store holds the writer and the reader pools.
type Store struct {
	w, r  *sql.DB
	clock func() time.Time
}

// Open creates the database file with mode 0600 when missing, opens the writer and reader pools
// in WAL mode with foreign keys on and a 5 s busy timeout, and applies pending migrations.
func Open(path string, clock func() time.Time) (*Store, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // path is built from the data dir
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	w, err := openPool(path, true)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := openPool(path, false)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	s := &Store{w: w, r: r, clock: clock}
	if err := s.migrate(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func openPool(path string, writer bool) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if writer {
		q.Set("_txlock", "immediate")
	} else {
		q.Add("_pragma", "query_only(1)")
	}
	u := url.URL{Scheme: "file", Opaque: escapePath(path), RawQuery: q.Encode()}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return db, nil
}

func escapePath(p string) string {
	u := url.URL{Path: p}
	return u.EscapedPath()
}

// Close closes both pools. The writer is closed last so pending WAL content is checkpointed.
func (s *Store) Close() error {
	rerr := s.r.Close()
	if err := s.w.Close(); err != nil {
		return err
	}
	return rerr
}

// Write runs fn in one write transaction and commits when it returns nil.
func (s *Store) Write(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func unix(t time.Time) int64 { return t.Unix() }

func fromUnix(sec int64) time.Time { return time.Unix(sec, 0).UTC() }
