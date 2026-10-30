package importer

import (
	"strings"
	"testing"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

func TestLocalityKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, a, b string
		same       bool
	}{
		{"saint abbreviated with a stop", "St. Petersburg", "Saint Petersburg", true},
		{"saint abbreviated without a stop", "st petersburg", "Saint Petersburg", true},
		{"saint inside the name", "Port St. Lucie", "Port Saint Lucie", true},
		{"sainte", "Ste. Genevieve", "Sainte Genevieve", true},
		{"fort", "Ft. Myers", "Fort Myers", true},
		{"mount", "Mt. Pleasant", "Mount Pleasant", true},
		{"case and whitespace", "  el   PASO ", "El Paso", true},
		{"hyphen", "Winston-Salem", "Winston Salem", true},
		{"apostrophe", "Coeur d’Alene", "Coeur d'Alene", true},
		{"different cities", "St. Petersburg", "Petersburg", false},
		{"st only as a whole word", "Stuart", "Saint Uart", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := localityKey(tc.a) == localityKey(tc.b); got != tc.same {
				t.Errorf("localityKey(%q) = %q, localityKey(%q) = %q; same = %v, want %v",
					tc.a, localityKey(tc.a), tc.b, localityKey(tc.b), got, tc.same)
			}
		})
	}
}

// FR-050: an address that writes "St. Petersburg" files under the city
// Overture names "Saint Petersburg", and what stays unfiled is counted by
// locality, most first.
func TestPrepareFilesAbbreviatedLocality(t *testing.T) {
	t.Parallel()
	divisions := `{"id":"div-us","subtype":"country","country":"US","names":{"primary":"United States"}}
{"id":"div-fl","subtype":"region","country":"US","region":"US-FL","names":{"primary":"Florida"},"parent_division_id":"div-us"}
{"id":"div-stpete","subtype":"locality","class":"city","country":"US","region":"US-FL","names":{"primary":"Saint Petersburg"},"parent_division_id":"div-fl"}
{"id":"div-ftmyers","subtype":"locality","class":"city","country":"US","region":"US-FL","names":{"primary":"Fort Myers"},"parent_division_id":"div-fl"}
`
	places := `{"fsq_place_id":"a","name":"Bagel Shop","locality":"St. Petersburg","region":"FL","country":"US"}
{"fsq_place_id":"b","name":"Pier Cafe","locality":"st petersburg","region":"FL","country":"US"}
{"fsq_place_id":"c","name":"Edison Diner","locality":"Ft. Myers","region":"FL","country":"US"}
{"fsq_place_id":"d","name":"Gulf Shack","locality":"Gulfport","region":"FL","country":"US"}
{"fsq_place_id":"e","name":"Bay Shack","locality":"gulfport","region":"FL","country":"US"}
{"fsq_place_id":"f","name":"Key Shack","locality":"Tierra Verde","region":"FL","country":"US"}
`
	plan, err := Prepare(strings.NewReader(divisions), strings.NewReader(places))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	parents := map[string]string{}
	for _, n := range plan.Nodes() {
		parents[n.Name] = n.ParentKey
	}
	for name, want := range map[string]string{
		"Bagel Shop": "overture:div-stpete", "Pier Cafe": "overture:div-stpete", "Edison Diner": "overture:div-ftmyers",
	} {
		if parents[name] != want {
			t.Errorf("%s filed under %q, want %q", name, parents[name], want)
		}
	}
	if plan.Unfiled != 3 {
		t.Errorf("Unfiled = %d, want 3", plan.Unfiled)
	}
	top := plan.UnfiledLocalities(1)
	if len(top) != 1 || top[0] != (LocalityCount{Locality: "Gulfport, FL", Count: 2}) {
		t.Errorf("UnfiledLocalities(1) = %+v, want [{Gulfport, FL 2}]", top)
	}
}

