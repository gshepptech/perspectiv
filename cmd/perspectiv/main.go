// Command perspectiv is the server: one binary whose ROLE setting selects
// what the process runs.
//
//	perspectiv                       serve per ROLE (api, worker or all)
//	perspectiv migrate               apply the goose and River migrations, then exit
//	perspectiv admin grant <username> give an existing account the ADMIN role
//
// It is the composition root: configuration is read here and nowhere else,
// and every long-lived dependency is built here and passed down. Exit status
// is 0 on success, 1 when the work fails (an unknown username, a migration or
// serve error) and 2 on a usage or configuration error.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"perspectiv/internal/api/logging"
	"perspectiv/internal/api/telemetry"
	"perspectiv/internal/db"
	"perspectiv/internal/moderation"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stderr)
	stop()
	os.Exit(code)
}

const usage = `usage:
  perspectiv                         serve per ROLE (api, worker or all; default all)
  perspectiv migrate                 apply pending migrations to DATABASE_URL and exit
  perspectiv admin grant <username>  give the account with this username the ADMIN role

Environment: ROLE, DATABASE_URL, HTTP_ADDR, PUBLIC_BASE_URL, JWT_SECRET,
COOKIE_SECURE, MAIL_PROVIDER, SMTP_ADDR, RESEND_API_KEY, MAIL_FROM,
RANK_INTERVAL, CANON_MIN_MEMBERS, EXPIRY_WINDOW, AVATAR_DIR, MIGRATE_ON_START,
LOG_LEVEL, OTEL_EXPORTER_OTLP_ENDPOINT.
`

// run is the whole program; main only supplies the process's arguments,
// environment and stderr, and ctx ends on SIGINT or SIGTERM.
func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	if len(args) == 0 {
		cfg, err := loadConfig(getenv)
		if err != nil {
			say(stderr, fmt.Sprintf("perspectiv: invalid configuration:\n%v\n", err))
			return exitUsage
		}
		return serve(ctx, cfg, stderr)
	}
	switch {
	case args[0] == "migrate" && len(args) == 1:
		return withPool(ctx, getenv, stderr, func(pool *pgxpool.Pool, log zerolog.Logger) int {
			if err := db.Migrate(ctx, pool); err != nil {
				log.Error().Err(err).Msg("migration failed")
				return exitFailure
			}
			log.Info().Msg("migrations applied")
			return exitOK
		})
	case args[0] == "admin" && len(args) == 3 && args[1] == "grant":
		return grantAdmin(ctx, args[1:], getenv, stderr)
	case len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help"):
		say(stderr, usage)
		return exitOK
	default:
		say(stderr, usage)
		return exitUsage
	}
}

// grantAdmin runs `admin grant <username>`; args is ["grant", username].
func grantAdmin(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	if len(args) != 2 {
		say(stderr, usage)
		return exitUsage
	}
	username := args[1]
	return withPool(ctx, getenv, stderr, func(pool *pgxpool.Pool, log zerolog.Logger) int {
		if err := moderation.GrantAdmin(ctx, pool, username); err != nil {
			log.Error().Err(err).Str("username", username).Msg("admin grant failed")
			return exitFailure
		}
		log.Info().Str("username", username).Msg("admin role granted")
		return exitOK
	})
}

// withPool runs a one-shot subcommand against DATABASE_URL.
func withPool(ctx context.Context, getenv func(string) string, stderr io.Writer, fn func(*pgxpool.Pool, zerolog.Logger) int) int {
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		say(stderr, "perspectiv: DATABASE_URL is required\n")
		return exitUsage
	}
	log := logging.New(stderr, getenv("LOG_LEVEL"))
	pool, err := openPool(ctx, dsn, log)
	if err != nil {
		log.Error().Err(err).Msg("database unavailable")
		return exitUsage
	}
	defer pool.Close()
	return fn(pool, log)
}

// openPool builds the pgx pool with the composite query tracer. pgxpool
// connects lazily, so an unreachable database surfaces on first use.
func openPool(ctx context.Context, dsn string, log zerolog.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// The parse error can echo the password; never log it.
		return nil, errors.New("DATABASE_URL is not a valid connection string")
	}
	cfg.ConnConfig.Tracer = telemetry.PoolTracer(log)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

// say writes a message for the operator. A failed write to stderr has nowhere
// to be reported, and the exit code still carries the outcome.
func say(w io.Writer, msg string) {
	_, _ = io.WriteString(w, msg)
}
