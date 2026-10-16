//go:build integration

package rank_test

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore"
	"perspectiv/internal/lore/rank"
	"perspectiv/internal/testdb"
)

const (
	standingCanon   = "LORE_STANDING_CANON"
	standingSublore = "LORE_STANDING_SUBLORE"
)

// world is one test database with a place, one arc there, the lore service
// and a rank worker built the way the composition root builds it.
type world struct {
	t       *testing.T
	pool    *pgxpool.Pool
	river   *river.Client[pgx.Tx]
	enq     *jobs.Enqueuer
	svc     *lore.Service
	metrics *metrics.Metrics
	worker  river.Worker[jobs.RankArgs]
	place   uuid.UUID
	founder uuid.UUID
	arc     uuid.UUID
	users   int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	pool := testdb.New(t)
	m := metrics.New(prometheus.NewRegistry())
	client, err := jobs.NewClient(pool, zerolog.Nop(), m, nil, nil)
	if err != nil {
		t.Fatalf("river client: %v", err)
	}
	w := &world{t: t, pool: pool, river: client, enq: jobs.NewEnqueuer(client), metrics: m}
	w.svc = lore.NewService(pool, w.enq, m)
	w.worker = rank.NewWorker(pool, w.svc, m, w.enq, 4)
	w.founder = w.user()
	w.place = w.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', 'Ohio', 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`)
	w.arc = w.dayArc("2026-10-01")
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

func (w *world) user() uuid.UUID {
	w.t.Helper()
	w.users++
	name := fmt.Sprintf("user%d", w.users)
	return w.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at)
		VALUES ($1, 'x', $2, now()) RETURNING id`, name+"@example.test", name)
}

func (w *world) dayArc(date string) uuid.UUID {
	w.t.Helper()
	return w.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, $2, 'A real title', $3) RETURNING id`, w.place, date, w.founder)
}

func (w *world) hourArc(parent uuid.UUID, date string, hour int) uuid.UUID {
	w.t.Helper()
	return w.id(`INSERT INTO arcs (place_id, local_date, hour, parent_arc_id, title, created_by)
		VALUES ($1, $2, $3, $4, 'An hour title', $5) RETURNING id`, w.place, date, hour, parent, w.founder)
}

// group publishes n perspectives in arc by n new authors, chained by accepted
// claims (each perspective's author claims a character in the one before),
// so they form one connected group: one lore of n members once membership is
// recomputed. The arc is marked lore-dirty, as the claim resolver would.
func (w *world) group(arc uuid.UUID, n int) []uuid.UUID {
	w.t.Helper()
	ps := make([]uuid.UUID, n)
	for i := range ps {
		ps[i] = w.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
			VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())
			RETURNING id`, arc, w.user())
		if i == 0 {
			continue
		}
		ch := w.id(`INSERT INTO characters (perspective_id, label, position)
			VALUES ($1, 'the barista', 1) RETURNING id`, ps[i-1])
		w.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status, resolved_at)
			SELECT $1, author_id, id, 'CLAIM_STATUS_ACCEPTED', now() FROM perspectives WHERE id = $2`, ch, ps[i])
	}
	w.dirty(arc)
	return ps
}

func (w *world) dirty(arc uuid.UUID) {
	w.t.Helper()
	w.exec(`UPDATE arcs SET lore_dirty_at = clock_timestamp() WHERE id = $1`, arc)
}

// vote casts k up-votes on perspective p from k new users. The votes trigger
// marks the arc lore-dirty.
func (w *world) vote(p uuid.UUID, k int) {
	w.t.Helper()
	for range k {
		w.exec(`INSERT INTO votes (perspective_id, user_id) VALUES ($1, $2)`, p, w.user())
	}
}

// run performs one rank pass through the worker.
func (w *world) run() {
	w.t.Helper()
	if err := w.worker.Work(context.Background(), &river.Job[jobs.RankArgs]{}); err != nil {
		w.t.Fatalf("rank Work: %v", err)
	}
}

// loreOf is the lore a perspective belongs to after membership recompute.
func (w *world) loreOf(p uuid.UUID) uuid.UUID {
	w.t.Helper()
	var id uuid.NullUUID
	if err := w.pool.QueryRow(context.Background(), `SELECT lore_id FROM perspectives WHERE id = $1`, p).Scan(&id); err != nil {
		w.t.Fatalf("read lore_id: %v", err)
	}
	if !id.Valid {
		w.t.Fatalf("perspective %s is in no lore", p)
	}
	return id.UUID
}

type loreRow struct {
	status           string
	members, upvotes int32
	score            float64
	computed         bool
}

func (w *world) lore(id uuid.UUID) loreRow {
	w.t.Helper()
	var r loreRow
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status, member_count, upvotes, rank_score, computed_at IS NOT NULL FROM lores WHERE id = $1`, id).
		Scan(&r.status, &r.members, &r.upvotes, &r.score, &r.computed); err != nil {
		w.t.Fatalf("read lore: %v", err)
	}
	return r
}

type arcRow struct {
	canon   uuid.NullUUID
	score   float64
	members int32
	dirty   bool
}

func (w *world) arcState(arc uuid.UUID) arcRow {
	w.t.Helper()
	var r arcRow
	if err := w.pool.QueryRow(context.Background(),
		`SELECT canon_lore_id, canon_score, member_count, lore_dirty_at IS NOT NULL FROM arcs WHERE id = $1`, arc).
		Scan(&r.canon, &r.score, &r.members, &r.dirty); err != nil {
		w.t.Fatalf("read arc: %v", err)
	}
	return r
}

type change struct {
	from, to           uuid.NullUUID
	fromScore, toScore *float64
}

func (w *world) changes(arc uuid.UUID) []change {
	w.t.Helper()
	rows, err := w.pool.Query(context.Background(),
		`SELECT from_lore_id, to_lore_id, from_score, to_score FROM canon_changes WHERE arc_id = $1 ORDER BY id`, arc)
	if err != nil {
		w.t.Fatalf("query canon_changes: %v", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (change, error) {
		var c change
		err := r.Scan(&c.from, &c.to, &c.fromScore, &c.toScore)
		return c, err
	})
	if err != nil {
		w.t.Fatalf("scan canon_changes: %v", err)
	}
	return out
}

// canonNotifications returns the lore id of every canon-change notify job
// for arc, in insert order; "" for a cleared canon.
func (w *world) canonNotifications(arc uuid.UUID) []string {
	w.t.Helper()
	rows, err := w.pool.Query(context.Background(), `SELECT args->>'LoreID' FROM river_job
		WHERE kind = 'notify' AND (args->>'Kind')::int = $2 AND args->>'ArcID' = $1 ORDER BY id`,
		arc.String(), int32(perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE))
	if err != nil {
		w.t.Fatalf("query notify jobs: %v", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		w.t.Fatalf("scan notify jobs: %v", err)
	}
	for i, s := range out {
		if s == uuid.Nil.String() {
			out[i] = ""
		}
	}
	return out
}

func scoreOf(members, upvotes int) float64 {
	return math.Log(1+float64(members)) * math.Log(1+float64(upvotes))
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func valid(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }
