package place

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
)

func TestLocalitySplits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		want       []split
	}{
		{name: "one word has no split", text: "starbucks", want: nil},
		{
			name: "last word, then first word",
			text: "starbucks tampa",
			want: []split{{name: "starbucks", locality: "tampa"}, {name: "tampa", locality: "starbucks"}},
		},
		{
			name: "longer localities first, at most three words",
			text: "a b c d e",
			want: []split{
				{name: "a b", locality: "c d e"}, {name: "d e", locality: "a b c"},
				{name: "a b c", locality: "d e"}, {name: "c d e", locality: "a b"},
				{name: "a b c d", locality: "e"}, {name: "b c d e", locality: "a"},
			},
		},
		{
			name: "a last name word in is dropped",
			text: "starbucks in tampa",
			want: []split{
				{name: "starbucks", locality: "in tampa"}, {name: "tampa", locality: "starbucks in"},
				{name: "starbucks", locality: "tampa"}, {name: "in tampa", locality: "starbucks"},
			},
		},
		{
			name: "comma: name before, locality after, then the other way",
			text: "starbucks, tampa",
			want: []split{{name: "starbucks", locality: "tampa"}, {name: "tampa", locality: "starbucks"}},
		},
		{
			name: "comma: a state after the town is ignored",
			text: "starbucks ,tampa, fl",
			want: []split{{name: "starbucks", locality: "tampa"}, {name: "tampa", locality: "starbucks"}},
		},
		{name: "comma with nothing before it", text: ", tampa", want: nil},
		{
			name: "case is kept, and a last In is dropped whatever its case",
			text: "Joes In Tampa",
			want: []split{
				{name: "Joes", locality: "In Tampa"}, {name: "Tampa", locality: "Joes In"},
				{name: "Joes", locality: "Tampa"}, {name: "In Tampa", locality: "Joes"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := localitySplits(tc.text); !slices.Equal(got, tc.want) {
				t.Fatalf("localitySplits(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestSearchDisplay(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{in: "  Starbucks   TAMPA ", want: "Starbucks TAMPA"},
		{in: "starbucks,", want: "starbucks"},
		{in: ", starbucks , ", want: "starbucks"},
		{in: "starbucks, tampa", want: "starbucks, tampa"},
		{in: " , ", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := searchDisplay(tc.in); got != tc.want {
				t.Fatalf("searchDisplay(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLocalityLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, locality string
		ancestors      []Ref
		want           string
	}{
		{name: "a state stands alone", locality: "Florida", want: "Florida"},
		{name: "a city and its state", locality: "Tampa", ancestors: []Ref{{Name: "Florida"}}, want: "Tampa, Florida"},
		{
			name:      "a town, nearest first",
			locality:  "Southtown",
			ancestors: []Ref{{Name: "Illinois"}, {Name: "Springfield"}},
			want:      "Southtown, Springfield, Illinois",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := localityLabel(tc.locality, tc.ancestors); got != tc.want {
				t.Fatalf("localityLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLocalityNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want []string
	}{
		{in: "tampa", want: []string{"tampa"}},
		{in: "st petersburg", want: []string{"st petersburg", "st. petersburg", "saint petersburg"}},
		{in: "saint petersburg", want: []string{"saint petersburg", "st petersburg", "st. petersburg"}},
		{in: "ft myers", want: []string{"ft myers", "ft. myers", "fort myers"}},
		{in: "port st lucie", want: []string{"port st lucie", "port st. lucie", "port saint lucie"}},
		{in: "mt dora", want: []string{"mt dora", "mt. dora", "mount dora"}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := localityNames(tc.in); !slices.Equal(got, tc.want) {
				t.Fatalf("localityNames(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLocalityMatches(t *testing.T) {
	t.Parallel()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	found := []Locality{{ID: a, Name: "saint petersburg"}, {ID: b, Name: "tampa"}, {ID: c, Name: "st. petersburg"}, {ID: a, Name: "saint petersburg"}}
	many := make([]Locality, maxLocalitiesPerName+5)
	for i := range many {
		many[i] = Locality{ID: uuid.New(), Name: "valley"}
	}
	ids := func(ls []Locality) []uuid.UUID {
		out := make([]uuid.UUID, len(ls))
		for i, l := range ls {
			out[i] = l.ID
		}
		return out
	}
	tests := []struct {
		name, locality string
		found          []Locality
		want           []uuid.UUID
	}{
		{name: "every spelling, in found order, each once", locality: "st petersburg", found: found, want: []uuid.UUID{a, c}},
		{name: "none named", locality: "orlando", found: found, want: nil},
		{name: "at most maxLocalitiesPerName", locality: "valley", found: many, want: ids(many[:maxLocalitiesPerName])},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ids(localityMatches(tc.found, tc.locality)); !slices.Equal(got, tc.want) {
				t.Fatalf("ids = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeSearch(t *testing.T) {
	t.Parallel()
	p := func(name string, prefix bool) Place {
		return Place{ID: uuid.NewSHA1(uuid.Nil, []byte(name)), Name: name, namePrefix: prefix}
	}
	many := func(n int) []Place {
		out := make([]Place, n)
		for i := range out {
			out[i] = p(fmt.Sprintf("kiosk %02d", i), true)
		}
		return out
	}
	names := func(ps []Place) []string {
		out := make([]string, len(ps))
		for i, x := range ps {
			out[i] = x.Name
		}
		return out
	}
	tests := []struct {
		name     string
		plain    []Place
		scoped   [][]Place
		want     []string
		wantMore bool
	}{
		{
			name:   "name starts, then each locality, then substrings, each once",
			plain:  []Place{p("miami beach", true), p("old miami beach", false)},
			scoped: [][]Place{{p("beach cafe", true), p("miami beach", true)}, {p("miami pier", true)}},
			want:   []string{"miami beach", "beach cafe", "miami pier", "old miami beach"},
		},
		{name: "nothing found", want: []string{}},
		{name: "exactly the cap is not more", plain: many(SearchLimit), want: names(many(SearchLimit))},
		{name: "one over the cap is more", plain: many(SearchLimit + 1), want: names(many(SearchLimit)), wantMore: true},
		{
			name:     "a locality list over the cap is more",
			scoped:   [][]Place{many(SearchLimit + 1)},
			want:     names(many(SearchLimit)),
			wantMore: true,
		},
		{
			name:     "lists that fit alone but not together are more",
			plain:    many(15),
			scoped:   [][]Place{{p("a", true), p("b", true), p("c", true), p("d", true), p("e", true), p("f", true)}},
			want:     append(names(many(15)), "a", "b", "c", "d", "e"),
			wantMore: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, more := mergeSearch(tc.plain, tc.scoped)
			if !slices.Equal(names(got), tc.want) || more != tc.wantMore {
				t.Fatalf("merge = %q, more %v; want %q, more %v", names(got), more, tc.want, tc.wantMore)
			}
		})
	}
}

// A query over maxSearchQuery characters is refused before the store is
// read: the Service here has none.
func TestSearchRefusesLongQueries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, query string
	}{
		{name: "two hundred and one letters", query: strings.Repeat("a", maxSearchQuery+1)},
		{name: "two hundred and one multibyte characters", query: strings.Repeat("é", maxSearchQuery+1)},
		{name: "a megabyte of words", query: strings.Repeat("starbucks tampa ", 1<<16)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := (&Service{}).Search(context.Background(), tc.query, uuid.Nil, perspectivv1.PlaceKind_PLACE_KIND_UNSPECIFIED)
			if code, ok := apiutil.CodeOf(err); !ok || code != connect.CodeInvalidArgument {
				t.Fatalf("Search error = %v, want InvalidArgument", err)
			}
		})
	}
}
