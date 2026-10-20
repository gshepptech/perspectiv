package importer_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place/importer"
)

const (
	state = perspectivv1.PlaceKind_PLACE_KIND_STATE
	city  = perspectivv1.PlaceKind_PLACE_KIND_CITY
	town  = perspectivv1.PlaceKind_PLACE_KIND_TOWN
	mall  = perspectivv1.PlaceKind_PLACE_KIND_MALL
	store = perspectivv1.PlaceKind_PLACE_KIND_STORE
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func fixturePlan(t *testing.T) *importer.Plan {
	t.Helper()
	plan, err := importer.Prepare(openFixture(t, "us_divisions.ndjson"), openFixture(t, "us_places.ndjson"))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return plan
}

// FR-050, OT-010: every row of the US fixtures has a kind, a parent of the
// expected node and an IANA zone.
func TestPrepareFixtures(t *testing.T) {
	t.Parallel()
	plan := fixturePlan(t)
	byKey := map[string]importer.Node{}
	for _, n := range plan.Nodes() {
		byKey[n.Key] = n
	}

	cases := []struct {
		key, name, parent, zone string
		kind                    perspectivv1.PlaceKind
		source                  perspectivv1.PlaceSource
	}{
		{"overture:div-tx", "Texas", "", "America/Chicago", state, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-fl", "Florida", "", "America/New_York", state, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-az", "Arizona", "", "America/Phoenix", state, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-elpaso", "El Paso", "overture:div-tx", "America/Denver", city, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-houston", "Houston", "overture:div-tx", "America/Chicago", city, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-pensacola", "Pensacola", "overture:div-fl", "America/Chicago", city, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-miami", "Miami", "overture:div-fl", "America/New_York", city, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-phoenix", "Phoenix", "overture:div-az", "America/Phoenix", city, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-socorro", "Socorro", "overture:div-tx", "America/Denver", town, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-montecillo", "Montecillo", "overture:div-elpaso", "America/Denver", town, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"overture:div-windowrock", "Window Rock", "overture:div-az", "America/Denver", town, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"foursquare:fsq-cielo", "Cielo Vista Mall", "overture:div-elpaso", "America/Denver", mall, perspectivv1.PlaceSource_PLACE_SOURCE_FOURSQUARE},
		{"overture:ov-galleria", "The Galleria", "overture:div-houston", "America/Chicago", mall, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"foursquare:fsq-books", "Desert Books", "overture:div-elpaso", "America/Denver", store, perspectivv1.PlaceSource_PLACE_SOURCE_FOURSQUARE},
		{"overture:ov-coffee", "Palafox Coffee", "overture:div-pensacola", "America/Chicago", store, perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE},
		{"foursquare:fsq-navajo-store", "Window Rock Trading Post", "overture:div-windowrock", "America/Denver", store, perspectivv1.PlaceSource_PLACE_SOURCE_FOURSQUARE},
	}
	if got, want := len(plan.Nodes()), len(cases); got != want {
		t.Errorf("plan has %d nodes, want %d", got, want)
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			n, ok := byKey[tc.key]
			if !ok {
				t.Fatalf("no node %s", tc.key)
			}
			if n.Name != tc.name || n.Kind != tc.kind || n.ParentKey != tc.parent || n.Timezone != tc.zone || n.Source != tc.source {
				t.Errorf("node = {%q %v parent %q %q %v}, want {%q %v parent %q %q %v}",
					n.Name, n.Kind, n.ParentKey, n.Timezone, n.Source, tc.name, tc.kind, tc.parent, tc.zone, tc.source)
			}
		})
	}
}

func TestPrepareOrderMergeAndSkip(t *testing.T) {
	t.Parallel()
	plan := fixturePlan(t)

	t.Run("parents come before children", func(t *testing.T) {
		t.Parallel()
		seen := map[string]bool{}
		for _, n := range plan.Nodes() {
			if n.ParentKey != "" && !seen[n.ParentKey] {
				t.Errorf("%s precedes its parent %s", n.Key, n.ParentKey)
			}
			seen[n.Key] = true
		}
	})
	t.Run("same-named store under one parent is merged", func(t *testing.T) {
		t.Parallel()
		if got := plan.Merged()["foursquare:fsq-books-2"]; got != "foursquare:fsq-books" {
			t.Errorf("fsq-books-2 merged into %q, want foursquare:fsq-books", got)
		}
	})
	t.Run("non-US records are skipped", func(t *testing.T) {
		t.Parallel()
		// Ontario, Toronto and the Toronto mall.
		if plan.Skipped != 3 {
			t.Errorf("Skipped = %d, want 3", plan.Skipped)
		}
	})
}

