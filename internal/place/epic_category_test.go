//go:build integration

package place_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// setCategory files a place under a category, as the place import does.
func (f fx) setCategory(placeID uuid.UUID, c perspectivv1.PlaceCategory) {
	f.t.Helper()
	f.exec(`UPDATE places SET category = $2 WHERE id = $1`, placeID, c.String())
}

const (
	sports = perspectivv1.PlaceCategory_PLACE_CATEGORY_SPORTS_AND_RECREATION
	food   = perspectivv1.PlaceCategory_PLACE_CATEGORY_FOOD_AND_DRINK
)

// TestGetEpicCategory covers canon by category: with a category, RANKED and
// NEWEST list only the days whose own place has it, everywhere or under a
// place, never under a hidden place, and page with the same keysets; TALES
// filters the same way.
func TestGetEpicCategory(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	by := h.user(true)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	store := func(name string, parent uuid.UUID, cat perspectivv1.PlaceCategory) uuid.UUID {
		id := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, name, parent)
		h.setCategory(id, cat)
		return id
	}
	// Each day is created on its own date, n days after t0.
	day := func(placeID uuid.UUID, title string, n int, status perspectivv1.LoreStatus) uuid.UUID {
		created := t0.AddDate(0, 0, n)
		a := h.arc(placeID, created.Format(time.DateOnly), title, by, created)
		h.perspective(a, h.user(true), perspectivv1.Genre_GENRE_DRAMA, status, created, title+" opens")
		return a
	}
	gym := store("Iron Gym", h.city, sports)
	courts := store("Lake Courts", h.otherCity, sports)
	cafe := store("Bean Bar", h.city, food)
	ohio := h.place(perspectivv1.PlaceKind_PLACE_KIND_STATE, "Ohio", uuid.Nil)
	field := store("Buckeye Field", ohio, sports)
	rink := store("Hidden Rink", h.town, sports)

	h.canon(day(gym, "Gym final", 0, perspectivv1.LoreStatus_LORE_STATUS_IN_LORE), 5, 3)
	h.canon(day(courts, "Courts open", 1, perspectivv1.LoreStatus_LORE_STATUS_IN_LORE), 3, 2)
	h.members(day(field, "Field day", 2, perspectivv1.LoreStatus_LORE_STATUS_PENDING), 2)
	day(gym, "Gym quiet day", 3, perspectivv1.LoreStatus_LORE_STATUS_SOLO)
	day(cafe, "Cafe morning", 4, perspectivv1.LoreStatus_LORE_STATUS_SOLO)
	// The city has no category; the store's does not carry up to it.
	day(h.city, "Downtown parade", 5, perspectivv1.LoreStatus_LORE_STATUS_SOLO)
	// A sports day under a hidden place is not listed.
	day(rink, "Rink closing", 6, perspectivv1.LoreStatus_LORE_STATUS_SOLO)
	h.hide(h.town)
	// A place filed under a category after its days began brings them along.
	late := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Late Lanes", h.otherCity)
	day(late, "Bowling night", 7, perspectivv1.LoreStatus_LORE_STATUS_PENDING)
	h.setCategory(late, sports)

	ranked, newest := perspectivv1.EpicTab_EPIC_TAB_RANKED, perspectivv1.EpicTab_EPIC_TAB_NEWEST
	tests := []struct {
		name    string
		placeID uuid.UUID
		tab     perspectivv1.EpicTab
		cat     perspectivv1.PlaceCategory
		want    []string
	}{
		{name: "sports everywhere, ranked", tab: ranked, cat: sports,
			want: []string{"Gym final", "Courts open", "Field day", "Bowling night", "Gym quiet day"}},
		{name: "sports everywhere, newest", tab: newest, cat: sports,
			want: []string{"Bowling night", "Gym quiet day", "Field day", "Courts open", "Gym final"}},
		{name: "sports under a state, ranked", placeID: h.state, tab: ranked, cat: sports,
			want: []string{"Gym final", "Courts open", "Bowling night", "Gym quiet day"}},
		{name: "sports under a city, newest", placeID: h.otherCity, tab: newest, cat: sports,
			want: []string{"Bowling night", "Courts open"}},
		{name: "food everywhere", tab: ranked, cat: food, want: []string{"Cafe morning"}},
		{name: "a category with no days", tab: ranked, cat: perspectivv1.PlaceCategory_PLACE_CATEGORY_HEALTH, want: nil},
		{name: "no category lists every day under the city", placeID: h.city, tab: newest,
			want: []string{"Downtown parade", "Cafe morning", "Gym quiet day", "Gym final"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := func(size int32) *perspectivv1.GetEpicRequest {
				r := &perspectivv1.GetEpicRequest{Tab: tc.tab, Category: tc.cat, PageSize: size}
				if tc.placeID != uuid.Nil {
					r.PlaceId = tc.placeID.String()
				}
				return r
			}
			if got := epicPages(t, c, req(0)); !slices.Equal(got, tc.want) {
				t.Fatalf("one page = %q, want %q", got, tc.want)
			}
			// Paged one day at a time, the keyset walks the same list.
			if got := epicPages(t, c, req(1)); !slices.Equal(got, tc.want) {
				t.Fatalf("paged = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("tales", func(t *testing.T) {
		t.Parallel()
		res := epic(t, c, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_TALES, Category: sports})
		var titles []string
		for _, tl := range res.GetTales() {
			titles = append(titles, tl.GetArcTitle())
		}
		if want := []string{"Gym quiet day"}; !slices.Equal(titles, want) {
			t.Fatalf("sports tales under the city = %q, want %q", titles, want)
		}
	})

	t.Run("an unknown category", func(t *testing.T) {
		t.Parallel()
		_, err := c.GetEpic(context.Background(), as(uuid.Nil, &perspectivv1.GetEpicRequest{
			Tab: ranked, Category: perspectivv1.PlaceCategory(99),
		}))
		wantCode(t, err, connect.CodeInvalidArgument)
	})
}

// TestGetEpicCategoryFollowsReclassification: a place moved to another
// category takes its days with it.
func TestGetEpicCategoryFollowsReclassification(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	by := h.user(true)
	h.setCategory(h.store, sports)
	a := h.arc(h.store, "2026-10-01", "Book signing", by, time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	h.perspective(a, by, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, time.Now(), "x")
	h.setCategory(h.store, perspectivv1.PlaceCategory_PLACE_CATEGORY_SHOPPING)

	tests := []struct {
		name string
		cat  perspectivv1.PlaceCategory
		want []string
	}{
		{name: "gone from the old category", cat: sports, want: nil},
		{name: "listed under the new one", cat: perspectivv1.PlaceCategory_PLACE_CATEGORY_SHOPPING, want: []string{"Book signing"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := epicPages(t, c, &perspectivv1.GetEpicRequest{Tab: perspectivv1.EpicTab_EPIC_TAB_RANKED, Category: tc.cat})
			if !slices.Equal(got, tc.want) {
				t.Fatalf("days = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestListStoriedCategories: the categories with a readable day, in enum
// order; a day whose only story is hidden or deleted, or whose place is
// hidden, does not count.
func TestListStoriedCategories(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	by := h.user(true)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	list := func() []perspectivv1.PlaceCategory {
		t.Helper()
		res, err := c.ListStoriedCategories(context.Background(), as(uuid.Nil, &perspectivv1.ListStoriedCategoriesRequest{}))
		if err != nil {
			t.Fatalf("ListStoriedCategories: %v", err)
		}
		return res.Msg.GetCategories()
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("with no days = %v, want none", got)
	}

	storyAt := func(name string, parent uuid.UUID, cat perspectivv1.PlaceCategory) uuid.UUID {
		id := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, name, parent)
		h.setCategory(id, cat)
		a := h.arc(id, "2026-10-01", name+" day", by, t0)
		return h.perspective(a, by, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "x")
	}
	storyAt("Iron Gym", h.city, sports)
	storyAt("Night Owl", h.city, perspectivv1.PlaceCategory_PLACE_CATEGORY_NIGHTLIFE)
	storyAt("River Park", h.otherCity, perspectivv1.PlaceCategory_PLACE_CATEGORY_OUTDOORS)
	hidden := storyAt("Bean Bar", h.city, food)
	h.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)
	deleted := storyAt("Grand Hotel", h.city, perspectivv1.PlaceCategory_PLACE_CATEGORY_LODGING)
	h.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, deleted)
	storyAt("Town Clinic", h.town, perspectivv1.PlaceCategory_PLACE_CATEGORY_HEALTH)
	h.hide(h.town)

	want := []perspectivv1.PlaceCategory{
		perspectivv1.PlaceCategory_PLACE_CATEGORY_NIGHTLIFE,
		perspectivv1.PlaceCategory_PLACE_CATEGORY_OUTDOORS,
		sports,
	}
	if got := list(); !slices.Equal(got, want) {
		t.Fatalf("categories = %v, want %v", got, want)
	}
}
