// Package db owns the PostgreSQL schema: the goose migrations embedded from
// migrations/ and the migrator that applies them together with River's own
// job-queue migrations.
//
// Every domain's sqlc.yaml points its schema at internal/db/migrations, so
// the migrations are the single definition of the tables the queries run
// against.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var embedded embed.FS

// migrationLockKey is the Postgres advisory lock Migrate holds for its whole
// run, so two processes starting together (a migration Job and a server with
// MIGRATE_ON_START, or parallel replicas) apply each migration exactly once.
// The value is arbitrary but fixed: crc32("perspectiv.migrate").
const migrationLockKey int64 = 0x5045_5253

// Migrations returns the embedded goose migration files, rooted at the
// migrations directory.
func Migrations() fs.FS {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		// fs.Sub fails only on an invalid path; "migrations" is a constant.
		panic(err)
	}
	return sub
}

// Migrate takes a session-level Postgres advisory lock, applies every pending
// goose migration, then every pending River migration, and releases the lock.
// It returns the first error; a migration that fails leaves the versions
// before it applied, as goose runs each migration in its own transaction.
//
// The lock is held on one pooled connection while the migrations run on
// others, so the pool needs at least two connections (pgxpool's default
// minimum maximum is four).
func Migrate(ctx context.Context, pool *pgxpool.Pool) (err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration lock connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return fmt.Errorf("take migration lock: %w", err)
	}
	defer func() {
		// Unlock with a fresh context: the caller's may be cancelled, and a
		// session lock left behind would block every later migration on this
		// connection's lifetime.
		_, unlockErr := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockKey)
		if unlockErr != nil {
			// The connection's state is unknown; drop it so the lock dies
			// with the session instead of returning to the pool.
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
			err = errors.Join(err, fmt.Errorf("release migration lock: %w", unlockErr))
		}
	}()

	if err := gooseUp(ctx, pool); err != nil {
		return err
	}
	return riverUp(ctx, pool)
}

func gooseUp(ctx context.Context, pool *pgxpool.Pool) (err error) {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close goose connection: %w", closeErr))
		}
	}()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, Migrations())
	if err != nil {
		return fmt.Errorf("load goose migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply goose migrations: %w", err)
	}
	return nil
}

func riverUp(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("load river migrations: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("apply river migrations: %w", err)
	}
	return nil
}

// WriteFingerprint writes every input of the migrated schema to w: each goose
// migration's name and content and each River migration's version and SQL.
// Two builds whose fingerprints hash equal produce the same schema, which is
// what lets the test harness reuse a template database.
func WriteFingerprint(w io.Writer) error {
	names, err := fs.Glob(Migrations(), "*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	for _, name := range names {
		body, err := fs.ReadFile(Migrations(), name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := writeAll(w, "goose:", path.Base(name), "\x00", string(body), "\x00"); err != nil {
			return err
		}
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(nil), nil)
	if err != nil {
		return fmt.Errorf("load river migrations: %w", err)
	}
	for _, m := range migrator.AllVersions() {
		if err := writeAll(w, "river:", strconv.Itoa(m.Version), "\x00", m.SQLUp, "\x00"); err != nil {
			return err
		}
	}
	return nil
}

func writeAll(w io.Writer, parts ...string) error {
	for _, p := range parts {
		if _, err := io.WriteString(w, p); err != nil {
			return fmt.Errorf("write fingerprint: %w", err)
		}
	}
	return nil
}
