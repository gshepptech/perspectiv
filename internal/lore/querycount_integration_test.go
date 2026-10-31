//go:build integration

package lore_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/api/telemetry"
	"perspectiv/internal/lore"
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

// countingService is a lore service on a second pool over the same test
// database whose every statement lands in the returned log. The reads it
// serves send no notifications.
func countingService(t *testing.T, pool *pgxpool.Pool) (*lore.Service, *queryLog) {
	t.Helper()
	log := &queryLog{}
	cfg := pool.Config()
	cfg.ConnConfig.Tracer = log
	counted, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("counting pool: %v", err)
	}
	t.Cleanup(counted.Close)
	return lore.NewService(counted, nil, metrics.New(prometheus.NewRegistry())), log
}

// A lore page's reads send a fixed number of statements. Reading the lore
// checks its arc's place is not hidden in the same statement (GetLore
// carries the flag), so a change that adds a round trip to one of them
// fails here and has to say why the page needs it. The lore has two members
// linked by an encounter and a thread with one post and one reply.
func TestLoreReads_QueryCount(t *testing.T) {
	t.Parallel()
	th := newThread(t)
	ctx := context.Background()
	post := th.mustAdd(th.ana.user, "Who else saw the fryer go up?", "")
	th.mustAdd(th.ben.user, "I did, from the counter.", post.GetId())
	loreID := uuid.MustParse(th.lore)
	postID := uuid.MustParse(post.GetId())

	svc, log := countingService(t, th.pool)
	tests := []struct {
		name string
		call func() error
		want []string
	}{
		{
			name: "GetLore",
			call: func() error {
				_, err := svc.GetLore(ctx, loreID)
				return err
			},
			want: []string{"GetLore", "ListLoreMembers", "ListLoreEncounters"},
		},
		{
			name: "ListLorePosts signed out",
			call: func() error {
				_, _, err := svc.LorePosts(ctx, uuid.Nil, loreID, perspectivv1.LorePostSort_LORE_POST_SORT_TOP, "", 10)
				return err
			},
			want: []string{"GetLore", "ListThreadTop", "ListFirstReplies"},
		},
		{
			name: "ListLorePosts newest signed in",
			call: func() error {
				_, _, err := svc.LorePosts(ctx, th.ben.user, loreID, perspectivv1.LorePostSort_LORE_POST_SORT_NEW, "", 10)
				return err
			},
			want: []string{"GetLore", "ListThreadNew", "ListFirstReplies", "ViewerUpvoted"},
		},
		{
			name: "ListLorePostReplies signed out",
			call: func() error {
				_, _, err := svc.LorePostReplies(ctx, uuid.Nil, postID, "", 10)
				return err
			},
			want: []string{"GetThreadPost", "GetLore", "ListReplies"},
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
