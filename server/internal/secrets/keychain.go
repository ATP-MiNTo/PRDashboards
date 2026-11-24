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

// Keychain stores secrets in the operating system keychain. Values are base64 encoded because
// the keychain APIs take strings.
type Keychain struct{}

// Get implements Store.
func (Keychain) Get(name string) ([]byte, error) {
	v, err := keyring.Get(Service, name)
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
	if err := keyring.Set(Service, name, base64.StdEncoding.EncodeToString(value)); err != nil {
		return fmt.Errorf("keychain set %s: %w", name, err)
	}
	return nil
}

// Delete implements Store.
func (Keychain) Delete(name string) error {
	err := keyring.Delete(Service, name)
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
