//go:build integration

package perspective_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// An author shares a published perspective, optionally naming a character;
// anyone can open the link; someone who writes in the same arc through it is
// recorded and the inviter is notified. A hidden or foreign target refuses.
func TestInvites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	author, friend, stranger := w.user(), w.user(), w.user()
	req := w.publishReq()
	req.Parts = []string{"The power went out halfway through open mic. Someone by the window started singing anyway."}
	req.Characters = []*perspectivv1.CharacterInput{{Label: "the guy in the yellow raincoat"}}
	pub := w.publish(author, req)
	character := w.get(author, pub.GetPerspectiveId()).GetCharacters()[0].GetId()

	invite := func(user uuid.UUID, characterID string) (string, error) {
		t.Helper()
		r, err := w.client.CreateInvite(ctx, as(user, &perspectivv1.CreateInviteRequest{PerspectiveId: pub.GetPerspectiveId(), CharacterId: characterID}))
		if err != nil {
			return "", err
		}
		return r.Msg.GetCode(), nil
	}
	plain, err := invite(author, "")
	if err != nil || len(plain) != 10 {
		t.Fatalf("plain invite = %q, %v", plain, err)
	}
	named, err := invite(author, character)
	if err != nil || named == plain {
		t.Fatalf("character invite = %q, %v; want a second code", named, err)
	}
	if again, _ := invite(author, character); again != named {
		t.Fatalf("asking twice gave %q then %q; want one link", named, again)
	}
	if _, err := invite(stranger, ""); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("stranger invite code = %v, want PermissionDenied", codeOf(err))
	}
	if _, err := invite(author, uuid.NewString()); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("foreign character code = %v, want InvalidArgument", codeOf(err))
	}

	got, err := w.client.GetInvite(ctx, as(uuid.Nil, &perspectivv1.GetInviteRequest{Code: named}))
	if err != nil {
		t.Fatalf("GetInvite signed out: %v", err)
	}
	inv := got.Msg.GetInvite()
	if inv.GetInviter().GetUserId() != author.String() || inv.GetPlace().GetName() != "Joe's Diner" || inv.GetLocalDate() != day ||
		inv.GetCharacterLabel() != "the guy in the yellow raincoat" || inv.GetExcerpt() == "" || inv.GetArcId() != pub.GetArcId() {
		t.Fatalf("invite = %v", inv)
	}
	if _, err := w.client.GetInvite(ctx, as(uuid.Nil, &perspectivv1.GetInviteRequest{Code: "zzzzzzzzzz"})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown code = %v, want NotFound", codeOf(err))
	}

	// The friend writes their side in the same arc through the link.
	join := w.publishReq()
	join.TitleIfNewArc, join.InviteCode = "", named
	answered := w.publish(friend, join)
	if got := w.count(`SELECT count(*) FROM perspectives WHERE id = $1 AND invite_code = $2`, answered.GetPerspectiveId(), named); got != 1 {
		t.Fatalf("invite recorded on the friend's perspective: %d", got)
	}
	if got := w.count(`SELECT count(*) FROM river_job WHERE kind = 'notify' AND args->>'Kind' = '7' AND args->>'PerspectiveID' = $1`, answered.GetPerspectiveId()); got != 1 {
		t.Fatalf("invite-answered notify jobs = %d, want 1", got)
	}
	// A publish in another arc does not answer the invite.
	elsewhere := w.publishReq()
	elsewhere.LocalDate, elsewhere.InviteCode = "2026-10-14", named
	other := w.publish(stranger, elsewhere)
	if got := w.count(`SELECT count(*) FROM perspectives WHERE id = $1 AND invite_code IS NOT NULL`, other.GetPerspectiveId()); got != 0 {
		t.Fatal("a publish in another arc recorded the invite")
	}
	bad := w.publishReq()
	bad.InviteCode = "not-a-code"
	if _, err := w.client.Publish(ctx, as(w.user(), bad)); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("malformed invite code = %v, want InvalidArgument", codeOf(err))
	}

	// Once the perspective is hidden, the link shows nothing.
	w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, pub.GetPerspectiveId())
	if _, err := w.client.GetInvite(ctx, as(uuid.Nil, &perspectivv1.GetInviteRequest{Code: named})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("hidden perspective invite = %v, want NotFound", codeOf(err))
	}
}
