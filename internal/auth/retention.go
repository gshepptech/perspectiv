package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"

	"perspectiv/internal/auth/dbq"
)

// How long security records outlive their use. The privacy page states
// these; a change here changes what it promises.
const (
	// EmailTokenRetention keeps a spent or expired emailed link this long.
	EmailTokenRetention = 7 * 24 * time.Hour
	// SessionRetention keeps a session, and its refresh tokens, this long
	// after it was revoked or expired.
	SessionRetention = 30 * 24 * time.Hour
	// LoginAttemptRetention keeps a sign-in attempt this long; the lockout
	// reads only the last LockoutWindow.
	LoginAttemptRetention = 30 * 24 * time.Hour
	// RateWindowRetention keeps a rate-limit counter this long after its
	// window opened; the longest limit window is an hour.
	RateWindowRetention = 24 * time.Hour
	// TwoFactorAttemptRetention keeps a checked second-factor code's record
	// this long; the two-factor lockout reads only the last day.
	TwoFactorAttemptRetention = 30 * 24 * time.Hour
	// TwoFactorChallengeRetention keeps a sign-in's second-step challenge
	// this long after it expired; it is never usable after expiry.
	TwoFactorChallengeRetention = 24 * time.Hour
)

const (
	retentionInterval = 24 * time.Hour
	retentionBatch    = 5000
)

// RetentionArgs is the periodic job that deletes expired security records.
type RetentionArgs struct{}

// Kind is the River job kind.
func (RetentionArgs) Kind() string { return "auth_retention" }

// RetentionPeriodicJob runs the retention pass once a day.
func RetentionPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(retentionInterval),
		func() (river.JobArgs, *river.InsertOpts) { return RetentionArgs{}, &river.InsertOpts{MaxAttempts: 3} },
		&river.PeriodicJobOpts{ID: RetentionArgs{}.Kind(), RunOnStart: true},
	)
}

type retentionWorker struct {
	river.WorkerDefaults[RetentionArgs]
	q   *dbq.Queries
	now func() time.Time
	log zerolog.Logger
}

// NewRetentionWorker returns the worker that deletes, in batches, the
// emailed links, ended sessions, sign-in attempts, rate-limit counters,
// second-factor attempts and expired second-step challenges older than
// their retention.
func NewRetentionWorker(pool *pgxpool.Pool, log zerolog.Logger) river.Worker[RetentionArgs] {
	return &retentionWorker{q: dbq.New(pool), now: time.Now, log: log.With().Str("component", "auth_retention").Logger()}
}

func (w *retentionWorker) Work(ctx context.Context, _ *river.Job[RetentionArgs]) error {
	return Purge(ctx, w.q, w.now(), w.log)
}

// purge is one table's batched delete: rows older than before, at most
// batch at a time.
type purge struct {
	name  string
	keep  time.Duration
	batch func(ctx context.Context, before time.Time) (int64, error)
}

// Purge deletes every security record past its retention at now. Each table
// is drained in batches so no single statement holds locks for long.
func Purge(ctx context.Context, q *dbq.Queries, now time.Time, log zerolog.Logger) error {
	purges := []purge{
		{"email_tokens", EmailTokenRetention, func(ctx context.Context, before time.Time) (int64, error) {
			return q.PurgeEmailTokens(ctx, dbq.PurgeEmailTokensParams{Before: before, Batch: retentionBatch})
		}},
		{"sessions", SessionRetention, func(ctx context.Context, before time.Time) (int64, error) {
			return q.PurgeSessions(ctx, dbq.PurgeSessionsParams{Before: before, Batch: retentionBatch})
		}},
		{"login_attempts", LoginAttemptRetention, func(ctx context.Context, before time.Time) (int64, error) {
			return q.PurgeLoginAttempts(ctx, dbq.PurgeLoginAttemptsParams{Before: before, Batch: retentionBatch})
		}},
		{"rate_limit_counters", RateWindowRetention, func(ctx context.Context, before time.Time) (int64, error) {
			return q.PurgeRateWindows(ctx, dbq.PurgeRateWindowsParams{Before: before, Batch: retentionBatch})
		}},
		{"two_factor_attempts", TwoFactorAttemptRetention, func(ctx context.Context, before time.Time) (int64, error) {
			return q.PurgeTwoFactorAttempts(ctx, dbq.PurgeTwoFactorAttemptsParams{Before: before, Batch: retentionBatch})
		}},
		{"two_factor_challenges", TwoFactorChallengeRetention, func(ctx context.Context, before time.Time) (int64, error) {
			return q.PurgeTwoFactorChallenges(ctx, dbq.PurgeTwoFactorChallengesParams{Before: before, Batch: retentionBatch})
		}},
	}
	for _, p := range purges {
		before := now.Add(-p.keep)
		var total int64
		for {
			n, err := p.batch(ctx, before)
			if err != nil {
				return fmt.Errorf("purge %s: %w", p.name, err)
			}
			total += n
			if n < retentionBatch {
				break
			}
		}
		log.Info().Str("event", "retention_purged").Str("table", p.name).Int64("rows", total).Msg("old records deleted")
	}
	return nil
}
