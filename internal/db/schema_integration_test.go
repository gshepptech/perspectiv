//go:build integration

package db_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/reflect/protoreflect"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/testdb"
)

const (
	uniqueViolation = "23505"
	checkViolation  = "23514"
)

// fixture inserts rows through plain SQL so each schema test controls exactly
// what it writes.
type fixture struct {
	t    *testing.T
	pool *pgxpool.Pool
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return fixture{t: t, pool: testdb.New(t)}
}

func (f fixture) id(query string, args ...any) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&id); err != nil {
		f.t.Fatalf("%s: %v", strings.Fields(query)[2], err)
	}
	return id
}

func (f fixture) exec(query string, args ...any) error {
	f.t.Helper()
	_, err := f.pool.Exec(context.Background(), query, args...)
	return err
}

func (f fixture) user(name string) uuid.UUID {
	f.t.Helper()
	return f.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at)
		VALUES ($1, 'x', $2, now()) RETURNING id`, name+"@example.test", name)
}

func (f fixture) place(kind string, name string, parent *uuid.UUID) uuid.UUID {
	f.t.Helper()
	return f.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ($1, $2, $3, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, kind, name, parent)
}

func (f fixture) dayArc(place, by uuid.UUID, date string) uuid.UUID {
	f.t.Helper()
	return f.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, $2, 'A real title', $3) RETURNING id`, place, date, by)
}

func (f fixture) published(arc, author uuid.UUID) uuid.UUID {
	f.t.Helper()
	return f.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())
		RETURNING id`, arc, author)
}

func (f fixture) character(perspective uuid.UUID, position int) uuid.UUID {
	f.t.Helper()
	return f.id(`INSERT INTO characters (perspective_id, label, position)
		VALUES ($1, 'the cashier', $2) RETURNING id`, perspective, position)
}

// world is one user with a published perspective in a day arc at a store.
type world struct {
	fixture
	alice, state, city, store, arc, perspective uuid.UUID
}

func newWorld(t *testing.T) world {
	t.Helper()
	f := newFixture(t)
	w := world{fixture: f}
	w.alice = f.user("alice")
	w.state = f.place("PLACE_KIND_STATE", "New York", nil)
	w.city = f.place("PLACE_KIND_CITY", "Albany", &w.state)
	w.store = f.place("PLACE_KIND_STORE", "Corner Deli", &w.city)
	w.arc = f.dayArc(w.store, w.alice, "2026-10-11")
	w.perspective = f.published(w.arc, w.alice)
	return w
}

// queryStrings runs a query whose rows are one text column and returns them.
func queryStrings(t *testing.T, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, query string, args ...any) []string {
	t.Helper()
	rows, err := q.Query(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return out
}

// wantErr fails unless err is a Postgres error with the code and, when
// constraint is not empty, the constraint name given.
func wantErr(t *testing.T, err error, code, constraint string) {
	t.Helper()
	gotCode, gotConstraint := pgCode(err)
	if gotCode != code || (constraint != "" && gotConstraint != constraint) {
		t.Fatalf("error = %v (code %q, constraint %q), want code %s on %s", err, gotCode, gotConstraint, code, constraint)
	}
}

func wantOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestNoTenantColumn covers GI-004, NFR-005 and OT-048: the schema is
// single-tenant and no table carries a tenant key.
func TestNoTenantColumn(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	found := queryStrings(t, pool, `
		SELECT table_name || '.' || column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND (column_name = 'org_id' OR column_name LIKE '%tenant%' OR column_name LIKE 'org\_%')`)
	if len(found) > 0 {
		t.Fatalf("tenant columns found: %v", found)
	}
	var tables int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'users'`).Scan(&tables); err != nil || tables != 1 {
		t.Fatalf("users table missing (count %d, err %v): the check above ran against an empty schema", tables, err)
	}
}

