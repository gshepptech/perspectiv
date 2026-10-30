//go:build integration

package perspective_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

func (w *world) save(user uuid.UUID, id string, on bool) (bool, error) {
	w.t.Helper()
	res, err := w.client.SavePerspective(context.Background(), as(user, &perspectivv1.SavePerspectiveRequest{PerspectiveId: id, On: on}))
	if err != nil {
		return false, err
	}
	return res.Msg.GetSaved(), nil
}

// listSaved returns the ids of every page of user's saved stories, page by
// page, at pageSize stories a page.
func (w *world) listSaved(user uuid.UUID, pageSize int32) [][]string {
	w.t.Helper()
	var pages [][]string
	token := ""
	for {
		res, err := w.client.ListSaved(context.Background(), as(user, &perspectivv1.ListSavedRequest{PageToken: token, PageSize: pageSize}))
		if err != nil {
			w.t.Fatalf("ListSaved: %v", err)
		}
		var ids []string
		for _, p := range res.Msg.GetPerspectives() {
			ids = append(ids, p.GetId())
		}
		pages = append(pages, ids)
		token = res.Msg.GetNextPageToken()
		if token == "" {
			return pages
		}
	}
}

// A member saves and unsaves a readable story. Either is a no-op when
// already so, a second save keeps the first save's time, and the story's
// read reply follows each step.
func TestSavePerspective(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	author, reader := w.user(), w.user()
	story := w.publish(author, w.publishReq()).GetPerspectiveId()
	saves := func() int {
		return w.count(`SELECT count(*) FROM saved_perspectives WHERE user_id = $1 AND perspective_id = $2`, reader, story)
	}

	steps := []struct {
		name      string
		on        bool
		wantRows  int
		wantSaved bool
	}{
		{name: "save", on: true, wantRows: 1, wantSaved: true},
		{name: "save again is a no-op", on: true, wantRows: 1, wantSaved: true},
		{name: "unsave", on: false, wantRows: 0, wantSaved: false},
		{name: "unsave again is a no-op", on: false, wantRows: 0, wantSaved: false},
		{name: "save once more", on: true, wantRows: 1, wantSaved: true},
	}
	for _, st := range steps {
		got, err := w.save(reader, story, st.on)
		if err != nil {
			t.Fatalf("%s: SavePerspective: %v", st.name, err)
		}
		if got != st.wantSaved || saves() != st.wantRows {
			t.Fatalf("%s: saved = %v with %d rows, want %v with %d", st.name, got, saves(), st.wantSaved, st.wantRows)
		}
		if flag := w.get(reader, story).GetSaved(); flag != st.wantSaved {
			t.Fatalf("%s: Get saved = %v, want %v", st.name, flag, st.wantSaved)
		}
	}

	w.exec(`UPDATE saved_perspectives SET created_at = now() - interval '1 hour' WHERE user_id = $1`, reader)
	if _, err := w.save(reader, story, true); err != nil {
		t.Fatalf("SavePerspective: %v", err)
	}
	if kept := w.count(`SELECT count(*) FROM saved_perspectives
		WHERE user_id = $1 AND created_at < now() - interval '30 minutes'`, reader); kept != 1 {
		t.Fatal("a second save replaced the first save's time")
	}
}

// The story's read reply says whether the caller saved it: true only for
// the member who did, false for anyone else and for a signed-out reader. An
// author may save their own story.
func TestSavedFlagOnGet(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	author, reader, other := w.user(), w.user(), w.user()
	story := w.publish(author, w.publishReq()).GetPerspectiveId()
	if _, err := w.save(reader, story, true); err != nil {
		t.Fatalf("SavePerspective: %v", err)
	}

	views := []struct {
		name   string
		viewer uuid.UUID
		want   bool
	}{
		{name: "the saver", viewer: reader, want: true},
		{name: "another member", viewer: other, want: false},
		{name: "a signed-out reader", viewer: uuid.Nil, want: false},
	}
	for _, v := range views {
		t.Run("Get as "+v.name, func(t *testing.T) {
			t.Parallel()
			if got := w.get(v.viewer, story).GetSaved(); got != v.want {
				t.Fatalf("saved = %v, want %v", got, v.want)
			}
		})
	}

	t.Run("an author saves their own story", func(t *testing.T) {
		t.Parallel()
		if got, err := w.save(author, story, true); err != nil || !got {
			t.Fatalf("SavePerspective = %v, %v; want saved", got, err)
		}
	})
}

// SavePerspective and ListSaved need a signed-in caller and a well-formed,
// existing story id.
func TestSavePerspectiveRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	reader := w.user()
	story := w.publish(w.user(), w.publishReq()).GetPerspectiveId()

	refusals := []struct {
		name string
		user uuid.UUID
		id   string
		want connect.Code
	}{
		{name: "signed out", user: uuid.Nil, id: story, want: connect.CodeUnauthenticated},
		{name: "malformed id", user: reader, id: "not-a-uuid", want: connect.CodeInvalidArgument},
		{name: "unknown story", user: reader, id: uuid.NewString(), want: connect.CodeNotFound},
	}
	for _, r := range refusals {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			_, err := w.client.SavePerspective(ctx, as(r.user, &perspectivv1.SavePerspectiveRequest{PerspectiveId: r.id, On: true}))
			if codeOf(err) != r.want {
				t.Fatalf("SavePerspective = %v, want %v", err, r.want)
			}
		})
	}
	if _, err := w.client.ListSaved(ctx, as(uuid.Nil, &perspectivv1.ListSavedRequest{})); codeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("signed-out ListSaved = %v, want Unauthenticated", err)
	}
}

// A story that is not readable to the caller cannot be saved or unsaved:
// someone else's draft, the caller's own draft, a deleted or hidden story,
// and one filed at or below a hidden place are each NotFound, and no save
// row is written.
func TestSavePerspectiveNotFoundWhenUnreadable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	state := w.id(`SELECT parent_id FROM places WHERE id = $1`, w.place)
	mall := w.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_MALL', 'Riverside Mall', $1, 'America/New_York', 'PLACE_SOURCE_USER') RETURNING id`, state)
	store := w.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_STORE', 'Pretzel Stand', $1, 'America/New_York', 'PLACE_SOURCE_USER') RETURNING id`, mall)
	reader := w.user()

	draft := func(author uuid.UUID) string {
		res, err := w.client.SaveDraft(ctx, as(author, &perspectivv1.SaveDraftRequest{
			PlaceId: w.place.String(), LocalDate: day, Parts: []string{"half a thought"},
		}))
		if err != nil {
			t.Fatalf("SaveDraft: %v", err)
		}
		return res.Msg.GetPerspective().GetId()
	}
	published := func() string { return w.publish(w.user(), w.publishReq()).GetPerspectiveId() }

	deleted := published()
	if _, err := w.client.Delete(ctx, as(w.author(deleted), &perspectivv1.PerspectiveServiceDeleteRequest{PerspectiveId: deleted})); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	hidden := published()
	w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)
	req := w.publishReq()
	req.PlaceId, req.TitleIfNewArc = store.String(), "Closing time at the pretzel stand"
	underHidden := w.publish(w.user(), req).GetPerspectiveId()
	w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, mall)

	tests := []struct {
		name string
		id   string
	}{
		{name: "someone else's draft", id: draft(w.user())},
		{name: "the caller's own draft", id: draft(reader)},
		{name: "a deleted story", id: deleted},
		{name: "a hidden story", id: hidden},
		{name: "a story below a hidden place", id: underHidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, on := range []bool{true, false} {
				if _, err := w.save(reader, tt.id, on); codeOf(err) != connect.CodeNotFound {
					t.Errorf("on=%v: SavePerspective = %v, want NotFound", on, err)
				}
			}
			if n := w.count(`SELECT count(*) FROM saved_perspectives WHERE perspective_id = $1`, tt.id); n != 0 {
				t.Fatalf("saved_perspectives rows = %d, want 0", n)
			}
		})
	}
}

// ListSaved pages the caller's saved stories most recently saved first. A
// saved story that stops being readable drops out silently and its save is
// kept, so it comes back once the story is restored; nobody else's saves
// are listed.
func TestListSaved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	reader, other := w.user(), w.user()
	stories := make([]string, 5)
	for i := range stories {
		stories[i] = w.publish(w.user(), w.publishReq()).GetPerspectiveId()
		if _, err := w.save(reader, stories[i], true); err != nil {
			t.Fatalf("SavePerspective: %v", err)
		}
		// Saved i minutes after the first, so the newest save is the last.
		w.exec(`UPDATE saved_perspectives SET created_at = now() - make_interval(mins => $1)
			WHERE user_id = $2 AND perspective_id = $3`, 10-i, reader, stories[i])
	}
	if _, err := w.save(other, stories[0], true); err != nil {
		t.Fatalf("SavePerspective: %v", err)
	}
	newestFirst := slices.Clone(stories)
	slices.Reverse(newestFirst)

	if got := w.listSaved(reader, 2); !slices.EqualFunc(got, [][]string{newestFirst[:2], newestFirst[2:4], newestFirst[4:]}, slices.Equal) {
		t.Fatalf("pages = %v, want %v in pages of 2", got, newestFirst)
	}
	if got := w.listSaved(other, 0); !slices.EqualFunc(got, [][]string{{stories[0]}}, slices.Equal) {
		t.Fatalf("other's pages = %v, want only their own save", got)
	}

	res, err := w.client.ListSaved(ctx, as(reader, &perspectivv1.ListSavedRequest{PageSize: 1}))
	if err != nil {
		t.Fatalf("ListSaved: %v", err)
	}
	first := res.Msg.GetPerspectives()[0]
	if !first.GetSaved() || len(first.GetParts()) != 1 || first.GetPlace().GetName() != "Joe's Diner" || first.GetArcTitle() == "" {
		t.Fatalf("listed story = %+v, want the whole story as ListByAuthor gives it, marked saved", first)
	}

	hidden := stories[3]
	w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)
	without := slices.DeleteFunc(slices.Clone(newestFirst), func(id string) bool { return id == hidden })
	if got := w.listSaved(reader, 2); !slices.EqualFunc(got, [][]string{without[:2], without[2:]}, slices.Equal) {
		t.Fatalf("pages with a hidden story = %v, want %v", got, without)
	}
	if n := w.count(`SELECT count(*) FROM saved_perspectives WHERE user_id = $1 AND perspective_id = $2`, reader, hidden); n != 1 {
		t.Fatal("hiding a story removed its save")
	}

	w.exec(`UPDATE perspectives SET hidden_at = NULL WHERE id = $1`, hidden)
	if got := w.listSaved(reader, 10); !slices.EqualFunc(got, [][]string{newestFirst}, slices.Equal) {
		t.Fatalf("pages once restored = %v, want %v", got, newestFirst)
	}

	if _, err := w.client.ListSaved(ctx, as(reader, &perspectivv1.ListSavedRequest{PageToken: "bogus"})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("ListSaved with a bad token = %v, want InvalidArgument", err)
	}
}

// author returns the author of the perspective id.
func (w *world) author(id string) uuid.UUID {
	w.t.Helper()
	return w.id(`SELECT author_id FROM perspectives WHERE id = $1`, id)
}
