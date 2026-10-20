//go:build integration

package place_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) {
	testdb.Main(m)
}

// fx writes rows owned by other domains (users, arcs, perspectives, lores)
// through plain SQL, so each test controls exactly the state the place
// queries read.
type fx struct {
	t    *testing.T
	pool *pgxpool.Pool
}

func newFx(t *testing.T) fx {
	t.Helper()
	return fx{t: t, pool: testdb.New(t)}
}

var userSeq atomic.Int64

func (f fx) id(query string, args ...any) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&id); err != nil {
		f.t.Fatalf("%s: %v", strings.Join(strings.Fields(query)[:3], " "), err)
	}
	return id
}

func (f fx) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), query, args...); err != nil {
		f.t.Fatalf("%s: %v", strings.Join(strings.Fields(query)[:2], " "), err)
	}
}

// user inserts an account; verified sets email_verified_at.
func (f fx) user(verified bool) uuid.UUID {
	f.t.Helper()
	n := userSeq.Add(1)
	var at *time.Time
	if verified {
		now := time.Now()
		at = &now
	}
	return f.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at, email_verified_at)
		VALUES ($1, 'x', $2, now(), $3) RETURNING id`,
		fmt.Sprintf("u%d@example.test", n), fmt.Sprintf("user_%d", n), at)
}

// place inserts an imported place; parent uuid.Nil makes a root.
func (f fx) place(kind perspectivv1.PlaceKind, name string, parent uuid.UUID) uuid.UUID {
	f.t.Helper()
	var p *uuid.UUID
	if parent != uuid.Nil {
		p = &parent
	}
	return f.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ($1, $2, $3, 'America/Chicago', 'PLACE_SOURCE_OVERTURE') RETURNING id`, kind.String(), name, p)
}

func (f fx) hide(placeID uuid.UUID) {
	f.t.Helper()
	f.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, placeID)
}

// arc inserts a day arc created at createdAt.
func (f fx) arc(placeID uuid.UUID, date, title string, by uuid.UUID, createdAt time.Time) uuid.UUID {
	f.t.Helper()
	return f.id(`INSERT INTO arcs (place_id, local_date, title, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, placeID, date, title, by, createdAt)
}

// hourArc inserts an hour child of a day arc.
func (f fx) hourArc(day uuid.UUID, hour int, title string, by uuid.UUID, createdAt time.Time) uuid.UUID {
	f.t.Helper()
	return f.id(`INSERT INTO arcs (place_id, local_date, hour, parent_arc_id, title, created_by, created_at)
		SELECT place_id, local_date, $2, id, $3, $4, $5 FROM arcs WHERE id = $1 RETURNING id`,
		day, hour, title, by, createdAt)
}

// canon gives an arc a canon lore and the stored score the rank job keeps.
func (f fx) canon(arcID uuid.UUID, score float64, members int) uuid.UUID {
	f.t.Helper()
	lore := f.id(`INSERT INTO lores (arc_id, status, member_count, rank_score)
		VALUES ($1, 'LORE_STANDING_CANON', $2, $3) RETURNING id`, arcID, members, score)
	f.exec(`UPDATE arcs SET canon_lore_id = $2, canon_score = $3, member_count = $4 WHERE id = $1`,
		arcID, lore, score, members)
	return lore
}

func (f fx) members(arcID uuid.UUID, members int) {
	f.t.Helper()
	f.exec(`UPDATE arcs SET member_count = $2 WHERE id = $1`, arcID, members)
}

// perspective inserts a published perspective with one part.
func (f fx) perspective(arcID, author uuid.UUID, genre perspectivv1.Genre, status perspectivv1.LoreStatus, at time.Time, body string) uuid.UUID {
	f.t.Helper()
	id := f.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, $3, 'PERSPECTIVE_STATUS_PUBLISHED', $4, $5) RETURNING id`,
		arcID, author, genre.String(), status.String(), at)
	f.exec(`INSERT INTO perspective_parts (perspective_id, position, body) VALUES ($1, 1, $2)`, id, body)
	return id
}

// hierarchy is one full drill-down chain plus a sibling city.
type hierarchy struct {
	fx
	state, city, town, mall, store, otherCity uuid.UUID
}

func newHierarchy(t *testing.T) hierarchy {
	t.Helper()
	f := newFx(t)
	h := hierarchy{fx: f}
	h.state = f.place(perspectivv1.PlaceKind_PLACE_KIND_STATE, "Illinois", uuid.Nil)
	h.city = f.place(perspectivv1.PlaceKind_PLACE_KIND_CITY, "Springfield", h.state)
	h.town = f.place(perspectivv1.PlaceKind_PLACE_KIND_TOWN, "Southtown", h.city)
	h.mall = f.place(perspectivv1.PlaceKind_PLACE_KIND_MALL, "White Oaks Mall", h.town)
	h.store = f.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Corner Books", h.mall)
	h.otherCity = f.place(perspectivv1.PlaceKind_PLACE_KIND_CITY, "Chicago", h.state)
	return h
}

