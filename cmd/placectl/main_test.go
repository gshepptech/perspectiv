package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(name string) string {
	return filepath.Join("..", "..", "internal", "place", "importer", "testdata", name)
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// Usage errors and bad files exit non-zero before any database is touched:
// DATABASE_URL points nowhere reachable, so reaching the database would hang
// or fail differently.
func TestRunExitCodesWithoutDatabase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.ndjson")
	if err := os.WriteFile(bad, []byte("{\"id\": oops}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(dir, "orphan.ndjson")
	if err := os.WriteFile(orphan, []byte(`{"id":"c","subtype":"locality","class":"city","country":"US","names":{"primary":"Lost"},"parent_division_id":"nowhere"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unreachable := map[string]string{"DATABASE_URL": "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"}

	cases := []struct {
		name       string
		args       []string
		env        map[string]string
		want       int
		wantStderr string
	}{
		{"no subcommand", nil, unreachable, exitUsage, "usage"},
		{"unknown subcommand", []string{"export"}, unreachable, exitUsage, "usage"},
		{"missing places flag", []string{"import", "--divisions", fixture("us_divisions.ndjson")}, unreachable, exitUsage, "usage"},
		{"unknown flag", []string{"import", "--bogus"}, unreachable, exitUsage, "bogus"},
		{"no DATABASE_URL", []string{"import", "--divisions", fixture("us_divisions.ndjson"), "--places", fixture("us_places.ndjson")}, nil, exitUsage, "DATABASE_URL"},
		{"missing file", []string{"import", "--divisions", filepath.Join(dir, "absent"), "--places", fixture("us_places.ndjson")}, unreachable, exitFailure, "divisions file"},
		{"unparsable divisions", []string{"import", "--divisions", bad, "--places", fixture("us_places.ndjson")}, unreachable, exitFailure, "divisions file line 1"},
		{"unparsable places", []string{"import", "--divisions", fixture("us_divisions.ndjson"), "--places", bad}, unreachable, exitFailure, "places file line 1"},
		{"unresolved parent", []string{"import", "--divisions", orphan, "--places", fixture("us_places.ndjson")}, unreachable, exitFailure, "parent not resolved"},
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
				t.Errorf("stderr does not mention %q:\n%s", tc.wantStderr, stderr.String())
			}
		})
	}
}
