//go:build integration

package tooling

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// extractHEAD writes the committed tree at HEAD (what a fresh clone holds,
// with no untracked go.sum, web/dist or lockfile riding along) into a new
// directory and returns it.
func extractHEAD(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "archive", "--format=tar", "HEAD")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git archive HEAD: %v\n%s", err, stderr.String())
	}
	dst := t.TempDir()
	tr := tar.NewReader(bytes.NewReader(out))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		p := filepath.Join(dst, filepath.FromSlash(hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o750); err != nil {
				t.Fatalf("mkdir %s: %v", p, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o755)
			if err != nil {
				t.Fatalf("create %s: %v", p, err)
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // archive of our own HEAD
				t.Fatalf("write %s: %v", p, err)
			}
			if err := f.Close(); err != nil {
				t.Fatalf("close %s: %v", p, err)
			}
		case tar.TypeSymlink:
			if err := os.Symlink(hdr.Linkname, p); err != nil {
				t.Fatalf("symlink %s: %v", p, err)
			}
		}
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
