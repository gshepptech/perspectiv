//go:build integration

package lore_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/lore"
)

// A case with code 0 is one the rules allow.
func TestClaimCharacterRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(w *world, a, b author) (caller, character uuid.UUID)
		code  connect.Code
	}{
		{
			name: "claimant with no perspective anywhere is refused",
			setup: func(w *world, a, _ author) (uuid.UUID, uuid.UUID) {
				return w.user("stranger"), a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant whose only perspective is in another arc is refused",
			setup: func(w *world, a, _ author) (uuid.UUID, uuid.UUID) {
				u := w.user("elsewhere")
				w.published(w.newArc("2026-10-02", u), u, statusPending)
				return u, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant with only a draft is refused",
			setup: func(w *world, a, _ author) (uuid.UUID, uuid.UUID) {
				u := w.user("drafter")
				w.exec(`INSERT INTO perspectives (author_id, place_id, local_date) VALUES ($1, $2, '2026-10-01')`, u, w.place)
				return u, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant whose perspective in the arc is deleted is refused",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, b.perspective)
				return b.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "author claiming a character in their own perspective is refused",
			setup: func(_ *world, a, _ author) (uuid.UUID, uuid.UUID) {
				return a.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "character in a REMOVED perspective is refused",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET lore_status = $2 WHERE id = $1`, a.perspective, statusRemoved)
				return b.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant whose own perspective is REMOVED is refused",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET lore_status = $2 WHERE id = $1`, b.perspective, statusRemoved)
				return b.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant rejected on this character before is refused",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				id := w.mustClaim(b.user, a.character)
				if _, err := w.resolve(a.user, id, false); err != nil {
					w.t.Fatalf("reject: %v", err)
				}
				return b.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "character with another user's pending claim already exists",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				c := w.author("carol")
				w.mustClaim(c.user, a.character)
				return b.user, a.character
			},
			code: connect.CodeAlreadyExists,
		},
		{
			name: "character with another user's accepted claim already exists",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.link(a, w.author("carol"))
				return b.user, a.character
			},
			code: connect.CodeAlreadyExists,
		},
		{
			name: "the same claimant's second pending claim already exists",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.mustClaim(b.user, a.character)
				return b.user, a.character
			},
			code: connect.CodeAlreadyExists,
		},
		{
			name: "a second character in the same story is refused: one character per story",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.mustClaim(b.user, a.character)
				return b.user, w.character(a.perspective, "the second stranger")
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "after a rejection, another character in the same story is open",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				id := w.mustClaim(b.user, a.character)
				if _, err := w.resolve(a.user, id, false); err != nil {
					w.t.Fatalf("reject: %v", err)
				}
				return b.user, w.character(a.perspective, "the other stranger")
			},
			code: 0,
		},
		{
			name: "claimant blocked by the character's author is denied",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, a.user, b.user)
				return b.user, a.character
			},
			code: connect.CodePermissionDenied,
		},
		{
			name: "unknown character is not found",
			setup: func(_ *world, _, b author) (uuid.UUID, uuid.UUID) {
				return b.user, uuid.New()
			},
			code: connect.CodeNotFound,
		},
		{
			name: "character in a deleted perspective is not found",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, a.perspective)
				return b.user, a.character
			},
			code: connect.CodeNotFound,
		},
		{
			name: "character in a hidden perspective is not found",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, a.perspective)
				return b.user, a.character
			},
			code: connect.CodeNotFound,
		},
		{
			name: "signed-out caller is unauthenticated",
			setup: func(_ *world, a, _ author) (uuid.UUID, uuid.UUID) {
				return uuid.Nil, a.character
			},
			code: connect.CodeUnauthenticated,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			a, b := w.author("alice"), w.author("bob")
			caller, character := tc.setup(w, a, b)
			_, err := w.claim(caller, character)
			if tc.code == 0 {
				if err != nil {
					t.Fatalf("claim = %v, want it allowed", err)
				}
				return
			}
			wantCode(t, err, tc.code)
		})
	}
}

