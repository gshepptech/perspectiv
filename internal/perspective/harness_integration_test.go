//go:build integration

package perspective_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/arc"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore"
	"perspectiv/internal/perspective"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// expiryWindow is the window the test service schedules expiry jobs with.
const expiryWindow = 72 * time.Hour

// day is the place-local date most tests publish on.
const day = "2026-10-15"

// world is one test database with a place, the perspective service and its
// handler served over HTTP. A request names its caller in the X-Test-User
// header, which a test interceptor turns into the context caller the
// production authentication interceptor would set.
type world struct {
	t       *testing.T
	pool    *pgxpool.Pool
	metrics *metrics.Metrics
	client  perspectivv1connect.PerspectiveServiceClient
	place   uuid.UUID
	users   int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	pool := testdb.New(t)
	m := metrics.New(prometheus.NewRegistry())
	river, err := jobs.NewClient(pool, zerolog.Nop(), m, nil, nil)
	if err != nil {
		t.Fatalf("river client: %v", err)
	}
	enq := jobs.NewEnqueuer(river)
	svc := perspective.NewService(pool, arc.NewService(pool), lore.NewService(pool, enq, m), enq, m, expiryWindow)

	w := &world{t: t, pool: pool, metrics: m}
	path, handler := perspective.NewHandler(svc).Mount(connect.WithInterceptors(testCaller()))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	w.client = perspectivv1connect.NewPerspectiveServiceClient(srv.Client(), srv.URL)

	state := w.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', 'Ohio', 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`)
	w.place = w.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_STORE', 'Joe''s Diner', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, state)
	return w
}

func testCaller() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if id := req.Header().Get("X-Test-User"); id != "" {
				ctx = apiutil.WithCaller(ctx, apiutil.Caller{UserID: uuid.MustParse(id), Role: perspectivv1.Role_ROLE_USER})
			}
			return next(ctx, req)
		}
	}
}

func (w *world) id(query string, args ...any) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(), query, args...).Scan(&id); err != nil {
		w.t.Fatalf("%s: %v", query, err)
	}
	return id
}

func (w *world) exec(query string, args ...any) {
	w.t.Helper()
	if _, err := w.pool.Exec(context.Background(), query, args...); err != nil {
		w.t.Fatalf("%s: %v", query, err)
	}
}

func (w *world) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		w.t.Fatalf("%s: %v", query, err)
	}
	return n
}

// user creates a verified user.
func (w *world) user() uuid.UUID {
	w.t.Helper()
	w.users++
	name := fmt.Sprintf("writer%d", w.users)
	return w.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at, email_verified_at)
		VALUES ($1, 'x', $2, now(), now()) RETURNING id`, name+"@example.test", name)
}

// unverifiedUser creates a user who has not verified their email.
func (w *world) unverifiedUser() uuid.UUID {
	w.t.Helper()
	w.users++
	name := fmt.Sprintf("newbie%d", w.users)
	return w.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at)
		VALUES ($1, 'x', $2, now()) RETURNING id`, name+"@example.test", name)
}

func as[T any](user uuid.UUID, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	if user != uuid.Nil {
		req.Header().Set("X-Test-User", user.String())
	}
	return req
}

func codeOf(err error) connect.Code {
	if err == nil {
		return 0
	}
	return connect.CodeOf(err)
}

func hourPtr(h int32) *int32 { return &h }

// words returns a part body of n words.
func words(n int) string {
	return strings.TrimSpace(strings.Repeat("word ", n))
}

// publishReq is a valid publish at the world's place on day, as a new arc.
func (w *world) publishReq() *perspectivv1.PublishRequest {
	return &perspectivv1.PublishRequest{
		PlaceId:       w.place.String(),
		LocalDate:     day,
		TitleIfNewArc: "The night the fryer caught fire",
		Genre:         perspectivv1.Genre_GENRE_DRAMA,
		Parts:         []string{"I ordered pancakes at midnight."},
	}
}

// publish publishes req as user and fails the test on an error.
func (w *world) publish(user uuid.UUID, req *perspectivv1.PublishRequest) *perspectivv1.PublishResponse {
	w.t.Helper()
	resp, err := w.client.Publish(context.Background(), as(user, req))
	if err != nil {
		w.t.Fatalf("Publish: %v", err)
	}
	return resp.Msg
}

func (w *world) get(user uuid.UUID, id string) *perspectivv1.Perspective {
	w.t.Helper()
	resp, err := w.client.Get(context.Background(), as(user, &perspectivv1.PerspectiveServiceGetRequest{PerspectiveId: id}))
	if err != nil {
		w.t.Fatalf("Get(%s): %v", id, err)
	}
	return resp.Msg.GetPerspective()
}

// claim records a claim with status by claimant's perspective on a character.
func (w *world) claim(characterID string, claimantPerspective string, status string) uuid.UUID {
	w.t.Helper()
	return w.id(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status, resolved_at)
		SELECT $1, author_id, id, $3, CASE WHEN $3 = 'CLAIM_STATUS_PENDING' THEN NULL ELSE now() END
		FROM perspectives WHERE id = $2 RETURNING id`, characterID, claimantPerspective, status)
}
