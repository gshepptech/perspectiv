//go:build integration

package arc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"perspectiv/internal/arc"
	"perspectiv/internal/testdb"
)

// A writer sees what already exists for a place on a day: the day arc and
// its hours, each with its readable stories, so they can join the others.
func TestFindDayArc(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	ctx := context.Background()
	day := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	one := int32(13)
	publish := func(arcID uuid.UUID) {
		t.Helper()
		w.exec(t, `INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
			VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())`, arcID, w.user(t))
	}

	empty, err := w.svc.FindDayArc(ctx, w.diner, day)
	if err != nil || empty.GetArcId() != "" || len(empty.GetHours()) != 0 {
		t.Fatalf("an unwritten day = %v, %v; want empty", empty, err)
	}

	dayArc := w.mustResolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Title: "Rain at the diner", CreatedBy: w.user(t)})
	hourArc := w.mustResolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: &one, CreatedBy: w.user(t)})
	publish(dayArc)
	publish(dayArc)
	publish(hourArc)
	// A deleted story does not count.
	w.exec(t, `UPDATE perspectives SET deleted_at = now() WHERE id = (SELECT id FROM perspectives WHERE arc_id = $1 LIMIT 1)`, hourArc)
	publish(hourArc)

	got, err := w.svc.FindDayArc(ctx, w.diner, day)
	if err != nil {
		t.Fatalf("FindDayArc: %v", err)
	}
	if got.GetArcId() != dayArc.String() || got.GetTitle() != "Rain at the diner" || got.GetPerspectiveCount() != 2 {
		t.Fatalf("day = %v", got)
	}
	if h := got.GetHours(); len(h) != 1 || h[0].GetHour() != 13 || h[0].GetArcId() != hourArc.String() || h[0].GetPerspectiveCount() != 1 {
		t.Fatalf("hours = %v", h)
	}

	w.exec(t, `UPDATE places SET hidden_at = now() WHERE id = $1`, w.city)
	_, err = w.svc.FindDayArc(ctx, w.diner, day)
	wantCode(t, err, connect.CodeNotFound)
}
