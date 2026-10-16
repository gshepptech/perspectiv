// Command placectl loads open place data for the United States into the
// places table. It is the composition root for the import; the import itself
// lives in perspectiv/internal/place/importer.
//
//	placectl import --divisions <path> --places <path>
//
// It reads DATABASE_URL and LOG_LEVEL from the environment and expects a
// migrated database. It exits 0 on success, 1 when the import fails (a file
// that cannot be parsed, a parent that cannot be resolved, a database error)
// and 2 on a usage error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"perspectiv/internal/api/logging"
	"perspectiv/internal/api/telemetry"
	"perspectiv/internal/place/importer"
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

const usage = `usage: placectl import --divisions <path> --places <path>

Imports Overture divisions (STATE, CITY, TOWN) and Foursquare OS Places or
Overture places (MALL, STORE) for the United States from newline-delimited
JSON extracts. Environment: DATABASE_URL (required), LOG_LEVEL.
`

// run is the whole program; main only supplies the process's arguments,
// environment and stderr.
func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "import" {
		say(stderr, usage)
		return exitUsage
	}
	fs := flag.NewFlagSet("placectl import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	divisionsPath := fs.String("divisions", "", "Overture divisions extract (newline-delimited JSON)")
	placesPath := fs.String("places", "", "Foursquare OS Places or Overture places extract (newline-delimited JSON)")
	if err := fs.Parse(args[1:]); err != nil {
		return exitUsage
	}
	if *divisionsPath == "" || *placesPath == "" || fs.NArg() != 0 {
		say(stderr, usage)
		return exitUsage
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		say(stderr, "placectl: DATABASE_URL is required\n")
		return exitUsage
	}

	log := logging.New(stderr, getenv("LOG_LEVEL"))
	if err := importFiles(ctx, dsn, *divisionsPath, *placesPath, log); err != nil {
		log.Error().Err(err).Msg("place import failed")
		return exitFailure
	}
	return exitOK
}

func importFiles(ctx context.Context, dsn, divisionsPath, placesPath string, log zerolog.Logger) error {
	divisions, err := os.Open(divisionsPath) //nolint:gosec // the operator names the file to import
	if err != nil {
		return &importer.ParseError{File: "divisions", Err: err}
	}
	defer func() { _ = divisions.Close() }()
	places, err := os.Open(placesPath) //nolint:gosec // the operator names the file to import
	if err != nil {
		return &importer.ParseError{File: "places", Err: err}
	}
	defer func() { _ = places.Close() }()

	// Parse and resolve both files before connecting, so a bad file fails
	// fast and never holds a transaction open.
	plan, err := importer.Prepare(divisions, places)
	if err != nil {
		return err
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("DATABASE_URL is not a valid connection string")
	}
	cfg.ConnConfig.Tracer = telemetry.PoolTracer(log)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	_, err = importer.New(pool, log).Write(ctx, plan)
	return err
}

// say writes a message for the operator. A failed write to stderr has nowhere
// to be reported, and the exit code still carries the outcome.
func say(w io.Writer, msg string) {
	_, _ = io.WriteString(w, msg)
}
