//go:build integration

package arc_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/telemetry"
	"perspectiv/internal/arc"
	"perspectiv/internal/testdb"
)

// queryLog records the name of every statement a pool sends, BEGIN and
// ROLLBACK included (as "unnamed"), so a test can count a call's round trips.
type queryLog struct {
	mu    sync.Mutex
	names []string
}

func (l *queryLog) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, telemetry.QueryName(data.SQL))
	return ctx
}

func (l *queryLog) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// take returns the names recorded since the last take and starts afresh.
func (l *queryLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := l.names
	l.names = nil
	return names
}

// countingService is an arc service on a second pool over the same test
// database whose every statement lands in the returned log.
func countingService(t *testing.T, pool *pgxpool.Pool) (*arc.Service, *queryLog) {
	t.Helper()
	log := &queryLog{}
	cfg := pool.Config()
	cfg.ConnConfig.Tracer = log
	counted, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("counting pool: %v", err)
	}
	t.Cleanup(counted.Close)
	return arc.NewService(counted), log
}

// The arc page's reads send a fixed number of statements, BEGIN and
// ROLLBACK included. A change that adds a round trip to one of them fails
// here and has to say why the page needs it. The day arc has a canon, a
// sublore, a tale, an unlinked perspective, an open character and two hour
// arcs, one of them with a canon of its own. Two more days, one with one hour
// and one with five, each hour with a canon, members and an encounter, show
// that a day page's statements do not grow with its hours. A tales or
// unlinked page, and the open characters, read the arc's visibility in the
// list itself and read the arc row only when the list comes back empty, to
// tell NotFound from an empty list: the hour at 9 has neither.
func TestArcReads_QueryCount(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	ctx := context.Background()
	hourIn := func(h int32) arc.ResolveInput {
		return arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: hourPtr(h), Title: "The counted night", CreatedBy: w.user(t)}
	}
	h9 := w.mustResolve(t, hourIn(9))
	h20 := w.mustResolve(t, hourIn(20))
	dayID := w.arcRow(t, h9).parent.UUID

	c1 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	c2 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	w.link(t, c1, c2)
	w.lore(t, dayID, "LORE_STANDING_CANON", 2, c1, c2)
	s1 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	s2 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	w.lore(t, dayID, "LORE_STANDING_SUBLORE", 1, s1, s2)
	tale := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_SOLO"})
	w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_PENDING"})
	w.exec(t, `INSERT INTO characters (perspective_id, label, position) VALUES ($1, 'the cook', 1)`, tale)
	hc1 := w.perspective(t, h20, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	hc2 := w.perspective(t, h20, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	w.lore(t, h20, "LORE_STANDING_CANON", 1, hc1, hc2)
	viewer := w.user(t)
	w.exec(t, `INSERT INTO votes (perspective_id, user_id) VALUES ($1, $2)`, c1, viewer)
	oneHour := w.dayOfCanonHours(t, day.AddDate(0, 0, 1), 12)
	fiveHours := w.dayOfCanonHours(t, day.AddDate(0, 0, 2), 6, 9, 12, 18, 22)

	svc, log := countingService(t, w.pool)
	section := func(viewer uuid.UUID, s perspectivv1.ArcSection) func() error {
		return func() error {
			_, err := svc.ListArcSection(ctx, viewer, dayID, s, "", 10)
			return err
		}
	}
	tests := []struct {
		name string
		call func() error
		want int
	}{
		{name: "GetArc day signed out", want: 9, call: func() error {
			_, err := svc.GetArc(ctx, uuid.Nil, dayID, 10)
			return err
		}},
		{name: "GetArc day signed in", want: 11, call: func() error {
			_, err := svc.GetArc(ctx, viewer, dayID, 10)
			return err
		}},
		{name: "GetArc hour signed out", want: 8, call: func() error {
			_, err := svc.GetArc(ctx, uuid.Nil, h20, 10)
			return err
		}},
		{name: "ListArcSection sublores signed out", want: 6, call: section(uuid.Nil, perspectivv1.ArcSection_ARC_SECTION_SUBLORES)},
		{name: "ListArcSection tales signed out", want: 1, call: section(uuid.Nil, perspectivv1.ArcSection_ARC_SECTION_TALES)},
		{name: "ListArcSection unlinked signed out", want: 1, call: section(uuid.Nil, perspectivv1.ArcSection_ARC_SECTION_UNLINKED)},
		{name: "ListArcSection tales signed in", want: 4, call: section(viewer, perspectivv1.ArcSection_ARC_SECTION_TALES)},
		{name: "ListArcSection hours signed out", want: 7, call: section(uuid.Nil, perspectivv1.ArcSection_ARC_SECTION_HOURS)},
		{name: "GetArc day of one hour", want: 9, call: func() error {
			_, err := svc.GetArc(ctx, uuid.Nil, oneHour, 10)
			return err
		}},
		{name: "GetArc day of five hours", want: 9, call: func() error {
			_, err := svc.GetArc(ctx, uuid.Nil, fiveHours, 10)
			return err
		}},
		{name: "ListOpenCharacters", want: 1, call: func() error {
			_, err := svc.ListOpenCharacters(ctx, dayID)
			return err
		}},
		{name: "ListArcSection tales of an hour with none", want: 2, call: func() error {
			_, err := svc.ListArcSection(ctx, uuid.Nil, h9, perspectivv1.ArcSection_ARC_SECTION_TALES, "", 10)
			return err
		}},
		{name: "ListOpenCharacters of an hour with none", want: 2, call: func() error {
			_, err := svc.ListOpenCharacters(ctx, h9)
			return err
		}},
	}
	// The cases share one counting pool, so they run in order.
	sent := map[string]int{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log.take()
			if err := tc.call(); err != nil {
				t.Fatalf("call: %v", err)
			}
			got := log.take()
			t.Logf("%d statements: %s", len(got), strings.Join(got, ", "))
			sent[tc.name] = len(got)
			if len(got) != tc.want {
				t.Fatalf("sent %d statements, want %d: %s", len(got), tc.want, strings.Join(got, ", "))
			}
		})
	}
	if one, five := sent["GetArc day of one hour"], sent["GetArc day of five hours"]; one != five {
		t.Fatalf("a day of five hours sent %d statements, a day of one hour %d; want the same", five, one)
	}
}

// dayOfCanonHours makes a day at the diner on date whose hours each have a
// canon of two members linked by an encounter, and returns the day arc.
func (w *world) dayOfCanonHours(t *testing.T, date time.Time, hours ...int32) uuid.UUID {
	t.Helper()
	var dayID uuid.UUID
	for _, h := range hours {
		hourID := w.mustResolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: date, Hour: hourPtr(h), Title: "The counted night", CreatedBy: w.user(t)})
		dayID = w.arcRow(t, hourID).parent.UUID
		a := w.perspective(t, hourID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
		b := w.perspective(t, hourID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
		w.link(t, a, b)
		w.lore(t, hourID, "LORE_STANDING_CANON", 1, a, b)
	}
	return dayID
}
