//go:build integration

package lore_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

const (
	statusPending = "LORE_STATUS_PENDING"
	statusInLore  = "LORE_STATUS_IN_LORE"
	statusSolo    = "LORE_STATUS_SOLO"
	statusRemoved = "LORE_STATUS_REMOVED"

	claimPending  = "CLAIM_STATUS_PENDING"
	claimAccepted = "CLAIM_STATUS_ACCEPTED"
	claimRejected = "CLAIM_STATUS_REJECTED"
)

// world is one test database with a place, one arc at it, the lore service
// and its handler served over HTTP. A test request names its caller in the
// X-Test-User header, which a test interceptor turns into the context caller
// the production authentication interceptor would set.
type world struct {
	t       *testing.T
	pool    *pgxpool.Pool
	svc     *lore.Service
	metrics *metrics.Metrics
	client  perspectivv1connect.LoreServiceClient
	place   uuid.UUID
	arc     uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	pool := testdb.New(t)
	river, err := jobs.NewClient(pool, zerolog.Nop(), metrics.New(prometheus.NewRegistry()), nil, nil)
	if err != nil {
		t.Fatalf("river client: %v", err)
	}
	m := metrics.New(prometheus.NewRegistry())
	w := &world{t: t, pool: pool, metrics: m, svc: lore.NewService(pool, jobs.NewEnqueuer(river), m)}

	path, handler := lore.NewHandler(w.svc).Mount(connect.WithInterceptors(testCaller()))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	w.client = perspectivv1connect.NewLoreServiceClient(srv.Client(), srv.URL)

	owner := w.user("founder")
	w.place = w.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', 'Ohio', 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`)
	w.arc = w.newArc("2026-10-01", owner)
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

func (w *world) text(query string, args ...any) string {
	w.t.Helper()
	var s *string
	if err := w.pool.QueryRow(context.Background(), query, args...).Scan(&s); err != nil {
		w.t.Fatalf("%s: %v", query, err)
	}
	if s == nil {
		return ""
	}
	return *s
}

func (w *world) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		w.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (w *world) newArc(day string, by uuid.UUID) uuid.UUID {
	w.t.Helper()
	return w.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, $2, 'The night the fryer caught fire', $3) RETURNING id`, w.place, day, by)
}

func (w *world) user(name string) uuid.UUID {
	w.t.Helper()
	return w.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at, email_verified_at)
		VALUES ($1, 'x', $2, now(), now()) RETURNING id`, name+"@example.test", name)
}

// published inserts a published perspective by author in arc with the given
// lore status.
func (w *world) published(arc, author uuid.UUID, loreStatus string) uuid.UUID {
	w.t.Helper()
	return w.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', $3, now()) RETURNING id`,
		arc, author, loreStatus)
}

func (w *world) character(perspective uuid.UUID, label string) uuid.UUID {
	w.t.Helper()
	pos := w.count(`SELECT count(*) + 1 FROM characters WHERE perspective_id = $1`, perspective)
	return w.id(`INSERT INTO characters (perspective_id, label, position) VALUES ($1, $2, $3) RETURNING id`,
		perspective, label, pos)
}

// author is a user with a PENDING perspective in the world's arc and one
// character in it.
type author struct {
	user, perspective, character uuid.UUID
}

func (w *world) author(name string) author {
	w.t.Helper()
	u := w.user(name)
	p := w.published(w.arc, u, statusPending)
	return author{user: u, perspective: p, character: w.character(p, "the one who "+name+" saw")}
}

func asUser[T any](user uuid.UUID, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	if user != uuid.Nil {
		req.Header().Set("X-Test-User", user.String())
	}
	return req
}

