//go:build integration

package lore_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A-B, B-C, C-D form one lore of 4; E-F a second.
func TestRecomputeArcFormsConnectedGroups(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c, d := w.author("a"), w.author("b"), w.author("c"), w.author("d")
	e, f := w.author("e"), w.author("f")
	pending := w.author("pending")
	w.link(a, b)
	w.link(b, c)
	w.link(c, d)
	w.link(e, f)
	// A pending claim is not a link.
	w.mustClaim(pending.user, f.character)

	// A pair in another arc stays in its own arc's lore.
	other := w.newArc("2026-10-03", a.user)
	x := w.published(other, w.user("x"), statusPending)
	yUser := w.user("y")
	y := w.published(other, yUser, statusPending)
	xc := w.character(x, "someone")
	w.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status)
		VALUES ($1, $2, $3, $4)`, xc, yUser, y, claimAccepted)
	w.recompute(other)

	w.recompute(w.arc)

	big := w.loreOf(a.perspective)
	for _, p := range []author{b, c, d} {
		if got := w.loreOf(p.perspective); got != big {
			t.Errorf("perspective %s in lore %s, want %s", p.perspective, got, big)
		}
	}
	pair := w.loreOf(e.perspective)
	if pair == uuid.Nil || pair == big || w.loreOf(f.perspective) != pair {
		t.Errorf("E-F lore = %s / %s, want one lore distinct from %s", pair, w.loreOf(f.perspective), big)
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, big); got != 4 {
		t.Errorf("A-D lore member_count = %d, want 4", got)
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, pair); got != 2 {
		t.Errorf("E-F lore member_count = %d, want 2", got)
	}
	if got := w.loreOf(pending.perspective); got != uuid.Nil || w.loreStatus(pending.perspective) != statusPending {
		t.Errorf("perspective with only a pending claim: lore %s, status %q; want none, PENDING",
			got, w.loreStatus(pending.perspective))
	}
	if got := w.count(`SELECT count(*) FROM lores WHERE arc_id = $1 AND member_count > 0`, w.arc); got != 2 {
		t.Errorf("non-empty lores in the arc = %d, want 2", got)
	}
	ox, oy := w.loreOf(x), w.loreOf(y)
	if ox == uuid.Nil || ox != oy || ox == big || ox == pair {
		t.Errorf("other arc lore = %s / %s, want its own shared lore", ox, oy)
	}
	if got := w.count(`SELECT count(*) FROM lores WHERE id = $1 AND arc_id = $2`, ox, other); got != 1 {
		t.Error("other arc's lore is not filed under that arc")
	}

	// Recomputing again changes nothing: groups keep their lore ids.
	w.recompute(w.arc)
	if w.loreOf(a.perspective) != big || w.loreOf(e.perspective) != pair {
		t.Error("a second recompute moved a group to a new lore")
	}
	if got := w.count(`SELECT count(*) FROM lores WHERE arc_id = $1`, w.arc); got != 2 {
		t.Errorf("lores in the arc after a second recompute = %d, want 2", got)
	}
}

// A merge keeps the lowest id of two equally sized lores; the other keeps its
// row with member_count 0.
func TestRecomputeArcMergeKeepsLowestLoreID(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c, d := w.author("a"), w.author("b"), w.author("c"), w.author("d")
	w.link(a, b)
	w.link(c, d)
	l1, l2 := w.loreOf(a.perspective), w.loreOf(c.perspective)
	if l1 == l2 {
		t.Fatal("two separate pairs share a lore")
	}
	keep, drop := l1, l2
	if strings.Compare(l2.String(), l1.String()) < 0 {
		keep, drop = l2, l1
	}

	w.link(b, c)

	for _, p := range []author{a, b, c, d} {
		if got := w.loreOf(p.perspective); got != keep {
			t.Errorf("perspective %s in lore %s after merge, want %s", p.perspective, got, keep)
		}
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, keep); got != 4 {
		t.Errorf("kept lore member_count = %d, want 4", got)
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, drop); got != 0 {
		t.Errorf("vanished lore member_count = %d, want 0 with its row kept", got)
	}
}

// A split: the larger half keeps the lore id, the smaller half gets a new one.
func TestRecomputeArcSplitKeepsIDForLargerHalf(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c, d, e := w.author("a"), w.author("b"), w.author("c"), w.author("d"), w.author("e")
	w.link(a, b)
	cut := w.link(b, c)
	w.link(c, d)
	w.link(d, e)
	whole := w.loreOf(a.perspective)

	w.exec(`UPDATE claims SET status = $2 WHERE id = $1`, cut, claimRejected)
	w.recompute(w.arc)

	if got := w.loreOf(c.perspective); got != whole {
		t.Errorf("larger half C-D-E in lore %s, want the original %s", got, whole)
	}
	small := w.loreOf(a.perspective)
	if small == uuid.Nil || small == whole || w.loreOf(b.perspective) != small {
		t.Errorf("smaller half A-B lore = %s / %s, want one new lore", small, w.loreOf(b.perspective))
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, whole); got != 3 {
		t.Errorf("original lore member_count = %d, want 3", got)
	}
}

// Decision under test: a perspective that RecomputeArc leaves with no
// accepted link becomes a SOLO tale, outside every lore and revivable.
func TestRecomputeArcReleasesUnlinkedPerspectiveToSolo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		withdraw string
	}{
		{name: "partner deleted", withdraw: `UPDATE perspectives SET deleted_at = now() WHERE id = $1`},
		{name: "partner hidden", withdraw: `UPDATE perspectives SET hidden_at = now() WHERE id = $1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkReleasedToSolo(t, tc.withdraw)
		})
	}
}

