package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	fileVersion = 1
	saltLen     = 16
	nonceLen    = 12
	keyLen      = 32
	// header: magic, version, argon2 time, memory (KiB) and threads, salt, nonce.
	headerLen = 4 + 1 + 4 + 4 + 1 + saltLen + nonceLen

	vaultVersion = 1
	// vault header: magic, version, nonce.
	vaultHeaderLen = 4 + 1 + nonceLen
)

var (
	magic      = [4]byte{'P', 'R', 'D', 'S'}
	vaultMagic = [4]byte{'P', 'R', 'D', 'V'}
)

// Errors returned when opening the encrypted file.
var (
	ErrWrongPassphrase = errors.New("wrong passphrase or damaged secrets file")
	ErrNotSecretsFile  = errors.New("not a secrets file")
	ErrWrongVaultKey   = errors.New("the vault key does not match or the vault file is damaged")
	ErrNotVaultFile    = errors.New("not a vault file")
)

type kdfParams struct {
	time, memoryKiB uint32
	threads         uint8
}

var defaultKDF = kdfParams{time: 3, memoryKiB: 64 * 1024, threads: 4}

func (p kdfParams) valid() bool {
	return p.time >= 1 && p.time <= 16 && p.memoryKiB >= 8 && p.memoryKiB <= 1<<20 && p.threads >= 1 && p.threads <= 32
}

func (p kdfParams) key(pass, salt []byte) []byte {
	return argon2.IDKey(pass, salt, p.time, p.memoryKiB, p.threads, keyLen)
}

// File is a Store kept in one AES-256-GCM encrypted file. A codec supplies the key: either
// Argon2id over a passphrase with a fresh salt (secrets.enc) or a raw 32-byte key (vault.enc).
// Every write uses a fresh nonce and rewrites the whole file atomically with mode 0600.
type File struct {
	path  string
	codec codec
	rand  io.Reader

	mu sync.Mutex
	m  map[string][]byte
}

// codec seals and opens the file contents. The header it writes is the GCM additional data and
// ends with the nonce.
type codec interface {
	seal(plain []byte, rnd io.Reader) ([]byte, error)
	open(data []byte) ([]byte, error)
}

// passCodec derives the key from a passphrase with Argon2id and a fresh salt per write.
type passCodec struct {
	pass []byte
	kdf  kdfParams
}

func (c passCodec) seal(plain []byte, rnd io.Reader) ([]byte, error) {
	header := make([]byte, headerLen)
	copy(header, magic[:])
	header[4] = fileVersion
	binary.BigEndian.PutUint32(header[5:9], c.kdf.time)
	binary.BigEndian.PutUint32(header[9:13], c.kdf.memoryKiB)
	header[13] = c.kdf.threads
	if _, err := io.ReadFull(rnd, header[14:]); err != nil {
		return nil, err
	}
	gcm, err := newGCM(c.kdf.key(c.pass, header[14:14+saltLen]))
	if err != nil {
		return nil, err
	}
	return gcm.Seal(header, header[14+saltLen:headerLen], plain, header), nil
}

func (c passCodec) open(data []byte) ([]byte, error) {
	if len(data) < headerLen+16 || !bytes.Equal(data[:4], magic[:]) {
		return nil, ErrNotSecretsFile
	}
	if data[4] != fileVersion {
		return nil, fmt.Errorf("secrets file version %d is not supported", data[4])
	}
	p := kdfParams{
		time:      binary.BigEndian.Uint32(data[5:9]),
		memoryKiB: binary.BigEndian.Uint32(data[9:13]),
		threads:   data[13],
	}
	if !p.valid() {
		return nil, ErrNotSecretsFile
	}
	header, body := data[:headerLen], data[headerLen:]
	salt := data[14 : 14+saltLen]
	nonce := data[14+saltLen : headerLen]
	gcm, err := newGCM(p.key(c.pass, salt))
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, body, header)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	return plain, nil
}

// keyCodec seals with a raw 32-byte key. Its header is a different magic, a version and the
// nonce, so a passphrase file and a key file are never taken for each other.
type keyCodec struct{ gcm cipher.AEAD }

func newKeyCodec(key []byte) (keyCodec, error) {
	if len(key) != keyLen {
		return keyCodec{}, fmt.Errorf("vault key must be %d bytes, got %d", keyLen, len(key))
	}
	gcm, err := newGCM(key)
	return keyCodec{gcm: gcm}, err
}

