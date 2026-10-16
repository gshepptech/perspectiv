// Package rank is the periodic lore rank job. Each pass takes every arc
// marked lore-dirty, recomputes its lore membership through the lore
// service, scores every lore in it and moves canon to the best eligible lore,
// auditing every change.
//
// It is the only writer of lores.status, lores.rank_score, lores.upvotes,
// arcs.canon_lore_id, arcs.canon_score, arcs.member_count and canon_changes.
// Request handlers never write them; they mark the arc lore-dirty instead.
//
// Each arc is ranked on its own: an hour arc's canon is not its day arc's,
// and nothing is computed across place levels.
package rank

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore"
	"perspectiv/internal/lore/rank/dbq"
)

const (
	// DefaultInterval is how often the rank pass runs.
	DefaultInterval = 60 * time.Second
	// DefaultMinMembers is the fewest confirmed members a lore needs to be
	// eligible for canon.
	DefaultMinMembers = 4

	// arcPage is how many dirty arcs one query fetches.
	arcPage = 200
	// passTimeout bounds one pass. Arcs left unprocessed stay marked and the
	// next pass takes them.
	passTimeout = 5 * time.Minute
)

var (
	standingCanon   = perspectivv1.LoreStanding_LORE_STANDING_CANON.String()
	standingSublore = perspectivv1.LoreStanding_LORE_STANDING_SUBLORE.String()
)

// standing is one lore's inputs and score in a pass.
type standing struct {
	id               uuid.UUID
	members, upvotes int32
	canon            bool
	score            float64
}

// score is log(1+members) * log(1+upvotes), natural logs. Upvotes are the
// up-votes on the lore's member perspectives; no downvote exists.
func score(members, upvotes int32) float64 {
	return math.Log1p(float64(members)) * math.Log1p(float64(upvotes))
}

// pickCanon returns the index of the lore that should be canon, or false
// when no lore has minMembers members. Smaller lores are scored but never
// eligible, however high they score. A lore with no upvotes is still
// eligible: membership is the only gate, so a 4-member lore with no votes
// becomes canon when it is the only qualifier.
//
// Equal scores: the current canon keeps canon, so a challenger has to
// strictly outscore it and canon does not flap between tied lores. Among
// tied challengers the one with more members wins, then the lowest lore id
// (ids are UUIDv7, so the oldest lore).
func pickCanon(lores []standing, minMembers int32) (int, bool) {
	best := -1
	var top standing
	for i, l := range lores {
		if l.members < minMembers {
			continue
		}
		if best < 0 || better(l, top) {
			best, top = i, l
		}
	}
	return best, best >= 0
}

// better reports whether a outranks b under pickCanon's order.
func better(a, b standing) bool {
	switch {
	case a.score != b.score:
		return a.score > b.score
	case a.canon != b.canon:
		return a.canon
	case a.members != b.members:
		return a.members > b.members
	}
	return bytes.Compare(a.id[:], b.id[:]) < 0
}

// worker runs the rank pass.
type worker struct {
	river.WorkerDefaults[jobs.RankArgs]
	pool       *pgxpool.Pool
	lore       *lore.Service
	metrics    *metrics.Metrics
	enq        *jobs.Enqueuer
	minMembers int32
}

// NewWorker returns the worker for the periodic rank job. minMembers is the
// canon eligibility minimum; a value below 1 means DefaultMinMembers.
func NewWorker(pool *pgxpool.Pool, svc *lore.Service, m *metrics.Metrics, enq *jobs.Enqueuer, minMembers int) river.Worker[jobs.RankArgs] {
	if minMembers < 1 || minMembers > math.MaxInt32 {
		minMembers = DefaultMinMembers
	}
	return &worker{pool: pool, lore: svc, metrics: m, enq: enq, minMembers: int32(minMembers)}
}

