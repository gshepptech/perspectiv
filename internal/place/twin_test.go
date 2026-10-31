//go:build integration

package place_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// twinFx is a state with two cities and one storyless store: a second
// listing of Raymond James Stadium, filed under Tampa with the city in its
// name, the way the place data often lists it.
type twinFx struct {
	hierarchy
	tampa, lakeland, empty uuid.UUID
}

// The storyless listing's spot. 0.007 degrees of latitude is about 780 m.
const emptyLat, emptyLng = 27.982883, -82.501660

func newTwinFx(t *testing.T) twinFx {
	t.Helper()
	f := twinFx{hierarchy: newHierarchy(t)}
	f.tampa = f.place(perspectivv1.PlaceKind_PLACE_KIND_CITY, "Tampa", f.state)
	f.lakeland = f.place(perspectivv1.PlaceKind_PLACE_KIND_CITY, "Lakeland", f.state)
	f.empty = f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium, Tampa", f.tampa, emptyLat, emptyLng, "PLACE_CATEGORY_ARTS_AND_ENTERTAINMENT")
	return f
}

// listing inserts an imported store or mall at a point with a category.
func (f twinFx) listing(kind perspectivv1.PlaceKind, name string, parent uuid.UUID, lat, lng float64, category string) uuid.UUID {
	f.t.Helper()
	id := f.place(kind, name, parent)
	f.exec(`UPDATE places SET lat = $2, lng = $3, category = $4 WHERE id = $1`, id, lat, lng, category)
	return id
}

// storyDay numbers the days stories are written about, so no two stories
// at one place share a day.
var storyDay atomic.Int64

// story publishes one readable story at the place, on a day of its own, and
// returns its id.
func (f twinFx) story(placeID uuid.UUID) uuid.UUID {
	f.t.Helper()
	by := f.user(true)
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(storyDay.Add(1))).Format(time.DateOnly)
	a := f.arc(placeID, date, "Game day "+uuid.NewString()[:6], by, time.Now())
	return f.perspective(a, by, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, time.Now(), "text")
}

