//go:build integration

package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/testdb"
)

const waitTimeout = 45 * time.Second

// world holds one arc at one store with one author's PENDING perspective, the
// starting point of every expiry case.
type world struct {
	t           *testing.T
	pool        *pgxpool.Pool
	arc         uuid.UUID
	author      uuid.UUID
	perspective uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, pool: testdb.New(t)}
	w.author = w.user("author")
	place := w.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', 'Ohio', 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`)
	w.arc = w.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-01', 'A real title', $2) RETURNING id`, place, w.author)
	w.perspective = w.published(w.author)
	return w
}

func (w *world) id(query string, args ...any) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(), query, args...).Scan(&id); err != nil {
		w.t.Fatalf("%s: %v", query, err)
	}
	return id
}

func (w *world) exec(query string, args ...any) {
	w.t.Helper()
	if _, err := w.pool.Exec(context.Background(), query, args...); err != nil {
		w.t.Fatalf("%s: %v", query, err)
	}
}

func (w *world) user(name string) uuid.UUID {
	w.t.Helper()
	return w.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at)
		VALUES ($1, 'x', $2, now()) RETURNING id`, name+"@example.test", name)
}

func (w *world) published(author uuid.UUID) uuid.UUID {
	w.t.Helper()
	return w.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())
		RETURNING id`, w.arc, author)
}

func (w *world) character(perspective uuid.UUID) uuid.UUID {
	w.t.Helper()
	return w.id(`INSERT INTO characters (perspective_id, label, position)
		VALUES ($1, 'the barista', 1) RETURNING id`, perspective)
}

// claim records that the author of claimantPerspective says character was them.
func (w *world) claim(character, claimantPerspective uuid.UUID, status string) {
	w.t.Helper()
	w.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status)
		SELECT $1, author_id, id, $3 FROM perspectives WHERE id = $2`, character, claimantPerspective, status)
}

func (w *world) loreStatus(perspective uuid.UUID) string {
	w.t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT lore_status FROM perspectives WHERE id = $1`, perspective).Scan(&s); err != nil {
		w.t.Fatalf("read lore_status: %v", err)
	}
	return s
}

func (w *world) arcDirty() bool {
	w.t.Helper()
	var dirty bool
	if err := w.pool.QueryRow(context.Background(),
		`SELECT lore_dirty_at IS NOT NULL FROM arcs WHERE id = $1`, w.arc).Scan(&dirty); err != nil {
		w.t.Fatalf("read lore_dirty_at: %v", err)
	}
	return dirty
}

func newMetrics() *metrics.Metrics {
	return metrics.New(prometheus.NewRegistry())
}

func quietLog() zerolog.Logger {
	return zerolog.New(io.Discard)
}

