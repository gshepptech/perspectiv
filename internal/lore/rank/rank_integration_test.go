//go:build integration

package rank_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"

	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore/rank"
)

// Two 4-member lores: the higher score becomes canon, scores and counts are
// stored, an audit row records the change, the arc's dirty mark is cleared,
// the metric counts it and a canon-change event is enqueued.
func TestRankPromotesHighestScoringLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.group(w.arc, 4)
	b := w.group(w.arc, 4)
	w.vote(a[0], 2)
	w.vote(b[1], 3)
	w.vote(b[2], 2)

	w.run()

	loreA, loreB := w.loreOf(a[0]), w.loreOf(b[0])
	if loreA == loreB {
		t.Fatalf("two disconnected groups share lore %s", loreA)
	}
	for _, p := range a {
		if w.loreOf(p) != loreA {
			t.Fatalf("group A split across lores")
		}
	}
	gotA, gotB := w.lore(loreA), w.lore(loreB)
	if gotB.status != standingCanon || gotA.status != standingSublore {
		t.Fatalf("standings A=%s B=%s, want B canon", gotA.status, gotB.status)
	}
	if gotA.members != 4 || gotA.upvotes != 2 || !near(gotA.score, scoreOf(4, 2)) || !gotA.computed {
		t.Fatalf("lore A = %+v, want 4 members, 2 upvotes, score %v", gotA, scoreOf(4, 2))
	}
	if gotB.members != 4 || gotB.upvotes != 5 || !near(gotB.score, scoreOf(4, 5)) {
		t.Fatalf("lore B = %+v, want 4 members, 5 upvotes, score %v", gotB, scoreOf(4, 5))
	}
	arc := w.arcState(w.arc)
	if arc.canon != valid(loreB) || !near(arc.score, scoreOf(4, 5)) || arc.members != 8 || arc.dirty {
		t.Fatalf("arc = %+v, want canon %s, canon score %v, 8 members, mark cleared", arc, loreB, scoreOf(4, 5))
	}
	ch := w.changes(w.arc)
	if len(ch) != 1 || ch[0].from.Valid || ch[0].to != valid(loreB) || ch[0].fromScore != nil ||
		ch[0].toScore == nil || !near(*ch[0].toScore, scoreOf(4, 5)) {
		t.Fatalf("canon_changes = %+v, want one row none -> %s with its score", ch, loreB)
	}
	if got := testutil.ToFloat64(w.metrics.CanonChanges); got != 1 {
		t.Fatalf("canon_changes_total = %v, want 1", got)
	}
	if got := w.canonNotifications(w.arc); len(got) != 1 || got[0] != loreB.String() {
		t.Fatalf("canon-change notify jobs = %v, want [%s]", got, loreB)
	}
}

// A 3-member lore is ranked but never canon, however many upvotes it has; the
// best 4-member lore still becomes canon.
func TestRankSmallLoreNeverEligible(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	small := w.group(w.arc, 3)
	big := w.group(w.arc, 4)
	w.vote(small[0], 40)
	w.vote(big[0], 1)

	w.run()

	s, b := w.lore(w.loreOf(small[0])), w.lore(w.loreOf(big[0]))
	if s.score <= b.score {
		t.Fatalf("setup: small lore score %v should outscore the big lore's %v", s.score, b.score)
	}
	if s.status != standingSublore || b.status != standingCanon {
		t.Fatalf("standings small=%s big=%s, want big canon", s.status, b.status)
	}
	if !near(s.score, scoreOf(3, 40)) {
		t.Fatalf("small lore score = %v, want it ranked at %v", s.score, scoreOf(3, 40))
	}
}