func TestClaimCharacterRejectsMalformedIDs(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	b := w.author("bob")
	for _, id := range []string{"", "not-a-uuid"} {
		_, err := w.client.ClaimCharacter(context.Background(),
			asUser(b.user, &perspectivv1.ClaimCharacterRequest{CharacterId: id}))
		wantCode(t, err, connect.CodeInvalidArgument)
	}
}

// A claim is PENDING, notifies the author and leaves lore unchanged.
func TestClaimCharacterOpensPendingClaimAndNotifies(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b := w.author("alice"), w.author("bob")

	id := w.mustClaim(b.user, a.character)

	if got := w.claimStatus(id); got != claimPending {
		t.Errorf("claim row status = %q, want %q", got, claimPending)
	}
	if got := w.notifyJobs(perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED, id); got != 1 {
		t.Errorf("claim-received notify jobs = %d, want 1", got)
	}
	if got := w.count(`SELECT count(*) FROM river_job WHERE kind = 'notify'
		AND args->>'ActorID' = $1 AND args->>'PerspectiveID' = $2`, b.user.String(), a.perspective.String()); got != 1 {
		t.Errorf("notify job naming actor and perspective = %d, want 1", got)
	}
	for _, p := range []uuid.UUID{a.perspective, b.perspective} {
		if got := w.loreStatus(p); got != statusPending {
			t.Errorf("perspective %s lore status = %q, want PENDING while the claim is open", p, got)
		}
	}
}

// A claimant may claim a SOLO tale's character, and a SOLO claimant may claim.
func TestClaimCharacterAllowsSoloTales(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b := w.author("alice"), w.author("bob")
	w.exec(`UPDATE perspectives SET lore_status = $1`, statusSolo)
	if _, err := w.claim(b.user, a.character); err != nil {
		t.Fatalf("claim between SOLO tales: %v", err)
	}
}

func TestResolveClaimAcceptLinksBothIntoOneLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.author("alice")
	// bob's perspective declares no characters: it need not name alice.
	bob := w.user("bob")
	b := author{user: bob, perspective: w.published(w.arc, bob, statusPending)}
	id := w.mustClaim(b.user, a.character)
	w.clearDirty()

	status, err := w.resolve(a.user, id, true)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if status != perspectivv1.ClaimStatus_CLAIM_STATUS_ACCEPTED {
		t.Errorf("status = %v, want ACCEPTED", status)
	}
	if got := w.claimStatus(id); got != claimAccepted {
		t.Errorf("claim row status = %q, want ACCEPTED", got)
	}
	if w.count(`SELECT count(*) FROM claims WHERE id = $1 AND resolved_at IS NOT NULL`, id) != 1 {
		t.Error("resolved_at not set")
	}
	for _, p := range []uuid.UUID{a.perspective, b.perspective} {
		if got := w.loreStatus(p); got != statusInLore {
			t.Errorf("perspective %s lore status = %q, want IN_LORE", p, got)
		}
	}
	la, lb := w.loreOf(a.perspective), w.loreOf(b.perspective)
	if la == uuid.Nil || la != lb {
		t.Fatalf("lores = %s and %s, want one shared lore", la, lb)
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, la); got != 2 {
		t.Errorf("lore member_count = %d, want 2", got)
	}
	if got := w.count(`SELECT count(*) FROM perspectives WHERE lore_id = $1`, la); got != 2 {
		t.Errorf("perspectives carrying the lore id = %d, want 2", got)
	}
	if !w.arcDirty(w.arc) {
		t.Error("arc lore_dirty_at not set by the accept")
	}
	if got := testutil.ToFloat64(w.metrics.ClaimsAccepted); got != 1 {
		t.Errorf("claims_accepted_total = %v, want 1", got)
	}
	if got := w.notifyJobs(perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED, id); got != 1 {
		t.Errorf("claim-accepted notify jobs = %d, want 1", got)
	}

	var loreID uuid.UUID
	var members []lore.Member
	var ok bool
	err = w.inTx(func(tx pgx.Tx) error {
		var err error
		loreID, members, ok, err = w.svc.MembershipOf(context.Background(), tx, b.perspective)
		return err
	})
	if err != nil || !ok || loreID != la {
		t.Fatalf("MembershipOf = %s, ok %v, err %v; want lore %s", loreID, ok, err, la)
	}
	want := map[lore.Member]bool{
		{PerspectiveID: a.perspective, UserID: a.user}: true,
		{PerspectiveID: b.perspective, UserID: b.user}: true,
	}
	if len(members) != 2 || !want[members[0]] || !want[members[1]] {
		t.Errorf("members = %v, want alice and bob", members)
	}
}

