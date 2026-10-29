package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/rs/zerolog"

	"perspectiv/internal/jobs"
	"perspectiv/internal/notify/dbq"
)

// digestInterval is how often the digest job looks for accounts due one.
// Each account gets at most one digest a day (DigestDue's 23 hours).
const digestInterval = time.Hour

// digestBatch bounds the accounts one pass sends to; the rest wait an hour.
const digestBatch = 500

// Email data keys of the digest template.
const (
	dataItems = "items"
	dataCount = "count"
)

// DigestArgs is the periodic job that sends daily digests.
type DigestArgs struct{}

// Kind is the River job kind.
func (DigestArgs) Kind() string { return "notify_digest" }

// DigestPeriodicJob runs the digest pass every hour.
func DigestPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(digestInterval),
		func() (river.JobArgs, *river.InsertOpts) { return DigestArgs{}, &river.InsertOpts{MaxAttempts: 3} },
		&river.PeriodicJobOpts{ID: DigestArgs{}.Kind()},
	)
}

type digestWorker struct {
	river.WorkerDefaults[DigestArgs]
	pool *pgxpool.Pool
	log  zerolog.Logger
}

// NewDigestWorker returns the worker that sends each digest account its due
// summary: one email of the events since its last one, which are then
// deleted. Each account's items, the email job and its sent time commit in
// one transaction, so a retried pass never sends twice.
func NewDigestWorker(pool *pgxpool.Pool, log zerolog.Logger) river.Worker[DigestArgs] {
	return &digestWorker{pool: pool, log: log.With().Str("component", "notify_digest").Logger()}
}

func (w *digestWorker) Work(ctx context.Context, job *river.Job[DigestArgs]) error {
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("digest: %w", err)
	}
	enq := jobs.NewEnqueuer(client)
	due, err := dbq.New(w.pool).DigestDue(ctx, digestBatch)
	if err != nil {
		return fmt.Errorf("digest due: %w", err)
	}
	sent := 0
	for _, u := range due {
		if err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
			return sendDigest(ctx, dbq.New(tx), tx, enq, u)
		}); err != nil {
			return fmt.Errorf("digest for %s: %w", u.ID, err)
		}
		sent++
	}
	if err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		_, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job)
		return err
	}); err != nil {
		return err
	}
	w.log.Info().Int("sent", sent).Msg("digest pass")
	return nil
}

// sendDigest takes one account's items and queues their summary email.
func sendDigest(ctx context.Context, q *dbq.Queries, tx pgx.Tx, enq *jobs.Enqueuer, u dbq.DigestDueRow) error {
	rows, err := q.TakeDigestItems(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("take items: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	items := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		var item map[string]string
		if err := json.Unmarshal(r.Data, &item); err != nil {
			return fmt.Errorf("digest item: %w", err)
		}
		items = append(items, item)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return err
	}
	if err := enq.EmailTx(ctx, tx, jobs.EmailArgs{
		To: u.Email, Template: jobs.TemplateDigest,
		Data: map[string]string{dataItems: string(encoded), dataCount: strconv.Itoa(len(items))},
	}); err != nil {
		return err
	}
	return q.MarkDigestSent(ctx, u.ID)
}