// With no lore of 4 members there is no canon and no audit row.
func TestRankNoQualifyingLoreLeavesNoCanon(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	small := w.group(w.arc, 3)
	w.vote(small[1], 10)

	w.run()

	if got := w.lore(w.loreOf(small[0])); got.status != standingSublore || got.members != 3 || got.upvotes != 10 {
		t.Fatalf("lore = %+v, want a ranked 3-member sublore", got)
	}
	arc := w.arcState(w.arc)
	if arc.canon.Valid || arc.score != 0 || arc.members != 3 || arc.dirty {
		t.Fatalf("arc = %+v, want no canon, score 0, 3 members, mark cleared", arc)
	}
	if ch := w.changes(w.arc); len(ch) != 0 {
		t.Fatalf("canon_changes = %+v, want none", ch)
	}
}

// A sublore that overtakes the canon takes it; the old canon becomes sublore
// and the audit row records both ids and both scores.
func TestRankChallengerOvertakesCanon(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.group(w.arc, 4)
	b := w.group(w.arc, 5)
	w.vote(a[0], 3)
	w.run()
	loreA, loreB := w.loreOf(a[0]), w.loreOf(b[0])
	if w.lore(loreA).status != standingCanon {
		t.Fatalf("setup: lore A should be canon first")
	}

	w.vote(b[3], 4)
	if !w.arcState(w.arc).dirty {
		t.Fatalf("a new vote should mark the arc dirty")
	}
	w.run()

	if w.lore(loreA).status != standingSublore || w.lore(loreB).status != standingCanon {
		t.Fatalf("after overtaking: A=%s B=%s, want B canon and A sublore",
			w.lore(loreA).status, w.lore(loreB).status)
	}
	ch := w.changes(w.arc)
	if len(ch) != 2 {
		t.Fatalf("canon_changes = %+v, want 2 rows", ch)
	}
	last := ch[1]
	if last.from != valid(loreA) || last.to != valid(loreB) || last.fromScore == nil || last.toScore == nil ||
		!near(*last.fromScore, scoreOf(4, 3)) || !near(*last.toScore, scoreOf(5, 4)) {
		t.Fatalf("audit row = %+v, want %s (%v) -> %s (%v)", last, loreA, scoreOf(4, 3), loreB, scoreOf(5, 4))
	}
	if arc := w.arcState(w.arc); arc.canon != valid(loreB) || !near(arc.score, scoreOf(5, 4)) || arc.members != 9 {
		t.Fatalf("arc = %+v, want canon %s", arc, loreB)
	}
	if got := testutil.ToFloat64(w.metrics.CanonChanges); got != 2 {
		t.Fatalf("canon_changes_total = %v, want 2", got)
	}
}

// Running again with nothing changed writes nothing new: no audit row, no
// metric, no event; and a clean arc is not processed at all.
func TestRankIsIdempotent(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.group(w.arc, 4)
	w.vote(a[0], 1)
	w.run()
	w.run()
	w.dirty(w.arc)
	w.run()

	if ch := w.changes(w.arc); len(ch) != 1 {
		t.Fatalf("canon_changes = %+v, want 1 row after three passes", ch)
	}
	if got := testutil.ToFloat64(w.metrics.CanonChanges); got != 1 {
		t.Fatalf("canon_changes_total = %v, want 1", got)
	}
	if got := w.canonNotifications(w.arc); len(got) != 1 {
		t.Fatalf("canon-change events = %v, want 1", got)
	}
	if w.lore(w.loreOf(a[0])).status != standingCanon {
		t.Fatalf("canon lost on a no-op pass")
	}
}

