// Package auth issues launch tokens and signed session cookies for the local browser.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
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

// Sessions creates and verifies sessions. State is in memory only.
type Sessions struct {
	key   []byte
	clock func() time.Time
	rand  io.Reader

	mu sync.Mutex
	m  map[[sha256.Size]byte]*record
}

// NewSessions returns a session store signing cookies with key.
func NewSessions(key []byte, clock func() time.Time, rnd io.Reader) *Sessions {
	return &Sessions{key: key, clock: clock, rand: rnd, m: make(map[[sha256.Size]byte]*record)}
}

// Create starts a session.
func (s *Sessions) Create() (Session, error) {
	id := make([]byte, 32)
	if _, err := io.ReadFull(s.rand, id); err != nil {
		return Session{}, err
	}
	now := s.clock()
	s.mu.Lock()
	s.purge(now)
	rec := &record{lastIssued: now, expires: now.Add(SessionTTL)}
	s.m[sha256.Sum256(id)] = rec
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
	s.mu.Lock()
	defer s.mu.Unlock()
	h := sha256.Sum256(id)
	rec, ok := s.m[h]
	if !ok {
		return Session{}, false
	}
	if !now.Before(rec.expires) {
		delete(s.m, h)
		return Session{}, false
	}
	reissue := now.Sub(rec.lastIssued) >= ReissueAfter
	if reissue {
		rec.lastIssued = now
		rec.expires = now.Add(SessionTTL)
	}
	return s.session(id, rec, reissue), true
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

// LoadOrCreateKey reads the cookie signing key at path, creating it with 32 random bytes and
// mode 0600 when missing.
func LoadOrCreateKey(path string, rnd io.Reader) ([]byte, error) {
	key, err := os.ReadFile(path) //nolint:gosec // path is built from the data dir
	switch {
	case err == nil:
		if len(key) != keyBytes {
			return nil, fmt.Errorf("session key %s has %d bytes, want %d", path, len(key), keyBytes)
		}
		return key, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	key = make([]byte, keyBytes)
	if _, err := io.ReadFull(rnd, key); err != nil {
		return nil, err
	}
	if err := WriteFileAtomic(path, key); err != nil {
		return nil, err
	}
	return key, nil
}
