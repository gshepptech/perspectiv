package importer

import (
	"testing"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place/importer/dbq"
)

// metresNorth is a latitude offset of about m metres.
func metresNorth(m float64) float64 { return m / 111195 }

func TestClusterDuplicates(t *testing.T) {
	t.Parallel()
	id := func(n byte) uuid.UUID { return uuid.UUID{15: n} }
	place := func(n byte, key string, north float64, arcs, perspectives, dirt int, venue bool) dedupePlace {
		return dedupePlace{id: id(n), name: key, key: key, lat: 27.77 + metresNorth(north), lng: -82.64,
			arcs: arcs, perspectives: perspectives, dirt: dirt, venue: venue}
	}
	cases := []struct {
		name   string
		places []dedupePlace
		// want lists each cluster's ids, survivor first.
		want [][]byte
	}{
		{"near same-named places cluster, most arcs survive",
			[]dedupePlace{place(1, "bagels", 0, 0, 0, 0, false), place(2, "bagels", 50, 2, 0, 0, false)},
			[][]byte{{2, 1}}},
		{"perspectives break an arc tie",
			[]dedupePlace{place(1, "bagels", 0, 0, 0, 0, false), place(2, "bagels", 50, 0, 1, 0, false)},
			[][]byte{{2, 1}}},
		{"cleaner name breaks a story tie",
			[]dedupePlace{place(1, "bagels", 0, 0, 0, 2, false), place(2, "bagels", 50, 0, 0, 0, false)},
			[][]byte{{2, 1}}},
		{"older id breaks a name tie",
			[]dedupePlace{place(2, "bagels", 0, 0, 0, 0, false), place(1, "bagels", 50, 0, 0, 0, false)},
			[][]byte{{1, 2}}},
		{"too far apart",
			[]dedupePlace{place(1, "bagels", 0, 0, 0, 0, false), place(2, "bagels", 250, 0, 0, 0, false)},
			nil},
		{"different names",
			[]dedupePlace{place(1, "bagels", 0, 0, 0, 0, false), place(2, "donuts", 10, 0, 0, 0, false)},
			nil},
		{"a sports venue reaches 2 km",
			[]dedupePlace{place(1, "the stadium", 0, 0, 0, 0, true), place(2, "the stadium", 1500, 0, 0, 0, false)},
			[][]byte{{1, 2}}},
		{"not past 2 km",
			[]dedupePlace{place(1, "the stadium", 0, 0, 0, 0, true), place(2, "the stadium", 2500, 0, 0, 0, true)},
			nil},
		{"a chain along a street never merges end to end",
			[]dedupePlace{
				place(1, "coffee", 0, 0, 0, 0, false), place(2, "coffee", 150, 0, 0, 0, false),
				place(3, "coffee", 300, 0, 0, 0, false),
			},
			[][]byte{{1, 2}}},
		{"two storied places stay in one cluster",
			[]dedupePlace{place(1, "bagels", 0, 1, 0, 0, false), place(2, "bagels", 20, 3, 0, 0, false), place(3, "bagels", 40, 0, 0, 0, false)},
			[][]byte{{2, 1, 3}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := clusterDuplicates(tc.places)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d clusters, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, c := range got {
				if len(c) != len(tc.want[i]) {
					t.Fatalf("cluster %d has %d places, want %d", i, len(c), len(tc.want[i]))
				}
				for j, p := range c {
					if p.id != id(tc.want[i][j]) {
						t.Errorf("cluster %d place %d = %v, want id %d", i, j, p.id, tc.want[i][j])
					}
				}
			}
		})
	}
}

func TestToDedupePlace(t *testing.T) {
	t.Parallel()
	mall := perspectivv1.PlaceKind_PLACE_KIND_MALL.String()
	sports := perspectivv1.PlaceCategory_PLACE_CATEGORY_SPORTS_AND_RECREATION.String()
	cases := []struct {
		name  string
		row   dbq.ListDedupeCandidatesRow
		key   string
		dirt  int
		venue bool
	}{
		{"city suffix stripped from the key", dbq.ListDedupeCandidatesRow{Name: "Starbucks, St. Petersburg", AncestorNames: []string{"Florida", "Saint Petersburg"}}, "starbucks", 1, false},
		{"punctuation and case ignored", dbq.ListDedupeCandidatesRow{Name: "STAR-BUCKS!"}, "star bucks", 0, false},
		{"lower case and trailing stop are dirt", dbq.ListDedupeCandidatesRow{Name: "starbucks."}, "starbucks", 2, false},
		{"state code stripped from the key", dbq.ListDedupeCandidatesRow{Name: "Starbucks, FL", AncestorNames: []string{"Florida", "Tampa"}}, "starbucks", 1, false},
		{"another city stays in the key", dbq.ListDedupeCandidatesRow{Name: "Starbucks, Tampa", AncestorNames: []string{"Florida", "Clearwater"}}, "starbucks tampa", 0, false},
		{"a sports mall is a venue", dbq.ListDedupeCandidatesRow{Name: "Stadium", Kind: mall, Category: sports}, "stadium", 0, true},
		{"a sports store is not", dbq.ListDedupeCandidatesRow{Name: "Gym", Kind: "PLACE_KIND_STORE", Category: sports}, "gym", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := toDedupePlace(&tc.row)
			if got.key != tc.key || got.dirt != tc.dirt || got.venue != tc.venue {
				t.Errorf("key, dirt, venue = %q, %d, %v; want %q, %d, %v", got.key, got.dirt, got.venue, tc.key, tc.dirt, tc.venue)
			}
		})
	}
}

func TestHaversine(t *testing.T) {
	t.Parallel()
	// Tropicana Field to Raymond James Stadium is about 27 km.
	if d := haversine(27.7682, -82.6534, 27.9759, -82.5033); d < 26000 || d > 28000 {
		t.Errorf("haversine = %.0f m, want about 27 km", d)
	}
	if d := haversine(27.77, -82.64, 27.77+metresNorth(200), -82.64); d < 199 || d > 201 {
		t.Errorf("haversine over 200 m north = %.1f m", d)
	}
}