// TestEnumColumnsAcceptExactlyTheProtoValues evaluates each enum column's
// CHECK constraint, as Postgres stores it, against every proto value name and
// some impostors. The accepted set must equal the proto enum's non-zero
// values: the proto enum is the source of truth and the database mirrors it.
func TestEnumColumnsAcceptExactlyTheProtoValues(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	tests := []struct {
		table, column string
		enum          protoreflect.EnumDescriptor
	}{
		{"users", "role", perspectivv1.Role(0).Descriptor()},
		{"places", "kind", perspectivv1.PlaceKind(0).Descriptor()},
		{"places", "source", perspectivv1.PlaceSource(0).Descriptor()},
		{"perspectives", "genre", perspectivv1.Genre(0).Descriptor()},
		{"perspectives", "status", perspectivv1.PerspectiveStatus(0).Descriptor()},
		{"perspectives", "lore_status", perspectivv1.LoreStatus(0).Descriptor()},
		{"claims", "status", perspectivv1.ClaimStatus(0).Descriptor()},
		{"lores", "status", perspectivv1.LoreStanding(0).Descriptor()},
		{"reacts", "emoji", perspectivv1.Emoji(0).Descriptor()},
		{"reports", "target_kind", perspectivv1.ReportTargetKind(0).Descriptor()},
		{"reports", "reason", perspectivv1.ReportReason(0).Descriptor()},
		{"reports", "status", perspectivv1.ReportStatus(0).Descriptor()},
		{"reports", "resolution", perspectivv1.ModerationAction(0).Descriptor()},
		{"moderation_audit", "target_kind", perspectivv1.AuditTargetKind(0).Descriptor()},
		{"moderation_audit", "action", perspectivv1.AuditAction(0).Descriptor()},
		{"notifications", "kind", perspectivv1.NotificationKind(0).Descriptor()},
		{"notification_preferences", "kind", perspectivv1.NotificationKind(0).Descriptor()},
	}
	for _, tt := range tests {
		t.Run(tt.table+"."+tt.column, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var expr string
			err := pool.QueryRow(ctx, `
				SELECT pg_get_expr(c.conbin, c.conrelid) FROM pg_constraint c
				WHERE c.conrelid = $1::regclass AND c.conname = $2 AND c.contype = 'c'`,
				tt.table, tt.table+"_"+tt.column+"_check").Scan(&expr)
			if err != nil {
				t.Fatalf("no CHECK constraint %s_%s_check: %v", tt.table, tt.column, err)
			}
			var candidates, want []string
			vals := tt.enum.Values()
			for i := range vals.Len() {
				name := string(vals.Get(i).Name())
				candidates = append(candidates, name, strings.ToLower(name))
				if vals.Get(i).Number() != 0 {
					want = append(want, name)
				}
			}
			candidates = append(candidates, "BOGUS", "")
			// The table and column names come from the test table above.
			query := fmt.Sprintf(`SELECT %[1]s FROM unnest($1::text[]) AS t(%[1]s) WHERE %[2]s`, tt.column, expr)
			got := queryStrings(t, pool, query, candidates)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("%s.%s accepts %v, want exactly %v", tt.table, tt.column, got, want)
			}
		})
	}
}

