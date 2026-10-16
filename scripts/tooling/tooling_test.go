package tooling

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is the repository root, two directories above this file.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// runCmd runs name with args in dir and returns combined output and exit code.
func runCmd(t *testing.T, dir string, env []string, name string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0
	case errors.As(err, &exit):
		return out.String(), exit.ExitCode()
	default:
		t.Fatalf("run %s: %v", name, err)
		return "", -1
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// profile renders a coverage profile in which each package has ten
// one-statement blocks, the first covered of them hit.
func profile(covered map[string]int) string {
	var b strings.Builder
	b.WriteString("mode: atomic\n")
	for pkg, n := range covered {
		for i := 1; i <= 10; i++ {
			hit := 0
			if i <= n {
				hit = 1
			}
			fmt.Fprintf(&b, "%s/x.go:%d.1,%d.20 1 %d\n", pkg, i, i, hit)
		}
	}
	return b.String()
}

func TestMakeCoverageFloors(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	all := map[string]int{
		"perspectiv/internal/auth":       9,
		"perspectiv/internal/lore":       8,
		"perspectiv/internal/vote":       10,
		"perspectiv/internal/moderation": 8,
	}

	tests := []struct {
		name     string
		unit     map[string]int
		integ    map[string]int
		wantCode bool // true: make exits zero
		wantOut  []string
	}{
		{
			name:     "every floored package at or above 80 percent passes",
			unit:     all,
			integ:    map[string]int{},
			wantCode: true,
			wantOut:  []string{"perspectiv/internal/auth", "perspectiv/internal/moderation"},
		},
		{
			name: "one package below 80 percent fails the target",
			unit: map[string]int{
				"perspectiv/internal/auth": 9, "perspectiv/internal/lore": 7,
				"perspectiv/internal/vote": 10, "perspectiv/internal/moderation": 8,
			},
			integ:   map[string]int{},
			wantOut: []string{"perspectiv/internal/lore", "below floor 80.0%"},
		},
		{
			name: "a floored package with no tests fails the target",
			unit: map[string]int{
				"perspectiv/internal/auth": 9, "perspectiv/internal/lore": 8, "perspectiv/internal/vote": 10,
			},
			integ:   map[string]int{},
			wantOut: []string{"perspectiv/internal/moderation", "no coverage data"},
		},
		{
			name: "integration coverage lifts a package the unit tier leaves short",
			unit: map[string]int{
				"perspectiv/internal/auth": 9, "perspectiv/internal/lore": 2,
				"perspectiv/internal/vote": 10, "perspectiv/internal/moderation": 8,
			},
			integ:    map[string]int{"perspectiv/internal/lore": 9},
			wantCode: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			unit := filepath.Join(dir, "unit.out")
			integ := filepath.Join(dir, "integration.out")
			writeFile(t, unit, profile(tc.unit))
			writeFile(t, integ, profile(tc.integ))
			out, code := runCmd(t, root, nil, "make", "coverage-floors", "COVER_PROFILES="+unit+" "+integ)
			if (code == 0) != tc.wantCode {
				t.Fatalf("make coverage-floors exit %d, want success=%v; output:\n%s", code, tc.wantCode, out)
			}
			for _, want := range tc.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestNewMigration(t *testing.T) {
	t.Parallel()
	script := filepath.Join(repoRoot(t), "scripts", "new-migration.sh")

	tests := []struct {
		name     string
		existing []string
		arg      string
		wantFile string
	}{
		{name: "first migration is number one", arg: "create_users", wantFile: "000000000001_create_users.sql"},
		{
			name:     "next number follows the highest existing prefix",
			existing: []string{"000000000001_a.sql", "000000000009_b.sql", "000000000004_c.sql"},
			arg:      "add_places",
			wantFile: "000000000010_add_places.sql",
		},
		{
			name:     "files without a 12-digit prefix are ignored",
			existing: []string{"README.md", "42_short.sql", "000000000002_ok.sql"},
			arg:      "next_one",
			wantFile: "000000000003_next_one.sql",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := migrationsDir(t, tc.existing)
			out, code := runCmd(t, dir, []string{"MIGRATIONS_DIR=" + dir}, script, tc.arg)
			if code != 0 {
				t.Fatalf("exit %d; output:\n%s", code, out)
			}
			body, err := os.ReadFile(filepath.Join(dir, tc.wantFile))
			if err != nil {
				t.Fatalf("want %s: %v; output:\n%s", tc.wantFile, err, out)
			}
			for _, marker := range []string{"-- +goose Up", "-- +goose Down"} {
				if !strings.Contains(string(body), marker) {
					t.Errorf("%s missing %q", tc.wantFile, marker)
				}
			}
		})
	}
}

func TestNewMigrationRejectsBadNames(t *testing.T) {
	t.Parallel()
	script := filepath.Join(repoRoot(t), "scripts", "new-migration.sh")

	for _, name := range []string{"AddPlaces", "add-places", "", "1_starts_with_digit"} {
		t.Run("name "+strconv.Quote(name), func(t *testing.T) {
			t.Parallel()
			dir := migrationsDir(t, nil)
			out, code := runCmd(t, dir, []string{"MIGRATIONS_DIR=" + dir}, script, name)
			if code == 0 {
				t.Fatalf("want failure, got success; output:\n%s", out)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("read dir: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("a rejected name still wrote a file: %v", entries)
			}
		})
	}
}

// migrationsDir returns a temp directory holding the named migration files.
func migrationsDir(t *testing.T, files []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		writeFile(t, filepath.Join(dir, f), "-- +goose Up\n-- +goose Down\n")
	}
	return dir
}

// devCopy lays out a throwaway repo holding dev.sh and the tracked .env.dev,
// so the script's first-run behaviour can be observed without touching the
// real working tree.
func devCopy(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	for _, f := range []string{"scripts/dev.sh", ".env.dev"} {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		writeFile(t, filepath.Join(dir, f), string(b))
	}
	if err := os.Chmod(filepath.Join(dir, "scripts", "dev.sh"), 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return dir
}

// devEnv runs `dev.sh env` in a fresh copy, after writing local as
// .env.dev.local when it is not empty, and returns the copy's root and output.
func devEnv(t *testing.T, local string) (string, string) {
	t.Helper()
	dir := devCopy(t)
	if local != "" {
		writeFile(t, filepath.Join(dir, ".env.dev.local"), local)
	}
	out, code := runCmd(t, dir, nil, filepath.Join(dir, "scripts", "dev.sh"), "env")
	if code != 0 {
		t.Fatalf("dev.sh env exit %d; output:\n%s", code, out)
	}
	return dir, out
}

func TestDevEnvFirstRunCopiesTrackedDefaults(t *testing.T) {
	t.Parallel()
	dir, _ := devEnv(t, "")
	tracked, err := os.ReadFile(filepath.Join(dir, ".env.dev"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := os.ReadFile(filepath.Join(dir, ".env.dev.local"))
	if err != nil {
		t.Fatalf(".env.dev.local not created: %v", err)
	}
	if !bytes.Equal(tracked, local) {
		t.Error(".env.dev.local is not a copy of .env.dev")
	}
}

func TestDevEnvValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		local    string
		want     []string
		dontWant []string
	}{
		{
			name:  "defaults point mail at Mailpit and the database at Postgres 18",
			local: "",
			want: []string{
				"MAIL_PROVIDER=smtp\n",
				"SMTP_ADDR=localhost:1025\n",
				"MAILPIT_URL=http://localhost:8025\n",
				"POSTGRES_IMAGE=postgres:18-alpine\n",
				"MAIL_FROM=Perspectiv <no-reply@perspectiv.local>\n",
			},
		},
		{
			name:  "a value in .env.dev.local overrides .env.dev",
			local: "MAILPIT_URL=http://localhost:18025\n",
			want:  []string{"MAILPIT_URL=http://localhost:18025\n", "ROLE=all\n"},
		},
		{
			name:     "secrets are not printed",
			local:    "",
			dontWant: []string{"JWT_SECRET=", "RESEND_API_KEY=", "POSTGRES_PASSWORD=", "DATABASE_URL="},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, out := devEnv(t, tc.local)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("env output missing %q:\n%s", w, out)
				}
			}
			for _, w := range tc.dontWant {
				if strings.Contains(out, w) {
					t.Errorf("env output contains %q:\n%s", w, out)
				}
			}
		})
	}
}

func TestDevUnknownCommand(t *testing.T) {
	t.Parallel()
	dir := devCopy(t)
	out, code := runCmd(t, dir, nil, filepath.Join(dir, "scripts", "dev.sh"), "frobnicate")
	if code == 0 {
		t.Fatalf("want non-zero exit; output:\n%s", out)
	}
	if !strings.Contains(out, "unknown command: frobnicate") {
		t.Errorf("output does not name the command:\n%s", out)
	}
}
