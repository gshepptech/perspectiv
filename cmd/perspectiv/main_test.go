package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			got := run(context.Background(), tc.args, env(tc.env), &stderr)
			if got != tc.want {
				t.Errorf("exit = %d, want %d; stderr:\n%s", got, tc.want, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr lacks %q:\n%s", tc.wantStderr, stderr.String())
			}
		})
	}
}