func TestResolveClaimRejectLeavesLoreAlone(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b := w.author("alice"), w.author("bob")
	id := w.mustClaim(b.user, a.character)

	status, err := w.resolve(a.user, id, false)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if status != perspectivv1.ClaimStatus_CLAIM_STATUS_REJECTED || w.claimStatus(id) != claimRejected {
		t.Errorf("status = %v / %q, want REJECTED", status, w.claimStatus(id))
	}
	for _, p := range []uuid.UUID{a.perspective, b.perspective} {
		if got := w.loreStatus(p); got != statusPending {
			t.Errorf("perspective %s lore status = %q, want PENDING", p, got)
		}
	}
	if got := testutil.ToFloat64(w.metrics.ClaimsAccepted); got != 0 {
		t.Errorf("claims_accepted_total = %v, want 0", got)
	}
	// Another claimant may now claim the freed character.
	if _, err := w.claim(w.author("carol").user, a.character); err != nil {
		t.Errorf("claim by a third author after a rejection: %v", err)
	}
	// The rejected claimant may not.
	_, err = w.claim(b.user, a.character)
	wantCode(t, err, connect.CodeFailedPrecondition)
}

// TestResolveClaimRefusals covers the ways resolving a claim is refused.
func TestResolveClaimRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(w *world, a, b author, claimID string) (caller uuid.UUID, id string)
		code  connect.Code
	}{
		{
			name: "the claimant cannot resolve their own claim",
			setup: func(_ *world, _, b author, id string) (uuid.UUID, string) {
				return b.user, id
			},
			code: connect.CodePermissionDenied,
		},
		{
			name: "a bystander cannot resolve the claim",
			setup: func(w *world, _, _ author, id string) (uuid.UUID, string) {
				return w.author("carol").user, id
			},
			code: connect.CodePermissionDenied,
		},
		{
			name: "an accepted claim is not PENDING",
			setup: func(w *world, a, _ author, id string) (uuid.UUID, string) {
				if _, err := w.resolve(a.user, id, true); err != nil {
					w.t.Fatalf("accept: %v", err)
				}
				return a.user, id
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "a rejected claim is not PENDING",
			setup: func(w *world, a, _ author, id string) (uuid.UUID, string) {
				if _, err := w.resolve(a.user, id, false); err != nil {
					w.t.Fatalf("reject: %v", err)
				}
				return a.user, id
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "accepting when the claimant's perspective was deleted is refused",
			setup: func(w *world, a, b author, id string) (uuid.UUID, string) {
				w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, b.perspective)
				return a.user, id
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "accepting when the claimant's perspective is REMOVED is refused",
			setup: func(w *world, a, b author, id string) (uuid.UUID, string) {
				w.exec(`UPDATE perspectives SET lore_status = $2 WHERE id = $1`, b.perspective, statusRemoved)
				return a.user, id
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "an unknown claim is not found",
			setup: func(_ *world, a, _ author, _ string) (uuid.UUID, string) {
				return a.user, uuid.NewString()
			},
			code: connect.CodeNotFound,
		},
		{
			name: "a malformed claim id is invalid",
			setup: func(_ *world, a, _ author, _ string) (uuid.UUID, string) {
				return a.user, "nope"
			},
			code: connect.CodeInvalidArgument,
		},
		{
			name: "a signed-out caller is unauthenticated",
			setup: func(_ *world, _, _ author, id string) (uuid.UUID, string) {
				return uuid.Nil, id
			},
			code: connect.CodeUnauthenticated,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			a, b := w.author("alice"), w.author("bob")
			caller, id := tc.setup(w, a, b, w.mustClaim(b.user, a.character))
			_, err := w.resolve(caller, id, true)
			wantCode(t, err, tc.code)
		})
	}
}

// An expiry SOLO tale returns to IN_LORE on a later
// accepted claim, from either side of the claim.
func TestAcceptRevivesSoloTale(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		soloSide func(a, b author) uuid.UUID
	}{
		{name: "the claimed tale is SOLO", soloSide: func(a, _ author) uuid.UUID { return a.perspective }},
		{name: "the claimant's tale is SOLO", soloSide: func(_, b author) uuid.UUID { return b.perspective }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			a, b := w.author("alice"), w.author("bob")
			solo := tc.soloSide(a, b)
			w.exec(`UPDATE perspectives SET lore_status = $2 WHERE id = $1`, solo, statusSolo)
			w.link(a, b)
			if got := w.loreStatus(solo); got != statusInLore {
				t.Errorf("revived tale lore status = %q, want IN_LORE", got)
			}
			if w.loreOf(a.perspective) != w.loreOf(b.perspective) || w.loreOf(solo) == uuid.Nil {
				t.Error("revived tale is not in the shared lore")
			}
		})
	}
}

