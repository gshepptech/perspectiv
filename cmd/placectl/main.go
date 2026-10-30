// Command placectl loads open place data for the United States into the
// places table, and folds places listed twice. It is the composition root
// for both; the work itself lives in perspectiv/internal/place/importer.
//
//	placectl import --divisions <path> --places <path>
//	placectl dedupe [--apply]
//
// dedupe is a dry run unless --apply is given: it prints the clusters of
// duplicate stores and malls it found and what it would hide, and writes
// nothing.
//
// It reads DATABASE_URL and LOG_LEVEL from the environment and expects a
// migrated database. It exits 0 on success, 1 when the work fails (a file
// that cannot be parsed, a parent that cannot be resolved, a database error)
// and 2 on a usage error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strings"
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
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

const usage = `usage: placectl import --divisions <path> --places <path>
       placectl dedupe [--apply]

import loads Overture divisions (STATE, CITY, TOWN) and Foursquare OS Places
or Overture places (MALL, STORE) for the United States from newline-delimited
JSON extracts.

dedupe finds visible stores and malls listed twice (one name within 200 m, or
2 km for a sports venue) and hides the copies with no stories or drafts,
moving their places and followers to the one that stays. It is a dry run
that writes nothing unless --apply is given.

Environment: DATABASE_URL (required), LOG_LEVEL.
`

// run is the whole program; main only supplies the process's arguments,
// environment, stdout and stderr.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		say(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "import":
		return runImport(ctx, args[1:], getenv, stderr)
	case "dedupe":
		return runDedupe(ctx, args[1:], getenv, stdout, stderr)
	default:
		say(stderr, usage)
		return exitUsage
	}
}

func runImport(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	fs := flag.NewFlagSet("placectl import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	divisionsPath := fs.String("divisions", "", "Overture divisions extract (newline-delimited JSON)")
	placesPath := fs.String("places", "", "Foursquare OS Places or Overture places extract (newline-delimited JSON)")
	if err := fs.Parse(args); err != nil {
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

	pool, err := connect(ctx, dsn, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	_, err = importer.New(pool, log).Write(ctx, plan)
	return err
}

func connect(ctx context.Context, dsn string, log zerolog.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("DATABASE_URL is not a valid connection string")
	}
	cfg.ConnConfig.Tracer = telemetry.PoolTracer(log)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

func runDedupe(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("placectl dedupe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	apply := fs.Bool("apply", false, "hide the duplicates; without it nothing is written")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		say(stderr, usage)
		return exitUsage
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		say(stderr, "placectl: DATABASE_URL is required\n")
		return exitUsage
	}

	log := logging.New(stderr, getenv("LOG_LEVEL"))
	pool, err := connect(ctx, dsn, log)
	if err != nil {
		log.Error().Err(err).Msg("place dedupe failed")
		return exitFailure
	}
	defer pool.Close()
	rep, err := importer.New(pool, log).Dedupe(ctx, *apply)
	if err != nil {
		log.Error().Err(err).Msg("place dedupe failed")
		return exitFailure
	}
	writeDedupeReport(stdout, &rep)
	return exitOK
}

// writeDedupeReport prints the summary first, then every merge and skip.
func writeDedupeReport(w io.Writer, rep *importer.DedupeReport) {
	var b strings.Builder
	if rep.Applied {
		b.WriteString("placectl dedupe: applied\n")
	} else {
		b.WriteString("placectl dedupe: dry run, nothing written (pass --apply to write)\n")
	}
	verb := "would merge"
	if rep.Applied {
		verb = "merged"
	}
	fmt.Fprintf(&b, "candidates: %d visible stores and malls with coordinates\n", rep.Candidates)
	fmt.Fprintf(&b, "clusters:   %d\n", rep.Clusters)
	fmt.Fprintf(&b, "%s: %d\n", verb, len(rep.Merges))
	fmt.Fprintf(&b, "skipped:    %d\n", len(rep.Skips))
	reasons := map[string]int{}
	for _, s := range rep.Skips {
		reasons[s.Reason]++
	}
	for _, r := range slices.Sorted(maps.Keys(reasons)) {
		fmt.Fprintf(&b, "  %d: %s\n", reasons[r], r)
	}
	for _, m := range rep.Merges {
		fmt.Fprintf(&b, "%s %s %q into %s %q (%.0f m, %d places moved)\n",
			verb, m.Loser, m.LoserName, m.Survivor, m.SurvivorName, m.Distance, m.Children)
	}
	for _, s := range rep.Skips {
		fmt.Fprintf(&b, "skipped %s %q (survivor %s): %s\n", s.Place, s.Name, s.Survivor, s.Reason)
	}
	say(w, b.String())
}

// say writes a message for the operator. A failed write to stderr has nowhere
// to be reported, and the exit code still carries the outcome.
func say(w io.Writer, msg string) {
	_, _ = io.WriteString(w, msg)
}