// PeriodicJob schedules the rank pass every interval (DefaultInterval when
// interval is not positive), with one pass as soon as the worker starts. A
// failed pass is not retried: the arcs it did not finish stay marked and the
// next scheduled pass takes them.
func PeriodicJob(interval time.Duration) *river.PeriodicJob {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) {
			return jobs.RankArgs{}, &river.InsertOpts{MaxAttempts: 1}
		},
		&river.PeriodicJobOpts{ID: jobs.RankArgs{}.Kind(), RunOnStart: true},
	)
}

// Timeout overrides River's one-minute default for a pass.
func (w *worker) Timeout(*river.Job[jobs.RankArgs]) time.Duration { return passTimeout }

// Work ranks every arc marked lore-dirty, each in its own transaction. Votes
// mark the arc through a trigger, so an arc whose vote totals changed is
// among them. One arc's failure does not stop the others; the pass returns
// every arc error joined, and the failed arcs stay marked.
//
// Arcs are walked in id order from a cursor, so a pass ends even while
// writers keep marking arcs; an arc marked behind the cursor waits for the
// next pass.
func (w *worker) Work(ctx context.Context, _ *river.Job[jobs.RankArgs]) error {
	q := dbq.New(w.pool)
	var errs []error
	after := uuid.Nil
	for {
		ids, err := q.DirtyArcs(ctx, dbq.DirtyArcsParams{After: after, RowLimit: arcPage})
		if err != nil {
			return errors.Join(append(errs, fmt.Errorf("list dirty arcs: %w", err))...)
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return errors.Join(append(errs, err)...)
			}
			if err := w.rankArc(ctx, id); err != nil {
				errs = append(errs, fmt.Errorf("rank arc %s: %w", id, err))
			}
		}
		if len(ids) < arcPage {
			return errors.Join(errs...)
		}
		after = ids[len(ids)-1]
	}
}

// arcResult is what one arc's pass decided, for the caller to count once the
// transaction commits.
type arcResult struct {
	changed bool
}

func (w *worker) rankArc(ctx context.Context, arcID uuid.UUID) error {
	var res arcResult
	err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		var err error
		res, err = w.rankArcTx(ctx, tx, arcID)
		return err
	})
	if err != nil {
		return err
	}
	if res.changed {
		w.metrics.CanonChanges.Inc()
	}
	return nil
}

// rankArcTx recomputes and ranks one arc inside tx.
//
// The dirty mark is read first, before the lore lock, and cleared at the end
// only if it still holds that value. Writers outside the lore domain (a
// perspective delete or hide, a vote) mark the arc without taking the lore
// lock, so their change may land after membership or votes were read here;
// their mark then differs from the one read and survives for the next pass.
// The arc row is written last, the lock order every lore writer follows.
func (w *worker) rankArcTx(ctx context.Context, tx pgx.Tx, arcID uuid.UUID) (arcResult, error) {
	q := dbq.New(tx)
	seen, err := q.ArcDirtyMark(ctx, arcID)
	if errors.Is(err, pgx.ErrNoRows) {
		return arcResult{}, nil
	}
	if err != nil {
		return arcResult{}, fmt.Errorf("read dirty mark: %w", err)
	}
	if !seen.Valid {
		// Another pass took this arc after it was listed.
		return arcResult{}, nil
	}
	if err := w.lore.RecomputeArc(ctx, tx, arcID); err != nil {
		return arcResult{}, fmt.Errorf("recompute membership: %w", err)
	}

	rows, err := q.ArcLoreTallies(ctx, arcID)
	if err != nil {
		return arcResult{}, fmt.Errorf("tally lores: %w", err)
	}
	lores, incumbent, totalMembers := standings(rows)
	if err := storeScores(ctx, q, arcID, lores); err != nil {
		return arcResult{}, err
	}

	next := -1
	if i, ok := pickCanon(lores, w.minMembers); ok {
		next = i
	}
	changed := idAt(lores, next) != idAt(lores, incumbent)
	if changed {
		if err := w.moveCanon(ctx, tx, q, arcID, lores, incumbent, next); err != nil {
			return arcResult{}, err
		}
	}

	arc := dbq.StoreArcCanonParams{ID: arcID, MemberCount: totalMembers, Seen: seen}
	if next >= 0 {
		arc.CanonLoreID = uuid.NullUUID{UUID: lores[next].id, Valid: true}
		arc.CanonScore = lores[next].score
	}
	if err := q.StoreArcCanon(ctx, arc); err != nil {
		return arcResult{}, fmt.Errorf("store arc canon: %w", err)
	}
	return arcResult{changed: changed}, nil
}

// standings scores the tallied lores. It returns them, the index of the
// current canon (-1 when none) and the arc's member count: every confirmed
// member across the arc's lores.
func standings(rows []dbq.ArcLoreTalliesRow) ([]standing, int, int32) {
	lores := make([]standing, len(rows))
	incumbent := -1
	var total int32
	for i, r := range rows {
		lores[i] = standing{
			id: r.ID, members: r.MemberCount, upvotes: r.Upvotes,
			canon: r.Status == standingCanon, score: score(r.MemberCount, r.Upvotes),
		}
		if lores[i].canon {
			incumbent = i
		}
		total += r.MemberCount
	}
	return lores, incumbent, total
}

func storeScores(ctx context.Context, q *dbq.Queries, arcID uuid.UUID, lores []standing) error {
	if len(lores) == 0 {
		return nil
	}
	p := dbq.StoreLoreScoresParams{ArcID: arcID}
	for _, l := range lores {
		p.Ids = append(p.Ids, l.id)
		p.Upvotes = append(p.Upvotes, l.upvotes)
		p.Scores = append(p.Scores, l.score)
	}
	if err := q.StoreLoreScores(ctx, p); err != nil {
		return fmt.Errorf("store lore scores: %w", err)
	}
	return nil
}

// moveCanon demotes the current canon, promotes the next one (either may be
// absent), writes the audit row with both ids and the scores of this pass,
// and enqueues the canon-change event for notify to fan out.
func (w *worker) moveCanon(ctx context.Context, tx pgx.Tx, q *dbq.Queries, arcID uuid.UUID,
	lores []standing, incumbent, next int,
) error {
	if err := q.SetArcStanding(ctx, dbq.SetArcStandingParams{
		ArcID: arcID, FromStatus: standingCanon, ToStatus: standingSublore, KeepID: idAt(lores, next),
	}); err != nil {
		return fmt.Errorf("demote canon: %w", err)
	}
	if next >= 0 {
		if err := q.SetLoreStanding(ctx, dbq.SetLoreStandingParams{
			ID: lores[next].id, ArcID: arcID, Status: standingCanon,
		}); err != nil {
			return fmt.Errorf("promote canon: %w", err)
		}
	}
	audit := dbq.InsertCanonChangeParams{ArcID: arcID}
	if incumbent >= 0 {
		audit.FromLoreID = uuid.NullUUID{UUID: lores[incumbent].id, Valid: true}
		audit.FromScore = pgtype.Float8{Float64: lores[incumbent].score, Valid: true}
	}
	if next >= 0 {
		audit.ToLoreID = uuid.NullUUID{UUID: lores[next].id, Valid: true}
		audit.ToScore = pgtype.Float8{Float64: lores[next].score, Valid: true}
	}
	if err := q.InsertCanonChange(ctx, audit); err != nil {
		return fmt.Errorf("audit canon change: %w", err)
	}
	if err := w.enq.NotifyTx(ctx, tx, jobs.NotifyArgs{
		Kind:   perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE,
		ArcID:  arcID,
		LoreID: idAt(lores, next),
	}); err != nil {
		return fmt.Errorf("enqueue canon change: %w", err)
	}
	return nil
}

// idAt is the lore id at i, uuid.Nil for -1.
func idAt(lores []standing, i int) uuid.UUID {
	if i < 0 {
		return uuid.Nil
	}
	return lores[i].id
}
