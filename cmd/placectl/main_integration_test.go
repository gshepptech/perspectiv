//go:build integration

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

func countPlaces(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM places`).Scan(&n); err != nil {
		t.Fatalf("count places: %v", err)
	}
	return n
}

// placectl import of the United States fixtures produces STATE, CITY
// and TOWN rows with parent links and an IANA zone on every row.
func TestPlacectlImportFixtures(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	var stderr bytes.Buffer
	code := run(context.Background(),
		[]string{"import", "--divisions", fixture("us_divisions.ndjson"), "--places", fixture("us_places.ndjson")},
		env(map[string]string{"DATABASE_URL": pool.Config().ConnString(), "LOG_LEVEL": "info"}), io.Discard, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	var states, cities, towns, orphans, noZone, badParent int
	err := pool.QueryRow(context.Background(), `
		SELECT
		  count(*) FILTER (WHERE p.kind = 'PLACE_KIND_STATE'),
		  count(*) FILTER (WHERE p.kind = 'PLACE_KIND_CITY'),
		  count(*) FILTER (WHERE p.kind = 'PLACE_KIND_TOWN'),
		  count(*) FILTER (WHERE p.kind <> 'PLACE_KIND_STATE' AND pp.id IS NULL),
		  count(*) FILTER (WHERE p.timezone = ''),
		  count(*) FILTER (WHERE p.kind = 'PLACE_KIND_CITY' AND pp.kind <> 'PLACE_KIND_STATE')
		FROM places p LEFT JOIN places pp ON pp.id = p.parent_id`).
		Scan(&states, &cities, &towns, &orphans, &noZone, &badParent)
	if err != nil {
		t.Fatalf("summarise places: %v", err)
	}
	if states != 3 || cities != 5 || towns != 3 {
		t.Errorf("imported %d states, %d cities, %d towns; want 3, 5, 3", states, cities, towns)
	}
	if orphans != 0 || noZone != 0 || badParent != 0 {
		t.Errorf("%d rows without a parent, %d without a zone, %d cities not under a state", orphans, noZone, badParent)
	}
	if !bytes.Contains(stderr.Bytes(), []byte(`"place import committed"`)) {
		t.Errorf("no commit log line on stderr:\n%s", stderr.String())
	}
}

// An unparsable file or an unresolved division parent exits non-zero
// and writes nothing.
func TestPlacectlImportFailuresWriteNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.ndjson")
	if err := os.WriteFile(bad, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(dir, "orphan.ndjson")
	if err := os.WriteFile(orphan, []byte(`{"id":"c","subtype":"locality","class":"city","country":"US","names":{"primary":"Lost"},"parent_division_id":"nowhere"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name              string
		divisions, places string
	}{
		{"unparsable divisions", bad, fixture("us_places.ndjson")},
		{"unparsable places", fixture("us_divisions.ndjson"), bad},
		{"city with no parent division", orphan, fixture("us_places.ndjson")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pool := testdb.New(t)
			var stderr bytes.Buffer
			code := run(context.Background(),
				[]string{"import", "--divisions", tc.divisions, "--places", tc.places},
				env(map[string]string{"DATABASE_URL": pool.Config().ConnString()}), io.Discard, &stderr)
			if code != exitFailure {
				t.Errorf("exit = %d, want %d; stderr:\n%s", code, exitFailure, stderr.String())
			}
			if n := countPlaces(t, pool); n != 0 {
				t.Errorf("failed import left %d places", n)
			}
		})
	}
}

// A US places extract with POIs whose locality is empty or is not an
// imported city or town still imports; those POIs are left out, counted and
// reported, and every other row is written.
func TestPlacectlImportLeavesOutUnfiledPlaces(t *testing.T) {
	t.Parallel()
	places, err := os.ReadFile(fixture("us_places.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	unfiled := `{"fsq_place_id":"fsq-x1","name":"Roadside Diner","country":"US","region":"TX","locality":null}` + "\n" +
		`{"fsq_place_id":"fsq-x2","name":"Corner Bodega","country":"US","region":"TX","locality":"Northeast El Paso"}` + "\n"
	path := filepath.Join(t.TempDir(), "places.ndjson")
	if err := os.WriteFile(path, append(places, unfiled...), 0o600); err != nil {
		t.Fatal(err)
	}

	pool := testdb.New(t)
	var stderr bytes.Buffer
	code := run(context.Background(),
		[]string{"import", "--divisions", fixture("us_divisions.ndjson"), "--places", path},
		env(map[string]string{"DATABASE_URL": pool.Config().ConnString(), "LOG_LEVEL": "info"}), io.Discard, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	// The fixtures alone plan 16 nodes (the second bookstore is merged into
	// the first, so it is not one of them).
	if n := countPlaces(t, pool); n != 16 {
		t.Errorf("imported %d places, want the fixtures' 16", n)
	}
	var stray int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM places WHERE name IN ('Roadside Diner', 'Corner Bodega')`).Scan(&stray); err != nil {
		t.Fatalf("count unfiled: %v", err)
	}
	if stray != 0 {
		t.Errorf("%d unfiled POIs were written", stray)
	}
	if !bytes.Contains(stderr.Bytes(), []byte(`"unfiled":2`)) {
		t.Errorf("commit log does not report 2 unfiled places:\n%s", stderr.String())
	}
}

// placectl dedupe is a dry run by default and prints its summary; --apply
// hides the copy, and a second run finds nothing more to merge.
func TestPlacectlDedupe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	dbEnv := env(map[string]string{"DATABASE_URL": pool.Config().ConnString()})
	if code := run(ctx, []string{"import", "--divisions", fixture("us_divisions.ndjson"), "--places", fixture("us_places.ndjson")},
		dbEnv, io.Discard, io.Discard); code != exitOK {
		t.Fatalf("import exit = %d", code)
	}
	// A second listing of Desert Books, 30 m from the imported one.
	if _, err := pool.Exec(ctx, `
		INSERT INTO places (kind, name, parent_id, timezone, lat, lng, source)
		SELECT 'PLACE_KIND_STORE', 'Desert Books, El Paso', parent_id, timezone, lat + 0.0003, lng, 'PLACE_SOURCE_USER'
		FROM places WHERE name = 'Desert Books'`); err != nil {
		t.Fatalf("insert copy: %v", err)
	}

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"dry run", []string{"dedupe"}, []string{"dry run, nothing written", "clusters:   1", "would merge: 1", `"Desert Books, El Paso" into`}},
		{"apply", []string{"dedupe", "--apply"}, []string{"applied", "clusters:   1", "merged: 1"}},
		{"second run", []string{"dedupe", "--apply"}, []string{"clusters:   0", "merged: 0"}},
	}
	for _, tc := range cases { // serial: each step sees the one before
		var stdout, stderr bytes.Buffer
		if code := run(ctx, tc.args, dbEnv, &stdout, &stderr); code != exitOK {
			t.Fatalf("%s: exit = %d; stderr:\n%s", tc.name, code, stderr.String())
		}
		for _, w := range tc.want {
			if !strings.Contains(stdout.String(), w) {
				t.Errorf("%s: stdout does not contain %q:\n%s", tc.name, w, stdout.String())
			}
		}
	}
	var hidden int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM places WHERE hidden_at IS NOT NULL`).Scan(&hidden); err != nil {
		t.Fatalf("count hidden: %v", err)
	}
	if hidden != 1 {
		t.Errorf("%d places hidden, want the one copy", hidden)
	}
}
