//go:build integration

package arc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc"
	"perspectiv/internal/testdb"
)

func wantCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want %v", want)
	}
	if got, typed := apiutil.CodeOf(err); !typed || got != want {
		t.Fatalf("err = %v (code %v), want %v", err, got, want)
	}
}

type arcRow struct {
	placeID uuid.UUID
	date    time.Time
	hour    *int16
	parent  uuid.NullUUID
	title   string
	creator uuid.UUID
}

func (w *world) arcRow(t *testing.T, id uuid.UUID) arcRow {
	t.Helper()
	var r arcRow
	err := w.pool.QueryRow(context.Background(),
		`SELECT place_id, local_date, hour, parent_arc_id, title, created_by FROM arcs WHERE id = $1`, id).
		Scan(&r.placeID, &r.date, &r.hour, &r.parent, &r.title, &r.creator)
	if err != nil {
		t.Fatalf("read arc %s: %v", id, err)
	}
	return r
}

// FR-013, AC-011, OT-012: the first writer creates and titles the day arc; a
// second writer joins it without a title.
func TestResolveDayArc_FirstWriterCreatesSecondJoins(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	first, second := w.user(t), w.user(t)

	id, created, err := w.resolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Title: "  The jukebox night ", CreatedBy: first})
	if err != nil || !created {
		t.Fatalf("first writer: id=%s created=%v err=%v, want a created arc", id, created, err)
	}
	got := w.arcRow(t, id)
	if got.title != "The jukebox night" || got.creator != first || got.hour != nil || got.parent.Valid ||
		got.placeID != w.diner || !got.date.Equal(day) {
		t.Fatalf("day arc row = %+v", got)
	}

	joined, created, err := w.resolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, CreatedBy: second})
	if err != nil || created || joined != id {
		t.Fatalf("second writer: id=%s created=%v err=%v, want to join %s", joined, created, err, id)
	}
	// A title from a late writer does not rename the arc.
	again, _, err := w.resolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Title: "Something else", CreatedBy: w.user(t)})
	if err != nil || again != id || w.arcRow(t, id).title != "The jukebox night" {
		t.Fatalf("third writer: id=%s err=%v title=%q", again, err, w.arcRow(t, id).title)
	}
	if n := w.arcCount(t); n != 1 {
		t.Fatalf("arcs = %d, want 1", n)
	}
}

// A local date carrying a time of day or a zone still files on its calendar day.
func TestResolveDayArc_DateOnly(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	a := w.mustResolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Title: "Late shift", CreatedBy: w.user(t)})
	b := w.mustResolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: time.Date(2026, 10, 16, 23, 30, 0, 0, ny), CreatedBy: w.user(t)})
	if a != b {
		t.Fatalf("same calendar day resolved to %s and %s", a, b)
	}
	c := w.mustResolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day.AddDate(0, 0, 1), Title: "Next morning", CreatedBy: w.user(t)})
	if c == a {
		t.Fatal("next day joined the previous day's arc")
	}
}

// AC-011, OT-013: title rules apply when the publish creates the arc.
func TestResolveDayArc_TitleRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		ok    bool
	}{
		{name: "missing title rejected", title: ""},
		{name: "place name rejected", title: "Joe's Diner"},
		{name: "date rejected", title: "2026-10-16"},
		{name: "place and date rejected", title: "Joe's Diner 2026-10-16"},
		{name: "two characters rejected", title: "ab"},
		{name: "three characters accepted", title: "abc", ok: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, testdb.New(t))
			id, created, err := w.resolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Title: tc.title, CreatedBy: w.user(t)})
			if tc.ok {
				if err != nil || !created || w.arcRow(t, id).title != tc.title {
					t.Fatalf("created=%v err=%v", created, err)
				}
				return
			}
			wantCode(t, err, connect.CodeInvalidArgument)
			if n := w.arcCount(t); n != 0 {
				t.Fatalf("arcs = %d after a rejected title, want 0", n)
			}
		})
	}
}

func TestResolve_RejectsBadInput(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	hidden := w.place(t, "PLACE_KIND_STORE", "Closed Cafe", w.city)
	w.exec(t, `UPDATE places SET hidden_at = now() WHERE id = $1`, hidden)
	u := w.user(t)

	tests := []struct {
		name string
		in   arc.ResolveInput
		want connect.Code
	}{
		{name: "unknown place", in: arc.ResolveInput{PlaceID: uuid.New(), LocalDate: day, Title: "A title", CreatedBy: u}, want: connect.CodeNotFound},
		{name: "hidden place", in: arc.ResolveInput{PlaceID: hidden, LocalDate: day, Title: "A title", CreatedBy: u}, want: connect.CodeNotFound},
		{name: "no place", in: arc.ResolveInput{LocalDate: day, Title: "A title", CreatedBy: u}, want: connect.CodeInvalidArgument},
		{name: "no date", in: arc.ResolveInput{PlaceID: w.diner, Title: "A title", CreatedBy: u}, want: connect.CodeInvalidArgument},
		{name: "no author", in: arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Title: "A title"}, want: connect.CodeInvalidArgument},
		{name: "hour below range", in: arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: hourPtr(-1), Title: "A title", CreatedBy: u}, want: connect.CodeInvalidArgument},
		{name: "hour above range", in: arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: hourPtr(24), Title: "A title", CreatedBy: u}, want: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		_, _, err := w.resolve(t, tc.in)
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, err, tc.want)
		})
	}
	if n := w.arcCount(t); n != 0 {
		t.Fatalf("arcs = %d, want 0", n)
	}
}

