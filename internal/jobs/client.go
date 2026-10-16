package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/rs/zerolog"

	"perspectiv/internal/api/metrics"
)

// maxWorkers is how many jobs one worker process runs at once on the default
// queue.
const maxWorkers = 10

// NewClient builds the River client. With workers nil it is insert-only, the
// API role's client: it inserts jobs and never works them. With workers set it
// works the default queue and runs the periodic jobs; periodic jobs without
// workers are refused, since nothing could work what they insert.
//
// Every failed job attempt, an error or a panic, increments m.JobFailures. A
// worker that cancels its own job with river.JobCancel has decided to drop
// it, and an attempt interrupted by client shutdown is retried; neither is
// counted, since River does not report them as failures.
func NewClient(pool *pgxpool.Pool, log zerolog.Logger, m *metrics.Metrics, workers *river.Workers,
	periodic []*river.PeriodicJob,
) (*river.Client[pgx.Tx], error) {
	if workers == nil && len(periodic) > 0 {
		return nil, errors.New("river client: periodic jobs need workers")
	}
	cfg := &river.Config{
		Logger:       slog.New(zerolog.NewSlogHandler(log.With().Str("component", "river").Logger())),
		ErrorHandler: failureCounter{m: m, log: log},
	}
	if workers != nil {
		cfg.Workers = workers
		cfg.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: maxWorkers}}
		cfg.PeriodicJobs = periodic
	}
	client, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	return client, nil
}

// failureCounter counts every failed attempt and leaves River's retry
// schedule unchanged.
type failureCounter struct {
	m   *metrics.Metrics
	log zerolog.Logger
}

func (f failureCounter) HandleError(_ context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	f.m.JobFailures.Inc()
	f.log.Error().Err(err).Int64("job_id", job.ID).Str("job_kind", job.Kind).
		Int("attempt", job.Attempt).Msg("job attempt failed")
	return nil
}

func (f failureCounter) HandlePanic(_ context.Context, job *rivertype.JobRow, panicVal any, trace string) *river.ErrorHandlerResult {
	f.m.JobFailures.Inc()
	f.log.Error().Interface("panic", panicVal).Str("trace", trace).Int64("job_id", job.ID).
		Str("job_kind", job.Kind).Int("attempt", job.Attempt).Msg("job attempt panicked")
	return nil
}
