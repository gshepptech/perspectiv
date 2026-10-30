//go:build integration

package user_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// ExportMyData returns the caller's own data as one JSON document: their
// account with its email, their stories with parts and characters, claims
// both ways, follows, saved stories and more, and nobody else's email.
func TestExportMyData(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := e.user("exporter"), e.user("otherone")
	place := e.store()
	arc := e.dayArc(place, me)
	mine := e.published(arc, me)
	e.exec(`INSERT INTO perspective_parts (perspective_id, position, body) VALUES ($1, 1, 'The deli was packed.'), ($1, 2, 'Then quiet.')`, mine)
	cook := e.id(`INSERT INTO characters (perspective_id, label, position) VALUES ($1, 'the cook', 1) RETURNING id`, mine)
	theirs := e.published(arc, other)
	waiter := e.id(`INSERT INTO characters (perspective_id, label, position) VALUES ($1, 'the waiter', 1) RETURNING id`, theirs)
	e.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status) VALUES ($1, $2, $3, 'CLAIM_STATUS_PENDING')`, waiter, me, mine)
	e.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status) VALUES ($1, $2, $3, 'CLAIM_STATUS_ACCEPTED')`, cook, other, theirs)
	e.exec(`INSERT INTO place_follows (user_id, place_id) VALUES ($1, $2)`, me, place)
	// The writers the member follows are theirs to export; who follows them
	// is not.
	fan := e.user("fanofmine")
	e.exec(`INSERT INTO user_follows (follower_id, followee_id) VALUES ($1, $2), ($3, $1)`, me, other, fan)
	e.exec(`INSERT INTO votes (perspective_id, user_id) VALUES ($1, $2)`, theirs, me)
	// The stories the member saved are theirs to export, hidden ones too;
	// who saved the member's own stories is not.
	e.exec(`INSERT INTO saved_perspectives (user_id, perspective_id, created_at) VALUES ($1, $2, '2026-10-20T12:00:00Z'), ($3, $4, now())`,
		me, theirs, fan, mine)
	e.draft(me)

	res, err := e.client(me).ExportMyData(ctx, connect.NewRequest(&perspectivv1.ExportMyDataRequest{}))
	if err != nil {
		t.Fatalf("ExportMyData: %v", err)
	}
	doc := res.Msg.GetJson()
	var got struct {
		Account struct {
			Email    string `json:"email"`
			Username string `json:"username"`
		} `json:"account"`
		Perspectives []struct {
			Status     string   `json:"status"`
			Parts      []string `json:"parts"`
			Characters []struct {
				Label string `json:"label"`
			} `json:"characters"`
		} `json:"perspectives"`
		ClaimsMade []struct {
			Character string `json:"character"`
			Status    string `json:"status"`
		} `json:"claims_made"`
		ClaimsReceived []struct {
			Character string `json:"character"`
			By        string `json:"by"`
		} `json:"claims_received"`
		PlacesFollowed []struct {
			Name string `json:"name"`
		} `json:"places_followed"`
		WritersFollowed []struct {
			Username string `json:"username"`
			Since    string `json:"since"`
		} `json:"writers_followed"`
		Upvotes []struct {
			PerspectiveID uuid.UUID `json:"perspective_id"`
		} `json:"upvotes"`
		SavedStories []struct {
			PerspectiveID uuid.UUID `json:"perspective_id"`
			SavedAt       time.Time `json:"saved_at"`
		} `json:"saved_stories"`
		ThreadPosts []any `json:"thread_posts"`
	}
	if err := json.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("export is not JSON: %v\n%s", err, doc)
	}
	switch {
	case got.Account.Email != "exporter@example.test" || got.Account.Username != "exporter":
		t.Fatalf("account = %+v", got.Account)
	case len(got.Perspectives) != 2:
		t.Fatalf("perspectives = %+v, want the story and the draft", got.Perspectives)
	case strings.Join(got.Perspectives[0].Parts, "|") != "The deli was packed.|Then quiet." || got.Perspectives[0].Characters[0].Label != "the cook":
		t.Fatalf("story = %+v", got.Perspectives[0])
	case len(got.ClaimsMade) != 1 || got.ClaimsMade[0].Character != "the waiter" || got.ClaimsMade[0].Status != "CLAIM_STATUS_PENDING":
		t.Fatalf("claims made = %+v", got.ClaimsMade)
	case len(got.ClaimsReceived) != 1 || got.ClaimsReceived[0].By != "otherone":
		t.Fatalf("claims received = %+v", got.ClaimsReceived)
	case len(got.PlacesFollowed) != 1 || got.PlacesFollowed[0].Name != "Corner Deli":
		t.Fatalf("follows = %+v", got.PlacesFollowed)
	case len(got.WritersFollowed) != 1 || got.WritersFollowed[0].Username != "otherone" || got.WritersFollowed[0].Since == "":
		t.Fatalf("writers followed = %+v", got.WritersFollowed)
	case strings.Contains(doc, "fanofmine"):
		t.Fatal("the export names who follows the member")
	case len(got.Upvotes) != 1 || got.Upvotes[0].PerspectiveID != theirs:
		t.Fatalf("upvotes = %+v", got.Upvotes)
	case len(got.SavedStories) != 1 || got.SavedStories[0].PerspectiveID != theirs ||
		!got.SavedStories[0].SavedAt.Equal(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)):
		t.Fatalf("saved stories = %+v, want the one story the member saved and when", got.SavedStories)
	case got.ThreadPosts == nil:
		t.Fatal("an empty section must be an empty list, not missing")
	}
	if strings.Contains(doc, "otherone@example.test") {
		t.Fatal("the export carries another member's email")
	}

	_, err = e.client(uuid.Nil).ExportMyData(ctx, connect.NewRequest(&perspectivv1.ExportMyDataRequest{}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("signed out: err = %v, want Unauthenticated", err)
	}
}
