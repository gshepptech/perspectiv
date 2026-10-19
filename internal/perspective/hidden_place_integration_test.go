//go:build integration

package perspective_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// Hiding a place hides its whole subtree on the perspective read paths: a
// published perspective filed at the hidden place or below it reads as a
// hidden placeholder with no place, arc or text, to anyone and in the
// author's ListMine, and a draft saved there keeps its text for its author
// but no longer names the place.
func TestHiddenPlaceHidesPerspectives(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	city := w.id(`SELECT parent_id FROM places WHERE id = $1`, w.place)
	mall := w.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_MALL', 'John Smith Home Mall', $1, 'America/New_York', 'PLACE_SOURCE_USER') RETURNING id`, city)
	store := w.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_STORE', 'Corner Shop', $1, 'America/New_York', 'PLACE_SOURCE_USER') RETURNING id`, mall)
	author := w.user()

	at := func(place uuid.UUID, title string) string {
		req := w.publishReq()
		req.PlaceId = place.String()
		req.TitleIfNewArc = title
		return w.publish(author, req).GetPerspectiveId()
	}
	atMall := at(mall, "Evening at the mall")
	atStore := at(store, "A night at the corner shop")
	draft, err := w.client.SaveDraft(ctx, as(author, &perspectivv1.SaveDraftRequest{
		PlaceId: store.String(), LocalDate: day, Parts: []string{"half a thought"},
	}))
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	draftID := draft.Msg.GetPerspective().GetId()
	visible := at(w.place, "The night the fryer caught fire")

	w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, mall)

	mine, err := w.client.ListMine(ctx, as(author, &perspectivv1.ListMineRequest{}))
	if err != nil {
		t.Fatalf("ListMine: %v", err)
	}
	listed := map[string]*perspectivv1.Perspective{}
	for _, p := range mine.Msg.GetPerspectives() {
		listed[p.GetId()] = p
	}

	hidden := []struct {
		name string
		got  *perspectivv1.Perspective
	}{
		{name: "anonymous Get at the hidden place", got: w.get(uuid.Nil, atMall)},
		{name: "anonymous Get below the hidden place", got: w.get(uuid.Nil, atStore)},
		{name: "author Get below the hidden place", got: w.get(author, atStore)},
		{name: "ListMine at the hidden place", got: listed[atMall]},
		{name: "ListMine below the hidden place", got: listed[atStore]},
	}
	for _, tc := range hidden {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := tc.got
			if p == nil {
				t.Fatal("missing")
			}
			if !p.GetHidden() || p.GetPlace() != nil || p.GetArcTitle() != "" || p.GetArcId() != "" ||
				len(p.GetParts()) != 0 || p.GetAuthor() != nil {
				t.Fatalf("view = %+v, want a hidden placeholder naming no place or arc", p)
			}
		})
	}

	t.Run("author's draft below the hidden place", func(t *testing.T) {
		t.Parallel()
		for _, p := range []*perspectivv1.Perspective{w.get(author, draftID), listed[draftID]} {
			if p == nil || p.GetPlace() != nil || len(p.GetParts()) != 1 {
				t.Fatalf("draft view = %+v, want its text and no place", p)
			}
		}
	})

	t.Run("perspective outside the hidden subtree", func(t *testing.T) {
		t.Parallel()
		p := w.get(uuid.Nil, visible)
		if p.GetHidden() || p.GetPlace().GetName() != "Joe's Diner" || len(p.GetParts()) != 1 {
			t.Fatalf("view = %+v", p)
		}
	})
}
