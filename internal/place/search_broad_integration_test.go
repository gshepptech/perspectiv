//go:build integration

package place_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place/dbq"
)

// mixedPlaces is a hierarchy whose names are short runs of "a" and "b", so
// one- and two-letter prefixes match places of every kind, many of them
// with the same name at different kinds and depths. Some places are hidden,
// some sit under a hidden place, and some have stories, so every rank key
// and filter of SearchPlaces has something to tell apart.
type mixedPlaces struct {
	fx
	states, cities []uuid.UUID
	hiddenCity     uuid.UUID
	// names are every run of "a" and "b" one to four letters long, handed
	// out in an order drawn from seed; used holds each parent's names taken.
	names   []string
	seed    uint32
	used    map[uuid.UUID]map[string]bool
	writers []uuid.UUID
}

func newMixedPlaces(t *testing.T) *mixedPlaces {
	t.Helper()
	m := &mixedPlaces{fx: newFx(t), used: map[uuid.UUID]map[string]bool{}, seed: 2463534242}
	for n := 1; n <= 4; n++ {
		for bits := range 1 << n {
			var b strings.Builder
			for i := range n {
				b.WriteByte("ab"[bits>>i&1])
			}
			m.names = append(m.names, b.String())
		}
	}
	// One story per writer on a day, so a place has up to two.
	m.writers = []uuid.UUID{m.user(true), m.user(true)}
	for _, n := range []string{"A", "Ab", "aba", "B", "Bb"} {
		state := m.place(perspectivv1.PlaceKind_PLACE_KIND_STATE, n, uuid.Nil)
		m.states = append(m.states, state)
		for range 4 {
			city := m.add(perspectivv1.PlaceKind_PLACE_KIND_CITY, state)
			m.cities = append(m.cities, city)
			for range 3 {
				m.town(city)
			}
		}
	}
	m.hiddenCity = m.cities[3]
	m.hide(m.hiddenCity)
	m.hide(m.cities[7])
	return m
}

// nameCount is how many runs of "a" and "b" are one to four letters long.
const nameCount = 2 + 4 + 8 + 16

// roll returns the next of a fixed sequence of numbers below n (xorshift),
// so the fixture is the same on every run.
func (m *mixedPlaces) roll(n uint32) uint32 {
	m.seed ^= m.seed << 13
	m.seed ^= m.seed >> 17
	m.seed ^= m.seed << 5
	return m.seed % n
}

// add inserts a place with a name its parent does not have yet; one name in
// five starts with a capital.
func (m *mixedPlaces) add(kind perspectivv1.PlaceKind, parent uuid.UUID) uuid.UUID {
	m.t.Helper()
	if m.used[parent] == nil {
		m.used[parent] = map[string]bool{}
	}
	for {
		n := m.names[m.roll(nameCount)]
		if m.used[parent][n] {
			continue
		}
		m.used[parent][n] = true
		if m.roll(5) == 0 {
			n = strings.ToUpper(n[:1]) + n[1:]
		}
		return m.place(kind, n, parent)
	}
}

// town adds a town with two stores, a mall of three stores, and sometimes
// a story.
func (m *mixedPlaces) town(city uuid.UUID) {
	m.t.Helper()
	town := m.add(perspectivv1.PlaceKind_PLACE_KIND_TOWN, city)
	m.add(perspectivv1.PlaceKind_PLACE_KIND_STORE, town)
	m.add(perspectivv1.PlaceKind_PLACE_KIND_STORE, town)
	mall := m.add(perspectivv1.PlaceKind_PLACE_KIND_MALL, town)
	for range 3 {
		m.store(mall)
	}
	if m.roll(4) == 0 {
		m.story(town, 1)
	}
}

// store adds a store under a mall; about one in eight is hidden and one in
// eight has a story or two.
func (m *mixedPlaces) store(mall uuid.UUID) {
	m.t.Helper()
	st := m.add(perspectivv1.PlaceKind_PLACE_KIND_STORE, mall)
	switch m.roll(8) {
	case 0:
		m.hide(st)
	case 1:
		m.story(st, 1+int(m.roll(2)))
	}
}

func (m *mixedPlaces) story(placeID uuid.UUID, n int) {
	m.t.Helper()
	a := m.arc(placeID, "2024-05-01", "a day", m.writers[0], time.Now())
	for _, w := range m.writers[:n] {
		m.perspective(a, w, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, time.Now(), "it happened")
	}
}

