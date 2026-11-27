package auth

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
)

// MaxGitHubTokens is the most GitHub tokens that can be connected at once.
const MaxGitHubTokens = 10

// MaxTokenName is the longest name of a token, in characters.
const MaxTokenName = 40

// Errors of the list of GitHub tokens.
var (
	// ErrTokenLimit means MaxGitHubTokens tokens are stored already.
	ErrTokenLimit = errors.New("the limit of GitHub tokens is reached")
	// ErrTokenDuplicate means the same token string is stored already (see DuplicateError).
	ErrTokenDuplicate = errors.New("this GitHub token is already stored")
	// ErrTokenName means a name is empty, too long or has control characters.
	ErrTokenName = errors.New("invalid token name")
	// ErrTokenNameTaken means another token has the name, ignoring case.
	ErrTokenNameTaken = errors.New("another GitHub token has this name")
	// ErrTokenNotFound means no stored token has the id.
	ErrTokenNotFound = errors.New("no such GitHub token")
)

// DuplicateError says which stored token a new one repeats. It matches ErrTokenDuplicate.
type DuplicateError struct{ Name string }

func (e *DuplicateError) Error() string { return "this GitHub token is already stored as " + e.Name }

// Is makes errors.Is(err, ErrTokenDuplicate) work.
func (e *DuplicateError) Is(target error) bool { return target == ErrTokenDuplicate }

// ValidName trims a token name and checks it: 1 to MaxTokenName characters, no control
// characters. Uniqueness is checked against the list.
func ValidName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > MaxTokenName || !utf8.ValidString(name) ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", ErrTokenName
	}
	return name, nil
}

func nameTaken(list []GitHubToken, name, exceptID string) bool {
	return slices.ContainsFunc(list, func(t GitHubToken) bool {
		return t.ID != exceptID && strings.EqualFold(t.Name, name)
	})
}

// defaultName is "GitHub App (login)", "OAuth (login)" or "Token (login)", shortened to fit and
// made unique with a number.
func defaultName(method, login string, list []GitHubToken) string {
	prefix := "Token"
	switch method {
	case MethodApp:
		prefix = "GitHub App"
	case MethodOAuth:
		prefix = "OAuth"
	}
	base := prefix
	if login != "" {
		base += " (" + login + ")"
	}
	cut := func(s string, n int) string {
		r := []rune(s)
		if len(r) <= n {
			return s
		}
		return string(r[:n-1]) + "…"
	}
	name := cut(base, MaxTokenName)
	for n := 2; nameTaken(list, name, ""); n++ {
		suffix := " " + strconv.Itoa(n)
		name = cut(base, MaxTokenName-len(suffix)) + suffix
	}
	return name
}

// admit checks that a new token fits: the limit, not stored yet, and a free name when one is given.
func admit(list []GitHubToken, token, name string) error {
	if len(list) >= MaxGitHubTokens {
		return ErrTokenLimit
	}
	for _, t := range list {
		if subtle.ConstantTimeCompare([]byte(t.Token), []byte(token)) == 1 {
			return &DuplicateError{Name: t.Name}
		}
	}
	if name != "" && nameTaken(list, name, "") {
		return ErrTokenNameTaken
	}
	return nil
}

// findToken returns the token with the given id and its position. An empty id is the primary
// (first) token. No tokens is ErrNotConnected, an unknown id ErrTokenNotFound.
func findToken(list []GitHubToken, id string) (GitHubToken, int, error) {
	if len(list) == 0 {
		return GitHubToken{}, -1, ErrNotConnected
	}
	if id == "" {
		return list[0], 0, nil
	}
	if at := slices.IndexFunc(list, func(t GitHubToken) bool { return t.ID == id }); at >= 0 {
		return list[at], at, nil
	}
	return GitHubToken{}, -1, ErrTokenNotFound
}

func (o *OAuth) githubToken(id string) (GitHubToken, error) {
	list, err := o.tokens()
	if err != nil {
		return GitHubToken{}, err
	}
	t, _, err := findToken(list, id)
	return t, err
}

// newID returns 8 random hex digits that no entry of list has.
func (o *OAuth) newID(list []GitHubToken) (string, error) {
	for range 16 {
		b := make([]byte, 4)
		if _, err := o.cfg.Rand.Read(b); err != nil {
			return "", err
		}
		id := hex.EncodeToString(b)
		if !slices.ContainsFunc(list, func(t GitHubToken) bool { return t.ID == id }) {
			return id, nil
		}
	}
	return "", errors.New("no free token id")
}

// readTokens reads the list item. found is false when the item does not exist.
func (o *OAuth) readTokens() (list []GitHubToken, found bool, err error) {
	list, err = loadJSON[[]GitHubToken](o.cfg.Secrets, secrets.GitHubTokens)
	if errors.Is(err, ErrNotConnected) {
		return nil, false, nil
	}
	return list, err == nil, err
}