func nodeByKey(t *testing.T, plan *importer.Plan, key string) importer.Node {
	t.Helper()
	for _, n := range plan.Nodes() {
		if n.Key == key {
			return n
		}
	}
	t.Fatalf("no node %s", key)
	return importer.Node{}
}

func TestPrepareCoordinates(t *testing.T) {
	t.Parallel()
	plan := fixturePlan(t)
	cases := []struct {
		key      string
		lng, lat float64
	}{
		{"overture:div-houston", -95.37, 29.76},  // GeoJSON Feature geometry
		{"overture:div-elpaso", -106.44, 31.76},  // flat lng and lat
		{"foursquare:fsq-cielo", -106.38, 31.78}, // Foursquare longitude and latitude
		{"overture:ov-coffee", -87.21, 30.41},    // Overture place Feature
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			n := nodeByKey(t, plan, tc.key)
			if n.Lng == nil || n.Lat == nil || *n.Lng != tc.lng || *n.Lat != tc.lat {
				t.Errorf("coordinates = %v, %v; want %v, %v", n.Lng, n.Lat, tc.lng, tc.lat)
			}
		})
	}
	if n := nodeByKey(t, plan, "overture:div-tx"); n.Lng == nil {
		t.Error("Texas coordinates from Feature geometry not read")
	}
}

const (
	usDiv    = `{"id":"div-us","subtype":"country","country":"US","names":{"primary":"United States"}}`
	txDiv    = `{"id":"div-tx","subtype":"region","country":"US","region":"US-TX","names":{"primary":"Texas"},"parent_division_id":"div-us"}`
	cityDiv  = `{"id":"div-austin","subtype":"locality","class":"city","country":"US","region":"US-TX","names":{"primary":"Austin"},"parent_division_id":"div-tx"}`
	goodDivs = usDiv + "\n" + txDiv + "\n" + cityDiv + "\n"
)