func TestCleanPlaceName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, locality, region, want string
	}{
		{"own locality", "Joe's Pizza, St. Petersburg", "St Petersburg", "FL", "Joe's Pizza"},
		{"own locality spelled out", "Joe's Pizza, Saint Petersburg", "St. Petersburg", "FL", "Joe's Pizza"},
		{"own state code", "Gator Bait, FL", "Tampa", "US-FL", "Gator Bait"},
		{"own state name", "Gator Bait, Florida", "Tampa", "FL", "Gator Bait"},
		{"locality, state and country", "Gator Bait, Tampa, FL, USA", "Tampa", "FL", "Gator Bait"},
		{"united states", "Gator Bait, United States", "Tampa", "FL", "Gator Bait"},
		{"another state stays", "Texas Roadhouse, TX", "Tampa", "FL", "Texas Roadhouse, TX"},
		{"another place stays", "Cozumel, Mexico", "Tampa", "FL", "Cozumel, Mexico"},
		{"suffix only is kept", ", Tampa", "Tampa", "FL", ", Tampa"},
		{"trailing comma", "Sunset Grill,", "Tampa", "FL", "Sunset Grill"},
		{"trailing dash", "Sunset Grill -", "Tampa", "FL", "Sunset Grill"},
		{"trailing full stop", "Sunset Grill.", "Tampa", "FL", "Sunset Grill"},
		{"abbreviation keeps its stop", "Smith & Co.", "Tampa", "FL", "Smith & Co."},
		{"inc keeps its stop", "Acme Inc.", "Tampa", "FL", "Acme Inc."},
		{"initials keep their stops", "Made in U.S.A.", "Tampa", "FL", "Made in U.S.A."},
		{"exclamation stays", "Yo! Sushi!", "Tampa", "FL", "Yo! Sushi!"},
		{"all lower case is title-cased", "the hall of fame and museum", "Tampa", "FL", "The Hall of Fame and Museum"},
		{"minor word last is capitalised", "the place to be in", "Tampa", "FL", "The Place to Be In"},
		{"apostrophe and hyphen", "joe's bar-b-que", "Tampa", "FL", "Joe's Bar-B-Que"},
		{"mixed case is left alone", "iHop", "Tampa", "FL", "iHop"},
		{"whitespace collapsed", "  Desert   Books ", "El Paso", "TX", "Desert Books"},
		{"no letters is kept", ".", "Tampa", "FL", "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cleanPlaceName(tc.in, tc.locality, tc.region); got != tc.want {
				t.Errorf("cleanPlaceName(%q, %q, %q) = %q, want %q", tc.in, tc.locality, tc.region, got, tc.want)
			}
		})
	}
}

// A sports venue files under sports and recreation whatever its group, and
// imports as a MALL so its stands and gates can go inside it.
func TestSportsVenueOverride(t *testing.T) {
	t.Parallel()
	const (
		sports   = perspectivv1.PlaceCategory_PLACE_CATEGORY_SPORTS_AND_RECREATION
		mallKind = perspectivv1.PlaceKind_PLACE_KIND_MALL
		store    = perspectivv1.PlaceKind_PLACE_KIND_STORE
	)
	addr := `"addresses":[{"locality":"Tampa","region":"FL","country":"US"}]`
	cases := []struct {
		name     string
		line     string
		kind     perspectivv1.PlaceKind
		category perspectivv1.PlaceCategory
	}{
		{"overture stadium_arena", `{"id":"o1","names":{"primary":"Raymond James Stadium"},"basic_category":"stadium_arena",` + addr + `}`, mallKind, sports},
		{"overture arena under arts", `{"id":"o2","names":{"primary":"Amalie Arena"},"basic_category":"arena","taxonomy":{"primary":"arena","hierarchy":["arts_and_entertainment","arena"]},` + addr + `}`, mallKind, sports},
		{"overture ballpark", `{"id":"o3","names":{"primary":"Steinbrenner Field"},"categories":{"primary":"ballpark"},` + addr + `}`, mallKind, sports},
		{"overture race track in taxonomy", `{"id":"o4","names":{"primary":"Speedway"},"basic_category":"sports_venue","taxonomy":{"primary":"race_track","hierarchy":["sports_and_recreation","race_track"]},` + addr + `}`, mallKind, sports},
		{"overture sports complex", `{"id":"o5","names":{"primary":"Fields"},"basic_category":"sports_complex",` + addr + `}`, mallKind, sports},
		{"overture gym stays a store", `{"id":"o6","names":{"primary":"Gym"},"basic_category":"gym","taxonomy":{"primary":"gym","hierarchy":["sports_and_recreation","gym"]},` + addr + `}`, store, sports},
		{"overture sports bar stays nightlife", `{"id":"o7","names":{"primary":"Bar"},"basic_category":"sports_bar","taxonomy":{"primary":"sports_bar","hierarchy":["food_and_drink","bar","sports_bar"]},` + addr + `}`, store, perspectivv1.PlaceCategory_PLACE_CATEGORY_NIGHTLIFE},
		{"foursquare baseball stadium", `{"fsq_place_id":"f1","name":"Tropicana Field","locality":"Tampa","region":"FL","country":"US","fsq_category_labels":["Arts and Entertainment > Stadium > Baseball Stadium"]}`, mallKind, sports},
		{"foursquare racetrack", `{"fsq_place_id":"f2","name":"Track","locality":"Tampa","region":"FL","country":"US","fsq_category_labels":["Sports and Recreation > Racetrack"]}`, mallKind, sports},
		{"foursquare bookstore stays a store", `{"fsq_place_id":"f3","name":"Books","locality":"Tampa","region":"FL","country":"US","fsq_category_labels":["Retail > Bookstore"]}`, store, perspectivv1.PlaceCategory_PLACE_CATEGORY_SHOPPING},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got parsedPlace
			err := readRecords("places", strings.NewReader(tc.line), func(rec record) error {
				var err error
				got, err = parsePlace(rec)
				return err
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got.kind != tc.kind || got.category != tc.category {
				t.Errorf("kind, category = %v, %v; want %v, %v", got.kind, got.category, tc.kind, tc.category)
			}
		})
	}
}