// tokens returns the stored GitHub tokens, primary first, moving the legacy item github.token
// into the list when only that exists.
func (o *OAuth) tokens() ([]GitHubToken, error) {
	if list, found, err := o.readTokens(); found || err != nil {
		return list, err
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	return o.tokensLocked()
}

// tokensLocked is tokens with tokenMu held by the caller.
func (o *OAuth) tokensLocked() ([]GitHubToken, error) {
	list, found, err := o.readTokens()
	if found || err != nil {
		return list, err
	}
	legacy, err := loadJSON[GitHubToken](o.cfg.Secrets, secrets.GitHubToken)
	if errors.Is(err, ErrNotConnected) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// The legacy token becomes the primary entry with everything it had: method, refresh state,
	// expiry and the reconnect flag.
	if legacy.ID, err = o.newID(nil); err != nil {
		return nil, err
	}
	legacy.FineGrained = legacy.Method == MethodPAT && legacy.Warning == FineGrainedWarning
	legacy.Name = defaultName(legacy.Method, legacy.Login, nil)
	list = []GitHubToken{legacy}
	if err := saveJSON(o.cfg.Secrets, secrets.GitHubTokens, list); err != nil {
		return nil, fmt.Errorf("move the github token into the list: %w", err)
	}
	// A failed delete leaves a copy that is never read again while the list exists, and that
	// saveTokens removes when the list becomes empty.
	_ = o.cfg.Secrets.Delete(secrets.GitHubToken)
	return list, nil
}

// MigrateGitHub moves the legacy single GitHub token into the list now. It is called at start, and
// every read does the same lazily.
func (o *OAuth) MigrateGitHub() error {
	_, err := o.tokens()
	return err
}

// saveTokens stores the list in one write, or removes the items when it is empty. It must run
// with tokenMu held.
func (o *OAuth) saveTokens(list []GitHubToken) error {
	if len(list) == 0 {
		for _, name := range []string{secrets.GitHubTokens, secrets.GitHubToken} {
			if err := o.cfg.Secrets.Delete(name); err != nil && !errors.Is(err, secrets.ErrNotFound) {
				return err
			}
		}
		return nil
	}
	return saveJSON(o.cfg.Secrets, secrets.GitHubTokens, list)
}

// GitHubTokens lists the stored GitHub tokens, primary first. The entries carry the secrets: the
// caller must not pass them on.
func (o *OAuth) GitHubTokens() ([]GitHubToken, error) {
	list, err := o.tokens()
	return slices.Clone(list), err
}

// saveSignIn stores the token of a GitHub App sign-in. There is at most one App or OAuth entry:
// a new sign-in replaces it in place, keeping its id, its position and its name (a name that was
// the default follows the new login). Otherwise the entry is added, which the limit can refuse.
func (o *OAuth) saveSignIn(rec GitHubToken) error {
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	list, err := o.tokensLocked()
	if err != nil {
		return err
	}
	at := slices.IndexFunc(list, func(t GitHubToken) bool { return t.Method == MethodApp || t.Method == MethodOAuth })
	if at < 0 {
		if len(list) >= MaxGitHubTokens {
			return ErrTokenLimit
		}
		if rec.ID, err = o.newID(list); err != nil {
			return err
		}
		rec.Name = defaultName(rec.Method, rec.Login, list)
		return o.saveTokens(append(list, rec))
	}
	old := list[at]
	rec.ID, rec.Name = old.ID, old.Name
	if rec.Name == defaultName(old.Method, old.Login, slices.Delete(slices.Clone(list), at, at+1)) {
		rec.Name = defaultName(rec.Method, rec.Login, slices.Delete(slices.Clone(list), at, at+1))
	}
	list[at] = rec
	return o.saveTokens(list)
}

// RenameGitHubToken gives the token a new name.
func (o *OAuth) RenameGitHubToken(id, name string) (GitHubToken, error) {
	name, err := ValidName(name)
	if err != nil {
		return GitHubToken{}, err
	}
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	list, err := o.tokensLocked()
	if err != nil {
		return GitHubToken{}, err
	}
	if id == "" {
		return GitHubToken{}, ErrTokenNotFound
	}
	t, at, err := findToken(list, id)
	if errors.Is(err, ErrNotConnected) {
		return GitHubToken{}, ErrTokenNotFound
	}
	if err != nil {
		return GitHubToken{}, err
	}
	if nameTaken(list, name, t.ID) {
		return GitHubToken{}, ErrTokenNameTaken
	}
	list[at].Name = name
	if err := o.saveTokens(list); err != nil {
		return GitHubToken{}, err
	}
	return list[at], nil
}

// RemoveGitHubToken removes one token and returns how many are left. Removing the last one
// disconnects GitHub.
func (o *OAuth) RemoveGitHubToken(id string) (int, error) {
	o.tokenMu.Lock()
	defer o.tokenMu.Unlock()
	list, err := o.tokensLocked()
	if err != nil {
		return 0, err
	}
	at := slices.IndexFunc(list, func(t GitHubToken) bool { return id != "" && t.ID == id })
	if at < 0 {
		return len(list), ErrTokenNotFound
	}
	list = slices.Delete(list, at, at+1)
	return len(list), o.saveTokens(list)
}