// CT-016: a file that cannot be parsed and a division parent that cannot be
// resolved are errors naming the file and line. A place whose address names
// no imported city or town is not an error; see TestPrepareUnfiledPlaces.
func TestPrepareErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		divisions  string
		places     string
		wantParse  bool
		wantFile   string
		wantLine   int
		wantSubstr string
	}{
		{
			name: "invalid JSON in divisions", divisions: usDiv + "\n{not json\n", wantParse: true,
			wantFile: "divisions", wantLine: 2, wantSubstr: "invalid JSON",
		},
		{
			name: "array line in divisions", divisions: "[1,2]\n", wantParse: true,
			wantFile: "divisions", wantLine: 1, wantSubstr: "not a JSON object",
		},
		{
			name: "polygon geometry", wantParse: true, wantFile: "divisions", wantLine: 1, wantSubstr: "Point",
			divisions: `{"type":"Feature","geometry":{"type":"Polygon","coordinates":[]},"properties":{"id":"x","subtype":"region","country":"US","region":"US-TX","names":{"primary":"Texas"},"parent_division_id":"div-us"}}`,
		},
		{
			name: "missing subtype", divisions: `{"id":"x","country":"US"}`, wantParse: true,
			wantFile: "divisions", wantLine: 1, wantSubstr: "subtype",
		},
		{
			name: "region that is not a state", wantParse: true, wantFile: "divisions", wantLine: 2, wantSubstr: "US-ZZ",
			divisions: usDiv + "\n" + `{"id":"zz","subtype":"region","country":"US","region":"US-ZZ","names":{"primary":"Nowhere"},"parent_division_id":"div-us"}`,
		},
		{
			name: "unknown locality class", wantParse: true, wantFile: "divisions", wantLine: 3, wantSubstr: "class",
			divisions: usDiv + "\n" + txDiv + "\n" + `{"id":"q","subtype":"locality","class":"spaceport","country":"US","names":{"primary":"Q"},"parent_division_id":"div-tx"}`,
		},
		{
			name: "duplicate id", divisions: usDiv + "\n" + usDiv, wantParse: true,
			wantFile: "divisions", wantLine: 2, wantSubstr: "duplicate",
		},
		{
			name: "out of range coordinates", wantParse: true, wantFile: "divisions", wantLine: 1, wantSubstr: "range",
			divisions: `{"id":"div-us","subtype":"country","country":"US","lng":200,"lat":0}`,
		},
		{
			name: "invalid JSON in places", divisions: goodDivs, places: `{"fsq_place_id":`, wantParse: true,
			wantFile: "places", wantLine: 1, wantSubstr: "invalid JSON",
		},
		{
			name: "place with no id", divisions: goodDivs, places: `{"names":{"primary":"X"},"addresses":[{"country":"US"}]}`,
			wantParse: true, wantFile: "places", wantLine: 1, wantSubstr: "id",
		},
		{
			name: "place with no name", divisions: goodDivs, places: `{"fsq_place_id":"a","country":"US","region":"TX","locality":"Austin"}`,
			wantParse: true, wantFile: "places", wantLine: 1, wantSubstr: "name",
		},
		{
			name: "city whose parent is not in the file", wantFile: "divisions", wantLine: 2, wantSubstr: "div-missing",
			divisions: usDiv + "\n" + `{"id":"c","subtype":"locality","class":"city","country":"US","names":{"primary":"Lost"},"parent_division_id":"div-missing"}`,
		},
		{
			name: "city with no state above it", wantFile: "divisions", wantLine: 2, wantSubstr: "no state",
			divisions: usDiv + "\n" + `{"id":"c","subtype":"locality","class":"city","country":"US","names":{"primary":"Orphan"},"parent_division_id":"div-us"}`,
		},
		{
			name: "cyclic chain", wantFile: "divisions", wantLine: 3, wantSubstr: "cyclic",
			divisions: `{"id":"a","subtype":"county","country":"US","names":{"primary":"A County"},"parent_division_id":"b"}` + "\n" +
				`{"id":"b","subtype":"county","country":"US","names":{"primary":"B County"},"parent_division_id":"a"}` + "\n" +
				`{"id":"c","subtype":"locality","country":"US","names":{"primary":"C"},"parent_division_id":"a"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := importer.Prepare(strings.NewReader(tc.divisions), strings.NewReader(tc.places))
			checkPrepareError(t, err, tc.wantParse, tc.wantFile, tc.wantLine, tc.wantSubstr)
		})
	}
}

// checkPrepareError asserts err is a ParseError (wantParse) or an
// UnresolvedParentError at file:line whose message mentions substr.
func checkPrepareError(t *testing.T, err error, wantParse bool, file string, line int, substr string) {
	t.Helper()
	if err == nil {
		t.Fatal("Prepare succeeded, want an error")
	}
	if !strings.Contains(err.Error(), substr) {
		t.Errorf("error %q does not mention %q", err, substr)
	}
	var pe *importer.ParseError
	var ue *importer.UnresolvedParentError
	gotFile, gotLine := "", 0
	switch {
	case wantParse && errors.As(err, &pe):
		gotFile, gotLine = pe.File, pe.Line
	case !wantParse && errors.As(err, &ue):
		gotFile, gotLine = ue.File, ue.Line
	default:
		t.Fatalf("error %T %v, want ParseError=%v", err, err, wantParse)
	}
	if gotFile != file || gotLine != line {
		t.Errorf("error at %s:%d, want %s:%d", gotFile, gotLine, file, line)
	}
}

// FR-050: the US places releases carry POIs whose address locality is
// empty or names a neighbourhood, borough or unincorporated area. The free-text
// address is not an authoritative parent, so such a POI is left out and
// counted, and the rest of the import goes ahead.
func TestPrepareUnfiledPlaces(t *testing.T) {
	t.Parallel()
	divisions, err := os.ReadFile("testdata/us_divisions.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	places, err := os.ReadFile("testdata/us_places.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	want := len(fixturePlan(t).Nodes())

	cases := []struct {
		name    string
		extra   string
		unfiled int
	}{
		{"no extra places", "", 0},
		{"null locality", `{"fsq_place_id":"fsq-x1","name":"Roadside Diner","country":"US","region":"TX","locality":null}`, 1},
		{"empty locality", `{"fsq_place_id":"fsq-x2","name":"Roadside Diner","country":"US","region":"TX","locality":""}`, 1},
		{"borough locality", `{"fsq_place_id":"fsq-x3","name":"Corner Bodega","country":"US","region":"TX","locality":"Northeast El Paso"}`, 1},
		{"overture place with no address", `{"id":"ov-x4","names":{"primary":"Trailhead Kiosk"},"basic_category":"park","addresses":[{"country":"US","region":"US-TX"}]}`, 1},
		{"region that is not a state", `{"fsq_place_id":"fsq-x5","name":"Shop","country":"US","region":"Atlantis","locality":"El Paso"}`, 1},
		{
			"several unfiled places",
			`{"fsq_place_id":"fsq-x1","name":"Roadside Diner","country":"US","region":"TX","locality":null}` + "\n" +
				`{"fsq_place_id":"fsq-x3","name":"Corner Bodega","country":"US","region":"TX","locality":"Northeast El Paso"}`,
			2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := importer.Prepare(strings.NewReader(string(divisions)),
				strings.NewReader(string(places)+tc.extra+"\n"))
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if got := len(plan.Nodes()); got != want {
				t.Errorf("plan has %d nodes, want the fixtures' %d", got, want)
			}
			if plan.Unfiled != tc.unfiled {
				t.Errorf("Unfiled = %d, want %d", plan.Unfiled, tc.unfiled)
			}
			if plan.Skipped != 3 {
				t.Errorf("Skipped = %d, want the fixtures' 3 non-US records", plan.Skipped)
			}
		})
	}
}

func TestPrepareEmptyFiles(t *testing.T) {
	t.Parallel()
	plan, err := importer.Prepare(strings.NewReader(""), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(plan.Nodes()) != 0 || plan.Skipped != 0 || plan.Unfiled != 0 {
		t.Errorf("empty files gave %d nodes, %d skipped, %d unfiled", len(plan.Nodes()), plan.Skipped, plan.Unfiled)
	}
}

// Offices, services and care places nobody would write about, unclassified
// or low-confidence listings and bare addresses are left out and counted;
// places people go are kept.
func TestPrepareLeavesOutUnlistedCategories(t *testing.T) {
	t.Parallel()
	ep := `"addresses":[{"locality":"El Paso","region":"TX","country":"US"}]`
	places := strings.Join([]string{
		`{"id":"ov-realtor","names":{"primary":"Sunbelt Realty"},"basic_category":"real_estate_service",` + ep + `}`,
		`{"id":"ov-dentist","names":{"primary":"Bright Smile Dental"},"basic_category":"dental_clinic",` + ep + `}`,
		`{"id":"ov-laundry","names":{"primary":"Suds Laundromat"},"basic_category":"laundry_service",` + ep + `}`,
		`{"id":"ov-bar","names":{"primary":"Rosa's Cantina"},"basic_category":"bar",` + ep + `}`,
		`{"id":"ov-none","names":{"primary":"Plaza Bench"},` + ep + `}`,
		`{"id":"ov-low","names":{"primary":"Jon Watkins"},"basic_category":"bar","confidence":0.12,` + ep + `}`,
		`{"id":"ov-addr","names":{"primary":"4939 tamiami trl e"},"basic_category":"convenience_store","confidence":0.8,` + ep + `}`,
		`{"id":"ov-num","names":{"primary":"5 Brothers Key West"},"basic_category":"casual_eatery","confidence":0.97,` + ep + `}`,
		`{"fsq_place_id":"fsq-cpa","name":"Desert CPA","locality":"El Paso","region":"TX","country":"US","fsq_category_labels":["Business and Professional Services > Accounting and Bookkeeping Service"]}`,
		`{"id":"ov-dot","names":{"primary":"."},"basic_category":"bar",` + ep + `}`,
		`{"id":"ov-junk","names":{"primary":"Haul It Away"},"basic_category":"junk_removal",` +
			`"taxonomy":{"primary":"junk_removal","hierarchy":["services_and_business","home_service","junk_removal"]},` + ep + `}`,
		`{"id":"ov-coin","names":{"primary":"Coin Wash"},"basic_category":"laundry_service",` +
			`"taxonomy":{"primary":"laundromat","hierarchy":["services_and_business","laundry_service","laundromat"]},` + ep + `}`,
		`{"id":"ov-er","names":{"primary":"Thomason ER"},"basic_category":"emergency_or_urgent_care_facility",` +
			`"taxonomy":{"primary":"emergency_room","hierarchy":["health_care","emergency_or_urgent_care_facility"]},` + ep + `}`,
		`{"fsq_place_id":"fsq-wash","name":"Wash World","locality":"El Paso","region":"TX","country":"US","fsq_category_labels":["Business and Professional Services > Laundromat"]}`,
	}, "\n")
	plan, err := importer.Prepare(openFixture(t, "us_divisions.ndjson"), strings.NewReader(places))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if plan.Unlisted != 8 {
		t.Errorf("Unlisted = %d, want 8 (realtor, dentist, accountant, junk removal, no category, low confidence, an address, a name with no letters)", plan.Unlisted)
	}
	have := map[string]bool{}
	for _, n := range plan.Nodes() {
		have[n.Name] = true
	}
	for _, name := range []string{"Suds Laundromat", "Rosa's Cantina", "Wash World", "Coin Wash", "Thomason ER", "5 Brothers Key West"} {
		if !have[name] {
			t.Errorf("%s was left out; want it kept", name)
		}
	}
	for _, name := range []string{"Sunbelt Realty", "Bright Smile Dental", "Desert CPA", "Haul It Away", "Plaza Bench", "Jon Watkins", "4939 tamiami trl e"} {
		if have[name] {
			t.Errorf("%s was imported; want it left out", name)
		}
	}
}