func (c keyCodec) seal(plain []byte, rnd io.Reader) ([]byte, error) {
	header := make([]byte, vaultHeaderLen, vaultHeaderLen+len(plain)+c.gcm.Overhead())
	copy(header, vaultMagic[:])
	header[4] = vaultVersion
	nonce := header[5:]
	if _, err := io.ReadFull(rnd, nonce); err != nil {
		return nil, err
	}
	return c.gcm.Seal(header, nonce, plain, header), nil
}

func (c keyCodec) open(data []byte) ([]byte, error) {
	if len(data) < vaultHeaderLen+c.gcm.Overhead() || !bytes.Equal(data[:4], vaultMagic[:]) {
		return nil, ErrNotVaultFile
	}
	if data[4] != vaultVersion {
		return nil, fmt.Errorf("vault file version %d is not supported", data[4])
	}
	header, body := data[:vaultHeaderLen], data[vaultHeaderLen:]
	plain, err := c.gcm.Open(nil, header[5:], body, header)
	if err != nil {
		return nil, ErrWrongVaultKey
	}
	return plain, nil
}

// OpenFile opens the secrets file at path, or creates it when missing. A wrong passphrase
// returns ErrWrongPassphrase.
func OpenFile(path string, passphrase []byte, rnd io.Reader) (*File, error) {
	return openFile(path, passphrase, rnd, defaultKDF)
}

func openFile(path string, passphrase []byte, rnd io.Reader, kdf kdfParams) (*File, error) {
	return openCodec(path, passCodec{pass: append([]byte(nil), passphrase...), kdf: kdf}, rnd)
}

// OpenVault opens the vault file at path sealed with the raw 32-byte key, or creates it when
// missing. A wrong key or a damaged file returns ErrWrongVaultKey, a file of another kind
// ErrNotVaultFile. The file is never replaced when it cannot be opened.
func OpenVault(path string, key []byte, rnd io.Reader) (*File, error) {
	c, err := newKeyCodec(key)
	if err != nil {
		return nil, err
	}
	return openCodec(path, c, rnd)
}

func openCodec(path string, c codec, rnd io.Reader) (*File, error) {
	f := &File{path: path, codec: c, rand: rnd, m: map[string][]byte{}}
	data, err := os.ReadFile(path) //nolint:gosec // path is built from the data dir
	switch {
	case err == nil:
		plain, err := c.open(data)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(plain, &f.m); err != nil {
			return nil, ErrNotSecretsFile
		}
		if f.m == nil {
			f.m = map[string][]byte{}
		}
	case errors.Is(err, os.ErrNotExist):
		if err := f.flush(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("read secrets file: %w", err)
	}
	return f, nil
}

// Exists reports whether a secrets file is present at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// flush encrypts the whole map under a fresh nonce and replaces the file.
func (f *File) flush() error {
	plain, err := json.Marshal(f.m)
	if err != nil {
		return err
	}
	out, err := f.codec.seal(plain, f.rand)
	if err != nil {
		return err
	}
	return writeAtomic(f.path, out)
}

// writeAtomic writes data to a 0600 temp file in the same directory, syncs it and renames it
// over path.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

// Get implements Store.
func (f *File) Get(name string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[name]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), v...), nil
}

// Set implements Store.
func (f *File) Set(name string, value []byte) error {
	return f.setMany(map[string][]byte{name: value})
}

// setMany stores all items with one write of the file. When the write fails nothing changes.
func (f *File) setMany(items map[string][]byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	type old struct {
		v   []byte
		had bool
	}
	prev := make(map[string]old, len(items))
	for name, value := range items {
		v, had := f.m[name]
		prev[name] = old{v, had}
		f.m[name] = append([]byte(nil), value...)
	}
	if err := f.flush(); err != nil {
		for name, p := range prev {
			if p.had {
				f.m[name] = p.v
			} else {
				delete(f.m, name)
			}
		}
		return fmt.Errorf("write secrets file: %w", err)
	}
	return nil
}

// Delete implements Store.
func (f *File) Delete(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	prev, ok := f.m[name]
	if !ok {
		return ErrNotFound
	}
	delete(f.m, name)
	if err := f.flush(); err != nil {
		f.m[name] = prev
		return fmt.Errorf("write secrets file: %w", err)
	}
	return nil
}