func checkReleasedToSolo(t *testing.T, withdraw string) {
	t.Helper()
	w := newWorld(t)
	e, f := w.author("e"), w.author("f")
	g := w.author("g")
	id := w.link(e, f)
	pendingOnF := w.mustClaim(g.user, f.character)
	loreID := w.loreOf(e.perspective)

	w.exec(withdraw, f.perspective)
	w.recompute(w.arc)

	if got := w.claimStatus(id); got != claimRejected {
		t.Errorf("accepted claim touching the withdrawn perspective = %q, want REJECTED", got)
	}
	if got := w.claimStatus(pendingOnF); got != claimRejected {
		t.Errorf("pending claim on the withdrawn perspective = %q, want REJECTED", got)
	}
	if got := w.loreStatus(e.perspective); got != statusSolo {
		t.Errorf("left-behind perspective lore status = %q, want SOLO", got)
	}
	if got := w.loreOf(e.perspective); got != uuid.Nil {
		t.Errorf("left-behind perspective still a member of %s", got)
	}
	if got := w.count(`SELECT count(*) FROM perspectives WHERE id = $1 AND lore_id IS NULL`, e.perspective); got != 1 {
		t.Error("left-behind perspective still carries a lore id")
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, loreID); got != 0 {
		t.Errorf("vanished lore member_count = %d, want 0", got)
	}

	// The SOLO tale is revivable by a later accepted claim.
	w.link(e, g)
	if got := w.loreStatus(e.perspective); got != statusInLore {
		t.Errorf("revived tale lore status = %q, want IN_LORE", got)
	}
}

func TestRemoveFromLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c, d := w.author("a"), w.author("b"), w.author("c"), w.author("d")
	w.link(a, b)
	w.link(b, c)
	w.link(c, d)
	onlooker := w.author("onlooker")
	pendingOnB := w.mustClaim(onlooker.user, w.character(b.perspective, "the waiter"))
	// b's author has a pending claim on another character in the arc.
	extra := w.character(d.perspective, "the dishwasher")
	pendingByB := w.mustClaim(b.user, extra)
	whole := w.loreOf(c.perspective)
	w.clearDirty()

	if err := w.remove(b.perspective); err != nil {
		t.Fatalf("RemoveFromLore: %v", err)
	}

	if got := w.loreStatus(b.perspective); got != statusRemoved {
		t.Errorf("removed perspective lore status = %q, want REMOVED", got)
	}
	if got := w.loreOf(b.perspective); got != uuid.Nil {
		t.Errorf("removed perspective still a member of %s", got)
	}
	if got := w.count(`SELECT count(*) FROM claims c JOIN characters ch ON ch.id = c.character_id
		WHERE (ch.perspective_id = $1 OR c.claimant_user_id = $2) AND c.status <> $3`,
		b.perspective, b.user, claimRejected); got != 0 {
		t.Errorf("claims on or by the removed perspective not rejected: %d", got)
	}
	if w.claimStatus(pendingOnB) != claimRejected || w.claimStatus(pendingByB) != claimRejected {
		t.Error("pending claims involving the removed perspective not rejected")
	}
	if !w.arcDirty(w.arc) {
		t.Error("arc lore_dirty_at not set by the removal")
	}
	// A lost its only link; C-D stay linked and keep the larger share.
	if got := w.loreStatus(a.perspective); got != statusSolo {
		t.Errorf("A lore status = %q, want SOLO", got)
	}
	if w.loreOf(c.perspective) != whole || w.loreOf(d.perspective) != whole {
		t.Error("C-D no longer share the original lore")
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, whole); got != 2 {
		t.Errorf("lore member_count = %d, want 2", got)
	}

	// Idempotent on an already REMOVED perspective.
	if err := w.remove(b.perspective); err != nil {
		t.Errorf("second RemoveFromLore: %v", err)
	}
	if got := w.loreStatus(b.perspective); got != statusRemoved {
		t.Errorf("lore status after second removal = %q", got)
	}

	// REMOVED is terminal: claims on its characters and by its author fail.
	_, err := w.claim(w.author("late").user, b.character)
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = w.claim(b.user, w.author("later").character)
	wantCode(t, err, connect.CodeFailedPrecondition)
	// Removed perspectives stay readable: the row is untouched otherwise.
	if got := w.count(`SELECT count(*) FROM perspectives WHERE id = $1 AND deleted_at IS NULL AND hidden_at IS NULL`, b.perspective); got != 1 {
		t.Error("removed perspective is no longer readable")
	}
}

func TestRemoveFromLoreRefusals(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	err := w.remove(uuid.New())
	wantDomainCode(t, err, connect.CodeNotFound)

	u := w.user("drafter")
	draft := w.id(`INSERT INTO perspectives (author_id) VALUES ($1) RETURNING id`, u)
	err = w.remove(draft)
	wantDomainCode(t, err, connect.CodeFailedPrecondition)
}

// PENDING, SOLO and REMOVED perspectives are in no lore.
func TestMembershipOfOutsideLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for _, status := range []string{statusPending, statusSolo, statusRemoved} {
		p := w.published(w.arc, w.user(strings.ToLower(status)), status)
		err := w.inTx(func(tx pgx.Tx) error {
			id, members, ok, err := w.svc.MembershipOf(context.Background(), tx, p)
			if ok || id != uuid.Nil || members != nil {
				t.Errorf("%s: MembershipOf = %s, %v, ok %v; want none", status, id, members, ok)
			}
			return err
		})
		if err != nil {
			t.Errorf("%s: MembershipOf: %v", status, err)
		}
	}
	err := w.inTx(func(tx pgx.Tx) error {
		_, _, _, err := w.svc.MembershipOf(context.Background(), tx, uuid.New())
		return err
	})
	wantDomainCode(t, err, connect.CodeNotFound)
}

func TestRecomputeArcRejectsNilArc(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	err := w.inTx(func(tx pgx.Tx) error { return w.svc.RecomputeArc(context.Background(), tx, uuid.Nil) })
	wantDomainCode(t, err, connect.CodeInvalidArgument)
}