// TestExpiryWorker covers the PENDING to SOLO transition and its guard: only
// an ACCEPTED claim in either direction keeps a perspective out of SOLO.
func TestExpiryWorker(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		setup     func(w *world)
		target    func(w *world) uuid.UUID
		wantState string
		wantDirty bool
	}{
		{
			name:      "no claims becomes solo (AC-022, ST-002)",
			wantState: "LORE_STATUS_SOLO", wantDirty: true,
		},
		{
			name: "pending claim on its character does not count (FR-057, OT-022)",
			setup: func(w *world) {
				other := w.published(w.user("other"))
				w.claim(w.character(w.perspective), other, "CLAIM_STATUS_PENDING")
			},
			wantState: "LORE_STATUS_SOLO", wantDirty: true,
		},
		{
			name: "pending claim it made does not count (FR-057)",
			setup: func(w *world) {
				other := w.published(w.user("other"))
				w.claim(w.character(other), w.perspective, "CLAIM_STATUS_PENDING")
			},
			wantState: "LORE_STATUS_SOLO", wantDirty: true,
		},
		{
			name:      "other perspectives in the arc do not count (FR-057, OT-022)",
			setup:     func(w *world) { w.published(w.user("other")); w.published(w.user("third")) },
			wantState: "LORE_STATUS_SOLO", wantDirty: true,
		},
		{
			name: "rejected claim does not count",
			setup: func(w *world) {
				other := w.published(w.user("other"))
				w.claim(w.character(w.perspective), other, "CLAIM_STATUS_REJECTED")
			},
			wantState: "LORE_STATUS_SOLO", wantDirty: true,
		},
		{
			name: "accepted claim on its character keeps it pending (ST-002)",
			setup: func(w *world) {
				other := w.published(w.user("other"))
				w.claim(w.character(w.perspective), other, "CLAIM_STATUS_ACCEPTED")
			},
			wantState: "LORE_STATUS_PENDING", wantDirty: false,
		},
		{
			name: "accepted claim it made keeps it pending (ST-002)",
			setup: func(w *world) {
				other := w.published(w.user("other"))
				w.claim(w.character(other), w.perspective, "CLAIM_STATUS_ACCEPTED")
			},
			wantState: "LORE_STATUS_PENDING", wantDirty: false,
		},
		{
			name: "in lore is left alone",
			setup: func(w *world) {
				w.exec(`UPDATE perspectives SET lore_status = 'LORE_STATUS_IN_LORE' WHERE id = $1`, w.perspective)
			},
			wantState: "LORE_STATUS_IN_LORE", wantDirty: false,
		},
		{
			name: "removed is left alone",
			setup: func(w *world) {
				w.exec(`UPDATE perspectives SET lore_status = 'LORE_STATUS_REMOVED' WHERE id = $1`, w.perspective)
			},
			wantState: "LORE_STATUS_REMOVED", wantDirty: false,
		},
		{
			name: "already solo is left alone and does not dirty the arc",
			setup: func(w *world) {
				w.exec(`UPDATE perspectives SET lore_status = 'LORE_STATUS_SOLO' WHERE id = $1`, w.perspective)
			},
			wantState: "LORE_STATUS_SOLO", wantDirty: false,
		},
		{
			name:      "deleted before the job fires is left alone",
			setup:     func(w *world) { w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, w.perspective) },
			wantState: "LORE_STATUS_PENDING", wantDirty: false,
		},
		{
			name:      "hidden before the job fires is left alone",
			setup:     func(w *world) { w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, w.perspective) },
			wantState: "LORE_STATUS_PENDING", wantDirty: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			if tc.setup != nil {
				tc.setup(w)
			}
			worker := jobs.NewExpiryWorker(w.pool)
			job := &river.Job[jobs.ExpiryArgs]{Args: jobs.ExpiryArgs{PerspectiveID: w.perspective}}
			if err := worker.Work(context.Background(), job); err != nil {
				t.Fatalf("Work: %v", err)
			}
			if got := w.loreStatus(w.perspective); got != tc.wantState {
				t.Fatalf("lore_status = %s, want %s", got, tc.wantState)
			}
			if got := w.arcDirty(); got != tc.wantDirty {
				t.Fatalf("arc lore_dirty_at set = %v, want %v", got, tc.wantDirty)
			}
			// A second run (River retries, duplicate jobs) changes nothing more.
			if err := worker.Work(context.Background(), job); err != nil {
				t.Fatalf("second Work: %v", err)
			}
			if got := w.loreStatus(w.perspective); got != tc.wantState {
				t.Fatalf("after second run lore_status = %s, want %s", got, tc.wantState)
			}
		})
	}
}

func TestExpiryWorkerUnknownPerspectiveCompletes(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	worker := jobs.NewExpiryWorker(pool)
	job := &river.Job[jobs.ExpiryArgs]{Args: jobs.ExpiryArgs{PerspectiveID: uuid.New()}}
	if err := worker.Work(context.Background(), job); err != nil {
		t.Fatalf("Work on a missing perspective = %v, want nil so the job does not retry forever", err)
	}
}

type jobRow struct {
	state       string
	scheduledAt time.Time
	perspective uuid.UUID
}

