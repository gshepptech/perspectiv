// Package crypto owns all encryption at rest. Callers store the three values
// of a Sealed (key id, algorithm, ciphertext) in their own columns and hand
// them back to Decrypt; they never call age directly and never interpret the
// ciphertext.
//
// Key rotation: an Encryptor holds every active key. The first key is the
// current one and every Encrypt uses it; Decrypt selects a key by the row's
// key id, so rows written under an older key stay readable while a background
// job re-encrypts them. An old key is retired only once no row carries its id.
package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

// AlgorithmAgeX25519 marks a ciphertext as an age file encrypted to one
// X25519 recipient (X25519 key agreement, ChaCha20-Poly1305 payload). It is
// the version marker stored with every blob.
const AlgorithmAgeX25519 = "age-x25519-v1"

// Typed errors. Decrypt returns them wrapped; test with errors.Is.
var (
	// ErrUnknownAlgorithm: the blob's algorithm marker is not one this
	// package writes. It is never decrypted on a best-effort basis.
	ErrUnknownAlgorithm = errors.New("crypto: unknown algorithm")
	// ErrUnknownKey: no active key has the blob's key id.
	ErrUnknownKey = errors.New("crypto: unknown key id")
	// ErrNoKey: a constructor was given no key.
	ErrNoKey = errors.New("crypto: no key")
)

// Sealed is an encrypted value and the markers needed to decrypt it.
type Sealed struct {
	KeyID      string
	Algorithm  string
	Ciphertext []byte
}

// Encryptor encrypts with the current key and decrypts with any active key.
type Encryptor struct {
	currentID string
	keys      map[string]*age.X25519Identity
}

// NewEncryptor builds an Encryptor from age secret keys
// ("AGE-SECRET-KEY-1..."), one per line; blank lines and lines starting with
// "#" are ignored. The first key is the current one.
func NewEncryptor(secretKey string) (*Encryptor, error) {
	e := &Encryptor{keys: map[string]*age.X25519Identity{}}
	for line := range strings.Lines(secretKey) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, err := age.ParseX25519Identity(line)
		if err != nil {
			return nil, fmt.Errorf("crypto: parse key: %w", err)
		}
		kid := keyID(id.Recipient())
		if e.currentID == "" {
			e.currentID = kid
		}
		e.keys[kid] = id
	}
	if e.currentID == "" {
		return nil, ErrNoKey
	}
	return e, nil
}

// NewEncryptorFromFile reads NewEncryptor's key format from path.
func NewEncryptorFromFile(path string) (*Encryptor, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the key path is operator config, not request input.
	if err != nil {
		return nil, fmt.Errorf("crypto: read key file: %w", err)
	}
	return NewEncryptor(string(b))
}

// NewEncryptorFromEnv reads NewEncryptor's key format from the environment
// variable envVar. An unset or empty variable is ErrNoKey.
func NewEncryptorFromEnv(envVar string) (*Encryptor, error) {
	v := os.Getenv(envVar)
	if strings.TrimSpace(v) == "" {
		return nil, fmt.Errorf("crypto: %s is empty: %w", envVar, ErrNoKey)
	}
	return NewEncryptor(v)
}

// GenerateKey returns a new age X25519 secret key.
func GenerateKey() (string, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", fmt.Errorf("crypto: generate key: %w", err)
	}
	return id.String(), nil
}

// KeyID returns the id of the current key, the one Encrypt stamps.
func (e *Encryptor) KeyID() string { return e.currentID }

// Encrypt seals plaintext under the current key.
func (e *Encryptor) Encrypt(plaintext []byte) (Sealed, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, e.keys[e.currentID].Recipient())
	if err != nil {
		return Sealed{}, fmt.Errorf("crypto: encrypt: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return Sealed{}, fmt.Errorf("crypto: encrypt: %w", err)
	}
	if err := w.Close(); err != nil {
		return Sealed{}, fmt.Errorf("crypto: encrypt: %w", err)
	}
	return Sealed{KeyID: e.currentID, Algorithm: AlgorithmAgeX25519, Ciphertext: buf.Bytes()}, nil
}

// Decrypt opens s with the key its KeyID names. An unknown algorithm or key
// id is a typed error, as is a ciphertext that fails authentication.
func (e *Encryptor) Decrypt(s Sealed) ([]byte, error) {
	if s.Algorithm != AlgorithmAgeX25519 {
		return nil, fmt.Errorf("crypto: decrypt algorithm %q: %w", s.Algorithm, ErrUnknownAlgorithm)
	}
	id, ok := e.keys[s.KeyID]
	if !ok {
		return nil, fmt.Errorf("crypto: decrypt key %q: %w", s.KeyID, ErrUnknownKey)
	}
	r, err := age.Decrypt(bytes.NewReader(s.Ciphertext), id)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return out, nil
}

// keyID derives a stable, non-secret id from a key's public half.
func keyID(r *age.X25519Recipient) string {
	sum := sha256.Sum256([]byte(r.String()))
	return "age1:" + hex.EncodeToString(sum[:8])
}
