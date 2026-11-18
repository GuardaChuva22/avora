// Package vault seals credentials (M7): AES-256-GCM with a key that lives in
// a file outside the database (/etc/avora/vault.key on the VPS). A sealed
// value is bound to what it is for (the connector kind), so one cannot be
// swapped for another.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Vault struct{ aead cipher.AEAD }

// New uses a 32-byte key.
func New(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("the vault key must have 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

// Open reads a key written as 64 hex characters.
func Open(path string) (*Vault, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, fmt.Errorf("%s: not a hex key", path)
	}
	return New(key)
}

// Create writes a new random key to path (0600, parent folders 0700) and
// fails if the file already exists.
func Create(path string) error {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Random is a vault whose key dies with the process (development only).
func Random() *Vault {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	v, _ := New(key)
	return v
}

// Seal encrypts plain for purpose.
func (v *Vault) Seal(plain []byte, purpose string) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plain, []byte(purpose)), nil
}

// ErrSealed means the value was sealed with another key or for another purpose.
var ErrSealed = errors.New("credential cannot be opened (other key or other purpose)")

func (v *Vault) Open(sealed []byte, purpose string) ([]byte, error) {
	n := v.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrSealed
	}
	plain, err := v.aead.Open(nil, sealed[:n], sealed[n:], []byte(purpose))
	if err != nil {
		return nil, ErrSealed
	}
	return plain, nil
}