func refIDs(refs []place.Ref) []uuid.UUID {
	out := make([]uuid.UUID, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out
}

// TestStoreGetAncestors covers the ancestors CTE: a place's parent chain runs
// from the STATE down to its parent, and a place under a hidden ancestor is
// not found.
func TestStoreGetAncestors(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	s := place.NewStore(h.pool)
	ctx := context.Background()

	tests := []struct {
		name string
		id   uuid.UUID
		want []uuid.UUID
	}{
		{name: "state has no ancestors", id: h.state, want: []uuid.UUID{}},
		{name: "city", id: h.city, want: []uuid.UUID{h.state}},
		{name: "store has the full chain", id: h.store, want: []uuid.UUID{h.state, h.city, h.town, h.mall}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Get(ctx, tc.id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if !slices.Equal(refIDs(got.Ancestors), tc.want) {
				t.Fatalf("ancestors = %v, want %v", refIDs(got.Ancestors), tc.want)
			}
		})
	}

	t.Run("hidden ancestor hides the subtree", func(t *testing.T) {
		h := newHierarchy(t)
		s := place.NewStore(h.pool)
		h.hide(h.town)
		for _, id := range []uuid.UUID{h.town, h.mall, h.store} {
			if _, err := s.Get(ctx, id); !errors.Is(err, place.ErrNotFound) {
				t.Fatalf("Get(%v) err = %v, want ErrNotFound", id, err)
			}
		}
		if _, err := s.Get(ctx, h.city); err != nil {
			t.Fatalf("Get(city) err = %v, want visible", err)
		}
	})

	t.Run("missing place", func(t *testing.T) {
		if _, err := s.Get(ctx, uuid.New()); !errors.Is(err, place.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

// TestStoreInsertDedup covers dedup under a parent: the comparison ignores
// case and whitespace runs, and the same name under another parent is fine.
func TestStoreInsertDedup(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	s := place.NewStore(h.pool)
	ctx := context.Background()
	by := h.user(true)

	first, err := s.Insert(ctx, place.NewPlace{Name: "Book Nook", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentID: h.mall, CreatedBy: by})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if !first.UserAdded || first.Source != perspectivv1.PlaceSource_PLACE_SOURCE_USER || first.Timezone != "America/Chicago" {
		t.Fatalf("inserted = %+v, want user-added with the parent's timezone", first)
	}

	tests := []struct {
		name    string
		in      place.NewPlace
		wantErr error
	}{
		{name: "different case", in: place.NewPlace{Name: "BOOK NOOK", ParentID: h.mall}, wantErr: place.ErrDuplicateName},
		{name: "extra inner and outer whitespace", in: place.NewPlace{Name: "  book   nook ", ParentID: h.mall}, wantErr: place.ErrDuplicateName},
		{name: "same name under another parent", in: place.NewPlace{Name: "Book Nook", ParentID: h.town}},
		{name: "near duplicate is not blocked", in: place.NewPlace{Name: "Book Nooks", ParentID: h.mall}},
		{name: "missing parent", in: place.NewPlace{Name: "Anything", ParentID: uuid.New()}, wantErr: place.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Kind = perspectivv1.PlaceKind_PLACE_KIND_STORE
			tc.in.CreatedBy = by
			_, err := s.Insert(ctx, tc.in)
			if !errors.Is(err, tc.wantErr) && (tc.wantErr != nil || err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}

	t.Run("hidden parent", func(t *testing.T) {
		h.hide(h.otherCity)
		_, err := s.Insert(ctx, place.NewPlace{Name: "Hidden Store", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentID: h.otherCity, CreatedBy: by})
		if !errors.Is(err, place.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

// TestStoreKindRestriction covers the kind restriction at the store: the
// database refuses a kind outside the five levels, and the PlaceKind enum
// has exactly STATE, CITY, TOWN, MALL and STORE (AC-007).
func TestStoreKindRestriction(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	_, err := f.pool.Exec(context.Background(), `INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_COUNTRY', 'USA', 'UTC', 'PLACE_SOURCE_OVERTURE')`)
	if err == nil {
		t.Fatal("a sixth place kind was accepted")
	}

	var got []string
	for v, name := range perspectivv1.PlaceKind_name {
		if v != 0 {
			got = append(got, name)
		}
	}
	slices.Sort(got)
	want := []string{"PLACE_KIND_CITY", "PLACE_KIND_MALL", "PLACE_KIND_STATE", "PLACE_KIND_STORE", "PLACE_KIND_TOWN"}
	if !slices.Equal(got, want) {
		t.Fatalf("PlaceKind values = %v, want %v", got, want)
	}
}

// TestStoreDescendants covers the descendants CTE through the epic listing:
// arcs at the place and every visible descendant, nothing from a sibling,
// nothing under a hidden descendant.
func TestStoreDescendants(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	s := place.NewStore(h.pool)
	ctx := context.Background()
	by := h.user(true)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	arcs := map[string]uuid.UUID{}
	for i, p := range []struct {
		name string
		id   uuid.UUID
	}{{"city", h.city}, {"town", h.town}, {"mall", h.mall}, {"store", h.store}, {"other", h.otherCity}} {
		a := h.arc(p.id, "2026-10-01", "Arc at "+p.name, by, base.Add(time.Duration(i)*time.Minute))
		h.perspective(a, by, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, base, "words")
		arcs[p.name] = a
	}

	list := func(t *testing.T, root uuid.UUID) []uuid.UUID {
		t.Helper()
		got, err := s.EpicArcs(ctx, place.EpicArcsQuery{PlaceID: root, Order: place.OrderNewest, Limit: 50})
		if err != nil {
			t.Fatalf("EpicArcs: %v", err)
		}
		ids := make([]uuid.UUID, len(got))
		for i, a := range got {
			ids[i] = a.ID
		}
		return ids
	}

	if got, want := list(t, h.city), []uuid.UUID{arcs["store"], arcs["mall"], arcs["town"], arcs["city"]}; !slices.Equal(got, want) {
		t.Fatalf("city descendants = %v, want %v", got, want)
	}
	if got, want := list(t, h.mall), []uuid.UUID{arcs["store"], arcs["mall"]}; !slices.Equal(got, want) {
		t.Fatalf("mall descendants = %v, want %v", got, want)
	}
	if got, want := list(t, h.state), []uuid.UUID{arcs["other"], arcs["store"], arcs["mall"], arcs["town"], arcs["city"]}; !slices.Equal(got, want) {
		t.Fatalf("state descendants = %v, want %v", got, want)
	}

	h.hide(h.mall)
	if got, want := list(t, h.city), []uuid.UUID{arcs["town"], arcs["city"]}; !slices.Equal(got, want) {
		t.Fatalf("after hiding the mall = %v, want %v", got, want)
	}
}

// The places with stories under a parent are its children with a readable
// story at or below them, newest first; hidden ones and ones whose only
// story was deleted are left out. A place moved to another parent takes its
// arcs' paths with it.
func TestStoreStoriedChildren(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	s := place.NewStore(h.pool)
	ctx := context.Background()
	by := h.user(true)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	quiet := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Quiet Shop", h.city)
	gone := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Gone Shop", h.city)
	story := func(at uuid.UUID, mins int, deleted bool) {
		t.Helper()
		a := h.arc(at, "2026-10-01", "Arc", by, base.Add(time.Duration(mins)*time.Minute))
		p := h.perspective(a, by, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, base, "words")
		if deleted {
			h.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, p)
		}
	}
	story(h.store, 1, false) // under the mall, so the mall is the city's child with it
	story(h.town, 2, false)
	story(gone, 3, true)
	_ = quiet

	names := func(parent uuid.UUID) []string {
		t.Helper()
		ps, err := s.StoriedChildren(ctx, parent, 20)
		if err != nil {
			t.Fatalf("StoriedChildren: %v", err)
		}
		out := make([]string, len(ps))
		for i, p := range ps {
			out[i] = p.Name
		}
		return out
	}
	// The hierarchy is state > city > town > mall > store.
	if got := names(h.city); !slices.Equal(got, []string{h.name(h.town)}) {
		t.Fatalf("storied children of the city = %v, want the town", got)
	}
	if got := names(h.town); !slices.Equal(got, []string{h.name(h.mall)}) {
		t.Fatalf("storied children of the town = %v, want the mall (the town's own arc is not a child)", got)
	}
	if got := names(uuid.Nil); !slices.Equal(got, []string{h.name(h.state)}) {
		t.Fatalf("storied states = %v", got)
	}

	// Moving the store from the mall to the town moves its arc's path too.
	h.exec(`UPDATE places SET parent_id = $1 WHERE id = $2`, h.town, h.store)
	if got := names(h.mall); len(got) != 0 {
		t.Fatalf("storied children of the mall after the move = %v, want none", got)
	}
	if got := names(h.town); !slices.Equal(got, []string{h.name(h.store)}) {
		t.Fatalf("storied children of the town after the move = %v, want the store", got)
	}

	h.hide(h.town)
	if got := names(h.city); len(got) != 0 {
		t.Fatalf("after hiding the town = %v, want none", got)
	}
}

// Children sort by name ignoring leading quotes and symbols.
func TestStoreChildrenNameOrder(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	s := place.NewStore(h.pool)
	parent := h.place(perspectivv1.PlaceKind_PLACE_KIND_TOWN, "Order Town", h.city)
	for _, n := range []string{"#1 Garage Door", "\"Chichi\" Realty", "Apple Market", "Bistro"} {
		h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, n, parent)
	}
	ps, err := s.Children(context.Background(), parent, nil, 50)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	var got []string
	for _, p := range ps {
		got = append(got, p.Name)
	}
	// "#1 Garage Door" files under "1", before the letters, as digits do.
	if want := []string{"#1 Garage Door", "Apple Market", "Bistro", "\"Chichi\" Realty"}; !slices.Equal(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
}

func (f fx) name(id uuid.UUID) string {
	f.t.Helper()
	var n string
	if err := f.pool.QueryRow(context.Background(), `SELECT name FROM places WHERE id = $1`, id).Scan(&n); err != nil {
		f.t.Fatalf("place name: %v", err)
	}
	return n
}
