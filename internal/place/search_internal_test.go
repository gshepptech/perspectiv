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
			name: "a last name word in is dropped, and in before a town names no state",
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
			name: "comma: a state name after the town is ignored",
			text: "starbucks ,tampa, florida",
			want: []split{{name: "starbucks", locality: "tampa"}, {name: "tampa", locality: "starbucks"}},
		},
		{
			name: "comma: a state code after the town narrows the town, then names the state",
			text: "starbucks ,tampa, fl",
			want: []split{
				{name: "starbucks", locality: "tampa", state: "florida"}, {name: "tampa", locality: "starbucks", state: "florida"},
				{name: "starbucks tampa", state: "florida"},
			},
		},
		{name: "a last code is the state", text: "starbucks fl", want: []split{{name: "starbucks", state: "florida"}}},
		{name: "a first code is the state", text: "FL Starbucks", want: []split{{name: "Starbucks", state: "florida"}}},
		{name: "a town and a code", text: "tampa fl", want: []split{{name: "tampa", state: "florida"}}},
		{name: "a town, comma, a code", text: "Tampa, Fl.", want: []split{{name: "Tampa", state: "florida"}}},
		{
			name: "name, town and code: the town within the state first",
			text: "publix tampa fl",
			want: []split{
				{name: "publix", locality: "tampa", state: "florida"}, {name: "tampa", locality: "publix", state: "florida"},
				{name: "publix tampa", state: "florida"},
			},
		},
		{
			name: "name in town and code",
			text: "pizza in tampa fl",
			want: []split{
				{name: "pizza", locality: "in tampa", state: "florida"}, {name: "tampa", locality: "pizza in", state: "florida"},
				{name: "pizza", locality: "tampa", state: "florida"}, {name: "in tampa", locality: "pizza", state: "florida"},
				{name: "pizza in tampa", state: "florida"},
			},
		},
		{name: "a last word code after a name is the state", text: "pizza ok", want: []split{{name: "pizza", state: "oklahoma"}}},
		{
			name: "a last in after a name is Indiana, and not part of the name",
			text: "Starbucks In",
			want: []split{{name: "Starbucks", state: "indiana"}},
		},
		{
			name: "a first word code is a word",
			text: "ok pizza",
			want: []split{{name: "ok", locality: "pizza"}, {name: "pizza", locality: "ok"}},
		},
		{
			name: "mt first is Mount, not Montana",
			text: "mt dora",
			want: []split{{name: "mt", locality: "dora"}, {name: "dora", locality: "mt"}},
		},
		{name: "a code alone has no split", text: "ok", want: nil},
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

func TestStateOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text, wantState, wantRest string
	}{
		{name: "last, lower case", text: "starbucks fl", wantState: "florida", wantRest: "starbucks"},
		{name: "last, upper case", text: "starbucks FL", wantState: "florida", wantRest: "starbucks"},
		{name: "last, with a full stop", text: "Starbucks Fl.", wantState: "florida", wantRest: "Starbucks"},
		{name: "after a comma", text: "tampa, fl", wantState: "florida", wantRest: "tampa"},
		{name: "after a comma and no space", text: "tampa,FL", wantState: "florida", wantRest: "tampa"},
		{name: "first", text: "fl starbucks", wantState: "florida", wantRest: "starbucks"},
		{name: "first, before a comma", text: "FL., starbucks", wantState: "florida", wantRest: "starbucks"},
		{name: "last wins over first", text: "fl starbucks ga", wantState: "georgia", wantRest: "fl starbucks"},
		{name: "the district", text: "museum dc", wantState: "district of columbia", wantRest: "museum"},
		{name: "a territory", text: "beach pr", wantState: "puerto rico", wantRest: "beach"},
		{name: "a code alone", text: "fl"},
		{name: "a code with a full stop alone", text: "Fl."},
		{name: "a state's name is no code", text: "starbucks florida"},
		{name: "three letters are no code", text: "starbucks fla"},
		{name: "a code inside a word", text: "starbucks flats"},
		// The codes that are also words: a state only when last after another word.
		{name: "word code last after a word", text: "pizza ok", wantState: "oklahoma", wantRest: "pizza"},
		{name: "word code last after a comma", text: "pizza, OK", wantState: "oklahoma", wantRest: "pizza"},
		{name: "word code first is a word", text: "ok pizza"},
		{name: "word code alone", text: "ok"},
		{name: "in last is Indiana", text: "starbucks in", wantState: "indiana", wantRest: "starbucks"},
		{name: "in before a town is a word", text: "starbucks in tampa"},
		{name: "in first is a word", text: "in tampa"},
		{name: "me last is Maine", text: "lobster shack me", wantState: "maine", wantRest: "lobster shack"},
		{name: "or first is a word", text: "or something"},
		{name: "hi first is a word", text: "hi there"},
		{name: "oh first is a word", text: "oh my"},
		{name: "la first is a word", text: "la quinta"},
		{name: "co last is Colorado", text: "smith & co", wantState: "colorado", wantRest: "smith &"},
		{name: "mt first is Mount", text: "mt dora"},
		{name: "a word code after another word code", text: "in ok", wantState: "oklahoma", wantRest: "in"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state, rest, ok := stateOf(tc.text)
			if state != tc.wantState || rest != tc.wantRest || ok != (tc.wantState != "") {
				t.Fatalf("stateOf(%q) = %q, %q, %v; want %q, %q", tc.text, state, rest, ok, tc.wantState, tc.wantRest)
			}
		})
	}
}

