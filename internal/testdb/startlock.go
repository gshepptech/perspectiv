//go:build integration

package testdb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// startLockPath is a host-wide lock file shared by every test binary.
// `go test ./...` runs many binaries at once and each starts its own server;
// started together they compete for the Docker VM and none of them reaches
// ready inside its deadline. Holding this lock while a container is created
// and waited on lets one initdb run at a time; tests themselves still run in
// parallel once their server is up.
var startLockPath = filepath.Join(os.TempDir(), "perspectiv-testdb-start.lock")

// lockStart blocks until this process holds the start lock or ctx ends. The
// returned func releases it.
func lockStart(ctx context.Context) (func(), error) {
	f, err := os.OpenFile(startLockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open start lock %s: %w", startLockPath, err)
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", startLockPath, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("wait for start lock %s: %w", startLockPath, ctx.Err())
		case <-tick.C:
		}
	}
}
