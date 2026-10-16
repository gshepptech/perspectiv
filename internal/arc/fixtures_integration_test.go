//go:build integration

package arc_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc"
)

// day is the place-local date most tests publish on.
var day = time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)

// world is one test database with a place chain Ohio > Columbus > Joe's Diner.
type world struct {
	pool               *pgxpool.Pool
	svc                *arc.Service
	state, city, diner uuid.UUID
	users              int
}

func newWorld(t *testing.T, pool *pgxpool.Pool) *world {
	t.Helper()
	w := &world{pool: pool, svc: arc.NewService(pool)}
	w.state = w.place(t, "PLACE_KIND_STATE", "Ohio", uuid.Nil)
	w.city = w.place(t, "PLACE_KIND_CITY", "Columbus", w.state)
	w.diner = w.place(t, "PLACE_KIND_STORE", "Joe's Diner", w.city)
	return w
}

func (w *world) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := w.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func (w *world) id(t *testing.T, sql string, args ...any) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return id
}

func (w *world) place(t *testing.T, kind, name string, parent uuid.UUID) uuid.UUID {
	t.Helper()
	p := uuid.NullUUID{UUID: parent, Valid: parent != uuid.Nil}
	return w.id(t, `INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ($1, $2, $3, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, kind, name, p)
}

func (w *world) user(t *testing.T) uuid.UUID {
	t.Helper()
	w.users++
	name := fmt.Sprintf("writer%d", w.users)
	return w.id(t, `INSERT INTO users (email, password_hash, username, age_confirmed_at, email_verified_at)
		VALUES ($1, 'x', $2, now(), now()) RETURNING id`, name+"@example.test", name)
}

// resolve runs ResolveForPublish in its own committed transaction.
func (w *world) resolve(t *testing.T, in arc.ResolveInput) (uuid.UUID, bool, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	id, created, err := w.svc.ResolveForPublish(ctx, tx, in)
	if err != nil {
		return id, created, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id, created, nil
}

func (w *world) mustResolve(t *testing.T, in arc.ResolveInput) uuid.UUID {
	t.Helper()
	id, _, err := w.resolve(t, in)
	if err != nil {
		t.Fatalf("ResolveForPublish(%+v): %v", in, err)
	}
	return id
}

func (w *world) arcCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), `SELECT count(*) FROM arcs`).Scan(&n); err != nil {
		t.Fatalf("count arcs: %v", err)
	}
	return n
}

// perspective publishes a perspective by a new author directly in the table,
// as the perspective domain would, with one part holding body.
type perspectiveOpts struct {
	loreStatus string
	body       string
	deleted    bool
	hidden     bool
	publishAt  time.Time
}

func (w *world) perspective(t *testing.T, arcID uuid.UUID, o perspectiveOpts) uuid.UUID {
	t.Helper()
	if o.body == "" {
		o.body = "It was a night like any other."
	}
	if o.publishAt.IsZero() {
		o.publishAt = time.Now()
	}
	author := w.user(t)
	id := w.id(t, `INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', $3, $4) RETURNING id`,
		arcID, author, o.loreStatus, o.publishAt)
	w.exec(t, `INSERT INTO perspective_parts (perspective_id, position, body) VALUES ($1, 1, $2)`, id, o.body)
	if o.deleted {
		w.exec(t, `UPDATE perspectives SET deleted_at = now() WHERE id = $1`, id)
	}
	if o.hidden {
		w.exec(t, `UPDATE perspectives SET hidden_at = now() WHERE id = $1`, id)
	}
	return id
}

// lore writes a lore with the given members as the rank job would leave it.
func (w *world) lore(t *testing.T, arcID uuid.UUID, standing string, rank float64, members ...uuid.UUID) uuid.UUID {
	t.Helper()
	id := w.id(t, `INSERT INTO lores (arc_id, status, member_count, rank_score, computed_at)
		VALUES ($1, $2, $3, $4, now()) RETURNING id`, arcID, standing, len(members), rank)
	for _, m := range members {
		w.exec(t, `INSERT INTO lore_members (lore_id, perspective_id) VALUES ($1, $2)`, id, m)
		w.exec(t, `UPDATE perspectives SET lore_id = $1 WHERE id = $2`, id, m)
	}
	if standing == "LORE_STANDING_CANON" {
		w.exec(t, `UPDATE arcs SET canon_lore_id = $1, canon_score = $2, member_count = $3 WHERE id = $4`,
			id, rank, len(members), arcID)
	}
	return id
}

// link records an accepted claim: claimant's author says a character in p was them.
func (w *world) link(t *testing.T, p, claimant uuid.UUID) uuid.UUID {
	t.Helper()
	ch := w.id(t, `INSERT INTO characters (perspective_id, label, position)
		VALUES ($1, 'the cook', 1 + (SELECT count(*) FROM characters WHERE perspective_id = $1)) RETURNING id`, p)
	return w.id(t, `INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status, resolved_at)
		SELECT $1, author_id, id, 'CLAIM_STATUS_ACCEPTED', now() FROM perspectives WHERE id = $2 RETURNING id`, ch, claimant)
}

// client serves the arc handler and returns a client for it. A request whose
// X-Test-User header holds a user id runs as that signed-in user.
func (w *world) client(t *testing.T) perspectivv1connect.ArcServiceClient {
	t.Helper()
	asCaller := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if raw := req.Header().Get("X-Test-User"); raw != "" {
				ctx = apiutil.WithCaller(ctx, apiutil.Caller{
					UserID: uuid.MustParse(raw), Role: perspectivv1.Role_ROLE_USER, SessionID: uuid.New(),
				})
			}
			return next(ctx, req)
		}
	})
	path, h := arc.NewHandler(w.svc).Mount(connect.WithInterceptors(asCaller))
	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return perspectivv1connect.NewArcServiceClient(srv.Client(), srv.URL)
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
