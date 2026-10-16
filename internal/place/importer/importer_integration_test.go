//go:build integration

package importer_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place/importer"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

func importFixtures(t *testing.T, pool *pgxpool.Pool, divisions, places io.Reader) importer.Result {
	t.Helper()
	res, err := importer.New(pool, zerolog.Nop()).Import(context.Background(), divisions, places)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return res
}

type placeRow struct {
	id         uuid.UUID
	kind, name string
	parentKind *string
	parentName *string
	timezone   string
	source     string
	sourceID   *string
	userAdded  bool
	hidden     bool
}

func readPlaces(t *testing.T, pool *pgxpool.Pool) map[string]placeRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT p.id, p.kind, p.name, pp.kind, pp.name, p.timezone, p.source, p.source_id,
		       p.user_added, p.hidden_at IS NOT NULL
		FROM places p LEFT JOIN places pp ON pp.id = p.parent_id`)
	if err != nil {
		t.Fatalf("query places: %v", err)
	}
	defer rows.Close()
	out := map[string]placeRow{}
	for rows.Next() {
		var r placeRow
		if err := rows.Scan(&r.id, &r.kind, &r.name, &r.parentKind, &r.parentName, &r.timezone,
			&r.source, &r.sourceID, &r.userAdded, &r.hidden); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[r.name] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// OT-010, CT-016, FR-050: the US fixtures import as STATE, CITY, TOWN, MALL
// and STORE rows with parent links and an IANA zone on every row.
func TestImportFixtures(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	res := importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), openFixture(t, "us_places.ndjson"))

	wantKinds := map[perspectivv1.PlaceKind]int{state: 3, city: 5, town: 3, mall: 2, store: 3}
	for k, want := range wantKinds {
		if got := res.ByKind[k]; got != want {
			t.Errorf("Result.ByKind[%v] = %d, want %d", k, got, want)
		}
	}
	if res.Inserted != 16 || res.Updated != 0 || res.Matched != 0 || res.Merged != 1 || res.Skipped != 3 {
		t.Errorf("Result = %+v, want 16 inserted, 1 merged, 3 skipped", res)
	}

	got := readPlaces(t, pool)
	if len(got) != 16 {
		t.Fatalf("places has %d rows, want 16", len(got))
	}
	for name, r := range got {
		checkImportedRow(t, name, r)
	}

	str := func(s string) *string { return &s }
	cases := []struct {
		name, kind string
		parentKind *string
		parentName *string
		zone       string
		source     string
	}{
		{"Texas", "PLACE_KIND_STATE", nil, nil, "America/Chicago", "PLACE_SOURCE_OVERTURE"},
		{"Florida", "PLACE_KIND_STATE", nil, nil, "America/New_York", "PLACE_SOURCE_OVERTURE"},
		{"Arizona", "PLACE_KIND_STATE", nil, nil, "America/Phoenix", "PLACE_SOURCE_OVERTURE"},
		{"El Paso", "PLACE_KIND_CITY", str("PLACE_KIND_STATE"), str("Texas"), "America/Denver", "PLACE_SOURCE_OVERTURE"},
		{"Houston", "PLACE_KIND_CITY", str("PLACE_KIND_STATE"), str("Texas"), "America/Chicago", "PLACE_SOURCE_OVERTURE"},
		{"Pensacola", "PLACE_KIND_CITY", str("PLACE_KIND_STATE"), str("Florida"), "America/Chicago", "PLACE_SOURCE_OVERTURE"},
		{"Socorro", "PLACE_KIND_TOWN", str("PLACE_KIND_STATE"), str("Texas"), "America/Denver", "PLACE_SOURCE_OVERTURE"},
		{"Montecillo", "PLACE_KIND_TOWN", str("PLACE_KIND_CITY"), str("El Paso"), "America/Denver", "PLACE_SOURCE_OVERTURE"},
		{"Window Rock", "PLACE_KIND_TOWN", str("PLACE_KIND_STATE"), str("Arizona"), "America/Denver", "PLACE_SOURCE_OVERTURE"},
		{"Cielo Vista Mall", "PLACE_KIND_MALL", str("PLACE_KIND_CITY"), str("El Paso"), "America/Denver", "PLACE_SOURCE_FOURSQUARE"},
		{"The Galleria", "PLACE_KIND_MALL", str("PLACE_KIND_CITY"), str("Houston"), "America/Chicago", "PLACE_SOURCE_OVERTURE"},
		{"Desert Books", "PLACE_KIND_STORE", str("PLACE_KIND_CITY"), str("El Paso"), "America/Denver", "PLACE_SOURCE_FOURSQUARE"},
		{"Palafox Coffee", "PLACE_KIND_STORE", str("PLACE_KIND_CITY"), str("Pensacola"), "America/Chicago", "PLACE_SOURCE_OVERTURE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, ok := got[tc.name]
			if !ok {
				t.Fatalf("no row named %q", tc.name)
			}
			want := placeRow{kind: tc.kind, timezone: tc.zone, source: tc.source, parentKind: tc.parentKind, parentName: tc.parentName}
			if r.kind != want.kind || r.timezone != want.timezone || r.source != want.source ||
				!equalPtr(r.parentKind, want.parentKind) || !equalPtr(r.parentName, want.parentName) {
				t.Errorf("row = {%s %s %s parent %s %s}, want {%s %s %s parent %s %s}",
					r.kind, r.timezone, r.source, deref(r.parentKind), deref(r.parentName),
					want.kind, want.timezone, want.source, deref(want.parentKind), deref(want.parentName))
			}
		})
	}
}

// checkImportedRow asserts what every imported row carries: an IANA zone, a
// source id, and no user_added flag.
func checkImportedRow(t *testing.T, name string, r placeRow) {
	t.Helper()
	if _, err := time.LoadLocation(r.timezone); err != nil || r.timezone == "" {
		t.Errorf("%s: timezone %q is not an IANA zone", name, r.timezone)
	}
	if r.userAdded {
		t.Errorf("%s: imported row is user_added", name)
	}
	if r.sourceID == nil {
		t.Errorf("%s: imported row has no source_id", name)
	}
}

func equalPtr(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// A second run of the same files updates every row in place: same ids, no
// new rows, and an admin's hide survives.
func TestImportTwiceIsIdempotent(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), openFixture(t, "us_places.ndjson"))
	before := readPlaces(t, pool)
	if _, err := pool.Exec(context.Background(), `UPDATE places SET hidden_at = now() WHERE name = 'Desert Books'`); err != nil {
		t.Fatalf("hide: %v", err)
	}

	res := importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), openFixture(t, "us_places.ndjson"))
	if res.Inserted != 0 || res.Updated != 16 || res.Matched != 0 {
		t.Errorf("second run Result = %+v, want 16 updated", res)
	}
	after := readPlaces(t, pool)
	if len(after) != len(before) {
		t.Fatalf("second run left %d rows, want %d", len(after), len(before))
	}
	for name, r := range before {
		if after[name].id != r.id {
			t.Errorf("%s changed id on re-import", name)
		}
	}
	if !after["Desert Books"].hidden {
		t.Error("re-import unhid a hidden place")
	}
}

// A user-added store that already holds an imported store's name under the
// same city is reused, not duplicated and not overwritten.
func TestImportReusesUserAddedSibling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), strings.NewReader(""))

	var userID, pensacola uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM places WHERE name = 'Pensacola'`).Scan(&pensacola); err != nil {
		t.Fatalf("find Pensacola: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, username, password_hash, age_confirmed_at)
		VALUES ('u@example.com', 'adder', 'x', now()) RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var userPlace uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO places (kind, name, parent_id, timezone, source, user_added, created_by)
		VALUES ('PLACE_KIND_STORE', 'palafox  COFFEE', $1, 'America/Chicago', 'PLACE_SOURCE_USER', true, $2)
		RETURNING id`, pensacola, userID).Scan(&userPlace); err != nil {
		t.Fatalf("insert user place: %v", err)
	}

	res := importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), openFixture(t, "us_places.ndjson"))
	if res.Matched != 1 {
		t.Errorf("Matched = %d, want 1", res.Matched)
	}
	var n int
	var source string
	if err := pool.QueryRow(ctx, `
		SELECT count(*), min(source) FROM places
		WHERE parent_id = $1 AND lower(name) LIKE 'palafox%'`, pensacola).Scan(&n, &source); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 || source != "PLACE_SOURCE_USER" {
		t.Errorf("Pensacola has %d Palafox rows from %s, want the one user row", n, source)
	}
}

// A database failure part way through rolls the whole import back: the city
// queued before the failing row is not written.
func TestImportFailureWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), strings.NewReader(""))

	var texas uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM places WHERE name = 'Texas'`).Scan(&texas); err != nil {
		t.Fatalf("find Texas: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_CITY', 'Space City', $1, 'America/Chicago', 'PLACE_SOURCE_USER')`, texas); err != nil {
		t.Fatalf("insert Space City: %v", err)
	}
	// Austin is new; Houston is renamed onto the name Space City holds, which
	// the places_parent_name_key index rejects.
	divisions, err := os.ReadFile("testdata/us_divisions.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	austin := `{"id":"div-austin","subtype":"locality","class":"city","country":"US","region":"US-TX","names":{"primary":"Austin"},"parent_division_id":"div-tx"}` + "\n"
	renamed := austin + strings.Replace(string(divisions), `"primary":"Houston"`, `"primary":"Space City"`, 1)

	_, err = importer.New(pool, zerolog.Nop()).Import(ctx, strings.NewReader(renamed), strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "div-houston") {
		t.Fatalf("Import error = %v, want a write failure naming div-houston", err)
	}
	got := readPlaces(t, pool)
	if _, ok := got["Austin"]; ok {
		t.Error("Austin was written by a failed import")
	}
	if _, ok := got["Houston"]; !ok {
		t.Error("Houston lost its name in a failed import")
	}
}