// Every code that is also a word reads as a state last after a name and
// as a word anywhere else.
func TestWordCodesOnlyLast(t *testing.T) {
	t.Parallel()
	for _, code := range wordCodes {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			if _, ok := stateCodes[code]; !ok {
				t.Fatalf("word code %q is not in stateCodes", code)
			}
			if _, _, ok := stateOf("pizza " + code); !ok {
				t.Errorf("%q last is no state", code)
			}
			for _, text := range []string{code, code + " pizza", code + ", pizza", "pizza " + code + " tampa"} {
				if state, _, ok := stateOf(text); ok {
					t.Errorf("stateOf(%q) = %q, want no state", text, state)
				}
			}
		})
	}
}

// stateCodes reads the importer's state list: every code is two lowercase
// letters, and a few spot checks hold, including DC and a territory.
func TestStateCodes(t *testing.T) {
	t.Parallel()
	if len(stateCodes) < 51 {
		t.Fatalf("stateCodes has %d entries, want the 50 states and DC at least", len(stateCodes))
	}
	for code := range stateCodes {
		if len(code) != 2 || code != strings.ToLower(code) {
			t.Errorf("code %q is not two lowercase letters", code)
		}
	}
	for code, want := range map[string]string{"fl": "florida", "dc": "district of columbia", "il": "illinois", "pr": "puerto rico"} {
		if got := stateCodes[code]; got != want {
			t.Errorf("stateCodes[%q] = %q, want %q", code, got, want)
		}
	}
}

func TestSplitLocalities(t *testing.T) {
	t.Parallel()
	florida, washingtonCity, washingtonState, tampaFL := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	found := []Locality{
		{ID: washingtonCity, Name: "washington", ParentID: uuid.New()},
		{ID: washingtonState, Name: "washington"},
		{ID: florida, Name: "florida"},
	}
	inState := []Locality{{ID: tampaFL, Name: "tampa", ParentID: florida}}
	tests := []struct {
		name string
		sp   split
		want []uuid.UUID
	}{
		{name: "the state itself", sp: split{name: "starbucks", state: "florida"}, want: []uuid.UUID{florida}},
		{name: "the state, not a city of its name", sp: split{name: "starbucks", state: "washington"}, want: []uuid.UUID{washingtonState}},
		{name: "a state not found", sp: split{name: "starbucks", state: "texas"}},
		{name: "a town within the state", sp: split{name: "publix", locality: "tampa", state: "florida"}, want: []uuid.UUID{tampaFL}},
		{name: "a town not within the state", sp: split{name: "publix", locality: "washington", state: "florida"}},
		{name: "a town by name alone", sp: split{name: "starbucks", locality: "washington"}, want: []uuid.UUID{washingtonCity, washingtonState}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []uuid.UUID
			for _, l := range splitLocalities(tc.sp, found, inState) {
				got = append(got, l.ID)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ids = %v, want %v", got, tc.want)
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
