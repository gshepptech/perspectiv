//go:build integration

package place_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
)

// searchIDs runs SearchPlaces and returns the ids found and the more flag.
func searchIDs(t *testing.T, c perspectivv1connect.PlaceServiceClient, req *perspectivv1.SearchPlacesRequest) ([]string, bool) {
	t.Helper()
	res, err := c.SearchPlaces(context.Background(), as(uuid.Nil, req))
	if err != nil {
		t.Fatalf("SearchPlaces(%q): %v", req.GetQuery(), err)
	}
	ids := make([]string, len(res.Msg.GetPlaces()))
	for i, p := range res.Msg.GetPlaces() {
		ids[i] = p.GetId()
	}
	return ids, res.Msg.GetMore()
}

func idStrings(ids ...uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// TestSearchPlacesByLocality covers a search that names a place and the
// city or town it is in: every form of the split, the best locality first,
// St./Saint spellings, the plain search's name starts first and as the
// fallback, and the filters and moderation that still apply.
func TestSearchPlacesByLocality(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	store := perspectivv1.PlaceKind_PLACE_KIND_STORE
	indiana := h.place(perspectivv1.PlaceKind_PLACE_KIND_STATE, "Indiana", uuid.Nil)
	chicagoTown := h.place(perspectivv1.PlaceKind_PLACE_KIND_TOWN, "Chicago", indiana)
	stCharles := h.place(perspectivv1.PlaceKind_PLACE_KIND_CITY, "Saint Charles", h.state)
	lakeside := h.place(perspectivv1.PlaceKind_PLACE_KIND_TOWN, "Lakeside", h.city)

	inChicago := h.place(store, "Starbucks", h.otherCity)
	reserve := h.place(store, "Starbucks Reserve", h.otherCity)
	inChicagoTown := h.place(store, "Starbucks", chicagoTown)
	inSouthtown := h.place(store, "Starbucks", h.town)
	grill := h.place(store, "Grill", stCharles)
	h.place(store, "Starbucks", lakeside)
	h.hide(lakeside)
	namedDiner := h.place(store, "Springfield Diner", h.otherCity)
	diner := h.place(store, "Diner", h.city)

	chicagoFirst := idStrings(inChicago, reserve, inChicagoTown)
	tests := []struct {
		name string
		req  *perspectivv1.SearchPlacesRequest
		want []string
	}{
		{name: "name then town", req: &perspectivv1.SearchPlacesRequest{Query: "starbucks chicago"}, want: chicagoFirst},
		{name: "town then name", req: &perspectivv1.SearchPlacesRequest{Query: "Chicago Starbucks"}, want: chicagoFirst},
		{name: "name, comma, town", req: &perspectivv1.SearchPlacesRequest{Query: "starbucks, chicago"}, want: chicagoFirst},
		{name: "a state after the town", req: &perspectivv1.SearchPlacesRequest{Query: "Starbucks, Chicago, IL"}, want: chicagoFirst},
		{name: "name in town", req: &perspectivv1.SearchPlacesRequest{Query: "starbucks in chicago"}, want: chicagoFirst},
		{name: "a town under a city", req: &perspectivv1.SearchPlacesRequest{Query: "starbucks southtown"}, want: idStrings(inSouthtown)},
		{name: "a city covers its towns", req: &perspectivv1.SearchPlacesRequest{Query: "starbucks springfield"}, want: idStrings(inSouthtown)},
		{name: "st for saint", req: &perspectivv1.SearchPlacesRequest{Query: "grill st charles"}, want: idStrings(grill)},
		{name: "st. for saint", req: &perspectivv1.SearchPlacesRequest{Query: "grill, st. charles"}, want: idStrings(grill)},
		{
			name: "a name starting with the whole search comes first",
			req:  &perspectivv1.SearchPlacesRequest{Query: "springfield diner"},
			want: idStrings(namedDiner, diner),
		},
		{name: "plain search when no town is named", req: &perspectivv1.SearchPlacesRequest{Query: "white oaks mall"}, want: idStrings(h.mall)},
		{name: "nothing under the town and no plain match", req: &perspectivv1.SearchPlacesRequest{Query: "grill chicago"}, want: []string{}},
		{name: "a hidden town is not a locality", req: &perspectivv1.SearchPlacesRequest{Query: "starbucks lakeside"}, want: []string{}},
		{
			name: "kind filter applies under the town",
			req:  &perspectivv1.SearchPlacesRequest{Query: "starbucks chicago", Kind: perspectivv1.PlaceKind_PLACE_KIND_MALL},
			want: []string{},
		},
		{
			name: "parent filter limits the towns",
			req:  &perspectivv1.SearchPlacesRequest{Query: "starbucks chicago", ParentId: h.state.String()},
			want: idStrings(inChicago, reserve),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, more := searchIDs(t, c, tc.req)
			if !slices.Equal(got, tc.want) || more {
				t.Fatalf("ids = %v, more %v; want %v", got, more, tc.want)
			}
		})
	}

	// A story makes a town the best Chicago, ahead of the larger city.
	t.Run("a locality with stories comes first", func(t *testing.T) {
		by := h.user(true)
		t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
		h.perspective(h.arc(inChicagoTown, "2026-10-01", "Coffee", by, t0), by,
			perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "text")
		got, _ := searchIDs(t, c, &perspectivv1.SearchPlacesRequest{Query: "starbucks chicago"})
		if want := idStrings(inChicagoTown, inChicago, reserve); !slices.Equal(got, want) {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	})
}

// A story under a hidden place is not readable, so it does not make its
// town the best locality: the search would otherwise tell anyone that the
// hidden place holds a story.
func TestSearchLocalityIgnoresHiddenStories(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	store := perspectivv1.PlaceKind_PLACE_KIND_STORE
	indiana := h.place(perspectivv1.PlaceKind_PLACE_KIND_STATE, "Indiana", uuid.Nil)
	chicagoTown := h.place(perspectivv1.PlaceKind_PLACE_KIND_TOWN, "Chicago", indiana)
	inChicago := h.place(store, "Starbucks", h.otherCity)
	inChicagoTown := h.place(store, "Starbucks", chicagoTown)
	hiddenShop := h.place(store, "Closed Shop", chicagoTown)
	by := h.user(true)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	h.perspective(h.arc(hiddenShop, "2026-10-01", "Coffee", by, t0), by,
		perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "text")

	tests := []struct {
		name string
		hide bool
		want []string
	}{
		{name: "a readable story lifts the town", want: idStrings(inChicagoTown, inChicago)},
		{name: "a story under a hidden place does not", hide: true, want: idStrings(inChicago, inChicagoTown)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.hide {
				h.hide(hiddenShop)
			}
			got, _ := searchIDs(t, c, &perspectivv1.SearchPlacesRequest{Query: "starbucks chicago"})
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ids = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSearchPlacesMore covers the more flag: a search that hits the result
// cap says so, by itself or under a town, and one that fits does not.
func TestSearchPlacesMore(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	for i := range 20 {
		h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, fmt.Sprintf("Kiosk %02d", i), h.otherCity)
	}
	tests := []struct {
		name, query string
		extra       bool
		wantLen     int
		wantMore    bool
	}{
		{name: "exactly the cap", query: "kiosk", wantLen: 20},
		{name: "exactly the cap under a town", query: "kiosk chicago", wantLen: 20},
		{name: "over the cap", query: "kiosk", extra: true, wantLen: 20, wantMore: true},
		{name: "over the cap under a town", query: "kiosk chicago", extra: true, wantLen: 20, wantMore: true},
	}
	added := false
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.extra && !added {
				h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Kiosk 20", h.otherCity)
				added = true
			}
			got, more := searchIDs(t, c, &perspectivv1.SearchPlacesRequest{Query: tc.query})
			if len(got) != tc.wantLen || more != tc.wantMore {
				t.Fatalf("%d places, more %v; want %d, more %v", len(got), more, tc.wantLen, tc.wantMore)
			}
		})
	}
}

