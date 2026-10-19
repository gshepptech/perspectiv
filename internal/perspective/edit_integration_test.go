//go:build integration

package perspective_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// AC-014, OT-018, FR-018: text stays editable after publish; a character with
// an accepted claim cannot be removed, one without can.
func TestUpdateCharacters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me, other := w.user(), w.user()
	req := w.publishReq()
	req.Characters = []*perspectivv1.CharacterInput{{Label: "the cook"}, {Label: "the cashier"}, {Label: "the trucker"}}
	mine := w.publish(me, req)
	join := w.publishReq()
	join.TitleIfNewArc = ""
	theirs := w.publish(other, join)

	p := w.get(me, mine.GetPerspectiveId())
	cook, cashier, trucker := p.GetCharacters()[0], p.GetCharacters()[1], p.GetCharacters()[2]
	w.claim(cook.GetId(), theirs.GetPerspectiveId(), "CLAIM_STATUS_ACCEPTED")
	pendingClaimer := w.publish(w.user(), join)
	w.claim(cashier.GetId(), pendingClaimer.GetPerspectiveId(), "CLAIM_STATUS_PENDING")

	p = w.get(me, mine.GetPerspectiveId())
	if !p.GetCharacters()[0].GetHasAcceptedClaim() || !p.GetCharacters()[1].GetHasPendingClaim() {
		t.Fatalf("claim flags = %+v", p.GetCharacters())
	}

	update := func(chars ...*perspectivv1.CharacterInput) (*perspectivv1.Perspective, error) {
		resp, err := w.client.Update(ctx, as(me, &perspectivv1.PerspectiveServiceUpdateRequest{
			PerspectiveId: mine.GetPerspectiveId(), Genre: perspectivv1.Genre_GENRE_HORROR,
			Parts: []string{"A rewritten opening."}, Characters: chars,
		}))
		if err != nil {
			return nil, err
		}
		return resp.Msg.GetPerspective(), nil
	}

	// Removing the claimed cook is refused and changes nothing.
	if _, err := update(&perspectivv1.CharacterInput{Id: cashier.GetId(), Label: "the cashier"}); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("removing an accepted character code = %v, want FailedPrecondition", codeOf(err))
	}
	if got := w.get(me, mine.GetPerspectiveId()); len(got.GetCharacters()) != 3 || got.GetParts()[0].GetBody() != "I ordered pancakes at midnight." {
		t.Fatalf("a refused update changed the perspective: %+v", got)
	}

	// Removing the cashier (pending claim) and the trucker (none) works; the
	// cook is relabelled and moved, and a new character is added.
	got, err := update(
		&perspectivv1.CharacterInput{Label: "the waitress"},
		&perspectivv1.CharacterInput{Id: cook.GetId(), Label: "the short-order cook"},
	)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(got.GetCharacters()) != 2 || got.GetCharacters()[1].GetId() != cook.GetId() ||
		got.GetCharacters()[1].GetLabel() != "the short-order cook" || got.GetCharacters()[1].GetPosition() != 2 ||
		!got.GetCharacters()[1].GetHasAcceptedClaim() || got.GetCharacters()[0].GetLabel() != "the waitress" {
		t.Fatalf("characters after update = %+v", got.GetCharacters())
	}
	if got.GetGenre() != perspectivv1.Genre_GENRE_HORROR || got.GetParts()[0].GetBody() != "A rewritten opening." || len(got.GetParts()) != 1 {
		t.Fatalf("text and genre after update = %v %+v", got.GetGenre(), got.GetParts())
	}
	if n := w.count(`SELECT count(*) FROM characters WHERE id = ANY($1::uuid[])`, []string{cashier.GetId(), trucker.GetId()}); n != 0 {
		t.Fatalf("%d removed characters remain", n)
	}
	if n := w.count(`SELECT count(*) FROM claims WHERE status = 'CLAIM_STATUS_ACCEPTED'`); n != 1 {
		t.Fatalf("accepted claims = %d, want 1", n)
	}
}

// Update changes text, genre and characters only, of a published
// perspective, for its author.
func TestUpdateRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me := w.user()
	pub := w.publish(me, w.publishReq())
	draft, err := w.client.SaveDraft(ctx, as(me, &perspectivv1.SaveDraftRequest{Parts: []string{"draft"}}))
	if err != nil {
		t.Fatal(err)
	}
	valid := func(id string) *perspectivv1.PerspectiveServiceUpdateRequest {
		return &perspectivv1.PerspectiveServiceUpdateRequest{PerspectiveId: id, Genre: perspectivv1.Genre_GENRE_DRAMA, Parts: []string{"ok"}}
	}
	tests := []struct {
		name string
		user uuid.UUID
		req  *perspectivv1.PerspectiveServiceUpdateRequest
		want connect.Code
	}{
		{name: "another user", user: w.user(), req: valid(pub.GetPerspectiveId()), want: connect.CodeNotFound},
		{name: "signed out", user: uuid.Nil, req: valid(pub.GetPerspectiveId()), want: connect.CodeUnauthenticated},
		{name: "a draft", user: me, req: valid(draft.Msg.GetPerspective().GetId()), want: connect.CodeFailedPrecondition},
		{name: "unknown id", user: me, req: valid(uuid.NewString()), want: connect.CodeNotFound},
		{name: "301 words", user: me, req: &perspectivv1.PerspectiveServiceUpdateRequest{
			PerspectiveId: pub.GetPerspectiveId(), Genre: perspectivv1.Genre_GENRE_DRAMA, Parts: []string{words(301)},
		}, want: connect.CodeInvalidArgument},
		{name: "no genre", user: me, req: &perspectivv1.PerspectiveServiceUpdateRequest{
			PerspectiveId: pub.GetPerspectiveId(), Parts: []string{"ok"},
		}, want: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.client.Update(ctx, as(tc.user, tc.req))
			if got := codeOf(err); got != tc.want {
				t.Fatalf("code = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}

	// A hidden perspective cannot be edited.
	w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, pub.GetPerspectiveId())
	if _, err := w.client.Update(ctx, as(me, valid(pub.GetPerspectiveId()))); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("hidden update code = %v, want FailedPrecondition", codeOf(err))
	}
}

// ST-010, AC-038, OT-040, FR-036: delete soft-deletes, shows a placeholder,
// dissolves claims and marks the arc for the rank job.
func TestDeleteInLore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	a, b, c := w.user(), w.user(), w.user()
	req := w.publishReq()
	req.Characters = []*perspectivv1.CharacterInput{{Label: "the cook"}}
	pa := w.publish(a, req)
	join := w.publishReq()
	join.TitleIfNewArc = ""
	join.Characters = []*perspectivv1.CharacterInput{{Label: "the trucker"}}
	pb := w.publish(b, join)
	pc := w.publish(c, join)

	cook := w.get(a, pa.GetPerspectiveId()).GetCharacters()[0].GetId()
	trucker := w.get(b, pb.GetPerspectiveId()).GetCharacters()[0].GetId()
	// b claims a's cook (accepted) and c claims b's trucker (accepted): a-b-c.
	w.claim(cook, pb.GetPerspectiveId(), "CLAIM_STATUS_ACCEPTED")
	w.claim(trucker, pc.GetPerspectiveId(), "CLAIM_STATUS_ACCEPTED")
	loreID := w.id(`INSERT INTO lores (arc_id, member_count) VALUES ($1, 3) RETURNING id`, pa.GetArcId())
	for _, p := range []string{pa.GetPerspectiveId(), pb.GetPerspectiveId(), pc.GetPerspectiveId()} {
		w.exec(`INSERT INTO lore_members (lore_id, perspective_id) VALUES ($1, $2)`, loreID, p)
		w.exec(`UPDATE perspectives SET lore_status = 'LORE_STATUS_IN_LORE', lore_id = $1 WHERE id = $2`, loreID, p)
	}
	w.exec(`UPDATE arcs SET lore_dirty_at = NULL`)

	// Only the author may delete.
	if _, err := w.client.Delete(ctx, as(c, &perspectivv1.PerspectiveServiceDeleteRequest{PerspectiveId: pb.GetPerspectiveId()})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("delete by another code = %v, want NotFound", codeOf(err))
	}
	if _, err := w.client.Delete(ctx, as(b, &perspectivv1.PerspectiveServiceDeleteRequest{PerspectiveId: pb.GetPerspectiveId()})); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if n := w.count(`SELECT count(*) FROM perspectives WHERE id = $1 AND deleted_at IS NOT NULL`, pb.GetPerspectiveId()); n != 1 {
		t.Fatal("deleted_at not set")
	}
	if n := w.count(`SELECT count(*) FROM claims WHERE status <> 'CLAIM_STATUS_REJECTED'`); n != 0 {
		t.Fatalf("%d claims involving the deleted perspective are still live", n)
	}
	if n := w.count(`SELECT count(*) FROM lore_members`); n != 0 {
		t.Fatalf("lore members after delete = %d, want 0 (no links remain)", n)
	}
	if n := w.count(`SELECT count(*) FROM arcs WHERE id = $1 AND lore_dirty_at IS NOT NULL`, pa.GetArcId()); n != 1 {
		t.Fatal("arc not marked for the rank job")
	}

	// Readers see a placeholder: flags and ids, no text, characters or author.
	p := w.get(uuid.Nil, pb.GetPerspectiveId())
	if !p.GetDeleted() || p.GetId() != pb.GetPerspectiveId() || p.GetArcId() != pb.GetArcId() ||
		len(p.GetParts()) != 0 || len(p.GetCharacters()) != 0 || p.GetAuthor() != nil {
		t.Fatalf("placeholder = %+v", p)
	}

	// Deleting twice is NotFound; the author no longer lists it.
	if _, err := w.client.Delete(ctx, as(b, &perspectivv1.PerspectiveServiceDeleteRequest{PerspectiveId: pb.GetPerspectiveId()})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("second delete code = %v, want NotFound", codeOf(err))
	}
	list, err := w.client.ListMine(ctx, as(b, &perspectivv1.ListMineRequest{}))
	if err != nil || len(list.Msg.GetPerspectives()) != 0 {
		t.Fatalf("ListMine after delete = %v, %v", list, err)
	}
}