// TestGetPlaceTwin covers the empty place page's pointer to the copy people
// write about: placectl dedupe's name and distance rule, limited to a
// visible twin with a readable story, and only for a place with none.
func TestGetPlaceTwin(t *testing.T) {
	t.Parallel()
	const arts = "PLACE_CATEGORY_ARTS_AND_ENTERTAINMENT"
	const sports = "PLACE_CATEGORY_SPORTS_AND_RECREATION"
	tests := []struct {
		name string
		// setup returns the place GetPlace is asked about and the twin
		// expected, uuid.Nil for none.
		setup func(f twinFx) (asked, want uuid.UUID)
	}{
		{
			name: "same name under the same parent 780 m away is the twin",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				twin := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.tampa, 27.975933, -82.503369, arts)
				f.story(twin)
				return f.empty, twin
			},
		},
		{
			name: "same name under another parent within 200 m is the twin",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				twin := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.lakeland, emptyLat+0.001, emptyLng, arts)
				f.story(twin)
				return f.empty, twin
			},
		},
		{
			name: "a sports venue under another parent reaches 2 km",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				f.exec(`UPDATE places SET kind = 'PLACE_KIND_MALL', category = $2 WHERE id = $1`, f.empty, sports)
				twin := f.listing(perspectivv1.PlaceKind_PLACE_KIND_MALL, "Raymond James Stadium", f.lakeland, emptyLat+0.012, emptyLng, sports)
				f.story(twin)
				return f.empty, twin
			},
		},
		{
			name: "of two twins the one with more stories",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				near := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.tampa, emptyLat+0.0005, emptyLng, arts)
				busy := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "raymond james stadium.", f.tampa, emptyLat-0.007, emptyLng, arts)
				f.story(near)
				f.story(busy)
				f.story(busy)
				return f.empty, busy
			},
		},
		{
			name: "a hidden twin is not offered",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				twin := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.tampa, 27.975933, -82.503369, arts)
				f.story(twin)
				f.hide(twin)
				return f.empty, uuid.Nil
			},
		},
		{
			name: "a twin under a hidden parent is not offered",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				twin := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.lakeland, emptyLat+0.001, emptyLng, arts)
				f.story(twin)
				f.hide(f.lakeland)
				return f.empty, uuid.Nil
			},
		},
		{
			name: "a storyless twin is not offered",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.tampa, 27.975933, -82.503369, arts)
				return f.empty, uuid.Nil
			},
		},
		{
			name: "a twin whose only story is hidden is not offered",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				twin := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.tampa, 27.975933, -82.503369, arts)
				f.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, f.story(twin))
				return f.empty, uuid.Nil
			},
		},
		{
			name: "too far away under another parent",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				twin := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.lakeland, emptyLat+0.003, emptyLng, arts)
				f.story(twin)
				return f.empty, uuid.Nil
			},
		},
		{
			name: "too far away under the same parent",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				twin := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.tampa, emptyLat+0.03, emptyLng, arts)
				f.story(twin)
				return f.empty, uuid.Nil
			},
		},
		{
			name: "a different name is not a twin",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				other := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium Lot 6", f.tampa, emptyLat+0.0005, emptyLng, arts)
				f.story(other)
				return f.empty, uuid.Nil
			},
		},
		{
			name: "a place with its own story has no twin",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				twin := f.listing(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Raymond James Stadium", f.tampa, 27.975933, -82.503369, arts)
				f.story(twin)
				f.story(f.empty)
				return f.empty, uuid.Nil
			},
		},
		{
			name: "a city is never given a twin",
			setup: func(f twinFx) (uuid.UUID, uuid.UUID) {
				f.story(f.place(perspectivv1.PlaceKind_PLACE_KIND_CITY, "Tampa, Illinois", f.state))
				return f.tampa, uuid.Nil
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newTwinFx(t)
			asked, want := tc.setup(f)
			res, err := client(t, f.pool).GetPlace(context.Background(), as(uuid.Nil, &perspectivv1.GetPlaceRequest{PlaceId: asked.String()}))
			if err != nil {
				t.Fatalf("GetPlace: %v", err)
			}
			got := res.Msg.GetTwin()
			if want == uuid.Nil {
				if got != nil {
					t.Fatalf("twin = %v, want none", got)
				}
				return
			}
			if got.GetId() != want.String() || got.GetName() == "" {
				t.Fatalf("twin = %v, want %s", got, want)
			}
		})
	}
}

// TestGetEpicNewThisWeek covers the Newest tab's "New" mark: a day is new
// when a readable story on it was published within the seven days a
// followed place counts.
func TestGetEpicNewThisWeek(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	now := time.Now()
	day := func(title, date string, published ...time.Time) uuid.UUID {
		a := h.arc(h.store, date, title, h.user(true), now.Add(-30*24*time.Hour))
		var last uuid.UUID
		for _, at := range published {
			last = h.perspective(a, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, at, "text")
		}
		return last
	}
	day("Fresh", "2026-09-01", now.Add(-10*24*time.Hour), now.Add(-2*24*time.Hour))
	day("Old", "2026-09-02", now.Add(-10*24*time.Hour))
	hidden := day("Hidden news", "2026-09-03", now.Add(-12*24*time.Hour), now.Add(-time.Hour))
	h.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)

	for _, tab := range []perspectivv1.EpicTab{perspectivv1.EpicTab_EPIC_TAB_NEWEST, perspectivv1.EpicTab_EPIC_TAB_RANKED} {
		t.Run(tab.String(), func(t *testing.T) {
			t.Parallel()
			res := epic(t, c, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: tab})
			got := map[string]bool{}
			for _, a := range res.GetArcs() {
				got[a.GetTitle()] = a.GetNewThisWeek()
			}
			want := map[string]bool{"Fresh": true, "Old": false, "Hidden news": false}
			for title, isNew := range want {
				if v, ok := got[title]; !ok || v != isNew {
					t.Errorf("%s new_this_week = %v (listed %v), want %v", title, v, ok, isNew)
				}
			}
		})
	}
}
