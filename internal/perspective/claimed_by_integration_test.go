//go:build integration

package perspective_test

import (
	"testing"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// A character whose claim was accepted names who it turned out to be and
// links their own story of the day, while that story is readable; a
// pending claim names nobody, and a deleted claimant drops both.
func TestCharacterClaimedBy(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	me, other, third := w.user(), w.user(), w.user()
	req := w.publishReq()
	req.Characters = []*perspectivv1.CharacterInput{{Label: "the cook"}, {Label: "the cashier"}}
	mine := w.publish(me, req)
	join := w.publishReq()
	join.TitleIfNewArc = ""
	theirs := w.publish(other, join)
	thirds := w.publish(third, join)
	chars := w.get(me, mine.GetPerspectiveId()).GetCharacters()
	w.claim(chars[0].GetId(), theirs.GetPerspectiveId(), "CLAIM_STATUS_ACCEPTED")
	w.claim(chars[1].GetId(), thirds.GetPerspectiveId(), "CLAIM_STATUS_PENDING")

	cook := func() *perspectivv1.Character {
		t.Helper()
		return w.get(me, mine.GetPerspectiveId()).GetCharacters()[0]
	}
	if c := cook(); c.GetClaimedBy().GetUserId() != other.String() || c.GetClaimedByStoryId() != theirs.GetPerspectiveId() {
		t.Fatalf("accepted cook = %+v, want claimed by %s with story %s", c, other, theirs.GetPerspectiveId())
	}
	if c := w.get(me, mine.GetPerspectiveId()).GetCharacters()[1]; c.GetClaimedBy() != nil || c.GetClaimedByStoryId() != "" {
		t.Fatalf("pending cashier = %+v, want no claimant", c)
	}

	// A hidden story is not linked, though the claimant is still named.
	w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, theirs.GetPerspectiveId())
	if c := cook(); c.GetClaimedBy().GetUserId() != other.String() || c.GetClaimedByStoryId() != "" {
		t.Fatalf("hidden claimant story: cook = %+v", c)
	}
	w.exec(`UPDATE perspectives SET hidden_at = NULL WHERE id = $1`, theirs.GetPerspectiveId())

	// A deleted account is neither named nor linked.
	w.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, other)
	if c := cook(); c.GetClaimedBy() != nil || c.GetClaimedByStoryId() != "" {
		t.Fatalf("deleted claimant: cook = %+v", c)
	}
}
