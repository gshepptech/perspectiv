//go:build integration

package perspective_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// draftAt saves a new draft for user at the world's place on day, at hour
// when it is set, and returns its id.
func (w *world) draftAt(user uuid.UUID, hour *int32) string {
	w.t.Helper()
	res, err := w.client.SaveDraft(context.Background(), as(user, &perspectivv1.SaveDraftRequest{
		PlaceId: w.place.String(), LocalDate: day, Hour: hour, Parts: []string{"The fryer went up at nine."},
	}))
	if err != nil {
		w.t.Fatalf("SaveDraft: %v", err)
	}
	return res.Msg.GetPerspective().GetId()
}

// A draft has no arc of its own, so every read of it says what it would
// join: the title of the arc that exists for its place, day and hour, and
// the writer's own published story there, which means the draft can never be
// published. Get and ListMine say the same.
func TestDraftNamesTheDayItWouldJoin(t *testing.T) {
	t.Parallel()
	const dayTitle = "The night the fryer caught fire"
	tests := []struct {
		name string
		// setup runs before the draft is saved and returns the story the
		// draft should name as superseding it ("" for none).
		setup     func(w *world, writer uuid.UUID) string
		hour      *int32
		wantTitle string
	}{
		{
			name:  "nobody has written that day",
			setup: func(*world, uuid.UUID) string { return "" },
		},
		{
			name: "someone else started the day",
			setup: func(w *world, _ uuid.UUID) string {
				w.publish(w.user(), w.publishReq())
				return ""
			},
			wantTitle: dayTitle,
		},
		{
			name: "the writer already published for the day",
			setup: func(w *world, writer uuid.UUID) string {
				return w.publish(writer, w.publishReq()).GetPerspectiveId()
			},
			wantTitle: dayTitle,
		},
		{
			name: "the writer's story is in the day but the draft is for an hour nobody wrote",
			setup: func(w *world, writer uuid.UUID) string {
				w.publish(writer, w.publishReq())
				return ""
			},
			hour: hourPtr(13),
		},
		{
			name: "the draft's hour has an arc with the writer's story in it",
			setup: func(w *world, writer uuid.UUID) string {
				w.publish(w.user(), w.publishReq())
				req := w.publishReq()
				req.Hour = hourPtr(13)
				return w.publish(writer, req).GetPerspectiveId()
			},
			hour: hourPtr(13),
			// An hour of a titled day is named after the day.
			wantTitle: dayTitle + " · 13:00",
		},
		{
			name: "the writer deleted their story, which frees the day",
			setup: func(w *world, writer uuid.UUID) string {
				id := w.publish(writer, w.publishReq()).GetPerspectiveId()
				w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, id)
				return ""
			},
			wantTitle: dayTitle,
		},
		{
			name: "a moderator hid the writer's story, which still holds the day",
			setup: func(w *world, writer uuid.UUID) string {
				id := w.publish(writer, w.publishReq()).GetPerspectiveId()
				w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, id)
				return id
			},
			wantTitle: dayTitle,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			writer := w.user()
			wantSuperseded := tc.setup(w, writer)
			id := w.draftAt(writer, tc.hour)

			mine, err := w.client.ListMine(context.Background(), as(writer, &perspectivv1.ListMineRequest{
				Status: perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_DRAFT,
			}))
			if err != nil {
				t.Fatalf("ListMine: %v", err)
			}
			if len(mine.Msg.GetPerspectives()) != 1 {
				t.Fatalf("ListMine drafts = %d, want 1", len(mine.Msg.GetPerspectives()))
			}
			for source, p := range map[string]*perspectivv1.Perspective{"Get": w.get(writer, id), "ListMine": mine.Msg.GetPerspectives()[0]} {
				if p.GetArcTitle() != tc.wantTitle || p.GetSupersededById() != wantSuperseded {
					t.Errorf("%s: arc title %q, superseded by %q; want %q, %q",
						source, p.GetArcTitle(), p.GetSupersededById(), tc.wantTitle, wantSuperseded)
				}
				if p.GetArcId() != "" {
					t.Errorf("%s: a draft has arc id %q, want none", source, p.GetArcId())
				}
			}
		})
	}
}

// A draft at a hidden place names neither the place nor the day it would
// join, and a published story says nothing of drafts.
func TestDraftArcLeftOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		check func(t *testing.T, w *world)
	}{
		{
			name: "a draft at a hidden place",
			check: func(t *testing.T, w *world) {
				writer := w.user()
				w.publish(writer, w.publishReq())
				id := w.draftAt(writer, nil)
				w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, w.place)
				p := w.get(writer, id)
				if p.GetArcTitle() != "" || p.GetSupersededById() != "" || p.GetPlace() != nil {
					t.Fatalf("arc title %q, superseded by %q, place %v; want all empty", p.GetArcTitle(), p.GetSupersededById(), p.GetPlace())
				}
			},
		},
		{
			name: "a published story is never superseded",
			check: func(t *testing.T, w *world) {
				writer := w.user()
				id := w.publish(writer, w.publishReq()).GetPerspectiveId()
				if p := w.get(writer, id); p.GetSupersededById() != "" {
					t.Fatalf("superseded by %q, want none", p.GetSupersededById())
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.check(t, newWorld(t))
		})
	}
}
