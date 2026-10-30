package lore

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// id returns a fixed perspective id that sorts by n.
func id(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", n))
}

func TestConnectedGroups(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		edges [][2]uuid.UUID
		want  map[uuid.UUID][]uuid.UUID
	}{
		{name: "no claims, no groups", want: map[uuid.UUID][]uuid.UUID{}},
		{name: "one claim joins two", edges: [][2]uuid.UUID{{id(2), id(1)}},
			want: map[uuid.UUID][]uuid.UUID{id(1): {id(1), id(2)}}},
		{name: "a chain is one group keyed by its lowest id", edges: [][2]uuid.UUID{{id(5), id(4)}, {id(4), id(9)}, {id(9), id(3)}},
			want: map[uuid.UUID][]uuid.UUID{id(3): {id(3), id(4), id(5), id(9)}}},
		{name: "a cycle ends", edges: [][2]uuid.UUID{{id(1), id(2)}, {id(2), id(3)}, {id(3), id(1)}},
			want: map[uuid.UUID][]uuid.UUID{id(1): {id(1), id(2), id(3)}}},
		{name: "separate groups stay apart", edges: [][2]uuid.UUID{{id(1), id(2)}, {id(7), id(8)}},
			want: map[uuid.UUID][]uuid.UUID{id(1): {id(1), id(2)}, id(7): {id(7), id(8)}}},
		{name: "two groups merged by a later claim", edges: [][2]uuid.UUID{{id(6), id(7)}, {id(2), id(3)}, {id(7), id(3)}},
			want: map[uuid.UUID][]uuid.UUID{id(2): {id(2), id(3), id(6), id(7)}}},
		{name: "a repeated claim changes nothing", edges: [][2]uuid.UUID{{id(1), id(2)}, {id(2), id(1)}, {id(1), id(2)}},
			want: map[uuid.UUID][]uuid.UUID{id(1): {id(1), id(2)}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := connectedGroups(tc.edges)
			if len(got) != len(tc.want) {
				t.Fatalf("groups = %v, want %v", got, tc.want)
			}
			for key, members := range tc.want {
				if !slices.Equal(got[key], members) {
					t.Fatalf("group %v = %v, want %v (all: %v)", key, got[key], members, got)
				}
			}
		})
	}
}

// A large group is found in linear time; the SQL walk it replaced grew with
// the square of the group's size.
func TestConnectedGroupsLarge(t *testing.T) {
	t.Parallel()
	const n = 5000
	edges := make([][2]uuid.UUID, 0, n)
	for i := n; i > 1; i-- {
		edges = append(edges, [2]uuid.UUID{id(i), id(i - 1)})
	}
	got := connectedGroups(edges)
	if len(got) != 1 || len(got[id(1)]) != n {
		t.Fatalf("got %d groups, first of %d members; want one of %d", len(got), len(got[id(1)]), n)
	}
}