func expiryJobs(t *testing.T, pool *pgxpool.Pool) []jobRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT state::text, scheduled_at, (args->>'PerspectiveID')::uuid
		FROM river_job WHERE kind = 'perspective_expiry' ORDER BY id`)
	if err != nil {
		t.Fatalf("query river_job: %v", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (jobRow, error) {
		var j jobRow
		err := r.Scan(&j.state, &j.scheduledAt, &j.perspective)
		return j, err
	})
	if err != nil {
		t.Fatalf("scan river_job: %v", err)
	}
	return out
}

// enqueueExpiry inserts the expiry job for w's perspective at its publish time
// plus the default window, in a transaction it commits or rolls back.
func enqueueExpiry(t *testing.T, w *world, commit bool) time.Time {
	t.Helper()
	client, err := jobs.NewClient(w.pool, quietLog(), newMetrics(), nil, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	var publishedAt time.Time
	if err := w.pool.QueryRow(ctx, `SELECT published_at FROM perspectives WHERE id = $1`, w.perspective).
		Scan(&publishedAt); err != nil {
		t.Fatalf("read published_at: %v", err)
	}
	runAt := publishedAt.Add(jobs.DefaultExpiryWindow)

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := jobs.NewEnqueuer(client).ExpiryTx(ctx, tx, w.perspective, runAt); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ExpiryTx: %v", err)
	}
	if commit {
		err = tx.Commit(ctx)
	} else {
		err = tx.Rollback(ctx)
	}
	if err != nil {
		t.Fatalf("finish tx: %v", err)
	}
	return runAt
}

// The expiry job is inserted inside the caller's transaction: it exists right
// after commit with ScheduledAt equal to the run time, and not at all when the
// transaction rolls back.
func TestExpiryTxIsTransactional(t *testing.T) {
	t.Parallel()
	t.Run("commit leaves a scheduled job (FR-047, OT-022)", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		runAt := enqueueExpiry(t, w, true)
		got := expiryJobs(t, w.pool)
		if len(got) != 1 {
			t.Fatalf("expiry jobs = %d, want 1", len(got))
		}
		want := jobRow{state: "scheduled", scheduledAt: runAt, perspective: w.perspective}
		if got[0].state != want.state || !got[0].scheduledAt.Equal(want.scheduledAt) || got[0].perspective != want.perspective {
			t.Fatalf("job = %+v, want %+v (scheduled at published_at + 7 days)", got[0], want)
		}
	})
	t.Run("rollback leaves no job", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		enqueueExpiry(t, w, false)
		if got := expiryJobs(t, w.pool); len(got) != 0 {
			t.Fatalf("expiry jobs = %d, want 0", len(got))
		}
	})
}

func TestEmailAndNotifyTxInsertJobs(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	client, err := jobs.NewClient(pool, quietLog(), newMetrics(), nil, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	enq := jobs.NewEnqueuer(client)
	ctx := context.Background()
	arc := uuid.New()
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := enq.EmailTx(ctx, tx, jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplatePasswordReset,
			Data: map[string]string{"token": "abc"}}); err != nil {
			return err
		}
		return enq.NotifyTx(ctx, tx, jobs.NotifyArgs{
			Kind:  perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED,
			ArcID: arc,
		})
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var kinds []string
	rows, err := pool.Query(ctx, `SELECT kind FROM river_job WHERE state = 'available' ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	kinds, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(kinds) != 2 || kinds[0] != "email" || kinds[1] != "notify" {
		t.Fatalf("job kinds = %v, want [email notify]", kinds)
	}
	var token, arcArg string
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT args->'Data'->>'token' FROM river_job WHERE kind = 'email'),
		(SELECT args->>'ArcID' FROM river_job WHERE kind = 'notify')`).Scan(&token, &arcArg); err != nil {
		t.Fatalf("read args: %v", err)
	}
	if token != "abc" || arcArg != arc.String() {
		t.Fatalf("args token=%q arc=%q, want abc and %s", token, arcArg, arc)
	}
	// The stored notify args decode into a NotifyArgs, which is what a notify
	// worker reads.
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT args FROM river_job WHERE kind = 'notify'`).Scan(&raw); err != nil {
		t.Fatalf("read notify args: %v", err)
	}
	var decoded jobs.NotifyArgs
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode notify args %s: %v", raw, err)
	}
	want := jobs.NotifyArgs{Kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED, ArcID: arc}
	if decoded != want {
		t.Fatalf("notify args = %+v, want %+v", decoded, want)
	}
}

// An insert-only client (ROLE=api) inserts jobs but refuses to work them.
func TestInsertOnlyClientDoesNotStart(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	client, err := jobs.NewClient(pool, quietLog(), newMetrics(), nil, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Start(context.Background()); err == nil {
		_ = client.Stop(context.Background())
		t.Fatal("Start on an insert-only client succeeded, want an error")
	}
}

func TestNewClientRefusesPeriodicWithoutWorkers(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	periodic := []*river.PeriodicJob{river.NewPeriodicJob(river.PeriodicInterval(time.Minute),
		func() (river.JobArgs, *river.InsertOpts) { return jobs.RankArgs{}, nil }, nil)}
	if _, err := jobs.NewClient(pool, quietLog(), newMetrics(), nil, periodic); err == nil {
		t.Fatal("NewClient with periodic jobs and no workers succeeded, want an error")
	}
}

func startClient(t *testing.T, pool *pgxpool.Pool, m *metrics.Metrics, workers *river.Workers) *river.Client[pgx.Tx] {
	t.Helper()
	client, err := jobs.NewClient(pool, quietLog(), m, workers, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := client.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = client.Stop(stopCtx)
		cancel()
	})
	return client
}