// A draft deletes without touching any arc, and is gone even to its author.
func TestDeleteDraft(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me := w.user()
	d, err := w.client.SaveDraft(ctx, as(me, &perspectivv1.SaveDraftRequest{Parts: []string{"draft"}}))
	if err != nil {
		t.Fatal(err)
	}
	id := d.Msg.GetPerspective().GetId()
	if _, err := w.client.Delete(ctx, as(me, &perspectivv1.PerspectiveServiceDeleteRequest{PerspectiveId: id})); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := w.client.Get(ctx, as(me, &perspectivv1.PerspectiveServiceGetRequest{PerspectiveId: id})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("Get deleted draft code = %v, want NotFound", codeOf(err))
	}
}

// A hidden perspective reads as a placeholder.
func TestGetHidden(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	me := w.user()
	resp := w.publish(me, w.publishReq())
	w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, resp.GetPerspectiveId())
	p := w.get(me, resp.GetPerspectiveId())
	if !p.GetHidden() || len(p.GetParts()) != 0 || p.GetAuthor() != nil {
		t.Fatalf("hidden view = %+v", p)
	}
}

// ListMine pages the author's perspectives, newest edit first, and filters by
// status, place, day and hour.
func TestListMine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me := w.user()
	dayPub := w.publish(me, w.publishReq())
	hourReq := w.publishReq()
	hourReq.Hour = hourPtr(15)
	hourPub := w.publish(me, hourReq)
	draft, err := w.client.SaveDraft(ctx, as(me, &perspectivv1.SaveDraftRequest{PlaceId: w.place.String(), LocalDate: "2026-01-01"}))
	if err != nil {
		t.Fatal(err)
	}
	w.publish(w.user(), func() *perspectivv1.PublishRequest { r := w.publishReq(); r.TitleIfNewArc = ""; return r }())

	ids := func(req *perspectivv1.ListMineRequest) []string {
		t.Helper()
		resp, err := w.client.ListMine(ctx, as(me, req))
		if err != nil {
			t.Fatalf("ListMine: %v", err)
		}
		var out []string
		for _, p := range resp.Msg.GetPerspectives() {
			out = append(out, p.GetId())
		}
		return out
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	}

	draftID := draft.Msg.GetPerspective().GetId()
	eq(ids(&perspectivv1.ListMineRequest{}), draftID, hourPub.GetPerspectiveId(), dayPub.GetPerspectiveId())
	eq(ids(&perspectivv1.ListMineRequest{Status: perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_DRAFT}), draftID)
	eq(ids(&perspectivv1.ListMineRequest{PlaceId: w.place.String(), LocalDate: day}), dayPub.GetPerspectiveId())
	eq(ids(&perspectivv1.ListMineRequest{PlaceId: w.place.String(), LocalDate: day, Hour: hourPtr(15)}), hourPub.GetPerspectiveId())
	eq(ids(&perspectivv1.ListMineRequest{Hour: hourPtr(15)}), hourPub.GetPerspectiveId())

	// Paging one at a time walks the same order.
	eq(w.walkMine(me), draftID, hourPub.GetPerspectiveId(), dayPub.GetPerspectiveId())

	if _, err := w.client.ListMine(ctx, as(uuid.Nil, &perspectivv1.ListMineRequest{})); codeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("signed out code = %v", codeOf(err))
	}
	if _, err := w.client.ListMine(ctx, as(me, &perspectivv1.ListMineRequest{PageToken: "garbage"})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad token code = %v", codeOf(err))
	}
}