// TestArcUniquePerPlaceDayHour covers FR-062: the unique index treats a NULL
// hour as equal, so one place and day has one day arc and one arc per hour.
func TestArcUniquePerPlaceDayHour(t *testing.T) {
	t.Parallel()
	insert := `INSERT INTO arcs (place_id, local_date, hour, parent_arc_id, title, created_by)
		VALUES ($1, $2, $3, $4, 'A real title', $5)`
	tests := []struct {
		name  string
		date  string
		hour  *int
		want  string
		child bool
	}{
		{name: "second day arc on the same day collides", date: "2026-10-11", want: uniqueViolation},
		{name: "day arc on another day is fine", date: "2026-10-12"},
		{name: "first hour arc is fine", date: "2026-10-11", hour: new(15), child: true},
		{name: "another hour is fine", date: "2026-10-11", hour: new(16), child: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			var parent *uuid.UUID
			if tt.child {
				parent = &w.arc
			}
			err := w.exec(insert, w.store, tt.date, tt.hour, parent, w.alice)
			if tt.want != "" {
				wantErr(t, err, tt.want, "arcs_place_date_hour_key")
				return
			}
			wantOK(t, err)
			if tt.hour != nil {
				wantErr(t, w.exec(insert, w.store, tt.date, tt.hour, parent, w.alice), uniqueViolation, "arcs_place_date_hour_key")
			}
		})
	}

	t.Run("concurrent first writers converge on one day arc", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		user := f.user("bob")
		state := f.place("PLACE_KIND_STATE", "Ohio", nil)
		var wg sync.WaitGroup
		errs := make([]error, 8)
		for i := range errs {
			wg.Go(func() {
				errs[i] = f.exec(`INSERT INTO arcs (place_id, local_date, title, created_by)
					VALUES ($1, '2026-10-11', 'A real title', $2)
					ON CONFLICT (place_id, local_date, hour) DO NOTHING`, state, user)
			})
		}
		wg.Wait()
		for _, err := range errs {
			wantOK(t, err)
		}
		var n int
		if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM arcs WHERE place_id = $1`, state).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("day arcs = %d, want 1", n)
		}
	})

	t.Run("an hour arc needs its day arc and a day arc has no parent", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		wantErr(t, w.exec(insert, w.store, "2026-10-11", 9, nil, w.alice), checkViolation, "arcs_hour_parent_check")
		wantErr(t, w.exec(insert, w.store, "2026-10-13", nil, w.arc, w.alice), checkViolation, "arcs_hour_parent_check")
		wantErr(t, w.exec(insert, w.store, "2026-10-11", 24, w.arc, w.alice), checkViolation, "arcs_hour_check")
	})
}

// TestOnePerspectivePerUserPerArc covers FR-016.
func TestOnePerspectivePerUserPerArc(t *testing.T) {
	t.Parallel()
	t.Run("a second published perspective by the same author collides", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		err := w.exec(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
			VALUES ($1, $2, 'GENRE_COMEDY', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())`, w.arc, w.alice)
		wantErr(t, err, uniqueViolation, "perspectives_arc_author_key")
	})
	t.Run("another author in the same arc is fine", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		w.published(w.arc, w.user2())
	})
	t.Run("drafts outside any arc do not collide", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		for range 2 {
			wantOK(t, w.exec(`INSERT INTO perspectives (author_id, place_id, local_date) VALUES ($1, $2, '2026-10-11')`, w.alice, w.store))
		}
	})
}

func (w world) user2() uuid.UUID {
	w.t.Helper()
	return w.user("bob")
}

