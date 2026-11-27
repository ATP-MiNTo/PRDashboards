// Package secrets stores tokens and client secrets in the OS keychain or an encrypted file.
package secrets

import (
	"errors"
	"sync"
)

// Item names kept in a Store.
const (
	GitHubClient = "github.oauth.client"
	// GitHubToken is the single credential of versions before several tokens. The first start
	// that reads the list moves it into GitHubTokens and deletes it.
	GitHubToken = "github.token"
	// GitHubTokens is the JSON array of the GitHub credentials, primary first.
	GitHubTokens    = "github.tokens"
	AtlassianClient = "atlassian.oauth.client"
	AtlassianToken  = "atlassian.token"
	SessionKey      = "session.key"
)

// ErrNotFound is returned by Get and Delete for an item that does not exist.
var ErrNotFound = errors.New("secret not found")

// Store is a named secret store. Implementations are safe for concurrent use.
type Store interface {
	Get(name string) ([]byte, error)
	Set(name string, value []byte) error
	Delete(name string) error
}

// Memory is an in-memory Store for tests and the e2e build.
type Memory struct {
	mu sync.Mutex
	m  map[string][]byte
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{m: make(map[string][]byte)} }

// Get implements Store.
func (s *Memory) Get(name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[name]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), v...), nil
}

// Set implements Store.
func (s *Memory) Set(name string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[name] = append([]byte(nil), value...)
	return nil
}

// Delete implements Store.
func (s *Memory) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[name]; !ok {
		return ErrNotFound
	}
	delete(s.m, name)
	return nil
}