func (w *world) claim(caller, character uuid.UUID) (string, error) {
	w.t.Helper()
	resp, err := w.client.ClaimCharacter(context.Background(),
		asUser(caller, &perspectivv1.ClaimCharacterRequest{CharacterId: character.String()}))
	if err != nil {
		return "", err
	}
	if got := resp.Msg.GetStatus(); got != perspectivv1.ClaimStatus_CLAIM_STATUS_PENDING {
		w.t.Fatalf("claim status = %v, want PENDING", got)
	}
	return resp.Msg.GetClaimId(), nil
}

func (w *world) mustClaim(caller, character uuid.UUID) string {
	w.t.Helper()
	id, err := w.claim(caller, character)
	if err != nil {
		w.t.Fatalf("claim: %v", err)
	}
	return id
}

func (w *world) resolve(caller uuid.UUID, claimID string, accept bool) (perspectivv1.ClaimStatus, error) {
	w.t.Helper()
	resp, err := w.client.ResolveClaim(context.Background(),
		asUser(caller, &perspectivv1.ResolveClaimRequest{ClaimId: claimID, Accept: accept}))
	if err != nil {
		return 0, err
	}
	return resp.Msg.GetStatus(), nil
}

// link has b claim a's character and a accept it.
func (w *world) link(a, b author) string {
	w.t.Helper()
	id := w.mustClaim(b.user, a.character)
	if _, err := w.resolve(a.user, id, true); err != nil {
		w.t.Fatalf("accept: %v", err)
	}
	return id
}

func (w *world) loreStatus(perspective uuid.UUID) string {
	w.t.Helper()
	return w.text(`SELECT lore_status FROM perspectives WHERE id = $1`, perspective)
}

func (w *world) claimStatus(claimID string) string {
	w.t.Helper()
	return w.text(`SELECT status FROM claims WHERE id = $1`, claimID)
}

func (w *world) loreOf(perspective uuid.UUID) uuid.UUID {
	w.t.Helper()
	var id uuid.NullUUID
	err := w.pool.QueryRow(context.Background(),
		`SELECT lore_id FROM lore_members WHERE perspective_id = $1`, perspective).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil
	}
	if err != nil {
		w.t.Fatalf("read lore of %s: %v", perspective, err)
	}
	return id.UUID
}

func (w *world) arcDirty(arc uuid.UUID) bool {
	w.t.Helper()
	return w.count(`SELECT count(*) FROM arcs WHERE id = $1 AND lore_dirty_at IS NOT NULL`, arc) == 1
}

func (w *world) clearDirty() {
	w.t.Helper()
	w.exec(`UPDATE arcs SET lore_dirty_at = NULL`)
}

// notifyJobs counts notify jobs of the kind about the claim.
func (w *world) notifyJobs(kind perspectivv1.NotificationKind, claimID string) int {
	w.t.Helper()
	return w.count(`SELECT count(*) FROM river_job
		WHERE kind = 'notify' AND (args->>'Kind')::int = $1 AND args->>'ClaimID' = $2`, int32(kind), claimID)
}

// inTx runs fn in a committed transaction.
func (w *world) inTx(fn func(tx pgx.Tx) error) error {
	w.t.Helper()
	return pgx.BeginFunc(context.Background(), w.pool, fn)
}

func (w *world) recompute(arc uuid.UUID) {
	w.t.Helper()
	if err := w.inTx(func(tx pgx.Tx) error { return w.svc.RecomputeArc(context.Background(), tx, arc) }); err != nil {
		w.t.Fatalf("recompute: %v", err)
	}
}

func (w *world) remove(perspective uuid.UUID) error {
	w.t.Helper()
	return w.inTx(func(tx pgx.Tx) error { return w.svc.RemoveFromLore(context.Background(), tx, perspective) })
}

func wantCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %v", code)
	}
	if got := connect.CodeOf(err); got != code {
		t.Fatalf("error = %v (%v), want %v", err, got, code)
	}
}

func wantDomainCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	got, ok := apiutil.CodeOf(err)
	if err == nil || !ok || got != code {
		t.Fatalf("error = %v, want typed %v", err, code)
	}
}
