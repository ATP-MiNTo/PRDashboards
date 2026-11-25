package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// Service is the keychain service name.
const Service = "prdashboards"

// maxKeychainValue is the longest base64 value Keychain.Set accepts. The macOS backend of
// go-keyring passes the value through a `security -i` command that must stay under 4096 bytes,
// which fails from about 3000 characters on. The keychain only holds the 32-byte vault key, so
// this limit is a guard, not a working limit.
const maxKeychainValue = 2048

// ErrTooLarge is returned by Keychain.Set for a value too big for the OS keychain.
var ErrTooLarge = errors.New("value is too large for the OS keychain")

// keyringAPI is the go-keyring surface in use. Tests replace kr to model keychain limits.
type keyringAPI interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type goKeyring struct{}

func (goKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (goKeyring) Set(service, user, pass string) error     { return keyring.Set(service, user, pass) }
func (goKeyring) Delete(service, user string) error        { return keyring.Delete(service, user) }

var kr keyringAPI = goKeyring{}

// Keychain stores secrets in the operating system keychain. Values are base64 encoded because
// the keychain APIs take strings. Only small items belong here: see maxKeychainValue.
type Keychain struct{}

// Get implements Store.
func (Keychain) Get(name string) ([]byte, error) {
	v, err := kr.Get(Service, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("keychain get %s: %w", name, err)
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("keychain item %s is not valid: %w", name, err)
	}
	return b, nil
}

// Set implements Store.
func (Keychain) Set(name string, value []byte) error {
	enc := base64.StdEncoding.EncodeToString(value)
	if len(enc) > maxKeychainValue {
		return fmt.Errorf("keychain set %s: %w", name, ErrTooLarge)
	}
	if err := kr.Set(Service, name, enc); err != nil {
		return fmt.Errorf("keychain set %s: %w", name, err)
	}
	return nil
}

// Delete implements Store.
func (Keychain) Delete(name string) error {
	err := kr.Delete(Service, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("keychain delete %s: %w", name, err)
	}
	return nil
}

// Probe checks that the keychain works by writing, reading and deleting a throwaway item.
func (k Keychain) Probe() error {
	const name = "probe"
	want := make([]byte, 8)
	if _, err := rand.Read(want); err != nil {
		return err
	}
	if err := k.Set(name, want); err != nil {
		return err
	}
	got, err := k.Get(name)
	if err != nil {
		return err
	}
	if string(got) != string(want) {
		return errors.New("keychain probe read back a different value")
	}
	return k.Delete(name)
}
