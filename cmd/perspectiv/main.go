// Command perspectiv is the server: one binary whose ROLE setting selects
// what the process runs.
//
//	perspectiv                       serve per ROLE (api, worker or all)
//	perspectiv migrate               apply the goose and River migrations, then exit
//	perspectiv keygen                print a new encryption key for ENCRYPTION_KEYS
//	perspectiv admin grant <username> give an existing account the ADMIN role
//	perspectiv admin reset-two-factor <username> <operator> <reason>
//	                                 remove a member's two-factor sign-in
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
	"math"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"perspectiv/internal/api/logging"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/api/telemetry"
	"perspectiv/internal/auth"
	"perspectiv/internal/db"
	"perspectiv/internal/jobs"
	"perspectiv/internal/moderation"
	"perspectiv/pkg/crypto"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

const usage = `usage:
  perspectiv                         serve per ROLE (api, worker or all; default all)
  perspectiv migrate                 apply pending migrations to DATABASE_URL and exit
  perspectiv keygen                  print a new age secret key for ENCRYPTION_KEYS on stdout
  perspectiv admin grant <username>  give the account with this username the ADMIN role
  perspectiv admin reset-two-factor <username> <operator> <reason>
                                     turn off the account's two-factor sign-in and sign it
                                     out everywhere, for a member who lost both their
                                     authenticator and their recovery codes; operator (the
                                     name of whoever runs this) and reason (how the owner
                                     was confirmed) go in the audit log, and the member is
                                     emailed. Needs PUBLIC_BASE_URL too.

Environment: ROLE, DATABASE_URL, HTTP_ADDR, PUBLIC_BASE_URL, JWT_SECRET, ENCRYPTION_KEYS,
COOKIE_SECURE, MAIL_PROVIDER, SMTP_ADDR, RESEND_API_KEY, MAIL_FROM,
RANK_INTERVAL, CANON_MIN_MEMBERS, EXPIRY_WINDOW, AVATAR_DIR, MIGRATE_ON_START,
DB_MAX_CONNS, LOG_LEVEL, OTEL_EXPORTER_OTLP_ENDPOINT.
`

// run is the whole program; main only supplies the process's arguments,
// environment, stdout and stderr, and ctx ends on SIGINT or SIGTERM. Only
// keygen writes to stdout, so its key can be captured alone.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
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
	case args[0] == "keygen" && len(args) == 1:
		return keygen(stdout, stderr)
	case args[0] == "admin" && len(args) == 3 && args[1] == "grant":
		return grantAdmin(ctx, args[1:], getenv, stderr)
	case args[0] == "admin" && len(args) == 5 && args[1] == "reset-two-factor":
		return resetTwoFactor(ctx, args[2:], getenv, stderr)
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

// keygen prints a new age secret key, the format ENCRYPTION_KEYS holds,
// made by pkg/crypto. Production keys are made on a separate, audited host.
func keygen(stdout, stderr io.Writer) int {
	key, err := crypto.GenerateKey()
	if err != nil {
		say(stderr, fmt.Sprintf("perspectiv: %v\n", err))
		return exitFailure
	}
	say(stdout, key+"\n")
	return exitOK
}

// resetTwoFactor runs `admin reset-two-factor <username> <operator>
// <reason>`; args is [username, operator, reason].
func resetTwoFactor(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	if len(args) != 3 {
		say(stderr, usage)
		return exitUsage
	}
	username, operator, reason := args[0], args[1], args[2]
	base := getenv("PUBLIC_BASE_URL")
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		say(stderr, "perspectiv: PUBLIC_BASE_URL must be an absolute http(s) URL; the member's email links to it\n")
		return exitUsage
	}
	return withPool(ctx, getenv, stderr, func(pool *pgxpool.Pool, log zerolog.Logger) int {
		inserter, err := jobs.NewClient(pool, log, metrics.New(metrics.NewRegistry()), nil, nil)
		if err != nil {
			log.Error().Err(err).Msg("job client")
			return exitFailure
		}
		if err := auth.ResetTwoFactor(ctx, pool, jobs.NewEnqueuer(inserter), base, username, operator, reason); err != nil {
			log.Error().Err(err).Str("username", username).Str("operator", operator).Msg("two-factor reset failed")
			return exitFailure
		}
		log.Info().Str("event", "two_factor_reset_by_operator").Str("username", username).Str("operator", operator).Msg("two-factor sign-in removed; sessions revoked; member emailed")
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
	pool, err := openPool(ctx, dsn, 0, log)
	if err != nil {
		log.Error().Err(err).Msg("database unavailable")
		return exitUsage
	}
	defer pool.Close()
	return fn(pool, log)
}

// openPool builds the pgx pool with the composite query tracer, capped at
// maxConns when it is positive (a one-shot subcommand keeps pgx's default).
// pgxpool connects lazily, so an unreachable database surfaces on first use.
func openPool(ctx context.Context, dsn string, maxConns int, log zerolog.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// The parse error can echo the password; never log it.
		return nil, errors.New("DATABASE_URL is not a valid connection string")
	}
	cfg.ConnConfig.Tracer = telemetry.PoolTracer(log)
	if maxConns > 0 {
		cfg.MaxConns = int32(min(maxConns, math.MaxInt32)) //nolint:gosec // G115: clamped to int32 range just above
	}
	// pgx reuses prepared statements, and from a statement's sixth run
	// PostgreSQL may switch to a generic plan that ignores the arguments. The
	// listings filter with "argument IS NULL OR ..." and their row counts swing
	// by four orders of magnitude between places, so a generic plan misjudges
	// them badly (12 ms planned per call, 550 ms generic, measured at beta
	// scale). Planning each call costs a few milliseconds and is always right.
	cfg.ConnConfig.RuntimeParams["plan_cache_mode"] = "force_custom_plan"
	// The app's statements are short index reads. Once the planner's row
	// estimates grow, JIT compiles them for 20 to 30 ms each, far longer than
	// they run (measured on place search with beta-scale arcs loaded).
	cfg.ConnConfig.RuntimeParams["jit"] = "off"
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