// requireMatches fails unless the fixture gives every step of a broad_first
// search for "a" something to read: exact names, and prefix matches of each
// kind.
func (m *mixedPlaces) requireMatches(kinds []string) {
	m.t.Helper()
	ctx := context.Background()
	var exact int
	err := m.pool.QueryRow(ctx, `SELECT count(*) FROM places WHERE lower(name) = 'a' AND hidden_at IS NULL`).Scan(&exact)
	if err != nil || exact < 3 {
		m.t.Fatalf("places named a: %d, %v; want at least 3", exact, err)
	}
	for _, kind := range kinds {
		var n int
		err := m.pool.QueryRow(ctx, `SELECT count(*) FROM places
			WHERE kind = $1 AND lower(name) LIKE 'a_%' AND hidden_at IS NULL`, kind).Scan(&n)
		if err != nil || n < 2 {
			m.t.Fatalf("%s matching a: %d, %v; want at least 2", kind, n, err)
		}
	}
}

var (
	searchKinds  = []string{"PLACE_KIND_STATE", "PLACE_KIND_CITY", "PLACE_KIND_TOWN", "PLACE_KIND_MALL", "PLACE_KIND_STORE"}
	searchLimits = []int32{1, 2, 3, 5, 8, 13, 21, 40, 100}
)

// TestSearchPlacesBroadFirst checks that reading a short prefix a kind at a
// time (broad_first) returns what reading every prefix match at once
// returns, row for row: for one- and two-letter searches, everywhere and
// under one parent, with every kind filter and none, at candidate limits
// that end inside each of its steps. Without broad_first SearchPlaces runs
// the prefix and substring steps it has always run.
func TestSearchPlacesBroadFirst(t *testing.T) {
	t.Parallel()
	m := newMixedPlaces(t)
	m.requireMatches(searchKinds)
	q := dbq.New(m.pool)

	scopes := []struct {
		name    string
		parents []uuid.UUID
	}{
		{name: "everywhere"},
		{name: "under a state", parents: []uuid.UUID{m.states[1]}},
		{name: "under a city", parents: []uuid.UUID{m.cities[5]}},
		{name: "under a hidden city", parents: []uuid.UUID{m.hiddenCity}},
		{name: "under no parent", parents: []uuid.UUID{}},
	}
	type searchCase struct {
		name    string
		text    string
		parents []uuid.UUID
	}
	var tests []searchCase
	for _, text := range []string{"a", "b", "aa", "ab", "ba", "bb", "c"} {
		for _, s := range scopes {
			tests = append(tests, searchCase{name: fmt.Sprintf("%q %s", text, s.name), text: text, parents: s.parents})
		}
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, kind := range append([]string{""}, searchKinds...) {
				for _, lim := range searchLimits {
					compareBroadFirst(t, q, dbq.SearchPlacesParams{
						Exact: tc.text, Prefix: tc.text + "%", Contains: tc.text + "%",
						ParentIds: tc.parents, ResultLimit: lim, CandidateLimit: lim,
						Kind: pgtype.Text{String: kind, Valid: kind != ""},
					})
				}
			}
		})
	}
}

// compareBroadFirst runs one search without and with broad_first and fails
// when the rows differ.
func compareBroadFirst(t *testing.T, q *dbq.Queries, p dbq.SearchPlacesParams) {
	t.Helper()
	ctx := context.Background()
	p.BroadFirst = false
	all, err := q.SearchPlaces(ctx, p)
	if err != nil {
		t.Fatalf("all at once: %v", err)
	}
	p.BroadFirst = true
	broad, err := q.SearchPlaces(ctx, p)
	if err != nil {
		t.Fatalf("broad first: %v", err)
	}
	if !slices.Equal(all, broad) {
		t.Errorf("kind %q limit %d: broad first returned\n%v\nwant\n%v",
			p.Kind.String, p.CandidateLimit, rowNames(broad), rowNames(all))
	}
}

func rowNames(rows []dbq.SearchPlacesRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("%s(%s,%d)", r.Name, strings.TrimPrefix(r.Kind, "PLACE_KIND_"), r.Stories)
	}
	return out
}
