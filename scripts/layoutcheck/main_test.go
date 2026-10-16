package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule lays out a throwaway module named perspectiv under a temp dir.
// files maps a slash path to Go source; a value of "" gets a bare package
// clause named after its directory.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module perspectiv\n\ngo 1.27\n")
	for name, src := range files {
		if src == "" {
			src = "package " + filepath.Base(filepath.Dir(name)) + "\n"
		}
		write(t, root, name, src)
	}
	return root
}

func write(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// importing returns a source file for package pkg importing each path.
func importing(pkg string, paths ...string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\nimport (\n")
	for _, p := range paths {
		b.WriteString("\t_ \"" + p + "\"\n")
	}
	b.WriteString(")\n")
	return b.String()
}

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		files    map[string]string
		wantCode int
		wantOut  []string
	}{
		{
			name: "allowed layout passes",
			files: map[string]string{
				"internal/arc/arc.go":                 "",
				"internal/perspective/perspective.go": "",
				"internal/user/user.go":               "",
				"internal/jobs/jobs.go":               "",
				"internal/api/apiutil/apiutil.go":     "",
				"internal/api/api.go":                 "",
				"internal/db/db.go":                   "",
				"internal/testdb/testdb.go":           "",
				"pkg/crypto/crypto.go":                "",
				"internal/lore/lore.go": importing("lore",
					"perspectiv/internal/arc", "perspectiv/internal/perspective", "perspectiv/internal/user",
					"perspectiv/internal/jobs", "perspectiv/internal/api/apiutil"),
				"cmd/perspectiv/main.go": "package main\n\nfunc main() {}\n",
			},
			wantCode: 0,
		},
		{
			name: "unlisted top-level internal package fails",
			files: map[string]string{
				"internal/placeimport/importer.go": "",
			},
			wantCode: 1,
			wantOut:  []string{"perspectiv/internal/placeimport", "not an allowed top-level package"},
		},
		{
			name: "unlisted package under pkg fails",
			files: map[string]string{
				"pkg/strutil/strutil.go": "",
			},
			wantCode: 1,
			wantOut:  []string{"perspectiv/pkg/strutil"},
		},
		{
			name: "utils package nested in a domain fails",
			files: map[string]string{
				"internal/lore/utils/utils.go": "",
			},
			wantCode: 1,
			wantOut:  []string{"perspectiv/internal/lore/utils", "utils"},
		},
		{
			name: "helpers package fails",
			files: map[string]string{
				"internal/auth/helpers/helpers.go": "",
			},
			wantCode: 1,
			wantOut:  []string{"perspectiv/internal/auth/helpers"},
		},
		{
			name: "domain package importing four sibling domains fails",
			files: map[string]string{
				"internal/arc/arc.go":                 "",
				"internal/perspective/perspective.go": "",
				"internal/user/user.go":               "",
				"internal/lore/lore.go":               "",
				"internal/vote/vote.go": importing("vote",
					"perspectiv/internal/arc", "perspectiv/internal/perspective",
					"perspectiv/internal/user", "perspectiv/internal/lore"),
			},
			wantCode: 1,
			wantOut:  []string{"perspectiv/internal/vote", "4 sibling domains", "arc, lore, perspective, user"},
		},
		{
			name: "importing a sibling sub-package counts as that sibling once",
			files: map[string]string{
				"internal/notify/notify.go":        "",
				"internal/notify/mailer/mailer.go": "",
				"internal/arc/arc.go":              "",
				"internal/user/user.go":            "",
				"internal/moderation/moderation.go": importing("moderation",
					"perspectiv/internal/notify", "perspectiv/internal/notify/mailer",
					"perspectiv/internal/arc", "perspectiv/internal/user"),
			},
			wantCode: 0,
		},
		{
			name: "own-domain sub-packages and shared packages do not count",
			files: map[string]string{
				"internal/lore/lore.go":               "",
				"internal/arc/arc.go":                 "",
				"internal/perspective/perspective.go": "",
				"internal/user/user.go":               "",
				"internal/jobs/jobs.go":               "",
				"pkg/crypto/crypto.go":                "",
				"internal/lore/rank/rank.go": importing("rank",
					"perspectiv/internal/lore", "perspectiv/internal/arc", "perspectiv/internal/perspective",
					"perspectiv/internal/user", "perspectiv/internal/jobs", "perspectiv/pkg/crypto"),
			},
			wantCode: 0,
		},
		{
			name: "sibling imports in test files do not count",
			files: map[string]string{
				"internal/arc/arc.go":                 "",
				"internal/perspective/perspective.go": "",
				"internal/user/user.go":               "",
				"internal/lore/lore.go":               "",
				"internal/vote/vote.go":               importing("vote", "perspectiv/internal/arc"),
				"internal/vote/vote_test.go": importing("vote",
					"perspectiv/internal/perspective", "perspectiv/internal/user", "perspectiv/internal/lore"),
			},
			wantCode: 0,
		},
		{
			name: "a package that does not compile is still checked",
			files: map[string]string{
				"internal/widgets/widgets.go": "package widgets\n\nfunc broken( {\n",
			},
			wantCode: 1,
			wantOut:  []string{"perspectiv/internal/widgets"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := writeModule(t, tc.files)
			var out bytes.Buffer
			code := run([]string{"-root", root}, &out)
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

func TestRunNotAModule(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if code := run([]string{"-root", t.TempDir()}, &out); code != 2 {
		t.Fatalf("exit code = %d, want 2; output:\n%s", code, out.String())
	}
}
