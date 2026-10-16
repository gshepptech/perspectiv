package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const claudeWithDecl = "# Project\n\nDesign system file: `docs/design-system.md`\n"

func layout(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
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
			name: "named design system file referenced and present passes",
			files: map[string]string{
				"CLAUDE.md":                        claudeWithDecl,
				"docs/design-system.md":            "# Folio\n\nType, measure, palette.\n",
				"web/src/components/ui/Button.tsx": "export function Button() { return null }\n",
				"web/src/routes/read/ReadPage.tsx": "export function Component() { return null }\n",
			},
			wantCode: 0,
			wantOut:  []string{"Folio", "docs/design-system.md"},
		},
		{
			name: "no component code yet and no design file is a pending pass",
			files: map[string]string{
				"CLAUDE.md":        claudeWithDecl,
				"web/src/main.tsx": "export {}\n",
			},
			wantCode: 0,
			wantOut:  []string{"pending"},
		},
		{
			name: "component code without the design file fails",
			files: map[string]string{
				"CLAUDE.md":                        claudeWithDecl,
				"web/src/components/ui/Button.tsx": "export function Button() { return null }\n",
			},
			wantCode: 1,
			wantOut:  []string{"docs/design-system.md", "web/src/components/ui/Button.tsx"},
		},
		{
			name: "route component without the design file fails",
			files: map[string]string{
				"CLAUDE.md":                          claudeWithDecl,
				"web/src/routes/place/PlacePage.tsx": "export function Component() { return null }\n",
			},
			wantCode: 1,
			wantOut:  []string{"web/src/routes/place/PlacePage.tsx"},
		},
		{
			name: "design file without a level-one heading is unnamed",
			files: map[string]string{
				"CLAUDE.md":             claudeWithDecl,
				"docs/design-system.md": "Some rules but no name.\n## Type\n",
			},
			wantCode: 1,
			wantOut:  []string{"no name"},
		},
		{
			name: "CLAUDE.md without the declaration fails",
			files: map[string]string{
				"CLAUDE.md":             "# Project\n\nNo design system named.\n",
				"docs/design-system.md": "# Folio\n",
			},
			wantCode: 1,
			wantOut:  []string{"Design system file:"},
		},
		{
			name:     "missing CLAUDE.md fails",
			files:    map[string]string{"docs/design-system.md": "# Folio\n"},
			wantCode: 1,
			wantOut:  []string{"CLAUDE.md"},
		},
		{
			name: "declared path escaping the repository fails",
			files: map[string]string{
				"CLAUDE.md": "Design system file: `../elsewhere.md`\n",
			},
			wantCode: 1,
			wantOut:  []string{"inside the repository"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := layout(t, tc.files)
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