// Two claimants racing for one character: exactly one claim opens.
func TestConcurrentClaimsOnOneCharacter(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.author("alice")
	claimants := []author{w.author("b"), w.author("c"), w.author("d"), w.author("e")}
	errs := make(chan error, len(claimants))
	for _, cl := range claimants {
		go func() {
			_, err := w.client.ClaimCharacter(context.Background(),
				asUser(cl.user, &perspectivv1.ClaimCharacterRequest{CharacterId: a.character.String()}))
			errs <- err
		}()
	}
	ok, exists := 0, 0
	for range claimants {
		err := <-errs
		switch {
		case err == nil:
			ok++
		case connect.CodeOf(err) == connect.CodeAlreadyExists:
			exists++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || exists != len(claimants)-1 {
		t.Errorf("opened %d, already-exists %d; want 1 and %d", ok, exists, len(claimants)-1)
	}
}

// Hiding a place closes every arc at or below it to claims: a new claim on a
// character there is NotFound, and a claim opened before the hide cannot be
// accepted. Rejecting it still works, so the author can clear their inbox.
func TestHiddenPlaceRefusesClaims(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	city := w.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_CITY', 'Columbus', $1, 'America/New_York', 'PLACE_SOURCE_USER') RETURNING id`, w.place)
	w.exec(`UPDATE arcs SET place_id = $1 WHERE id = $2`, city, w.arc)
	a, b, c := w.author("ana"), w.author("ben"), w.author("cal")
	open := w.mustClaim(b.user, a.character)

	w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, w.place)

	_, err := w.claim(c.user, a.character)
	wantCode(t, err, connect.CodeNotFound)
	_, err = w.resolve(a.user, open, true)
	wantCode(t, err, connect.CodeFailedPrecondition)
	if got := w.claimStatus(open); got != "CLAIM_STATUS_PENDING" {
		t.Fatalf("claim after a refused accept = %s, want PENDING", got)
	}
	if got, err := w.resolve(a.user, open, false); err != nil || got != perspectivv1.ClaimStatus_CLAIM_STATUS_REJECTED {
		t.Fatalf("reject under a hidden place = %v, %v; want REJECTED", got, err)
	}
}
