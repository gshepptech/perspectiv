//go:build integration

package k3s

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// pinnedToolchain is the Go version go.mod pins, without the "go" prefix.
func pinnedToolchain(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "toolchain go"); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatal("go.mod has no toolchain line")
	return ""
}

// The image's Go toolchain is checked against go.mod before anything else is
// built, so a drifted GO_VERSION build argument fails the image build.
func TestDockerfileGoToolchainMatchesGoMod(t *testing.T) {
	root := repoRoot(t)
	pinned := pinnedToolchain(t, root)
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal("docker is not on PATH")
	}
	tests := []struct {
		name      string
		goVersion string
		wantOK    bool
	}{
		{name: "go.mod toolchain builds", goVersion: pinned, wantOK: true},
		{name: "default build argument builds", goVersion: "", wantOK: true},
		{name: "older patch release fails", goVersion: "1.27.1", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"build", "--target", "go-deps", "--progress", "plain", "-f", filepath.Join(root, "Dockerfile")}
			if tc.goVersion != "" {
				args = append(args, "--build-arg", "GO_VERSION="+tc.goVersion)
			}
			args = append(args, root)
			cmd := exec.CommandContext(t.Context(), docker, args...)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			err := cmd.Run()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				t.Fatalf("run docker: %v", err)
			}
			if ok := err == nil; ok != tc.wantOK {
				t.Fatalf("docker build ok = %v, want %v:\n%s", ok, tc.wantOK, out.String())
			}
			if !tc.wantOK && !strings.Contains(out.String(), "go.mod pins toolchain go"+pinned) {
				t.Errorf("failure does not explain the mismatch:\n%s", out.String())
			}
		})
	}
}