// TestPerspectiveGenre covers AC-015 and FR-017 in storage: a published
// perspective holds exactly one genre from the enum.
func TestPerspectiveGenre(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		genre      any
		want       string
		constraint string
	}{
		{name: "a genre from the enum", genre: "GENRE_SLICE_OF_LIFE"},
		{name: "no genre", genre: nil, want: checkViolation, constraint: "perspectives_published_check"},
		{name: "a genre outside the enum", genre: "GENRE_WESTERN", want: checkViolation, constraint: "perspectives_genre_check"},
		{name: "the unspecified value", genre: "GENRE_UNSPECIFIED", want: checkViolation, constraint: "perspectives_genre_check"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			err := w.exec(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
				VALUES ($1, $2, $3, 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())`, w.arc, w.user2(), tt.genre)
			if tt.want == "" {
				wantOK(t, err)
				return
			}
			wantErr(t, err, tt.want, tt.constraint)
		})
	}
}

// TestClaimUniqueness checks the two claim rules the schema enforces: one
// PENDING or ACCEPTED claim per character, and one claim per claimant per
// character even after a rejection.
func TestClaimUniqueness(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	bob, carol := w.user("bob"), w.user("carol")
	bobP, carolP := w.published(w.arc, bob), w.published(w.arc, carol)
	char := w.character(w.perspective, 1)
	claim := `INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id) VALUES ($1, $2, $3)`

	wantOK(t, w.exec(claim, char, bob, bobP))
	wantErr(t, w.exec(claim, char, carol, carolP), uniqueViolation, "claims_active_character_key")
	wantOK(t, w.exec(`UPDATE claims SET status = 'CLAIM_STATUS_REJECTED', resolved_at = now() WHERE character_id = $1`, char))
	wantErr(t, w.exec(claim, char, bob, bobP), uniqueViolation, "claims_character_claimant_key")
	wantOK(t, w.exec(claim, char, carol, carolP))
}

// TestOneCanonPerArc checks that an arc holds at most one CANON lore.
func TestOneCanonPerArc(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	lore := `INSERT INTO lores (arc_id, status) VALUES ($1, $2)`
	wantOK(t, w.exec(lore, w.arc, "LORE_STANDING_CANON"))
	wantOK(t, w.exec(lore, w.arc, "LORE_STANDING_SUBLORE"))
	wantErr(t, w.exec(lore, w.arc, "LORE_STANDING_CANON"), uniqueViolation, "lores_one_canon_per_arc_key")
}

// TestAuditTablesAreAppendOnly checks that canon_changes and moderation_audit
// refuse UPDATE and DELETE.
func TestAuditTablesAreAppendOnly(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	wantOK(t, w.exec(`INSERT INTO canon_changes (arc_id) VALUES ($1)`, w.arc))
	wantOK(t, w.exec(`INSERT INTO moderation_audit (action, target_kind, target_id) VALUES ('AUDIT_ACTION_ADMIN_GRANT', 'AUDIT_TARGET_KIND_USER', $1)`, w.alice))
	for _, stmt := range []string{
		`UPDATE canon_changes SET to_score = 1`,
		`DELETE FROM canon_changes`,
		`UPDATE moderation_audit SET reason = 'edited'`,
		`DELETE FROM moderation_audit`,
	} {
		t.Run(stmt, func(t *testing.T) {
			err := w.exec(stmt)
			if code, _ := pgCode(err); code != "42501" {
				t.Fatalf("%s: error %v, want insufficient_privilege", stmt, err)
			}
		})
	}
}

// TestVotesMarkTheArcForTheRankJob checks that adding and clearing an
// up-vote sets the arc's lore_dirty_at.
func TestVotesMarkTheArcForTheRankJob(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	bob := w.user2()
	dirty := func() bool {
		t.Helper()
		var set bool
		if err := w.pool.QueryRow(context.Background(), `SELECT lore_dirty_at IS NOT NULL FROM arcs WHERE id = $1`, w.arc).Scan(&set); err != nil {
			t.Fatal(err)
		}
		return set
	}
	if dirty() {
		t.Fatal("a new arc is already marked dirty")
	}
	wantOK(t, w.exec(`INSERT INTO votes (perspective_id, user_id) VALUES ($1, $2)`, w.perspective, bob))
	if !dirty() {
		t.Fatal("adding a vote did not mark the arc")
	}
	wantOK(t, w.exec(`UPDATE arcs SET lore_dirty_at = NULL WHERE id = $1`, w.arc))
	wantErr(t, w.exec(`INSERT INTO votes (perspective_id, user_id) VALUES ($1, $2)`, w.perspective, bob), uniqueViolation, "votes_pkey")
	wantOK(t, w.exec(`DELETE FROM votes WHERE perspective_id = $1 AND user_id = $2`, w.perspective, bob))
	if !dirty() {
		t.Fatal("clearing a vote did not mark the arc")
	}
}

// TestReactsOneOfEachEmoji checks that a user holds up to one of each emoji.
func TestReactsOneOfEachEmoji(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	bob := w.user2()
	react := `INSERT INTO reacts (perspective_id, user_id, emoji) VALUES ($1, $2, $3)`
	vals := perspectivv1.Emoji(0).Descriptor().Values()
	for i := 1; i < vals.Len(); i++ {
		wantOK(t, w.exec(react, w.perspective, bob, string(vals.Get(i).Name())))
	}
	wantErr(t, w.exec(react, w.perspective, bob, "EMOJI_HEART"), uniqueViolation, "reacts_pkey")
}

// TestNotificationPayloadSchema checks the writer-side validation of the
// notifications payload JSON Schema.
func TestNotificationPayloadSchema(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
		ok      bool
	}{
		{name: "empty object", payload: `{}`, ok: true},
		{name: "known ids", payload: `{"arc_id":"0199e000-0000-7000-8000-000000000001","actor_id":"0199e000-0000-7000-8000-000000000002"}`, ok: true},
		{name: "unknown key", payload: `{"email":"a@b.c"}`},
		{name: "id not a uuid", payload: `{"arc_id":"nope"}`},
		{name: "id not a string", payload: `{"arc_id":7}`},
		{name: "not an object", payload: `["0199e000-0000-7000-8000-000000000001"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			u := f.user("dana")
			err := f.exec(`INSERT INTO notifications (user_id, kind, payload) VALUES ($1, 'NOTIFICATION_KIND_CANON_CHANGE', $2::jsonb)`, u, tt.payload)
			if tt.ok {
				wantOK(t, err)
				return
			}
			wantErr(t, err, checkViolation, "notifications_payload_check")
		})
	}
}