// walkMine pages through user's ListMine one perspective at a time.
func (w *world) walkMine(user uuid.UUID) []string {
	w.t.Helper()
	var walked []string
	token := ""
	for range 10 {
		resp, err := w.client.ListMine(context.Background(), as(user, &perspectivv1.ListMineRequest{PageSize: 1, PageToken: token}))
		if err != nil {
			w.t.Fatalf("ListMine page: %v", err)
		}
		for _, p := range resp.Msg.GetPerspectives() {
			walked = append(walked, p.GetId())
		}
		if token = resp.Msg.GetNextPageToken(); token == "" {
			return walked
		}
	}
	w.t.Fatal("ListMine did not reach its last page")
	return nil
}

// A profile lists its author's published perspectives to anyone, newest
// first, tales (SOLO and REMOVED) included; drafts, deleted and hidden
// perspectives, and those at or below a hidden place, are left out.
func TestListByAuthor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me, other := w.user(), w.user()
	pub := func(place uuid.UUID, hour int32) string {
		t.Helper()
		r := w.publishReq()
		r.PlaceId, r.Hour = place.String(), hourPtr(hour)
		return w.publish(me, r).GetPerspectiveId()
	}
	ids := make([]string, 6)
	for i := range ids {
		ids[i] = pub(w.place, int32(10+i))
		w.exec(`UPDATE perspectives SET published_at = '2026-10-15T12:00:00Z'::timestamptz + make_interval(mins => $2) WHERE id = $1`, ids[i], i)
	}
	w.exec(`UPDATE perspectives SET lore_status = 'LORE_STATUS_SOLO' WHERE id = $1`, ids[2])
	w.exec(`UPDATE perspectives SET lore_status = 'LORE_STATUS_REMOVED' WHERE id = $1`, ids[3])
	w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, ids[4])
	w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, ids[5])
	city := w.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_CITY', 'Hidden Springs', (SELECT parent_id FROM places WHERE id = $1), 'America/New_York', 'PLACE_SOURCE_USER') RETURNING id`, w.place)
	store := w.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_STORE', 'Corner Shop', $1, 'America/New_York', 'PLACE_SOURCE_USER') RETURNING id`, city)
	pub(store, 9)
	w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, city)
	if _, err := w.client.SaveDraft(ctx, as(me, &perspectivv1.SaveDraftRequest{PlaceId: w.place.String(), LocalDate: "2026-01-01"})); err != nil {
		t.Fatal(err)
	}
	w.publish(other, func() *perspectivv1.PublishRequest { r := w.publishReq(); r.TitleIfNewArc = ""; return r }())

	var walked []string
	token := ""
	for range 10 {
		resp, err := w.client.ListByAuthor(ctx, as(uuid.Nil, &perspectivv1.ListByAuthorRequest{UserId: me.String(), PageSize: 1, PageToken: token}))
		if err != nil {
			t.Fatalf("ListByAuthor signed out: %v", err)
		}
		for _, p := range resp.Msg.GetPerspectives() {
			if p.GetAuthor().GetUserId() != me.String() || len(p.GetParts()) == 0 || p.GetArcTitle() == "" {
				t.Fatalf("listed perspective = %v; want the author's readable perspective", p)
			}
			walked = append(walked, p.GetId())
		}
		if token = resp.Msg.GetNextPageToken(); token == "" {
			break
		}
	}
	want := []string{ids[3], ids[2], ids[1], ids[0]}
	if strings.Join(walked, ",") != strings.Join(want, ",") {
		t.Fatalf("walked %v, want %v", walked, want)
	}
	if _, err := w.client.ListByAuthor(ctx, as(uuid.Nil, &perspectivv1.ListByAuthorRequest{UserId: "nope"})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad user id code = %v", codeOf(err))
	}
	if _, err := w.client.ListByAuthor(ctx, as(uuid.Nil, &perspectivv1.ListByAuthorRequest{UserId: me.String(), PageToken: "garbage"})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad token code = %v", codeOf(err))
	}
}
