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
)

var magic = [4]byte{'P', 'R', 'D', 'S'}

// Errors returned when opening the encrypted file.
var (
	ErrWrongPassphrase = errors.New("wrong passphrase or damaged secrets file")
	ErrNotSecretsFile  = errors.New("not a secrets file")
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

// File is a Store kept in one AES-256-GCM encrypted file. The key comes from Argon2id over a
// passphrase with a fresh salt, and every write uses a fresh nonce and rewrites the whole file
// atomically with mode 0600.
type File struct {
	path string
	pass []byte
	rand io.Reader
	kdf  kdfParams

	mu sync.Mutex
	m  map[string][]byte
}

// OpenFile opens the secrets file at path, or creates it when missing. A wrong passphrase
// returns ErrWrongPassphrase.
func OpenFile(path string, passphrase []byte, rnd io.Reader) (*File, error) {
	return openFile(path, passphrase, rnd, defaultKDF)
}

func openFile(path string, passphrase []byte, rnd io.Reader, kdf kdfParams) (*File, error) {
	f := &File{path: path, pass: append([]byte(nil), passphrase...), rand: rnd, kdf: kdf, m: map[string][]byte{}}
	data, err := os.ReadFile(path) //nolint:gosec // path is built from the data dir
	switch {
	case err == nil:
		if err := f.decode(data); err != nil {
			return nil, err
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

func (f *File) decode(data []byte) error {
	if len(data) < headerLen+16 || !bytes.Equal(data[:4], magic[:]) {
		return ErrNotSecretsFile
	}
	if data[4] != fileVersion {
		return fmt.Errorf("secrets file version %d is not supported", data[4])
	}
	p := kdfParams{
		time:      binary.BigEndian.Uint32(data[5:9]),
		memoryKiB: binary.BigEndian.Uint32(data[9:13]),
		threads:   data[13],
	}
	if !p.valid() {
		return ErrNotSecretsFile
	}
	header, body := data[:headerLen], data[headerLen:]
	salt := data[14 : 14+saltLen]
	nonce := data[14+saltLen : headerLen]
	gcm, err := newGCM(p.key(f.pass, salt))
	if err != nil {
		return err
	}
	plain, err := gcm.Open(nil, nonce, body, header)
	if err != nil {
		return ErrWrongPassphrase
	}
	var m map[string][]byte
	if err := json.Unmarshal(plain, &m); err != nil {
		return ErrNotSecretsFile
	}
	f.m = m
	return nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// flush encrypts the whole map under a fresh salt and nonce and replaces the file.
func (f *File) flush() error {
	plain, err := json.Marshal(f.m)
	if err != nil {
		return err
	}
	header := make([]byte, headerLen)
	copy(header, magic[:])
	header[4] = fileVersion
	binary.BigEndian.PutUint32(header[5:9], f.kdf.time)
	binary.BigEndian.PutUint32(header[9:13], f.kdf.memoryKiB)
	header[13] = f.kdf.threads
	if _, err := io.ReadFull(f.rand, header[14:]); err != nil {
		return err
	}
	gcm, err := newGCM(f.kdf.key(f.pass, header[14:14+saltLen]))
	if err != nil {
		return err
	}
	out := gcm.Seal(header, header[14+saltLen:headerLen], plain, header)
	return writeAtomic(f.path, out)
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
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
	f.mu.Lock()
	defer f.mu.Unlock()
	prev, had := f.m[name]
	f.m[name] = append([]byte(nil), value...)
	if err := f.flush(); err != nil {
		if had {
			f.m[name] = prev
		} else {
			delete(f.m, name)
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
