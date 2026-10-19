//go:build integration

package arc_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/arc"
	"perspectiv/internal/testdb"
)

// Hiding a place hides its whole subtree: an arc at a store inside a hidden
// mall is NotFound on the arc page and to its hour arc's creator, and a
// publish at that store is refused as NotFound.
func TestHiddenAncestorHidesSubtree(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	mall := w.place(t, "PLACE_KIND_MALL", "John Smith Home Mall", w.city)
	store := w.place(t, "PLACE_KIND_STORE", "Corner Shop", mall)
	creator := w.user(t)
	hourID := w.mustResolve(t, arc.ResolveInput{PlaceID: store, LocalDate: day, Hour: hourPtr(21), Title: "A night at the corner shop", CreatedBy: creator})
	dayID := w.arcRow(t, hourID).parent.UUID
	w.exec(t, `UPDATE places SET hidden_at = now() WHERE id = $1`, mall)
	client := w.client(t)

	reads := []struct {
		name string
		call func() error
	}{
		{name: "GetArc day arc below hidden place", call: func() error {
			_, err := client.GetArc(context.Background(), as(uuid.Nil, &perspectivv1.GetArcRequest{ArcId: dayID.String()}))
			return err
		}},
		{name: "GetArc hour arc below hidden place", call: func() error {
			_, err := client.GetArc(context.Background(), as(uuid.Nil, &perspectivv1.GetArcRequest{ArcId: hourID.String()}))
			return err
		}},
		{name: "RetitleHourArc below hidden place", call: func() error {
			_, err := client.RetitleHourArc(context.Background(), as(creator, &perspectivv1.RetitleHourArcRequest{ArcId: hourID.String(), Title: "Last orders"}))
			return err
		}},
	}
	for _, tc := range reads {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := codeOf(tc.call()); got != connect.CodeNotFound {
				t.Fatalf("code = %v, want %v", got, connect.CodeNotFound)
			}
		})
	}

	t.Run("publish below hidden place", func(t *testing.T) {
		_, _, err := w.resolve(t, arc.ResolveInput{PlaceID: store, LocalDate: day, Title: "Another evening", CreatedBy: w.user(t)})
		wantCode(t, err, connect.CodeNotFound)
	})
}
