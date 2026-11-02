//go:build integration

package main

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"perspectiv/internal/api/metrics"
	"perspectiv/internal/arc"
	"perspectiv/internal/lore"
	"perspectiv/internal/perspective"
	"perspectiv/internal/place"
	"perspectiv/internal/testdb"
)

// insert runs one INSERT ... RETURNING id.
func insert(t *testing.T, pool *pgxpool.Pool, query string, args ...any) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return id
}

// The previewer names what a signed-out reader may open, from the same
// public reads, and nothing else: hidden places, hidden stories and drafts
// have no preview.
func TestPreview(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	m := metrics.New(prometheus.NewRegistry())
	arcs := arc.NewService(pool)
	lores := lore.NewService(pool, nil, m)
	p := previewer{
		arcs: arcs, lores: lores, places: place.NewService(place.NewStore(pool)),
		perspectives: perspective.NewService(pool, arcs, lores, nil, m, time.Hour),
	}
	user := func(name string) uuid.UUID {
		return insert(t, pool, `INSERT INTO users (email, email_verified_at, password_hash, username, age_confirmed_at)
			VALUES ($1 || '@example.com', now(), 'x', $1, now()) RETURNING id`, name)
	}
	ann := user("ann")
	state := insert(t, pool, `INSERT INTO places (kind, name, timezone, source) VALUES ('PLACE_KIND_STATE', 'Illinois', 'America/Chicago', 'PLACE_SOURCE_OVERTURE') RETURNING id`)
	city := insert(t, pool, `INSERT INTO places (kind, name, parent_id, timezone, source) VALUES ('PLACE_KIND_CITY', 'Springfield', $1, 'America/Chicago', 'PLACE_SOURCE_OVERTURE') RETURNING id`, state)
	store := insert(t, pool, `INSERT INTO places (kind, name, parent_id, timezone, source, category) VALUES ('PLACE_KIND_STORE', 'Corner Books', $1, 'America/Chicago', 'PLACE_SOURCE_OVERTURE', 'PLACE_CATEGORY_SHOPPING') RETURNING id`, city)
	hiddenPlace := insert(t, pool, `INSERT INTO places (kind, name, parent_id, timezone, source, hidden_at, category) VALUES ('PLACE_KIND_STORE', 'Gone Books', $1, 'America/Chicago', 'PLACE_SOURCE_OVERTURE', now(), 'PLACE_CATEGORY_LODGING') RETURNING id`, city)
	arcID := insert(t, pool, `INSERT INTO arcs (place_id, local_date, title, created_by) VALUES ($1, '2026-10-03', 'Rain on the windows', $2) RETURNING id`, store, ann)
	visible := insert(t, pool, `INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now()) RETURNING id`, arcID, ann)
	if _, err := pool.Exec(context.Background(), `INSERT INTO perspective_parts (perspective_id, position, body)
		VALUES ($1, 1, 'The shop filled up with strangers.')`, visible); err != nil {
		t.Fatal(err)
	}
	hiddenStory := insert(t, pool, `INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at, hidden_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now(), now()) RETURNING id`, arcID, user("bob"))
	cal := user("cal")
	draft := insert(t, pool, `INSERT INTO perspectives (author_id, genre, status)
		VALUES ($1, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_DRAFT') RETURNING id`, cal)
	loreID := insert(t, pool, `INSERT INTO lores (arc_id, name, named_by, member_count) VALUES ($1, 'The rainy regulars', $2, 4) RETURNING id`, arcID, ann)
	// A readable story at the hidden place, whose category has no page.
	hiddenArc := insert(t, pool, `INSERT INTO arcs (place_id, local_date, title, created_by) VALUES ($1, '2026-10-03', 'Closing day', $2) RETURNING id`, hiddenPlace, ann)
	insert(t, pool, `INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now()) RETURNING id`, hiddenArc, cal)

	// The sitemap lists the same readable pages: the guide, the store's category, the
	// store and every place above it, the day, and the visible story; never
	// the hidden place or its category, the hidden story or the draft.
	checkSitemap(t, sitemap{
		places: p.places.Sitemap, days: arcs.Sitemap, stories: p.perspectives.Sitemap, categories: p.places.CategorySitemap,
	}, map[string][]string{
		"pages":   {"/how-it-works", "/best/shopping"},
		"places":  {"/places/" + store.String(), "/places/" + city.String(), "/places/" + state.String()},
		"days":    {"/arcs/" + arcID.String()},
		"stories": {"/perspectives/" + visible.String()},
	})

	tests := []struct {
		name      string
		path      string
		wantTitle string
		wantDesc  string
	}{
		{name: "arc", path: "/arcs/" + arcID.String(), wantTitle: "Rain on the windows", wantDesc: "at Corner Books, Springfield on October 3, 2026"},
		{name: "story", path: "/perspectives/" + visible.String(), wantTitle: "Rain on the windows · ann's side", wantDesc: "“The shop filled up with strangers.”"},
		{name: "named lore", path: "/lores/" + loreID.String(), wantTitle: "The rainy regulars", wantDesc: "4 people at Corner Books, Springfield"},
		{name: "place", path: "/places/" + store.String(), wantTitle: "Corner Books", wantDesc: "in Springfield, Illinois"},
		{name: "hidden place", path: "/places/" + hiddenPlace.String()},
		{name: "hidden story", path: "/perspectives/" + hiddenStory.String()},
		{name: "draft", path: "/perspectives/" + draft.String()},
		{name: "missing arc", path: "/arcs/" + uuid.NewString()},
		{name: "not an id", path: "/arcs/abc"},
		{name: "another page", path: "/claims"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := p.preview(context.Background(), tc.path)
			if tc.wantTitle == "" {
				if ok {
					t.Fatalf("preview = %+v, want none", got)
				}
				return
			}
			if !ok || got.Title != tc.wantTitle || !strings.Contains(got.Description, tc.wantDesc) {
				t.Fatalf("preview = %+v, %v; want title %q and a description with %q", got, ok, tc.wantTitle, tc.wantDesc)
			}
		})
	}
}

// checkSitemap asserts each sitemap kind lists exactly the wanted paths, in
// any order, each with a lastmod.
func checkSitemap(t *testing.T, sm sitemap, wantPages map[string][]string) {
	t.Helper()
	for kind, want := range wantPages {
		pages, err := sm.pages(context.Background(), kind)
		if err != nil {
			t.Fatalf("sitemap %s: %v", kind, err)
		}
		got := make([]string, len(pages))
		for i, pg := range pages {
			got[i] = pg.Path
			if pg.LastMod.IsZero() {
				t.Errorf("sitemap %s: %s has no lastmod", kind, pg.Path)
			}
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("sitemap %s = %v, want %v", kind, got, want)
		}
	}
}
