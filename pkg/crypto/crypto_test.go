package crypto_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"perspectiv/pkg/crypto"
)

func mustKey(t *testing.T) string {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return k
}

func mustEncryptor(t *testing.T, keys ...string) *crypto.Encryptor {
	t.Helper()
	e, err := crypto.NewEncryptor(strings.Join(keys, "\n"))
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	return e
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{name: "text", plaintext: []byte("a secret")},
		{name: "empty", plaintext: []byte{}},
		{name: "binary", plaintext: []byte{0, 1, 2, 255}},
	}
	e := mustEncryptor(t, mustKey(t))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := e.Encrypt(tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if s.Algorithm != crypto.AlgorithmAgeX25519 || s.KeyID != e.KeyID() {
				t.Fatalf("markers = (%q, %q), want (%q, %q)", s.KeyID, s.Algorithm, e.KeyID(), crypto.AlgorithmAgeX25519)
			}
			if len(tc.plaintext) > 0 && strings.Contains(string(s.Ciphertext), string(tc.plaintext)) {
				t.Fatal("ciphertext contains the plaintext")
			}
			got, err := e.Decrypt(s)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if string(got) != string(tc.plaintext) {
				t.Fatalf("Decrypt = %q, want %q", got, tc.plaintext)
			}
		})
	}
}

func TestRotation(t *testing.T) {
	t.Parallel()
	oldKey, newKey := mustKey(t), mustKey(t)
	before := mustEncryptor(t, oldKey)
	sealedOld, err := before.Encrypt([]byte("written before rotation"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	after := mustEncryptor(t, newKey, "# retiring", oldKey)
	if after.KeyID() == before.KeyID() {
		t.Fatal("current key id did not change after rotation")
	}
	got, err := after.Decrypt(sealedOld)
	if err != nil || string(got) != "written before rotation" {
		t.Fatalf("Decrypt old row = %q, %v", got, err)
	}
	sealedNew, err := after.Encrypt([]byte("x"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if sealedNew.KeyID != after.KeyID() {
		t.Fatalf("new write key id = %q, want current %q", sealedNew.KeyID, after.KeyID())
	}
	if _, err := before.Decrypt(sealedNew); !errors.Is(err, crypto.ErrUnknownKey) {
		t.Fatalf("old encryptor on new row: err = %v, want ErrUnknownKey", err)
	}
}

func TestDecryptErrors(t *testing.T) {
	t.Parallel()
	e := mustEncryptor(t, mustKey(t))
	good, err := e.Encrypt([]byte("payload"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	tampered := append([]byte(nil), good.Ciphertext...)
	tampered[len(tampered)-1] ^= 0xff

	tests := []struct {
		name    string
		sealed  crypto.Sealed
		wantErr error
	}{
		{name: "unknown algorithm", sealed: crypto.Sealed{KeyID: good.KeyID, Algorithm: "aes-gcm-v9", Ciphertext: good.Ciphertext}, wantErr: crypto.ErrUnknownAlgorithm},
		{name: "empty algorithm", sealed: crypto.Sealed{KeyID: good.KeyID, Ciphertext: good.Ciphertext}, wantErr: crypto.ErrUnknownAlgorithm},
		{name: "unknown key", sealed: crypto.Sealed{KeyID: "age1:0000", Algorithm: good.Algorithm, Ciphertext: good.Ciphertext}, wantErr: crypto.ErrUnknownKey},
		{name: "tampered ciphertext", sealed: crypto.Sealed{KeyID: good.KeyID, Algorithm: good.Algorithm, Ciphertext: tampered}},
		{name: "garbage ciphertext", sealed: crypto.Sealed{KeyID: good.KeyID, Algorithm: good.Algorithm, Ciphertext: []byte("not age")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := e.Decrypt(tc.sealed)
			if err == nil {
				t.Fatalf("Decrypt = %q, want an error", got)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestConstructors(t *testing.T) {
	t.Parallel()
	key := mustKey(t)
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if err := os.WriteFile(keyFile, []byte("# current\n"+key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		build   func() (*crypto.Encryptor, error)
		wantErr error
		wantAny bool
	}{
		{name: "from string", build: func() (*crypto.Encryptor, error) { return crypto.NewEncryptor(key) }},
		{name: "from file", build: func() (*crypto.Encryptor, error) { return crypto.NewEncryptorFromFile(keyFile) }},
		{name: "missing file", build: func() (*crypto.Encryptor, error) { return crypto.NewEncryptorFromFile(filepath.Join(dir, "nope")) }, wantErr: os.ErrNotExist},
		{name: "empty string", build: func() (*crypto.Encryptor, error) { return crypto.NewEncryptor(" \n# only a comment\n") }, wantErr: crypto.ErrNoKey},
		{name: "malformed key", build: func() (*crypto.Encryptor, error) { return crypto.NewEncryptor("AGE-SECRET-KEY-1NOTAKEY") }, wantAny: true},
		{name: "unset env", build: func() (*crypto.Encryptor, error) { return crypto.NewEncryptorFromEnv("PERSPECTIV_TEST_UNSET_KEY_VAR") }, wantErr: crypto.ErrNoKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, err := tc.build()
			switch {
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			case tc.wantAny && err == nil:
				t.Fatal("want an error")
			case tc.wantErr == nil && !tc.wantAny && (err != nil || e.KeyID() == ""):
				t.Fatalf("err = %v, want a usable encryptor", err)
			}
		})
	}
}

// TestFromEnv cannot run in parallel: it sets a process environment variable.
func TestFromEnv(t *testing.T) {
	key := mustKey(t)
	t.Setenv("PERSPECTIV_TEST_KEY", key)
	e, err := crypto.NewEncryptorFromEnv("PERSPECTIV_TEST_KEY")
	if err != nil {
		t.Fatalf("NewEncryptorFromEnv: %v", err)
	}
	want := mustEncryptor(t, key)
	if e.KeyID() != want.KeyID() {
		t.Fatalf("key id = %q, want %q", e.KeyID(), want.KeyID())
	}
}
