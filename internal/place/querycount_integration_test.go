//go:build integration

package place_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
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
// checks it is visible and reads its ancestors in the same statement, so a
// change that adds a round trip to one of them fails here and has to say
// why the page needs it.
func TestPlaceReads_QueryCount(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	ctx := context.Background()
	reader := h.user(true)
	signedIn := apiutil.WithCaller(ctx, apiutil.Caller{UserID: reader, Role: perspectivv1.Role_ROLE_USER})

	handler, log := countingHandler(t, h.pool)
	getPlace := func(ctx context.Context, id string) func() error {
		return func() error {
			_, err := handler.GetPlace(ctx, connect.NewRequest(&perspectivv1.GetPlaceRequest{PlaceId: id}))
			return err
		}
	}
	getEpic := func(tab perspectivv1.EpicTab) func() error {
		return func() error {
			_, err := handler.GetEpic(ctx, connect.NewRequest(&perspectivv1.GetEpicRequest{PlaceId: h.town.String(), Tab: tab}))
			return err
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
			call: getEpic(perspectivv1.EpicTab_EPIC_TAB_RANKED),
			want: []string{"GetVisiblePlace", "EpicArcsRanked"},
		},
		{
			name: "GetEpic place newest",
			call: getEpic(perspectivv1.EpicTab_EPIC_TAB_NEWEST),
			want: []string{"GetVisiblePlace", "EpicArcsNewest"},
		},
		{
			name: "GetEpic place tales",
			call: getEpic(perspectivv1.EpicTab_EPIC_TAB_TALES),
			want: []string{"GetVisiblePlace", "EpicTales"},
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