// TestPlaceNamesUniqueUnderAParent checks the dedup rule: case-insensitive,
// whitespace-normalized, per parent; and that only a STATE is a root.
func TestPlaceNamesUniqueUnderAParent(t *testing.T) {
	t.Parallel()
	insert := `INSERT INTO places (kind, name, parent_id, timezone, source, user_added)
		VALUES ($1, $2, $3, 'America/New_York', 'PLACE_SOURCE_USER', true)`
	tests := []struct {
		name, kind, place string
		underCity         bool
		want, constraint  string
	}{
		{name: "same name differing in case and spacing collides", kind: "PLACE_KIND_MALL", place: "  valley   MALL ", underCity: true, want: uniqueViolation, constraint: "places_parent_name_key"},
		{name: "same name under another parent is fine", kind: "PLACE_KIND_MALL", place: "Valley Mall"},
		{name: "a second state with one name collides", kind: "PLACE_KIND_STATE", place: "new york", want: uniqueViolation, constraint: "places_parent_name_key"},
		{name: "a city without a parent is refused", kind: "PLACE_KIND_CITY", place: "Nowhere", want: checkViolation, constraint: "places_root_is_state_check"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			state := f.place("PLACE_KIND_STATE", "New York", nil)
			city := f.place("PLACE_KIND_CITY", "Albany", &state)
			other := f.place("PLACE_KIND_CITY", "Troy", &state)
			wantOK(t, f.exec(insert, "PLACE_KIND_MALL", "Valley Mall", city))
			var parent *uuid.UUID
			switch {
			case tt.kind == "PLACE_KIND_STATE" || tt.constraint == "places_root_is_state_check":
			case tt.underCity:
				parent = &city
			default:
				parent = &other
			}
			err := f.exec(insert, tt.kind, tt.place, parent)
			if tt.want == "" {
				wantOK(t, err)
				return
			}
			wantErr(t, err, tt.want, tt.constraint)
		})
	}
}

// TestUsernameAndEmailAreCaseInsensitivelyUnique checks the account keys.
func TestUsernameAndEmailAreCaseInsensitivelyUnique(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, email, username, constraint string
	}{
		{name: "username differing only by case", email: "other@example.test", username: "ALICE", constraint: "users_username_lower_key"},
		{name: "email differing only by case", email: "ALICE@example.test", username: "other", constraint: "users_email_lower_key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.user("alice")
			err := f.exec(`INSERT INTO users (email, password_hash, username, age_confirmed_at) VALUES ($1, 'x', $2, now())`, tt.email, tt.username)
			wantErr(t, err, uniqueViolation, tt.constraint)
		})
	}
}

