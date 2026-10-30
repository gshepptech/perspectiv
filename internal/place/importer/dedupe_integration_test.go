//go:build integration

package importer_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"perspectiv/internal/place/importer"
	"perspectiv/internal/testdb"
)

// dedupeSeed is a city with duplicates of every kind the dedupe tells
// apart.
type dedupeSeed struct {
	mall, mallCopy, mallShouty, kiosk, farMall uuid.UUID
	draftBar, barCopy                          uuid.UUID
	church, churchDraft                        uuid.UUID
	books, booksMall, booksMallShop            uuid.UUID
	fan, regular                               uuid.UUID
}

// Pensacola's fixture point; 0.0005 degrees of latitude is about 55 m.
const seedLat, seedLng = 30.41, -87.21

func seedDuplicates(t *testing.T, pool *pgxpool.Pool) dedupeSeed {
	t.Helper()
	ctx := context.Background()
	importFixtures(t, pool, openFixture(t, "us_divisions.ndjson"), strings.NewReader(""))
	var city uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM places WHERE name = 'Pensacola'`).Scan(&city); err != nil {
		t.Fatalf("find Pensacola: %v", err)
	}
	n := 0
	place := func(kind, name string, parent uuid.UUID, north float64) uuid.UUID {
		t.Helper()
		n++
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO places (kind, name, parent_id, timezone, lat, lng, source, source_id)
			VALUES ($1, $2, $3, 'America/Chicago', $4, $5, 'PLACE_SOURCE_OVERTURE', $6) RETURNING id`,
			"PLACE_KIND_"+kind, name, parent, seedLat+north, seedLng, fmt.Sprintf("seed-%d", n)).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
		return id
	}
	user := func(name string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO users (email, username, password_hash, age_confirmed_at)
			VALUES ($1 || '@example.com', $1, 'x', now()) RETURNING id`, name).Scan(&id); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		return id
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
	}

	var s dedupeSeed
	s.fan, s.regular = user("fan"), user("regular")
	// A storied mall, two storyless copies nearby (one holding a kiosk with
	// a story of its own) and a copy 5 km away.
	s.mall = place("MALL", "Cordova Mall", city, 0)
	s.mallCopy = place("MALL", "Cordova Mall, Pensacola", city, 0.0007)
	s.mallShouty = place("STORE", "cordova mall.", city, 0.0011)
	s.kiosk = place("STORE", "Pretzel Kiosk", s.mallCopy, 0.0007)
	s.farMall = place("STORE", "Cordova Mall!", city, 0.045)
	// A bar with only a draft, and a storyless copy of it.
	s.draftBar = place("STORE", "Seville Quarter", city, 0.003)
	s.barCopy = place("STORE", "Seville Quarter, FL", city, 0.0034)
	// Two copies of a church, both with stories: reported only.
	s.church = place("STORE", "Old Christ Church", city, 0.006)
	s.churchDraft = place("STORE", "Old Christ Church.", city, 0.0062)
	// A storied bookshop, and a storyless copy listed as a mall with a shop
	// inside it that cannot be filed under a store.
	s.books = place("STORE", "Bayview Books", city, 0.009)
	s.booksMall = place("MALL", "Bayview Books, Pensacola", city, 0.0092)
	s.booksMallShop = place("STORE", "Bayview Cafe", s.booksMall, 0.0092)

	for _, p := range []uuid.UUID{s.mall, s.kiosk, s.church, s.books} {
		exec(`INSERT INTO arcs (place_id, local_date, title, created_by) VALUES ($1, '2026-03-01', 'A day there', $2)`, p, s.fan)
	}
	for _, p := range []uuid.UUID{s.draftBar, s.churchDraft} {
		exec(`INSERT INTO perspectives (author_id, place_id) VALUES ($1, $2)`, s.fan, p)
	}
	exec(`INSERT INTO place_follows (user_id, place_id) VALUES ($1, $2), ($3, $2), ($3, $4)`,
		s.fan, s.mallCopy, s.regular, s.mall)
	return s
}

// dedupeState is everything a dedupe may write, and the story rows it must
// never move.
type dedupeState struct {
	places, follows, audit, arcs, perspectives []string
}

func readDedupeState(t *testing.T, pool *pgxpool.Pool) dedupeState {
	t.Helper()
	col := func(sql string) []string {
		t.Helper()
		rows, err := pool.Query(context.Background(), sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, s)
		}
		slices.Sort(out)
		return out
	}
	return dedupeState{
		places:       col(`SELECT concat_ws(' ', id, parent_id, hidden_at IS NOT NULL, path::text) FROM places`),
		follows:      col(`SELECT concat_ws(' ', user_id, place_id) FROM place_follows`),
		audit:        col(`SELECT concat_ws(' ', action, target_kind, target_id, actor_id IS NULL, reason) FROM moderation_audit`),
		arcs:         col(`SELECT concat_ws(' ', id, place_id) FROM arcs`),
		perspectives: col(`SELECT concat_ws(' ', id, place_id) FROM perspectives`),
	}
}

func (s dedupeState) equal(o dedupeState) bool {
	return slices.Equal(s.places, o.places) && slices.Equal(s.follows, o.follows) &&
		slices.Equal(s.audit, o.audit) && slices.Equal(s.arcs, o.arcs) && slices.Equal(s.perspectives, o.perspectives)
}

func mergedLosers(rep importer.DedupeReport) map[uuid.UUID]uuid.UUID {
	out := map[uuid.UUID]uuid.UUID{}
	for _, m := range rep.Merges {
		out[m.Loser] = m.Survivor
	}
	return out
}

func skipReasons(rep importer.DedupeReport) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	for _, s := range rep.Skips {
		out[s.Place] = s.Reason
	}
	return out
}

// placectl dedupe: a dry run writes nothing; --apply hides only the
// storyless near copies, moving their places and followers to the survivor
// with an audit row each; a second run changes nothing; no arc or
// perspective ever changes place.
func TestDedupe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testdb.New(t)
	s := seedDuplicates(t, pool)
	im := importer.New(pool, zerolog.Nop())
	start := readDedupeState(t, pool)

	wantMerges := map[uuid.UUID]uuid.UUID{s.mallCopy: s.mall, s.mallShouty: s.mall, s.barCopy: s.draftBar}
	wantSkips := map[uuid.UUID]string{s.church: importer.SkipStoried, s.booksMall: importer.SkipChildLevel}

	dry, err := im.Dedupe(ctx, false)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !readDedupeState(t, pool).equal(start) {
		t.Fatal("dry run wrote to the database")
	}
	applied, err := im.Dedupe(ctx, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, rep := range []importer.DedupeReport{dry, applied} {
		if rep.Clusters != 4 {
			t.Errorf("applied=%v: %d clusters, want 4 (mall, bar, church, bookshop)", rep.Applied, rep.Clusters)
		}
		if got := mergedLosers(rep); !mapsEqual(got, wantMerges) {
			t.Errorf("applied=%v: merges %v, want %v", rep.Applied, got, wantMerges)
		}
		if got := skipReasons(rep); !mapsEqual(got, wantSkips) {
			t.Errorf("applied=%v: skips %v, want %v", rep.Applied, got, wantSkips)
		}
	}

	after := readDedupeState(t, pool)
	if !slices.Equal(after.arcs, start.arcs) || !slices.Equal(after.perspectives, start.perspectives) {
		t.Error("an arc or a perspective changed place")
	}
	checkDedupeRows(t, pool, s)

	again, err := im.Dedupe(ctx, true)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(again.Merges) != 0 {
		t.Errorf("second run merged %d places, want none", len(again.Merges))
	}
	if !readDedupeState(t, pool).equal(after) {
		t.Error("second run changed the database")
	}
}

func mapsEqual[V comparable](a, b map[uuid.UUID]V) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// checkDedupeRows asserts what the applied dedupe left in each table.
func checkDedupeRows(t *testing.T, pool *pgxpool.Pool, s dedupeSeed) {
	t.Helper()
	ctx := context.Background()
	hidden := map[uuid.UUID]bool{s.mallCopy: true, s.mallShouty: true, s.barCopy: true}
	for name, id := range map[string]uuid.UUID{
		"mall": s.mall, "mall copy": s.mallCopy, "shouty mall": s.mallShouty, "kiosk": s.kiosk, "far mall": s.farMall,
		"draft bar": s.draftBar, "bar copy": s.barCopy, "church": s.church, "church draft": s.churchDraft,
		"books": s.books, "books mall": s.booksMall, "books mall shop": s.booksMallShop,
	} {
		var isHidden bool
		if err := pool.QueryRow(ctx, `SELECT hidden_at IS NOT NULL FROM places WHERE id = $1`, id).Scan(&isHidden); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if isHidden != hidden[id] {
			t.Errorf("%s hidden = %v, want %v", name, isHidden, hidden[id])
		}
	}
	var kioskParent uuid.UUID
	var kioskArcPath []uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT p.parent_id, a.place_path FROM places p JOIN arcs a ON a.place_id = p.id
		WHERE p.id = $1`, s.kiosk).Scan(&kioskParent, &kioskArcPath); err != nil {
		t.Fatalf("read kiosk: %v", err)
	}
	if kioskParent != s.mall || !slices.Contains(kioskArcPath, s.mall) || slices.Contains(kioskArcPath, s.mallCopy) {
		t.Errorf("kiosk is under %s with arc path %v, want under the mall %s", kioskParent, kioskArcPath, s.mall)
	}
	var fanFollows, regularFollows, copyFollows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE user_id = $1 AND place_id = $3),
		       count(*) FILTER (WHERE user_id = $2 AND place_id = $3),
		       count(*) FILTER (WHERE place_id = $4)
		FROM place_follows`, s.fan, s.regular, s.mall, s.mallCopy).Scan(&fanFollows, &regularFollows, &copyFollows); err != nil {
		t.Fatalf("read follows: %v", err)
	}
	if fanFollows != 1 || regularFollows != 1 || copyFollows != 0 {
		t.Errorf("follows: fan %d, regular %d on the mall, %d left on the copy; want 1, 1, 0", fanFollows, regularFollows, copyFollows)
	}
	want := []string{
		auditLine(s.mallCopy, s.mall), auditLine(s.mallShouty, s.mall), auditLine(s.barCopy, s.draftBar),
	}
	slices.Sort(want)
	if got := readDedupeState(t, pool).audit; !slices.Equal(got, want) {
		t.Errorf("audit rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func auditLine(loser, survivor uuid.UUID) string {
	return fmt.Sprintf("AUDIT_ACTION_MERGE_PLACE AUDIT_TARGET_KIND_PLACE %s t deduplicated by placectl into %s", loser, survivor)
}
