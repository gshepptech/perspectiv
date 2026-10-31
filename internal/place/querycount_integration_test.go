//go:build integration

package place_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/telemetry"
	"perspectiv/internal/place"
)

// queryLog records the name of every statement a pool sends ("unnamed" for
// one without a sqlc name), so a test can count a call's round trips.
type queryLog struct {
	mu    sync.Mutex
	names []string
}

func (l *queryLog) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, telemetry.QueryName(data.SQL))
	return ctx
}

func (l *queryLog) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// take returns the names recorded since the last take and starts afresh.
func (l *queryLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := l.names
	l.names = nil
	return names
}

// countingHandler is a place handler on a second pool over the same test
// database whose every statement lands in the returned log.
func countingHandler(t *testing.T, pool *pgxpool.Pool) (*place.Handler, *queryLog) {
	t.Helper()
	log := &queryLog{}
	cfg := pool.Config()
	cfg.ConnConfig.Tracer = log
	counted, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("counting pool: %v", err)
	}
	t.Cleanup(counted.Close)
	return place.NewHandler(place.NewService(place.NewStore(counted))), log
}

// A place page's reads send a fixed number of statements. Reading one place
// checks it is visible and reads its ancestors in the same statement, and a
// place tab of the epic reads the place in the same statement as its
// listing, whether or not it lists anything. A change that adds a round trip
// to one of them fails here and has to say why the page needs it.
func TestPlaceReads_QueryCount(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	ctx := context.Background()
	reader := h.user(true)
	// A day with a tale under the town, so its tabs list something; the
	// other city lists nothing.
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tale := h.arc(h.store, "2026-10-01", "A day at the bookshop", reader, at)
	h.perspective(tale, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, at, "words")
	signedIn := apiutil.WithCaller(ctx, apiutil.Caller{UserID: reader, Role: perspectivv1.Role_ROLE_USER})

	handler, log := countingHandler(t, h.pool)
	getPlace := func(ctx context.Context, id string) func() error {
		return func() error {
			_, err := handler.GetPlace(ctx, connect.NewRequest(&perspectivv1.GetPlaceRequest{PlaceId: id}))
			return err
		}
	}
	getEpic := func(id uuid.UUID, tab perspectivv1.EpicTab, listed int) func() error {
		return func() error {
			res, err := handler.GetEpic(ctx, connect.NewRequest(&perspectivv1.GetEpicRequest{PlaceId: id.String(), Tab: tab}))
			if err != nil {
				return err
			}
			if got := len(res.Msg.GetArcs()) + len(res.Msg.GetTales()); got != listed || res.Msg.GetPlace().GetId() != id.String() {
				return fmt.Errorf("listed %d with place %q, want %d with %s", got, res.Msg.GetPlace().GetId(), listed, id)
			}
			return nil
		}
	}
	tests := []struct {
		name string
		call func() error
		want []string
	}{
		{
			name: "GetPlace town signed out",
			call: getPlace(ctx, h.town.String()),
			want: []string{"GetVisiblePlace"},
		},
		{
			name: "GetPlace store signed out",
			call: getPlace(ctx, h.store.String()),
			want: []string{"GetVisiblePlace", "TwinCandidates"},
		},
		{
			name: "GetPlace store signed in",
			call: getPlace(signedIn, h.store.String()),
			want: []string{"GetVisiblePlace", "IsFollowing", "TwinCandidates"},
		},
		{
			name: "GetEpic place ranked",
			call: getEpic(h.town, perspectivv1.EpicTab_EPIC_TAB_RANKED, 1),
			want: []string{"EpicArcsRanked"},
		},
		{
			name: "GetEpic place newest",
			call: getEpic(h.town, perspectivv1.EpicTab_EPIC_TAB_NEWEST, 1),
			want: []string{"EpicArcsNewest"},
		},
		{
			name: "GetEpic place tales",
			call: getEpic(h.town, perspectivv1.EpicTab_EPIC_TAB_TALES, 1),
			want: []string{"EpicTales"},
		},
		{
			name: "GetEpic place ranked, nothing listed",
			call: getEpic(h.otherCity, perspectivv1.EpicTab_EPIC_TAB_RANKED, 0),
			want: []string{"EpicArcsRanked"},
		},
		{
			name: "GetEpic place tales, nothing listed",
			call: getEpic(h.otherCity, perspectivv1.EpicTab_EPIC_TAB_TALES, 0),
			want: []string{"EpicTales"},
		},
		{
			name: "SearchPlaces under a parent",
			call: func() error {
				_, err := handler.SearchPlaces(ctx, connect.NewRequest(&perspectivv1.SearchPlacesRequest{Query: "co", ParentId: h.town.String()}))
				return err
			},
			want: []string{"GetVisiblePlace", "SearchPlaces", "ListAncestors"},
		},
	}
	// The cases share one counting pool, so they run in order.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log.take()
			if err := tc.call(); err != nil {
				t.Fatalf("call: %v", err)
			}
			got := log.take()
			if strings.Join(got, ", ") != strings.Join(tc.want, ", ") {
				t.Fatalf("sent %d statements (%s), want %d (%s)",
					len(got), strings.Join(got, ", "), len(tc.want), strings.Join(tc.want, ", "))
			}
		})
	}
}
