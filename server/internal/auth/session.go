// Package auth issues launch tokens and signed session cookies for the local browser.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/store"
)

const (
	// CookieName is the session cookie.
	CookieName = "prd_session"
	// SessionTTL is the sliding lifetime of a session.
	SessionTTL = 30 * 24 * time.Hour
	// ReissueAfter is the age of the last cookie issue after which the cookie is sent again.
	ReissueAfter = 24 * time.Hour

	keyBytes = 32
)

var b64 = base64.RawURLEncoding

// Session is an authenticated browser session.
type Session struct {
	// Key identifies the session in rate limiting and logs. It is derived by hashing the id.
	Key string
	// CSRF is the token the browser must echo in X-PRD-CSRF.
	CSRF string
	// Expires is when the session ends unless it is used again.
	Expires time.Time
	// Reissue is true when the cookie must be sent again to extend its lifetime.
	Reissue bool
	// Value is the cookie value for this session.
	Value string
}

type record struct {
	lastIssued time.Time
	expires    time.Time
}

// SessionStore persists sessions by the hash of their id.
type SessionStore interface {
	InsertSession(ctx context.Context, s store.Session) error
	SlideSession(ctx context.Context, idHash [sha256.Size]byte, lastIssued, expires time.Time) error
	DeleteSession(ctx context.Context, idHash [sha256.Size]byte) error
	DeleteAllSessions(ctx context.Context) error
	LoadSessions(ctx context.Context, now time.Time) ([]store.Session, error)
}

// Sessions creates and verifies sessions. The in-memory map is a cache in front of the
// SessionStore: creation, sliding and deletion are written through, and Load restores the
// unexpired sessions at start.
type Sessions struct {
	key     []byte
	clock   func() time.Time
	rand    io.Reader
	persist SessionStore
	log     *slog.Logger

	mu sync.Mutex
	m  map[[sha256.Size]byte]*record
}

// NewSessions returns a session store signing cookies with key. persist may be nil, which keeps
// sessions in memory only.
func NewSessions(key []byte, clock func() time.Time, rnd io.Reader, persist SessionStore, log *slog.Logger) *Sessions {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Sessions{key: key, clock: clock, rand: rnd, persist: persist, log: log, m: make(map[[sha256.Size]byte]*record)}
}

