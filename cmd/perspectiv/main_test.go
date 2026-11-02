package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"perspectiv/pkg/crypto"
)

// Usage and configuration errors exit before any database is reached:
// DATABASE_URL points nowhere, so touching it would fail differently.
func TestRunExitCodesWithoutDatabase(t *testing.T) {
	t.Parallel()
	unreachable := with(validEnv(), "DATABASE_URL", "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	cases := []struct {
		name       string
		args       []string
		env        map[string]string
		want       int
		wantStderr string
	}{
		{"unknown subcommand", []string{"serve-all"}, unreachable, exitUsage, "usage"},
		{"admin without grant", []string{"admin"}, unreachable, exitUsage, "usage"},
		{"admin unknown verb", []string{"admin", "revoke", "x"}, unreachable, exitUsage, "usage"},
		{"admin grant without username", []string{"admin", "grant"}, unreachable, exitUsage, "usage"},
		{"admin grant two usernames", []string{"admin", "grant", "a", "b"}, unreachable, exitUsage, "usage"},
		{"migrate extra argument", []string{"migrate", "now"}, unreachable, exitUsage, "usage"},
		{"migrate without DATABASE_URL", []string{"migrate"}, with(unreachable, "DATABASE_URL", ""), exitUsage, "DATABASE_URL"},
		{"grant without DATABASE_URL", []string{"admin", "grant", "founder"}, with(unreachable, "DATABASE_URL", ""), exitUsage, "DATABASE_URL"},
		{"serve with bad ROLE", nil, with(unreachable, "ROLE", "both"), exitUsage, "ROLE"},
		{"migrate unreachable database", []string{"migrate"}, unreachable, exitFailure, "migrat"},
		{"grant unreachable database", []string{"admin", "grant", "founder"}, unreachable, exitFailure, "grant"},
		{"keygen extra argument", []string{"keygen", "now"}, unreachable, exitUsage, "usage"},
		{"reset two-factor without a reason", []string{"admin", "reset-two-factor", "founder"}, unreachable, exitUsage, "usage"},
		{"reset two-factor without PUBLIC_BASE_URL", []string{"admin", "reset-two-factor", "founder", "owner confirmed"},
			with(unreachable, "PUBLIC_BASE_URL", ""), exitUsage, "PUBLIC_BASE_URL"},
		{"reset two-factor unreachable database", []string{"admin", "reset-two-factor", "founder", "owner confirmed"},
			unreachable, exitFailure, "two-factor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			got := run(context.Background(), tc.args, env(tc.env), io.Discard, &stderr)
			if got != tc.want {
				t.Errorf("exit = %d, want %d; stderr:\n%s", got, tc.want, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr lacks %q:\n%s", tc.wantStderr, stderr.String())
			}
		})
	}
}

// keygen prints one new age secret key on stdout and nothing else, so
// $(perspectiv keygen) is the key; the key opens what it seals.
func TestKeygen(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"keygen"}, env(nil), &stdout, &stderr); code != exitOK {
		t.Fatalf("keygen exit = %d; stderr:\n%s", code, stderr.String())
	}
	key := strings.TrimSuffix(stdout.String(), "\n")
	if !strings.HasPrefix(key, "AGE-SECRET-KEY-1") || strings.Contains(key, "\n") || stderr.Len() != 0 {
		t.Fatalf("stdout %q, stderr %q; want one key line and nothing else", stdout.String(), stderr.String())
	}
	enc, err := crypto.NewEncryptor(key)
	if err != nil {
		t.Fatalf("the printed key does not load: %v", err)
	}
	sealed, err := enc.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := enc.Decrypt(sealed); err != nil || string(got) != "secret" {
		t.Fatalf("round trip = %q, %v", got, err)
	}
	var again bytes.Buffer
	_ = run(context.Background(), []string{"keygen"}, env(nil), &again, io.Discard)
	if again.String() == stdout.String() {
		t.Fatal("two runs printed the same key")
	}
}