// TestHotQueriesAreIndexBacked covers the schema side of NFR-008: with
// sequential scans priced out, the epic, arc page and inbox reads run on
// indexes, so their cost tracks the page size rather than the table size.
func TestHotQueriesAreIndexBacked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name: "place descendants by recursive CTE",
			query: `WITH RECURSIVE sub AS (
					SELECT id FROM places WHERE id = $1
					UNION ALL SELECT p.id FROM places p JOIN sub ON p.parent_id = sub.id)
				SELECT id FROM sub`,
			args: []any{uuid.New()},
		},
		{
			name: "epic ranked page",
			query: `SELECT id FROM arcs WHERE place_id = ANY($1::uuid[]) AND parent_arc_id IS NULL
				ORDER BY (canon_lore_id IS NOT NULL) DESC, canon_score DESC, member_count DESC, created_at DESC, id DESC LIMIT 20`,
			args: []any{[]uuid.UUID{uuid.New(), uuid.New()}},
		},
		{
			name: "epic newest page after a cursor",
			query: `SELECT id FROM arcs WHERE place_id = ANY($1::uuid[]) AND (created_at, id) < ($2, $3)
				ORDER BY created_at DESC, id DESC LIMIT 20`,
			args: []any{[]uuid.UUID{uuid.New()}, "2026-10-11T00:00:00Z", uuid.New()},
		},
		{
			name: "epic tales page",
			query: `SELECT id FROM perspectives WHERE arc_id = ANY($1::uuid[])
				AND lore_status IN ('LORE_STATUS_SOLO', 'LORE_STATUS_REMOVED')
				ORDER BY published_at DESC, id DESC LIMIT 20`,
			args: []any{[]uuid.UUID{uuid.New()}},
		},
		{
			name:  "arc page perspectives",
			query: `SELECT id, lore_status FROM perspectives WHERE arc_id = $1 ORDER BY published_at DESC`,
			args:  []any{uuid.New()},
		},
		{
			name:  "arc page lores",
			query: `SELECT id FROM lores WHERE arc_id = $1 ORDER BY rank_score DESC, id`,
			args:  []any{uuid.New()},
		},
		{
			name:  "day arc hour children",
			query: `SELECT id FROM arcs WHERE parent_arc_id = $1 ORDER BY hour`,
			args:  []any{uuid.New()},
		},
		{
			name: "inbox page after a cursor",
			query: `SELECT id FROM notifications WHERE user_id = $1 AND (created_at, id) < ($2, $3)
				ORDER BY created_at DESC, id DESC LIMIT 20`,
			args: []any{uuid.New(), "2026-10-11T00:00:00Z", uuid.New()},
		},
		{
			name:  "unread count",
			query: `SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`,
			args:  []any{uuid.New()},
		},
		{
			name:  "rank job work queue",
			query: `SELECT id FROM arcs WHERE lore_dirty_at IS NOT NULL ORDER BY lore_dirty_at LIMIT 100`,
		},
		{
			name:  "place typeahead",
			query: `SELECT id FROM places WHERE lower(name) LIKE '%' || lower($1) || '%' LIMIT 20`,
			args:  []any{"mall"},
		},
	}
	pool := testdb.New(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
			if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
				t.Fatal(err)
			}
			plan := queryStrings(t, tx, "EXPLAIN "+tt.query, tt.args...)
			text := strings.Join(plan, "\n")
			if strings.Contains(text, "Seq Scan") || !strings.Contains(text, "Index") {
				t.Fatalf("plan is not index-backed:\n%s", text)
			}
		})
	}
}

// TestEachTestGetsItsOwnDatabase checks the harness's isolation: two tests
// writing the same unique row both succeed because each has its own database.
func TestEachTestGetsItsOwnDatabase(t *testing.T) {
	t.Parallel()
	for i := range 2 {
		t.Run(fmt.Sprintf("database %d", i), func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.user("same_name")
			var n int
			if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("users = %d, want 1: databases are shared", n)
			}
		})
	}
}