// Load reads the unexpired sessions from the store.
func (s *Sessions) Load(ctx context.Context) error {
	if s.persist == nil {
		return nil
	}
	rows, err := s.persist.LoadSessions(ctx, s.clock())
	if err != nil {
		return fmt.Errorf("load sessions: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range rows {
		s.m[r.IDHash] = &record{lastIssued: r.LastIssuedAt, expires: r.ExpiresAt}
	}
	return nil
}

// Create starts a session.
func (s *Sessions) Create(ctx context.Context) (Session, error) {
	id := make([]byte, 32)
	if _, err := io.ReadFull(s.rand, id); err != nil {
		return Session{}, err
	}
	now := s.clock()
	h := sha256.Sum256(id)
	rec := &record{lastIssued: now, expires: now.Add(SessionTTL)}
	if s.persist != nil {
		row := store.Session{IDHash: h, CreatedAt: now, LastIssuedAt: now, ExpiresAt: rec.expires}
		if err := s.persist.InsertSession(ctx, row); err != nil {
			return Session{}, fmt.Errorf("store session: %w", err)
		}
	}
	s.mu.Lock()
	s.purge(now)
	s.m[h] = rec
	s.mu.Unlock()
	return s.session(id, rec, false), nil
}

// Authenticate verifies a cookie value and slides the session. It reports false for forged,
// unknown and expired cookies.
func (s *Sessions) Authenticate(value string) (Session, bool) {
	id, ok := s.parse(value)
	if !ok {
		return Session{}, false
	}
	now := s.clock()
	h := sha256.Sum256(id)
	s.mu.Lock()
	rec, ok := s.m[h]
	if !ok {
		s.mu.Unlock()
		return Session{}, false
	}
	if !now.Before(rec.expires) {
		delete(s.m, h)
		s.mu.Unlock()
		s.write("delete expired session", func(ctx context.Context) error { return s.persist.DeleteSession(ctx, h) })
		return Session{}, false
	}
	reissue := now.Sub(rec.lastIssued) >= ReissueAfter
	if reissue {
		rec.lastIssued = now
		rec.expires = now.Add(SessionTTL)
	}
	out := s.session(id, rec, reissue)
	last, exp := rec.lastIssued, rec.expires
	s.mu.Unlock()
	if reissue {
		s.write("slide session", func(ctx context.Context) error { return s.persist.SlideSession(ctx, h, last, exp) })
	}
	return out, true
}

// write runs a best-effort store write for a session that the cache already decided about. A
// failure is logged, and the cache stays authoritative until the next start.
func (s *Sessions) write(what string, fn func(ctx context.Context) error) {
	if s.persist == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fn(ctx); err != nil {
		s.log.Error(what, "err", err)
	}
}

// RevokeAll signs out every browser. The cache is cleared first, so a store failure cannot leave
// a revoked session usable.
func (s *Sessions) RevokeAll(ctx context.Context) error {
	s.mu.Lock()
	clear(s.m)
	s.mu.Unlock()
	if s.persist == nil {
		return nil
	}
	if err := s.persist.DeleteAllSessions(ctx); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}

// ExpiredCookie returns a cookie that makes the browser drop the session cookie.
func ExpiredCookie() *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

// CheckCSRF compares a presented CSRF token with the session's in constant time.
func (s Session) CheckCSRF(token string) bool {
	return hmac.Equal([]byte(s.CSRF), []byte(token))
}

// Cookie returns the cookie to send for the session.
func (s Session) Cookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    s.Value,
		Path:     "/",
		MaxAge:   int(SessionTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

func (s *Sessions) session(id []byte, rec *record, reissue bool) Session {
	h := sha256.Sum256(id)
	return Session{
		Key:     b64.EncodeToString(h[:]),
		CSRF:    b64.EncodeToString(s.mac(append([]byte("csrf:"), id...))),
		Expires: rec.expires,
		Reissue: reissue,
		Value:   b64.EncodeToString(id) + "." + b64.EncodeToString(s.mac(id)),
	}
}

func (s *Sessions) parse(value string) ([]byte, bool) {
	idPart, sigPart, ok := strings.Cut(value, ".")
	if !ok {
		return nil, false
	}
	id, err := b64.DecodeString(idPart)
	if err != nil || len(id) == 0 {
		return nil, false
	}
	sig, err := b64.DecodeString(sigPart)
	if err != nil || !hmac.Equal(sig, s.mac(id)) {
		return nil, false
	}
	return id, true
}

func (s *Sessions) mac(msg []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write(msg)
	return m.Sum(nil)
}

func (s *Sessions) purge(now time.Time) {
	for h, rec := range s.m {
		if !now.Before(rec.expires) {
			delete(s.m, h)
		}
	}
}

// LoadOrCreateKey returns the cookie signing key kept in the secrets store under session.key,
// creating it with 32 random bytes when missing. A legacy key file from the first release is
// imported once and then deleted.
func LoadOrCreateKey(sec secrets.Store, legacyPath string, rnd io.Reader, log *slog.Logger) ([]byte, error) {
	key, err := sec.Get(secrets.SessionKey)
	switch {
	case err == nil:
		if len(key) != keyBytes {
			return nil, fmt.Errorf("stored session key has %d bytes, want %d", len(key), keyBytes)
		}
		removeLegacy(legacyPath, key, log)
		return key, nil
	case !errors.Is(err, secrets.ErrNotFound):
		return nil, err
	}
	legacy, err := os.ReadFile(legacyPath) //nolint:gosec // path is built from the data dir
	switch {
	case err == nil:
		if len(legacy) != keyBytes {
			return nil, fmt.Errorf("session key %s has %d bytes, want %d", legacyPath, len(legacy), keyBytes)
		}
		key = legacy
	case errors.Is(err, os.ErrNotExist):
		key = make([]byte, keyBytes)
		if _, err := io.ReadFull(rnd, key); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	if err := sec.Set(secrets.SessionKey, key); err != nil {
		return nil, err
	}
	removeLegacy(legacyPath, key, log)
	return key, nil
}

// removeLegacy deletes the old key file once the secrets store holds the key.
func removeLegacy(path string, stored []byte, log *slog.Logger) {
	old, err := os.ReadFile(path) //nolint:gosec // path is built from the data dir
	if err != nil {
		return
	}
	if !hmac.Equal(old, stored) {
		log.Warn("legacy session.key differs from the stored key; the stored key wins", "path", path)
	}
	if err := os.Remove(path); err != nil {
		log.Error("remove legacy session.key", "err", err)
		return
	}
	log.Info("moved the session signing key into the secrets store")
}
