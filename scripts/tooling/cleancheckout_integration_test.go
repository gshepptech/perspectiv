//go:build integration

package tooling

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extractHEAD writes the committed tree at HEAD (what a fresh clone holds,
// with no untracked go.sum, web/dist or lockfile riding along) into a new
// directory and returns it.
func extractHEAD(t *testing.T, root string) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "head.tar")
	if out, code := runCmd(t, root, nil, "git", "archive", "--format=tar", "-o", archive, "HEAD"); code != 0 {
		t.Fatalf("git archive HEAD exit %d: %s", code, out)
	}
	dst := t.TempDir()
	if out, code := runCmd(t, dst, nil, "tar", "-xf", archive); code != 0 {
		t.Fatalf("tar -xf exit %d: %s", code, out)
	}
	return dst
}

// TestCleanCheckoutBuildsWithMake proves a fresh clone compiles through the
// Makefile alone: go.sum and a tidy go.mod are committed, the pnpm lockfile is
// committed, `make vet` produces the embedded web/dist before the Go compiler
// needs it, and the build output it leaves behind is gitignored.
func TestCleanCheckoutBuildsWithMake(t *testing.T) {
	root := repoRoot(t)
	clean := extractHEAD(t, root)

	for _, f := range []string{"go.sum", "web/pnpm-lock.yaml", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(clean, f)); err != nil {
			t.Errorf("%s is not committed: %v", f, err)
		}
	}

	// -mod=readonly: an untidy go.mod or a missing go.sum entry is an error,
	// never a silent rewrite.
	out, code := runCmd(t, clean, []string{"GOFLAGS=-mod=readonly"}, "make", "vet")
	if code != 0 {
		t.Fatalf("make vet on a clean checkout exit %d; output:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(clean, "web", "dist", "index.html")); err != nil {
		t.Errorf("make vet did not leave web/dist/index.html: %v", err)
	}

	// The dev loop and builds leave these behind; none may show as untracked.
	for _, p := range []string{".env.dev.local", "bin/perspectiv", "tmp/air-perspectiv/perspectiv"} {
		writeFile(t, filepath.Join(clean, filepath.FromSlash(p)), "x\n")
	}
	if out, code := runCmd(t, clean, nil, "git", "init", "-q"); code != 0 {
		t.Fatalf("git init: %s", out)
	}
	status, code := runCmd(t, clean, nil, "git", "status", "--porcelain", "--untracked-files=all", "--",
		".env.dev.local", "bin", "tmp", "web/dist", "web/node_modules")
	if code != 0 {
		t.Fatalf("git status: %s", status)
	}
	if s := strings.TrimSpace(status); s != "" {
		t.Errorf("build and dev output is not gitignored:\n%s", s)
	}
}