// A canon lore reduced below 4 members by a deletion loses canon at the next
// run; with no other qualifier the arc's canon is cleared and audited.
func TestRankDemotesShrunkCanonAndClears(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		shrink func(w *world, member uuid.UUID)
	}{
		{
			name: "perspective deleted",
			shrink: func(w *world, member uuid.UUID) {
				w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, member)
				w.exec(`UPDATE arcs SET lore_dirty_at = now() FROM perspectives p WHERE p.id = $1 AND arcs.id = p.arc_id`, member)
			},
		},
		{
			name: "perspective hidden by moderation",
			shrink: func(w *world, member uuid.UUID) {
				w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, member)
				w.exec(`UPDATE arcs SET lore_dirty_at = now() FROM perspectives p WHERE p.id = $1 AND arcs.id = p.arc_id`, member)
			},
		},
		{
			name: "perspective voted out of lore",
			shrink: func(w *world, member uuid.UUID) {
				err := pgx.BeginFunc(context.Background(), w.pool, func(tx pgx.Tx) error {
					return w.svc.RemoveFromLore(context.Background(), tx, member)
				})
				if err != nil {
					w.t.Fatalf("RemoveFromLore: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			a := w.group(w.arc, 4)
			w.vote(a[1], 5)
			w.run()
			canon := w.loreOf(a[0])
			if w.lore(canon).status != standingCanon {
				t.Fatalf("setup: lore should be canon")
			}

			// The chain's end member drops out, leaving a connected 3.
			tc.shrink(w, a[3])
			w.run()

			assertCleared(t, w, canon, scoreOf(3, 5))
		})
	}
}

// assertCleared checks that canon, once promoted, was demoted to a 3-member
// sublore and the arc's canon cleared, with the clearing audited and
// announced.
func assertCleared(t *testing.T, w *world, canon uuid.UUID, fromScore float64) {
	t.Helper()
	got := w.lore(canon)
	if got.status != standingSublore || got.members != 3 {
		t.Fatalf("lore = %+v, want a 3-member sublore", got)
	}
	arc := w.arcState(w.arc)
	if arc.canon.Valid || arc.score != 0 || arc.dirty {
		t.Fatalf("arc = %+v, want canon cleared, score 0", arc)
	}
	ch := w.changes(w.arc)
	if len(ch) != 2 {
		t.Fatalf("canon_changes = %+v, want promotion then clearing", ch)
	}
	if c := ch[1]; c.from != valid(canon) || c.to.Valid || c.fromScore == nil || c.toScore != nil ||
		!near(*c.fromScore, fromScore) {
		t.Fatalf("clearing row = %+v, want %s (%v) -> none", c, canon, fromScore)
	}
	if got := w.canonNotifications(w.arc); len(got) != 2 || got[1] != "" {
		t.Fatalf("canon-change events = %v, want a second one with no lore", got)
	}
}

// When the canon shrinks below 4 and another lore qualifies, that lore takes
// canon in the same run, even though it scores less than the shrunk lore.
func TestRankShrunkCanonReplacedByQualifier(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.group(w.arc, 4)
	b := w.group(w.arc, 4)
	w.vote(a[0], 20)
	w.vote(b[0], 1)
	w.run()
	loreA, loreB := w.loreOf(a[0]), w.loreOf(b[0])
	if w.lore(loreA).status != standingCanon {
		t.Fatalf("setup: A should be canon")
	}

	w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, a[3])
	w.dirty(w.arc)
	w.run()

	if w.lore(loreA).status != standingSublore || w.lore(loreB).status != standingCanon {
		t.Fatalf("A=%s B=%s, want B canon", w.lore(loreA).status, w.lore(loreB).status)
	}
	ch := w.changes(w.arc)
	if c := ch[len(ch)-1]; c.from != valid(loreA) || c.to != valid(loreB) {
		t.Fatalf("audit row = %+v, want %s -> %s", c, loreA, loreB)
	}
}

// Equal scores: the canon keeps its standing; a challenger must strictly
// outscore it.
func TestRankTieKeepsIncumbent(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.group(w.arc, 4)
	b := w.group(w.arc, 6)
	w.vote(a[0], 6)
	w.run()
	loreA, loreB := w.loreOf(a[0]), w.loreOf(b[0])
	if w.lore(loreA).status != standingCanon {
		t.Fatalf("setup: A should be canon")
	}

	// 4 members and 6 upvotes ties 6 members and 4 upvotes exactly.
	w.vote(b[0], 4)
	w.run()

	if sa, sb := w.lore(loreA).score, w.lore(loreB).score; sa != sb {
		t.Fatalf("setup: scores %v and %v should tie", sa, sb)
	}
	if w.lore(loreA).status != standingCanon {
		t.Fatalf("tie moved canon away from the incumbent")
	}
	if ch := w.changes(w.arc); len(ch) != 1 {
		t.Fatalf("canon_changes = %+v, want only the first promotion", ch)
	}
}

