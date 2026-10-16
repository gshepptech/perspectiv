//go:build integration

package db_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"perspectiv/internal/db"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) {
	testdb.Main(m)
}

func gooseVersion(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var v int64
	err := pool.QueryRow(context.Background(),
		`SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&v)
	if err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	return v
}

func riverVersions(t *testing.T, pool *pgxpool.Pool) (existing, all []int) {
	t.Helper()
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatalf("river migrator: %v", err)
	}
	ex, err := m.ExistingVersions(context.Background())
	if err != nil {
		t.Fatalf("river existing versions: %v", err)
	}
	for _, v := range ex {
		existing = append(existing, v.Version)
	}
	for _, v := range m.AllVersions() {
		all = append(all, v.Version)
	}
	return existing, all
}

func migrationFileCount(t *testing.T) int64 {
	t.Helper()
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	return int64(len(entries))
}

// migrateConcurrently runs n Migrate calls at once and joins their errors.
func migrateConcurrently(ctx context.Context, pool *pgxpool.Pool, n int) error {
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() { errs[i] = db.Migrate(ctx, pool) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

// assertFullyMigrated checks that every goose migration was applied exactly
// once and every River migration was applied.
func assertFullyMigrated(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if got, want := gooseVersion(t, pool), migrationFileCount(t); got != want {
		t.Errorf("goose version = %d, want %d", got, want)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM goose_db_version WHERE version_id > 0`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if int64(n) != migrationFileCount(t) {
		t.Errorf("goose applied %d rows, want one per migration (%d): a concurrent run applied twice", n, migrationFileCount(t))
	}
	existing, all := riverVersions(t, pool)
	if len(all) == 0 || !slices.Equal(existing, all) {
		t.Errorf("river versions applied = %v, want %v", existing, all)
	}
}

// TestMigrateFreshDatabase checks that Migrate applies every goose migration
// and every River migration to an empty database, that a second run is a
// no-op, and that concurrent runs serialize on the advisory lock.
func TestMigrateFreshDatabase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		concurrent int
		repeat     int
	}{
		{name: "single run on an empty database", concurrent: 1, repeat: 1},
		{name: "second run is a no-op", concurrent: 1, repeat: 2},
		{name: "concurrent runs serialize", concurrent: 6, repeat: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pool := testdb.NewEmpty(t)
			ctx := context.Background()
			for range tt.repeat {
				if err := migrateConcurrently(ctx, pool, tt.concurrent); err != nil {
					t.Fatalf("Migrate: %v", err)
				}
			}
			assertFullyMigrated(t, pool)
		})
	}
}

// TestMigrateReportsFailure checks that Migrate returns an error instead of
// succeeding when the database cannot be reached.
func TestMigrateReportsFailure(t *testing.T) {
	t.Parallel()
	pool := testdb.NewEmpty(t)
	pool.Close()
	if err := db.Migrate(context.Background(), pool); err == nil {
		t.Fatal("Migrate on a closed pool returned nil, want an error")
	}
}

// TestMigrationsRollBack runs every goose Down to version 0, checks that no
// application table is left, and migrates up again.
func TestMigrationsRollBack(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	ctx := context.Background()
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = sqlDB.Close() })
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, os.DirFS("migrations"))
	if err != nil {
		t.Fatalf("goose provider: %v", err)
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("down to 0: %v", err)
	}
	rows, err := pool.Query(ctx, `
		SELECT table_name::text FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name NOT LIKE 'river%' AND table_name <> 'goose_db_version'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	left, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("tables left after rolling back every migration: %v", left)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate after rollback: %v", err)
	}
	assertFullyMigrated(t, pool)
}

// pgCode returns the SQLSTATE and constraint name of a Postgres error.
func pgCode(err error) (code, constraint string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code, pgErr.ConstraintName
	}
	return "", ""
}
