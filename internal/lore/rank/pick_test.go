package rank

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

func TestScore(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		members, upvotes int32
		want             float64
	}{
		{name: "no upvotes scores zero (FR-056)", members: 4, upvotes: 0, want: 0},
		{name: "no members scores zero", members: 0, upvotes: 9, want: 0},
		{name: "log(1+members) times log(1+upvotes) (FR-056, AC-028)", members: 4, upvotes: 3, want: math.Log(5) * math.Log(4)},
		{name: "larger inputs", members: 9, upvotes: 99, want: math.Log(10) * math.Log(100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := score(tc.members, tc.upvotes); math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("score(%d, %d) = %v, want %v", tc.members, tc.upvotes, got, tc.want)
			}
		})
	}
}

func TestPickCanon(t *testing.T) {
	t.Parallel()
	// ids in ascending order, so a, b, c sort a < b < c.
	a := uuid.MustParse("00000000-0000-7000-8000-00000000000a")
	b := uuid.MustParse("00000000-0000-7000-8000-00000000000b")
	c := uuid.MustParse("00000000-0000-7000-8000-00000000000c")
	lore := func(id uuid.UUID, members, upvotes int32, canon bool) standing {
		return standing{id: id, members: members, upvotes: upvotes, canon: canon, score: score(members, upvotes)}
	}
	cases := []struct {
		name  string
		lores []standing
		want  uuid.UUID // uuid.Nil: no canon
	}{
		{name: "no lores, no canon", want: uuid.Nil},
		{
			name:  "highest score among 4-member lores (OT-027)",
			lores: []standing{lore(a, 4, 2, false), lore(b, 4, 5, false)},
			want:  b,
		},
		{
			name:  "3-member lore is ranked but never eligible even when it outscores (OT-028, FR-063)",
			lores: []standing{lore(a, 3, 500, false), lore(b, 4, 1, false)},
			want:  b,
		},
		{
			name:  "only small lores leaves no canon (FR-061)",
			lores: []standing{lore(a, 3, 50, false), lore(b, 2, 9, false)},
			want:  uuid.Nil,
		},
		{
			name:  "canon below the minimum loses canon when nobody qualifies (ST-013)",
			lores: []standing{lore(a, 3, 50, true)},
			want:  uuid.Nil,
		},
		{
			name:  "challenger strictly outscoring the canon takes it (ST-008)",
			lores: []standing{lore(a, 4, 2, true), lore(b, 4, 3, false)},
			want:  b,
		},
		{
			name:  "eligible lore with no upvotes still becomes canon",
			lores: []standing{lore(a, 4, 0, false), lore(b, 3, 0, false)},
			want:  a,
		},
		{
			name:  "tie: the incumbent canon keeps it",
			lores: []standing{lore(a, 6, 4, false), lore(b, 4, 6, true)},
			want:  b,
		},
		{
			name:  "tie without the incumbent: more members wins",
			lores: []standing{lore(a, 4, 6, false), lore(b, 6, 4, false), lore(c, 4, 1, true)},
			want:  b,
		},
		{
			name:  "tie on score and members: lowest id wins",
			lores: []standing{lore(c, 5, 5, false), lore(b, 5, 5, false), lore(a, 4, 1, false)},
			want:  b,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := uuid.Nil
			if i, ok := pickCanon(tc.lores, 4); ok {
				got = tc.lores[i].id
			}
			if got != tc.want {
				t.Fatalf("pickCanon = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestPickCanonHonoursConfiguredMinimum(t *testing.T) {
	t.Parallel()
	a, b := uuid.New(), uuid.New()
	lores := []standing{
		{id: a, members: 2, upvotes: 9, score: score(2, 9)},
		{id: b, members: 3, upvotes: 1, score: score(3, 1)},
	}
	i, ok := pickCanon(lores, 2)
	if !ok || lores[i].id != a {
		t.Fatalf("minimum 2: want %s as canon, got ok=%v", a, ok)
	}
	if _, ok := pickCanon(lores, 4); ok {
		t.Fatalf("minimum 4: want no canon")
	}
}
