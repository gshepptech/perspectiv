//go:build integration

package testdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver pgtestdb connects with
	"github.com/peterldowns/pgtestdb"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"perspectiv/internal/db"
)

// Image is the PostgreSQL image every integration test runs against. It
// matches the dev loop and production major version.
const Image = "postgres:18-alpine"

const (
	superUser     = "postgres"
	superPassword = "postgres"
	// startTimeout bounds the container start and its readiness wait. The
	// module's own 60s readiness deadline is replaced so this is the single
	// bound.
	startTimeout = 3 * time.Minute
	// dataDir is the image's PGDATA parent. It is mounted as tmpfs: the
	// server is thrown away with the run, so its files never need to reach
	// a disk.
	dataDir = "/var/lib/postgresql"
	// reaperGrace is how long the reaper waits, after the last test binary
	// of a run disconnects, before it removes the shared server. It covers
	// the gap while `go test` is still compiling the next package.
	reaperGrace = "2m"
)

// server is the container's connection details, set once by Main before any
// test runs and read-only afterwards. It is package state because New's
// signature carries no handle to it.
var server *pgtestdb.Config

// Main attaches the test binary to the run's PostgreSQL server, starting it
// if no other binary of the same `go test` invocation has, then runs the
// tests and exits with m.Run's code. A server that cannot start fails the
// binary with exit code 1 rather than skipping: integration tests never pass
// vacuously.
//
// One server serves every test binary of a run. `go test ./...` runs up to
// GOMAXPROCS binaries at once; a container each meant a dozen initdb runs
// competing for the Docker VM, and they missed their readiness deadline.
// Tests stay isolated because every test gets its own pgtestdb database, and
// pgtestdb builds the shared template under an advisory lock. The server is
// named after the testcontainers session, which is the `go test` process, so
// two runs never share one; the session's reaper removes it when the run
// ends, so no binary terminates it.
func Main(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if os.Getenv("TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT") == "" && os.Getenv("RYUK_RECONNECTION_TIMEOUT") == "" {
		_ = os.Setenv("TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT", reaperGrace)
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	useDockerContext(ctx)
	ctr, err := postgres.Run(ctx, Image,
		postgres.WithUsername(superUser),
		postgres.WithPassword(superPassword),
		postgres.WithDatabase("postgres"),
		testcontainers.WithReuseByName(containerName()),
		testcontainers.WithTmpfs(map[string]string{dataDir: "rw"}),
		// The module already turns fsync off; the rest of the durability
		// work goes too, for the same reason: nothing outlives the run.
		testcontainers.WithCmdArgs("-c", "synchronous_commit=off", "-c", "full_page_writes=off"),
		// Same checks as postgres.BasicWaitStrategies (the server logs ready
		// twice because the entrypoint restarts it after initdb), but under
		// startTimeout instead of the default 60s.
		testcontainers.WithWaitStrategyAndDeadline(startTimeout,
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort("5432/tcp"),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testdb: start %s: %v\n", Image, err)
		return 1
	}

	host, err := ctr.Host(ctx)
	if err == nil {
		var port interface{ Port() string }
		port, err = ctr.MappedPort(ctx, "5432/tcp")
		if err == nil {
			server = &pgtestdb.Config{
				DriverName: "pgx",
				Host:       host,
				Port:       port.Port(),
				User:       superUser,
				Password:   superPassword,
				Database:   "postgres",
				Options:    "sslmode=disable",
				// A test that leaks a connection must not stop its database
				// being dropped; the leak is the test's bug, not a reason to
				// keep the database.
				ForceTerminateConnections: true,
			}
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "testdb: read container address: %v\n", err)
		return 1
	}
	cancel()
	return m.Run()
}

// containerName is the shared server's name for this `go test` run.
func containerName() string {
	return "perspectiv-testdb-" + testcontainers.SessionID()[:16]
}

// useDockerContext points testcontainers at the Docker CLI's current context
// when DOCKER_HOST is unset. testcontainers-go looks at DOCKER_HOST, its own
// properties file and the default socket paths, but not at `docker context`,
// so on a machine whose engine is reached through a context (OrbStack, Colima,
// Docker Desktop with a moved socket) the container would not start even
// though `docker` works. Any failure leaves the environment unchanged and
// testcontainers' own discovery decides.
func useDockerContext(ctx context.Context) {
	if os.Getenv("DOCKER_HOST") != "" {
		return
	}
	out, err := exec.CommandContext(ctx, "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output()
	if err != nil {
		return
	}
	if host := strings.TrimSpace(string(out)); host != "" {
		_ = os.Setenv("DOCKER_HOST", host)
	}
}

// New returns a pool on a fresh database migrated by db.Migrate. The pool is
// closed and the database dropped by t.Cleanup.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return open(t, migrator{})
}

// NewEmpty returns a pool on a fresh database with no schema at all, for tests
// of the migrator itself. The pool is closed and the database dropped by
// t.Cleanup.
func NewEmpty(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return open(t, pgtestdb.NoopMigrator{})
}

func open(t *testing.T, m pgtestdb.Migrator) *pgxpool.Pool {
	t.Helper()
	if server == nil {
		t.Fatal("testdb: no database server; call testdb.Main(m) from TestMain")
	}
	conf := pgtestdb.Custom(t, *server, m)
	pool, err := pgxpool.New(context.Background(), conf.URL())
	if err != nil {
		t.Fatalf("testdb: connect to %s: %v", conf.Database, err)
	}
	// Registered after pgtestdb's own cleanup, so it runs first: the pool is
	// closed before the database is dropped.
	t.Cleanup(pool.Close)
	return pool
}

// migrator builds the template database with the production migration path.
type migrator struct{}

// Hash identifies the migrated schema, so a template is rebuilt exactly when a
// goose or River migration changes.
func (migrator) Hash() (string, error) {
	sum := sha256.New()
	if err := db.WriteFingerprint(sum); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// Migrate runs db.Migrate against the new template database.
func (migrator) Migrate(ctx context.Context, _ *sql.DB, conf pgtestdb.Config) error {
	pool, err := pgxpool.New(ctx, conf.URL())
	if err != nil {
		return fmt.Errorf("connect to template %s: %w", conf.Database, err)
	}
	defer pool.Close()
	return db.Migrate(ctx, pool)
}
