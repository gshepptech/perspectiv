//go:build integration

package arc_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/arc"
	"perspectiv/internal/testdb"
)

// hourFacts is what one hour on a day page is expected to carry.
type hourFacts struct {
	hour       int32
	arcID      uuid.UUID
	canon      uuid.UUID // uuid.Nil for an hour with no canon
	members    []uuid.UUID
	encounters []string
	visible    int32
}

// A day page reads every hour's canon, members, encounters and story count
// together, and each fact still lands on its own hour: the hours differ in
// member count, encounters and readable stories, so a fact moved to the
// wrong hour fails here.
func TestGetArc_HourFactsLandOnTheirHour(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	in := func(h *int32) arc.ResolveInput {
		return arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: h, Title: "The busy night", CreatedBy: w.user(t)}
	}
	dayID := w.mustResolve(t, in(nil))
	t0 := time.Now().Add(-time.Hour)
	n := 0
	write := func(arcID uuid.UUID, status string, o perspectiveOpts) uuid.UUID {
		n++
		o.loreStatus, o.publishAt = status, t0.Add(time.Duration(n)*time.Minute)
		return w.perspective(t, arcID, o)
	}
	// hour builds one hour with a canon of size members (none when 0), an
	// encounter between each neighbouring pair of members, and extra solo
	// tales that count as readable stories.
	hour := func(h, size, solos int32) hourFacts {
		f := hourFacts{hour: h, arcID: w.mustResolve(t, in(hourPtr(h)))}
		for range size {
			f.members = append(f.members, write(f.arcID, "LORE_STATUS_IN_LORE", perspectiveOpts{}))
		}
		for i := int32(1); i < size; i++ {
			f.encounters = append(f.encounters, w.link(t, f.members[i-1], f.members[i]).String())
		}
		if size > 0 {
			f.canon = w.lore(t, f.arcID, "LORE_STANDING_CANON", float64(h), f.members...)
		}
		for range solos {
			write(f.arcID, "LORE_STATUS_SOLO", perspectiveOpts{})
		}
		// A hidden tale is on the page as a placeholder but is not counted.
		write(f.arcID, "LORE_STATUS_SOLO", perspectiveOpts{hidden: true})
		f.visible = size + solos
		return f
	}
	want := []hourFacts{hour(6, 2, 0), hour(9, 3, 1), hour(12, 0, 2), hour(15, 4, 0), hour(21, 2, 3)}

	resp, err := w.svc.GetArc(context.Background(), uuid.Nil, dayID, 10)
	if err != nil {
		t.Fatalf("GetArc(day): %v", err)
	}
	got := resp.GetHours()
	if len(got) != len(want) {
		t.Fatalf("got %d hours, want %d", len(got), len(want))
	}
	for i, tc := range want {
		t.Run(fmt.Sprintf("%02d:00", tc.hour), func(t *testing.T) {
			t.Parallel()
			h := got[i]
			if h.GetArc().GetId() != tc.arcID.String() || h.GetArc().GetHour() != tc.hour {
				t.Fatalf("hour %d is arc %s at %d, want %s", i, h.GetArc().GetId(), h.GetArc().GetHour(), tc.arcID)
			}
			if h.GetPerspectiveCount() != tc.visible {
				t.Fatalf("perspective count = %d, want %d", h.GetPerspectiveCount(), tc.visible)
			}
			checkHourCanon(t, h.GetCanon(), tc)
		})
	}
}

func checkHourCanon(t *testing.T, canon *perspectivv1.ArcLore, want hourFacts) {
	t.Helper()
	if want.canon == uuid.Nil {
		if canon != nil {
			t.Fatalf("canon = %v, want none", canon)
		}
		return
	}
	lore := canon.GetLore()
	if lore.GetId() != want.canon.String() || lore.GetArcId() != want.arcID.String() {
		t.Fatalf("canon = lore %s of arc %s, want lore %s of arc %s", lore.GetId(), lore.GetArcId(), want.canon, want.arcID)
	}
	sameIDs(t, "canon perspectives", ids(canon.GetPerspectives()), want.members...)
	for _, p := range canon.GetPerspectives() {
		if p.GetArcId() != want.arcID.String() || p.GetLoreId() != want.canon.String() {
			t.Fatalf("member %s = arc %s, lore %s; want arc %s, lore %s", p.GetId(), p.GetArcId(), p.GetLoreId(), want.arcID, want.canon)
		}
	}
	var members []string
	for _, m := range lore.GetMembers() {
		members = append(members, m.GetPerspectiveId())
	}
	sameIDs(t, "canon members", members, want.members...)
	var claims []string
	for _, e := range lore.GetEncounters() {
		claims = append(claims, e.GetClaimId())
	}
	if !slices.Equal(claims, want.encounters) {
		t.Fatalf("encounters = %v, want %v", claims, want.encounters)
	}
}