// TestSearchPlacesSplit covers the split SearchPlaces returns for the add
// form: the name part as typed and the best locality with its label, a
// state as a locality, and no split when nothing names a locality or a
// place's name starts with the whole search.
func TestSearchPlacesSplit(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	store := perspectivv1.PlaceKind_PLACE_KIND_STORE
	indiana := h.place(perspectivv1.PlaceKind_PLACE_KIND_STATE, "Indiana", uuid.Nil)
	chicagoTown := h.place(perspectivv1.PlaceKind_PLACE_KIND_TOWN, "Chicago", indiana)
	lakeside := h.place(perspectivv1.PlaceKind_PLACE_KIND_TOWN, "Lakeside", h.city)
	h.hide(lakeside)
	inChicago := h.place(store, "Starbucks", h.otherCity)
	inChicagoTown := h.place(store, "Starbucks", chicagoTown)
	inSouthtown := h.place(store, "Starbucks", h.town)
	h.place(store, "Springfield Diner", h.otherCity)

	tests := []struct {
		name    string
		req     *perspectivv1.SearchPlacesRequest
		want    *splitWant
		wantIDs []string
		wantNot []string
	}{
		{
			name: "a place not listed keeps its name as typed",
			req:  &perspectivv1.SearchPlacesRequest{Query: "joes  crab shack Chicago"},
			want: &splitWant{name: "joes crab shack", localityID: h.otherCity.String(), label: "Chicago, Illinois"},
		},
		{
			name: "a town is labelled up to its state",
			req:  &perspectivv1.SearchPlacesRequest{Query: "Starbucks, Southtown"},
			want: &splitWant{name: "Starbucks", localityID: h.town.String(), label: "Southtown, Springfield, Illinois"},
		},
		{
			name: "a last in is not part of the name",
			req:  &perspectivv1.SearchPlacesRequest{Query: "Corner Cafe in Chicago"},
			want: &splitWant{name: "Corner Cafe", localityID: h.otherCity.String(), label: "Chicago, Illinois"},
		},
		{
			name:    "a state narrows the search",
			req:     &perspectivv1.SearchPlacesRequest{Query: "starbucks illinois"},
			want:    &splitWant{name: "starbucks", localityID: h.state.String(), label: "Illinois"},
			wantIDs: idStrings(inChicago, inSouthtown),
			wantNot: idStrings(inChicagoTown),
		},
		{
			name: "the parent filter picks the locality",
			req:  &perspectivv1.SearchPlacesRequest{Query: "starbucks chicago", ParentId: indiana.String()},
			want: &splitWant{name: "starbucks", localityID: chicagoTown.String(), label: "Chicago, Indiana"},
		},
		{name: "one word has no split", req: &perspectivv1.SearchPlacesRequest{Query: "starbucks"}},
		{name: "no locality named", req: &perspectivv1.SearchPlacesRequest{Query: "white oaks mall"}},
		{name: "a hidden town is not a locality", req: &perspectivv1.SearchPlacesRequest{Query: "starbucks lakeside"}},
		{name: "a name starting with the whole search is a name", req: &perspectivv1.SearchPlacesRequest{Query: "springfield diner"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := c.SearchPlaces(context.Background(), as(uuid.Nil, tc.req))
			if err != nil {
				t.Fatalf("SearchPlaces(%q): %v", tc.req.GetQuery(), err)
			}
			checkSplit(t, res.Msg.GetSplit(), tc.want)
			ids := make([]string, len(res.Msg.GetPlaces()))
			for i, p := range res.Msg.GetPlaces() {
				ids[i] = p.GetId()
			}
			for _, id := range tc.wantIDs {
				if !slices.Contains(ids, id) {
					t.Errorf("places %v lack %s", ids, id)
				}
			}
			if i := slices.IndexFunc(tc.wantNot, func(id string) bool { return slices.Contains(ids, id) }); i >= 0 {
				t.Errorf("places %v hold %s, outside the state", ids, tc.wantNot[i])
			}
		})
	}
}

// splitWant is the split a search should return: the name part, the
// locality's id and its label.
type splitWant struct{ name, localityID, label string }

// checkSplit fails t unless got is the split want describes, or both are
// absent.
func checkSplit(t *testing.T, got *perspectivv1.SearchSplit, want *splitWant) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Fatalf("split = %v, want none", got)
	case want != nil && got == nil:
		t.Fatalf("no split, want %+v", *want)
	case want != nil:
		if g := (splitWant{got.GetName(), got.GetLocalityId(), got.GetLocalityLabel()}); g != *want {
			t.Fatalf("split = %+v, want %+v", g, *want)
		}
	}
}