// Only dirty arcs are processed, and each arc is ranked on its own: an hour
// arc's canon never touches its day arc or another place-level arc.
func TestRankProcessesOnlyDirtyArcsIndependently(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	hour := w.hourArc(w.arc, "2026-10-01", 9)
	other := w.dayArc("2026-10-02")

	h := w.group(hour, 4)
	w.vote(h[0], 2)
	d := w.group(w.arc, 4)
	w.vote(d[0], 1)
	o := w.group(other, 4)
	w.vote(o[0], 1)
	// The other arc is clean: whatever the job would decide, it must not run.
	w.exec(`UPDATE arcs SET lore_dirty_at = NULL WHERE id = $1`, other)

	w.run()

	if got := w.arcState(hour); got.canon != valid(w.loreOf(h[0])) || got.members != 4 {
		t.Fatalf("hour arc = %+v, want its own lore as canon", got)
	}
	if got := w.arcState(w.arc); got.canon != valid(w.loreOf(d[0])) || got.members != 4 {
		t.Fatalf("day arc = %+v, want its own lore as canon, not the hour's", got)
	}
	if got := w.arcState(other); got.canon.Valid || got.members != 0 {
		t.Fatalf("clean arc = %+v, want it untouched", got)
	}
	var lores int
	if err := w.pool.QueryRow(context.Background(), `SELECT count(*) FROM lores WHERE arc_id = $1`, other).Scan(&lores); err != nil {
		t.Fatalf("count lores: %v", err)
	}
	if lores != 0 {
		t.Fatalf("clean arc has %d lores, want membership not recomputed", lores)
	}
}

// A mark written after the pass read the arc's mark survives the pass, so a
// change the pass may have missed is picked up by the next one.
func TestRankKeepsLaterDirtyMark(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.group(w.arc, 4)

	// Hold the arc row so the pass blocks on its final arc update, then mark
	// the arc again and release it.
	ctx := context.Background()
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if _, err := tx.Exec(ctx, `SELECT 1 FROM arcs WHERE id = $1 FOR UPDATE`, w.arc); err != nil {
		t.Fatalf("lock arc: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- w.worker.Work(ctx, &river.Job[jobs.RankArgs]{}) }()

	waitForLockWait(t, w)
	if _, err := tx.Exec(ctx, `UPDATE arcs SET lore_dirty_at = clock_timestamp() WHERE id = $1`, w.arc); err != nil {
		t.Fatalf("re-mark arc: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Work: %v", err)
	}
	if !w.arcState(w.arc).dirty {
		t.Fatalf("the later mark was cleared; the next pass would skip this arc")
	}
	w.run()
	if w.arcState(w.arc).dirty {
		t.Fatalf("the next pass should clear the mark")
	}
}

// waitForLockWait waits until some backend in the test database is blocked
// on a lock.
func waitForLockWait(t *testing.T, w *world) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := w.pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock')`).Scan(&waiting); err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the rank pass never blocked on the arc row")
}

// The periodic job, run by a River client the way ROLE=worker runs it,
// promotes canon without anything else enqueuing a pass.
func TestPeriodicJobPromotesCanon(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.group(w.arc, 4)
	w.vote(a[0], 1)

	workers := river.NewWorkers()
	river.AddWorker(workers, w.worker)
	m := metrics.New(prometheus.NewRegistry())
	client, err := jobs.NewClient(w.pool, zerolog.Nop(), m, workers, []*river.PeriodicJob{rank.PeriodicJob(time.Second)})
	if err != nil {
		t.Fatalf("river client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start river: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = client.Stop(stopCtx)
	})

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if w.arcState(w.arc).canon.Valid {
			if got := w.arcState(w.arc).canon; got != valid(w.loreOf(a[0])) {
				t.Fatalf("canon = %v, want %s", got, w.loreOf(a[0]))
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no canon within 60 seconds of the job starting")
}
