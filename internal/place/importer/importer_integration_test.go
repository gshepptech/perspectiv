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

// The US fixtures import as STATE, CITY, TOWN, MALL
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
// queued before the failing row is not written, and Houston keeps its name.
func TestImportFailureWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), strings.NewReader(""))

	// Every row the importer writes is valid, so the failure is made: a
	// trigger in this test's own database rejects one write.
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_window_rock() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'rejected %', NEW.name; END $$;
		CREATE TRIGGER reject_window_rock BEFORE UPDATE ON places
		    FOR EACH ROW WHEN (NEW.name = 'Window Rock') EXECUTE FUNCTION reject_window_rock();`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	divisions, err := os.ReadFile("testdata/us_divisions.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	austin := `{"id":"div-austin","subtype":"locality","class":"city","country":"US","region":"US-TX","names":{"primary":"Austin"},"parent_division_id":"div-tx"}` + "\n"
	renamed := austin + strings.Replace(string(divisions), `"primary":"Houston"`, `"primary":"Bayou City"`, 1)

	_, err = importer.New(pool, zerolog.Nop()).Import(ctx, strings.NewReader(renamed), strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "div-windowrock") {
		t.Fatalf("Import error = %v, want a write failure naming div-windowrock", err)
	}
	got := readPlaces(t, pool)
	if _, ok := got["Austin"]; ok {
		t.Error("Austin was written by a failed import")
	}
	if _, ok := got["Houston"]; !ok {
		t.Error("Houston lost its name in a failed import")
	}
}

// A re-imported row whose name, once cleaned, is another row's under the
// same parent keeps its old name; the other row is untouched, and the rest
// of the import is written rather than the whole transaction failing.
func TestImportKeepsNameOnCollision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), strings.NewReader(""))

	var pensacola, texas uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM places WHERE name = 'Pensacola'`).Scan(&pensacola); err != nil {
		t.Fatalf("find Pensacola: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM places WHERE name = 'Texas'`).Scan(&texas); err != nil {
		t.Fatalf("find Texas: %v", err)
	}
	// An earlier import wrote the store with its city in the name, and a
	// user has since added the clean name; a user city holds the name a
	// renamed Houston would take.
	var oldImport, userRow, spaceCity uuid.UUID
	for _, ins := range []struct {
		dst  *uuid.UUID
		sql  string
		args []any
	}{
		{&oldImport, `INSERT INTO places (kind, name, parent_id, timezone, source, source_id)
			VALUES ('PLACE_KIND_STORE', 'Joe''s Pizza, Pensacola', $1, 'America/Chicago', 'PLACE_SOURCE_FOURSQUARE', 'fsq-joe')
			RETURNING id`, []any{pensacola}},
		{&userRow, `INSERT INTO places (kind, name, parent_id, timezone, source, user_added)
			VALUES ('PLACE_KIND_STORE', 'joe''s pizza', $1, 'America/Chicago', 'PLACE_SOURCE_USER', true)
			RETURNING id`, []any{pensacola}},
		{&spaceCity, `INSERT INTO places (kind, name, parent_id, timezone, source)
			VALUES ('PLACE_KIND_CITY', 'Space City', $1, 'America/Chicago', 'PLACE_SOURCE_USER') RETURNING id`, []any{texas}},
	} {
		if err := pool.QueryRow(ctx, ins.sql, ins.args...).Scan(ins.dst); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	divisions, err := os.ReadFile("testdata/us_divisions.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	renamed := strings.Replace(string(divisions), `"primary":"Houston"`, `"primary":"Space City"`, 1)
	places := `{"fsq_place_id":"fsq-joe","name":"Joe's Pizza, Pensacola","locality":"Pensacola","region":"FL","country":"US"}
{"fsq_place_id":"fsq-bayfront","name":"Bayfront Books","locality":"Pensacola","region":"FL","country":"US"}
`
	res := importFixtures(t, pool, strings.NewReader(renamed), strings.NewReader(places))
	if res.Kept != 2 || res.Inserted != 1 {
		t.Errorf("Result = %+v, want 2 kept (Joe's Pizza, Houston) and 1 inserted", res)
	}
	names := map[uuid.UUID]string{}
	rows, err := pool.Query(ctx, `SELECT id, name FROM places`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names[id] = name
	}
	rows.Close()
	for id, want := range map[uuid.UUID]string{
		oldImport: "Joe's Pizza, Pensacola", userRow: "joe's pizza", spaceCity: "Space City",
	} {
		if names[id] != want {
			t.Errorf("row %s is named %q, want %q", id, names[id], want)
		}
	}
	got := readPlaces(t, pool)
	if _, ok := got["Houston"]; !ok {
		t.Error("Houston was renamed onto Space City")
	}
	if _, ok := got["Bayfront Books"]; !ok {
		t.Error("Bayfront Books was not written after a collision")
	}
}

// A stadium first imported as a store becomes a sports MALL on re-import by
// its source id; its arc is untouched, and a later import that lists it as a
// store again does not turn the venue back into a store.
func TestImportStadiumBecomesVenue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	line := func(labels string) io.Reader {
		return strings.NewReader(`{"fsq_place_id":"fsq-nrg","name":"NRG Stadium","latitude":29.68,"longitude":-95.41,` +
			`"locality":"Houston","region":"TX","country":"US","fsq_category_labels":["` + labels + `"]}` + "\n")
	}
	importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), line("Retail > Gift Shop"))

	var placeID, userID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM places WHERE source_id = 'fsq-nrg'`).Scan(&placeID); err != nil {
		t.Fatalf("find stadium: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, username, password_hash, age_confirmed_at)
		VALUES ('fan@example.com', 'fan', 'x', now()) RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	type arcRow struct {
		id, place uuid.UUID
		path      []uuid.UUID
		title     string
	}
	readArc := func() arcRow {
		t.Helper()
		var a arcRow
		if err := pool.QueryRow(ctx, `SELECT id, place_id, place_path, title FROM arcs`).Scan(&a.id, &a.place, &a.path, &a.title); err != nil {
			t.Fatalf("read arc: %v", err)
		}
		return a
	}
	if _, err := pool.Exec(ctx, `INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-02-08', 'Game night', $2)`, placeID, userID); err != nil {
		t.Fatalf("insert arc: %v", err)
	}
	before := readArc()

	cases := []struct {
		name, labels, kind string
	}{
		{"listed as a stadium", "Arts and Entertainment > Stadium > Football Stadium", "PLACE_KIND_MALL"},
		{"listed as a store again", "Retail > Gift Shop", "PLACE_KIND_MALL"},
	}
	for _, tc := range cases { // serial: each step re-imports into the same database
		res := importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), line(tc.labels))
		if res.Inserted != 0 {
			t.Errorf("%s: inserted %d rows, want the stadium updated in place", tc.name, res.Inserted)
		}
		var id uuid.UUID
		var kind string
		var category *string
		if err := pool.QueryRow(ctx, `SELECT id, kind, category FROM places WHERE source_id = 'fsq-nrg'`).
			Scan(&id, &kind, &category); err != nil {
			t.Fatalf("%s: read stadium: %v", tc.name, err)
		}
		if id != placeID || kind != tc.kind {
			t.Errorf("%s: stadium is %s %s, want %s %s", tc.name, id, kind, placeID, tc.kind)
		}
		if tc.labels != "Retail > Gift Shop" && deref(category) != "PLACE_CATEGORY_SPORTS_AND_RECREATION" {
			t.Errorf("%s: category = %s, want PLACE_CATEGORY_SPORTS_AND_RECREATION", tc.name, deref(category))
		}
		after := readArc()
		if after.id != before.id || after.place != before.place || after.title != before.title ||
			len(after.path) != len(before.path) || after.path[len(after.path)-1] != placeID {
			t.Errorf("%s: arc changed from %+v to %+v", tc.name, before, after)
		}
	}
}