// AC-011: the arc is created in the caller's transaction, so a publish that
// rolls back leaves no arc behind.
func TestResolve_CreatesInCallerTransaction(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	ctx := context.Background()
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.svc.ResolveForPublish(ctx, tx, arc.ResolveInput{
		PlaceID: w.diner, LocalDate: day, Hour: hourPtr(9), Title: "Rolled back", CreatedBy: w.user(t),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n := w.arcCount(t); n != 0 {
		t.Fatalf("arcs = %d after rollback, want 0", n)
	}
}

// FR-010, FR-011, AC-013: an hour creates or joins the hour child of the day
// arc. When one publish creates both, the title names the day arc and the
// hour child takes the day title plus the hour label.
func TestResolveHourArc(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	a, b, c := w.user(t), w.user(t), w.user(t)

	hourID, created, err := w.resolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: hourPtr(14), Title: "The long night", CreatedBy: a})
	if err != nil || !created {
		t.Fatalf("first hour publish: created=%v err=%v", created, err)
	}
	hour := w.arcRow(t, hourID)
	if hour.hour == nil || *hour.hour != 14 || !hour.parent.Valid || hour.title != "The long night · 14:00" || hour.creator != a {
		t.Fatalf("hour arc row = %+v", hour)
	}
	dayRow := w.arcRow(t, hour.parent.UUID)
	if dayRow.hour != nil || dayRow.parent.Valid || dayRow.title != "The long night" || dayRow.creator != a {
		t.Fatalf("day arc row = %+v", dayRow)
	}

	// A later writer on the day arc joins it without a title.
	dayID, created, err := w.resolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, CreatedBy: b})
	if err != nil || created || dayID != hour.parent.UUID {
		t.Fatalf("day publish: id=%s created=%v err=%v, want %s", dayID, created, err, hour.parent.UUID)
	}
	// Same hour joins; another hour creates a second child of the same day, untitled.
	same, created, err := w.resolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: hourPtr(14), CreatedBy: b})
	if err != nil || created || same != hourID {
		t.Fatalf("same hour: id=%s created=%v err=%v", same, created, err)
	}
	other, created, err := w.resolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: hourPtr(0), CreatedBy: c})
	if err != nil || !created || other == hourID {
		t.Fatalf("other hour: id=%s created=%v err=%v", other, created, err)
	}
	if r := w.arcRow(t, other); r.parent.UUID != dayID || r.title != "The long night · 00:00" || r.creator != c {
		t.Fatalf("second hour arc row = %+v", r)
	}
	if n := w.arcCount(t); n != 3 {
		t.Fatalf("arcs = %d, want one day arc and two hour arcs", n)
	}
}

// An hour publish with no day arc yet still needs the day arc's title.
func TestResolveHourArc_NewDayNeedsTitle(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	_, _, err := w.resolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: hourPtr(3), CreatedBy: w.user(t)})
	wantCode(t, err, connect.CodeInvalidArgument)
	if n := w.arcCount(t); n != 0 {
		t.Fatalf("arcs = %d, want 0", n)
	}
}

// OT-012, FR-062: two concurrent first writers produce exactly one day arc
// (and one hour arc); the second converges on the first's rows.
func TestResolve_ConcurrentFirstWritersConverge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		hour *int32
	}{
		{name: "day arc", hour: nil},
		{name: "day arc with hour child", hour: hourPtr(20)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			concurrentFirstWrites(t, tc.hour)
		})
	}
}

func concurrentFirstWrites(t *testing.T, hour *int32) {
	t.Helper()
	w := newWorld(t, testdb.New(t))
	ctx := context.Background()
	first, err := w.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Rollback(ctx) })
	firstID, firstCreated, err := w.svc.ResolveForPublish(ctx, first, arc.ResolveInput{
		PlaceID: w.diner, LocalDate: day, Hour: hour, Title: "First title", CreatedBy: w.user(t),
	})
	if err != nil || !firstCreated {
		t.Fatalf("first: created=%v err=%v", firstCreated, err)
	}

	type result struct {
		id      uuid.UUID
		created bool
		err     error
	}
	done := make(chan result, 1)
	second := w.user(t)
	go func() {
		tx, err := w.pool.Begin(ctx)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		id, created, err := w.svc.ResolveForPublish(ctx, tx, arc.ResolveInput{
			PlaceID: w.diner, LocalDate: day, Hour: hour, Title: "Second title", CreatedBy: second,
		})
		if err == nil {
			err = tx.Commit(ctx)
		}
		done <- result{id, created, err}
	}()

	// The second writer did not see the uncommitted arc, so it is now
	// blocked inserting into the unique index behind the first.
	waitForLockWait(t, w)
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || r.created || r.id != firstID {
		t.Fatalf("second: id=%s created=%v err=%v, want to join %s", r.id, r.created, r.err, firstID)
	}
	var days int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM arcs WHERE hour IS NULL`).Scan(&days); err != nil {
		t.Fatal(err)
	}
	if days != 1 {
		t.Fatalf("day arcs = %d, want 1", days)
	}
	if n := w.arcCount(t); (hour == nil && n != 1) || (hour != nil && n != 2) {
		t.Fatalf("arcs = %d", n)
	}
	if got := w.arcRow(t, firstID).title; hour == nil && got != "First title" {
		t.Fatalf("title = %q, want the first writer's", got)
	}
}

// waitForLockWait waits until some backend in w's database waits on a lock.
func waitForLockWait(t *testing.T, w *world) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		err := w.pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("second writer never blocked on the first writer's insert")
}