func waitFor(t *testing.T, events <-chan *river.Event, kind string) *river.Event {
	t.Helper()
	timer := time.NewTimer(waitTimeout)
	defer timer.Stop()
	for {
		select {
		case ev := <-events:
			if ev.Job.Kind == kind {
				return ev
			}
		case <-timer.C:
			t.Fatalf("no %s event within %s", kind, waitTimeout)
			return nil
		}
	}
}

// End to end through River: a job scheduled for the future does nothing until
// its time comes, then the running client marks the perspective SOLO.
func TestExpiryJobFiresAtScheduledTime(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	// A pending claim and another perspective must not save it (OT-022).
	other := w.published(w.user("other"))
	w.claim(w.character(w.perspective), other, "CLAIM_STATUS_PENDING")

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewExpiryWorker(w.pool))
	client := startClient(t, w.pool, newMetrics(), workers)
	done, cancel := client.Subscribe(river.EventKindJobCompleted)
	t.Cleanup(cancel)

	ctx := context.Background()
	err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		return jobs.NewEnqueuer(client).ExpiryTx(ctx, tx, w.perspective, time.Now().Add(jobs.DefaultExpiryWindow))
	})
	if err != nil {
		t.Fatalf("ExpiryTx: %v", err)
	}

	// Before the window ends the job stays scheduled and the perspective PENDING.
	time.Sleep(2 * time.Second)
	if got := w.loreStatus(w.perspective); got != "LORE_STATUS_PENDING" {
		t.Fatalf("before expiry lore_status = %s, want LORE_STATUS_PENDING", got)
	}
	if jobsNow := expiryJobs(t, w.pool); len(jobsNow) != 1 || jobsNow[0].state != "scheduled" {
		t.Fatalf("before expiry jobs = %+v, want one scheduled", jobsNow)
	}

	// Seven days pass.
	w.exec(`UPDATE river_job SET scheduled_at = now() - interval '1 second' WHERE kind = 'perspective_expiry'`)
	waitFor(t, done, jobs.ExpiryArgs{}.Kind())

	if got := w.loreStatus(w.perspective); got != "LORE_STATUS_SOLO" {
		t.Fatalf("after expiry lore_status = %s, want LORE_STATUS_SOLO", got)
	}
	if got := w.loreStatus(other); got != "LORE_STATUS_PENDING" {
		t.Fatalf("other perspective lore_status = %s, want it untouched", got)
	}
	if !w.arcDirty() {
		t.Fatal("arc lore_dirty_at not set after expiry")
	}
}

type failArgs struct{}

func (failArgs) Kind() string { return "test_fail" }

type failWorker struct {
	river.WorkerDefaults[failArgs]
}

func (failWorker) Work(context.Context, *river.Job[failArgs]) error { return errors.New("boom") }

type panicArgs struct{}

func (panicArgs) Kind() string { return "test_panic" }

type panicWorker struct {
	river.WorkerDefaults[panicArgs]
}

func (panicWorker) Work(context.Context, *river.Job[panicArgs]) error { panic("boom") }

// Every failed attempt, error or panic, counts in job_failures_total (NFR-009).
func TestFailedAttemptsIncrementJobFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args river.JobArgs
	}{
		{name: "error", args: failArgs{}},
		{name: "panic", args: panicArgs{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pool := testdb.New(t)
			m := newMetrics()
			workers := river.NewWorkers()
			river.AddWorker[failArgs](workers, failWorker{})
			river.AddWorker[panicArgs](workers, panicWorker{})
			client := startClient(t, pool, m, workers)
			failed, cancel := client.Subscribe(river.EventKindJobFailed)
			t.Cleanup(cancel)

			if _, err := client.Insert(context.Background(), tc.args, &river.InsertOpts{MaxAttempts: 1}); err != nil {
				t.Fatalf("Insert: %v", err)
			}
			waitFor(t, failed, tc.args.Kind())
			if got := testutil.ToFloat64(m.JobFailures); got != 1 {
				t.Fatalf("job_failures_total = %v, want 1", got)
			}
		})
	}
}
