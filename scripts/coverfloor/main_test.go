package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeProfile writes a Go coverage profile into dir and returns its path.
func writeProfile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	return p
}

// profLine renders one profile line for file f with n statements hit c times.
// line keeps block keys distinct within a file.
func profLine(f string, line, n, c int) string {
	return f + ":" + strconv.Itoa(line) + ".1," + strconv.Itoa(line) + ".10 " + strconv.Itoa(n) + " " + strconv.Itoa(c) + "\n"
}

func TestRun(t *testing.T) {
	t.Parallel()

	const auth = "perspectiv/internal/auth/service.go"
	const lore = "perspectiv/internal/lore/service.go"
	const rank = "perspectiv/internal/lore/rank/worker.go"

	tests := []struct {
		name     string
		profiles map[string]string
		floors   []string
		wantCode int
		wantOut  []string
	}{
		{
			name:     "package above floor passes",
			profiles: map[string]string{"unit.out": "mode: atomic\n" + profLine(auth, 1, 9, 1) + profLine(auth, 2, 1, 0)},
			floors:   []string{"perspectiv/internal/auth=80"},
			wantCode: 0,
			wantOut:  []string{"perspectiv/internal/auth", "90.0%", "ok"},
		},
		{
			name:     "package exactly at floor passes",
			profiles: map[string]string{"unit.out": "mode: atomic\n" + profLine(auth, 1, 8, 3) + profLine(auth, 2, 2, 0)},
			floors:   []string{"perspectiv/internal/auth=80"},
			wantCode: 0,
			wantOut:  []string{"80.0%"},
		},
		{
			name: "package below floor fails",
			profiles: map[string]string{"unit.out": "mode: atomic\n" +
				profLine(auth, 1, 799, 1) + profLine(auth, 2, 201, 0)},
			floors:   []string{"perspectiv/internal/auth=80"},
			wantCode: 1,
			wantOut:  []string{"perspectiv/internal/auth", "79.9%", "below floor 80.0%"},
		},
		{
			name:     "floored package absent from every profile fails closed",
			profiles: map[string]string{"unit.out": "mode: atomic\n" + profLine(auth, 1, 5, 1)},
			floors:   []string{"perspectiv/internal/auth=80", "perspectiv/internal/vote=80"},
			wantCode: 1,
			wantOut:  []string{"perspectiv/internal/vote", "no coverage data"},
		},
		{
			name: "unit and integration profiles merge per block",
			profiles: map[string]string{
				"unit.out":        "mode: atomic\n" + profLine(lore, 1, 5, 1) + profLine(lore, 2, 5, 0),
				"integration.out": "mode: atomic\n" + profLine(lore, 1, 5, 0) + profLine(lore, 2, 5, 4),
			},
			floors:   []string{"perspectiv/internal/lore=80"},
			wantCode: 0,
			wantOut:  []string{"100.0%"},
		},
		{
			name: "sub-package statements do not count toward the parent",
			profiles: map[string]string{"unit.out": "mode: atomic\n" +
				profLine(lore, 1, 10, 1) + profLine(rank, 1, 90, 0)},
			floors:   []string{"perspectiv/internal/lore=80"},
			wantCode: 0,
			wantOut:  []string{"perspectiv/internal/lore", "100.0%"},
		},
		{
			name:     "package with zero statements has no coverage data",
			profiles: map[string]string{"unit.out": "mode: atomic\n" + profLine(auth, 1, 0, 0)},
			floors:   []string{"perspectiv/internal/auth=80"},
			wantCode: 1,
			wantOut:  []string{"no coverage data"},
		},
		{
			name:     "malformed profile line is a usage error",
			profiles: map[string]string{"unit.out": "mode: atomic\nnot a profile line\n"},
			floors:   []string{"perspectiv/internal/auth=80"},
			wantCode: 2,
			wantOut:  []string{"unit.out:2"},
		},
		{
			name:     "missing mode header is a usage error",
			profiles: map[string]string{"unit.out": profLine(auth, 1, 1, 1)},
			floors:   []string{"perspectiv/internal/auth=80"},
			wantCode: 2,
			wantOut:  []string{"mode"},
		},
		{
			name:     "floor that is not a number is a usage error",
			profiles: map[string]string{"unit.out": "mode: atomic\n" + profLine(auth, 1, 1, 1)},
			floors:   []string{"perspectiv/internal/auth=high"},
			wantCode: 2,
			wantOut:  []string{"perspectiv/internal/auth=high"},
		},
		{
			name:     "floor above one hundred is a usage error",
			profiles: map[string]string{"unit.out": "mode: atomic\n" + profLine(auth, 1, 1, 1)},
			floors:   []string{"perspectiv/internal/auth=101"},
			wantCode: 2,
		},
		{
			name:     "no floors is a usage error",
			profiles: map[string]string{"unit.out": "mode: atomic\n" + profLine(auth, 1, 1, 1)},
			wantCode: 2,
			wantOut:  []string{"usage"},
		},
		{
			name:     "no profiles is a usage error",
			floors:   []string{"perspectiv/internal/auth=80"},
			wantCode: 2,
			wantOut:  []string{"usage"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var args []string
			for _, f := range tc.floors {
				args = append(args, "-floor", f)
			}
			for name, body := range tc.profiles {
				args = append(args, writeProfile(t, dir, name, body))
			}
			var out bytes.Buffer
			code := run(args, &out)
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d; output:\n%s", code, tc.wantCode, out.String())
			}
			for _, want := range tc.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestRunMissingProfileFile(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	code := run([]string{"-floor", "perspectiv/internal/auth=80", filepath.Join(t.TempDir(), "absent.out")}, &out)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; output:\n%s", code, out.String())
	}
}
