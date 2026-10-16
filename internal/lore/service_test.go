//go:build integration

package lore_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

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

// FR-019, FR-021, AC-018, AC-020, OT-019, OT-021, CT-006, ST-012, FR-053, FR-065.
func TestClaimCharacterRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(w *world, a, b author) (caller, character uuid.UUID)
		code  connect.Code
	}{
		{
			name: "claimant with no perspective anywhere is refused",
			setup: func(w *world, a, _ author) (uuid.UUID, uuid.UUID) {
				return w.user("stranger"), a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant whose only perspective is in another arc is refused",
			setup: func(w *world, a, _ author) (uuid.UUID, uuid.UUID) {
				u := w.user("elsewhere")
				w.published(w.newArc("2026-10-02", u), u, statusPending)
				return u, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant with only a draft is refused",
			setup: func(w *world, a, _ author) (uuid.UUID, uuid.UUID) {
				u := w.user("drafter")
				w.exec(`INSERT INTO perspectives (author_id, place_id, local_date) VALUES ($1, $2, '2026-10-01')`, u, w.place)
				return u, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant whose perspective in the arc is deleted is refused",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, b.perspective)
				return b.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "author claiming a character in their own perspective is refused",
			setup: func(_ *world, a, _ author) (uuid.UUID, uuid.UUID) {
				return a.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "character in a REMOVED perspective is refused",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET lore_status = $2 WHERE id = $1`, a.perspective, statusRemoved)
				return b.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant whose own perspective is REMOVED is refused",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET lore_status = $2 WHERE id = $1`, b.perspective, statusRemoved)
				return b.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "claimant rejected on this character before is refused",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				id := w.mustClaim(b.user, a.character)
				if _, err := w.resolve(a.user, id, false); err != nil {
					w.t.Fatalf("reject: %v", err)
				}
				return b.user, a.character
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "character with another user's pending claim already exists",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				c := w.author("carol")
				w.mustClaim(c.user, a.character)
				return b.user, a.character
			},
			code: connect.CodeAlreadyExists,
		},
		{
			name: "character with another user's accepted claim already exists",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.link(a, w.author("carol"))
				return b.user, a.character
			},
			code: connect.CodeAlreadyExists,
		},
		{
			name: "the same claimant's second pending claim already exists",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.mustClaim(b.user, a.character)
				return b.user, a.character
			},
			code: connect.CodeAlreadyExists,
		},
		{
			name: "claimant blocked by the character's author is denied",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, a.user, b.user)
				return b.user, a.character
			},
			code: connect.CodePermissionDenied,
		},
		{
			name: "unknown character is not found",
			setup: func(_ *world, _, b author) (uuid.UUID, uuid.UUID) {
				return b.user, uuid.New()
			},
			code: connect.CodeNotFound,
		},
		{
			name: "character in a deleted perspective is not found",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, a.perspective)
				return b.user, a.character
			},
			code: connect.CodeNotFound,
		},
		{
			name: "character in a hidden perspective is not found",
			setup: func(w *world, a, b author) (uuid.UUID, uuid.UUID) {
				w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, a.perspective)
				return b.user, a.character
			},
			code: connect.CodeNotFound,
		},
		{
			name: "signed-out caller is unauthenticated",
			setup: func(_ *world, a, _ author) (uuid.UUID, uuid.UUID) {
				return uuid.Nil, a.character
			},
			code: connect.CodeUnauthenticated,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			a, b := w.author("alice"), w.author("bob")
			caller, character := tc.setup(w, a, b)
			_, err := w.claim(caller, character)
			wantCode(t, err, tc.code)
		})
	}
}

func TestClaimCharacterRejectsMalformedIDs(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	b := w.author("bob")
	for _, id := range []string{"", "not-a-uuid"} {
		_, err := w.client.ClaimCharacter(context.Background(),
			asUser(b.user, &perspectivv1.ClaimCharacterRequest{CharacterId: id}))
		wantCode(t, err, connect.CodeInvalidArgument)
	}
}

// CT-006: a claim is PENDING, notifies the author and leaves lore unchanged.
func TestClaimCharacterOpensPendingClaimAndNotifies(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b := w.author("alice"), w.author("bob")

	id := w.mustClaim(b.user, a.character)

	if got := w.claimStatus(id); got != claimPending {
		t.Errorf("claim row status = %q, want %q", got, claimPending)
	}
	if got := w.notifyJobs(perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED, id); got != 1 {
		t.Errorf("claim-received notify jobs = %d, want 1", got)
	}
	if got := w.count(`SELECT count(*) FROM river_job WHERE kind = 'notify'
		AND args->>'ActorID' = $1 AND args->>'PerspectiveID' = $2`, b.user.String(), a.perspective.String()); got != 1 {
		t.Errorf("notify job naming actor and perspective = %d, want 1", got)
	}
	for _, p := range []uuid.UUID{a.perspective, b.perspective} {
		if got := w.loreStatus(p); got != statusPending {
			t.Errorf("perspective %s lore status = %q, want PENDING while the claim is open", p, got)
		}
	}
}

// A claimant may claim a SOLO tale's character, and a SOLO claimant may claim.
func TestClaimCharacterAllowsSoloTales(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b := w.author("alice"), w.author("bob")
	w.exec(`UPDATE perspectives SET lore_status = $1`, statusSolo)
	if _, err := w.claim(b.user, a.character); err != nil {
		t.Fatalf("claim between SOLO tales: %v", err)
	}
}

// CT-007, ST-003, ST-005, AC-019, OT-020, FR-020, NFR-009.
func TestResolveClaimAcceptLinksBothIntoOneLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.author("alice")
	// bob's perspective declares no characters: it need not name alice.
	bob := w.user("bob")
	b := author{user: bob, perspective: w.published(w.arc, bob, statusPending)}
	id := w.mustClaim(b.user, a.character)
	w.clearDirty()

	status, err := w.resolve(a.user, id, true)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if status != perspectivv1.ClaimStatus_CLAIM_STATUS_ACCEPTED {
		t.Errorf("status = %v, want ACCEPTED", status)
	}
	if got := w.claimStatus(id); got != claimAccepted {
		t.Errorf("claim row status = %q, want ACCEPTED", got)
	}
	if w.count(`SELECT count(*) FROM claims WHERE id = $1 AND resolved_at IS NOT NULL`, id) != 1 {
		t.Error("resolved_at not set")
	}
	for _, p := range []uuid.UUID{a.perspective, b.perspective} {
		if got := w.loreStatus(p); got != statusInLore {
			t.Errorf("perspective %s lore status = %q, want IN_LORE", p, got)
		}
	}
	la, lb := w.loreOf(a.perspective), w.loreOf(b.perspective)
	if la == uuid.Nil || la != lb {
		t.Fatalf("lores = %s and %s, want one shared lore", la, lb)
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, la); got != 2 {
		t.Errorf("lore member_count = %d, want 2", got)
	}
	if got := w.count(`SELECT count(*) FROM perspectives WHERE lore_id = $1`, la); got != 2 {
		t.Errorf("perspectives carrying the lore id = %d, want 2", got)
	}
	if !w.arcDirty(w.arc) {
		t.Error("arc lore_dirty_at not set by the accept")
	}
	if got := testutil.ToFloat64(w.metrics.ClaimsAccepted); got != 1 {
		t.Errorf("claims_accepted_total = %v, want 1", got)
	}
	if got := w.notifyJobs(perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED, id); got != 1 {
		t.Errorf("claim-accepted notify jobs = %d, want 1", got)
	}

	var loreID uuid.UUID
	var members []lore.Member
	var ok bool
	err = w.inTx(func(tx pgx.Tx) error {
		var err error
		loreID, members, ok, err = w.svc.MembershipOf(context.Background(), tx, b.perspective)
		return err
	})
	if err != nil || !ok || loreID != la {
		t.Fatalf("MembershipOf = %s, ok %v, err %v; want lore %s", loreID, ok, err, la)
	}
	want := map[lore.Member]bool{
		{PerspectiveID: a.perspective, UserID: a.user}: true,
		{PerspectiveID: b.perspective, UserID: b.user}: true,
	}
	if len(members) != 2 || !want[members[0]] || !want[members[1]] {
		t.Errorf("members = %v, want alice and bob", members)
	}
}

// CT-007, ST-006, OT-021.
func TestResolveClaimRejectLeavesLoreAlone(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b := w.author("alice"), w.author("bob")
	id := w.mustClaim(b.user, a.character)

	status, err := w.resolve(a.user, id, false)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if status != perspectivv1.ClaimStatus_CLAIM_STATUS_REJECTED || w.claimStatus(id) != claimRejected {
		t.Errorf("status = %v / %q, want REJECTED", status, w.claimStatus(id))
	}
	for _, p := range []uuid.UUID{a.perspective, b.perspective} {
		if got := w.loreStatus(p); got != statusPending {
			t.Errorf("perspective %s lore status = %q, want PENDING", p, got)
		}
	}
	if got := testutil.ToFloat64(w.metrics.ClaimsAccepted); got != 0 {
		t.Errorf("claims_accepted_total = %v, want 0", got)
	}
	// Another claimant may now claim the freed character.
	if _, err := w.claim(w.author("carol").user, a.character); err != nil {
		t.Errorf("claim by a third author after a rejection: %v", err)
	}
	// The rejected claimant may not.
	_, err = w.claim(b.user, a.character)
	wantCode(t, err, connect.CodeFailedPrecondition)
}

// CT-007 error cases.
func TestResolveClaimRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(w *world, a, b author, claimID string) (caller uuid.UUID, id string)
		code  connect.Code
	}{
		{
			name: "the claimant cannot resolve their own claim",
			setup: func(_ *world, _, b author, id string) (uuid.UUID, string) {
				return b.user, id
			},
			code: connect.CodePermissionDenied,
		},
		{
			name: "a bystander cannot resolve the claim",
			setup: func(w *world, _, _ author, id string) (uuid.UUID, string) {
				return w.author("carol").user, id
			},
			code: connect.CodePermissionDenied,
		},
		{
			name: "an accepted claim is not PENDING",
			setup: func(w *world, a, _ author, id string) (uuid.UUID, string) {
				if _, err := w.resolve(a.user, id, true); err != nil {
					w.t.Fatalf("accept: %v", err)
				}
				return a.user, id
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "a rejected claim is not PENDING",
			setup: func(w *world, a, _ author, id string) (uuid.UUID, string) {
				if _, err := w.resolve(a.user, id, false); err != nil {
					w.t.Fatalf("reject: %v", err)
				}
				return a.user, id
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "accepting when the claimant's perspective was deleted is refused",
			setup: func(w *world, a, b author, id string) (uuid.UUID, string) {
				w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, b.perspective)
				return a.user, id
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "accepting when the claimant's perspective is REMOVED is refused",
			setup: func(w *world, a, b author, id string) (uuid.UUID, string) {
				w.exec(`UPDATE perspectives SET lore_status = $2 WHERE id = $1`, b.perspective, statusRemoved)
				return a.user, id
			},
			code: connect.CodeFailedPrecondition,
		},
		{
			name: "an unknown claim is not found",
			setup: func(_ *world, a, _ author, _ string) (uuid.UUID, string) {
				return a.user, uuid.NewString()
			},
			code: connect.CodeNotFound,
		},
		{
			name: "a malformed claim id is invalid",
			setup: func(_ *world, a, _ author, _ string) (uuid.UUID, string) {
				return a.user, "nope"
			},
			code: connect.CodeInvalidArgument,
		},
		{
			name: "a signed-out caller is unauthenticated",
			setup: func(_ *world, _, _ author, id string) (uuid.UUID, string) {
				return uuid.Nil, id
			},
			code: connect.CodeUnauthenticated,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			a, b := w.author("alice"), w.author("bob")
			caller, id := tc.setup(w, a, b, w.mustClaim(b.user, a.character))
			_, err := w.resolve(caller, id, true)
			wantCode(t, err, tc.code)
		})
	}
}

// ST-004, AC-023, OT-023: an expiry SOLO tale returns to IN_LORE on a later
// accepted claim, from either side of the claim.
func TestAcceptRevivesSoloTale(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		soloSide func(a, b author) uuid.UUID
	}{
		{name: "the claimed tale is SOLO", soloSide: func(a, _ author) uuid.UUID { return a.perspective }},
		{name: "the claimant's tale is SOLO", soloSide: func(_, b author) uuid.UUID { return b.perspective }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			a, b := w.author("alice"), w.author("bob")
			solo := tc.soloSide(a, b)
			w.exec(`UPDATE perspectives SET lore_status = $2 WHERE id = $1`, solo, statusSolo)
			w.link(a, b)
			if got := w.loreStatus(solo); got != statusInLore {
				t.Errorf("revived tale lore status = %q, want IN_LORE", got)
			}
			if w.loreOf(a.perspective) != w.loreOf(b.perspective) || w.loreOf(solo) == uuid.Nil {
				t.Error("revived tale is not in the shared lore")
			}
		})
	}
}

// FR-045, AC-025, OT-024: A-B, B-C, C-D form one lore of 4; E-F a second.
func TestRecomputeArcFormsConnectedGroups(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c, d := w.author("a"), w.author("b"), w.author("c"), w.author("d")
	e, f := w.author("e"), w.author("f")
	pending := w.author("pending")
	w.link(a, b)
	w.link(b, c)
	w.link(c, d)
	w.link(e, f)
	// A pending claim is not a link.
	w.mustClaim(pending.user, f.character)

	// A pair in another arc stays in its own arc's lore.
	other := w.newArc("2026-10-03", a.user)
	x := w.published(other, w.user("x"), statusPending)
	yUser := w.user("y")
	y := w.published(other, yUser, statusPending)
	xc := w.character(x, "someone")
	w.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status)
		VALUES ($1, $2, $3, $4)`, xc, yUser, y, claimAccepted)
	w.recompute(other)

	w.recompute(w.arc)

	big := w.loreOf(a.perspective)
	for _, p := range []author{b, c, d} {
		if got := w.loreOf(p.perspective); got != big {
			t.Errorf("perspective %s in lore %s, want %s", p.perspective, got, big)
		}
	}
	pair := w.loreOf(e.perspective)
	if pair == uuid.Nil || pair == big || w.loreOf(f.perspective) != pair {
		t.Errorf("E-F lore = %s / %s, want one lore distinct from %s", pair, w.loreOf(f.perspective), big)
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, big); got != 4 {
		t.Errorf("A-D lore member_count = %d, want 4", got)
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, pair); got != 2 {
		t.Errorf("E-F lore member_count = %d, want 2", got)
	}
	if got := w.loreOf(pending.perspective); got != uuid.Nil || w.loreStatus(pending.perspective) != statusPending {
		t.Errorf("perspective with only a pending claim: lore %s, status %q; want none, PENDING",
			got, w.loreStatus(pending.perspective))
	}
	if got := w.count(`SELECT count(*) FROM lores WHERE arc_id = $1 AND member_count > 0`, w.arc); got != 2 {
		t.Errorf("non-empty lores in the arc = %d, want 2", got)
	}
	ox, oy := w.loreOf(x), w.loreOf(y)
	if ox == uuid.Nil || ox != oy || ox == big || ox == pair {
		t.Errorf("other arc lore = %s / %s, want its own shared lore", ox, oy)
	}
	if got := w.count(`SELECT count(*) FROM lores WHERE id = $1 AND arc_id = $2`, ox, other); got != 1 {
		t.Error("other arc's lore is not filed under that arc")
	}

	// Recomputing again changes nothing: groups keep their lore ids.
	w.recompute(w.arc)
	if w.loreOf(a.perspective) != big || w.loreOf(e.perspective) != pair {
		t.Error("a second recompute moved a group to a new lore")
	}
	if got := w.count(`SELECT count(*) FROM lores WHERE arc_id = $1`, w.arc); got != 2 {
		t.Errorf("lores in the arc after a second recompute = %d, want 2", got)
	}
}

// A merge keeps the lowest id of two equally sized lores; the other keeps its
// row with member_count 0.
func TestRecomputeArcMergeKeepsLowestLoreID(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c, d := w.author("a"), w.author("b"), w.author("c"), w.author("d")
	w.link(a, b)
	w.link(c, d)
	l1, l2 := w.loreOf(a.perspective), w.loreOf(c.perspective)
	if l1 == l2 {
		t.Fatal("two separate pairs share a lore")
	}
	keep, drop := l1, l2
	if strings.Compare(l2.String(), l1.String()) < 0 {
		keep, drop = l2, l1
	}

	w.link(b, c)

	for _, p := range []author{a, b, c, d} {
		if got := w.loreOf(p.perspective); got != keep {
			t.Errorf("perspective %s in lore %s after merge, want %s", p.perspective, got, keep)
		}
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, keep); got != 4 {
		t.Errorf("kept lore member_count = %d, want 4", got)
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, drop); got != 0 {
		t.Errorf("vanished lore member_count = %d, want 0 with its row kept", got)
	}
}

// A split: the larger half keeps the lore id, the smaller half gets a new one.
func TestRecomputeArcSplitKeepsIDForLargerHalf(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c, d, e := w.author("a"), w.author("b"), w.author("c"), w.author("d"), w.author("e")
	w.link(a, b)
	cut := w.link(b, c)
	w.link(c, d)
	w.link(d, e)
	whole := w.loreOf(a.perspective)

	w.exec(`UPDATE claims SET status = $2 WHERE id = $1`, cut, claimRejected)
	w.recompute(w.arc)

	if got := w.loreOf(c.perspective); got != whole {
		t.Errorf("larger half C-D-E in lore %s, want the original %s", got, whole)
	}
	small := w.loreOf(a.perspective)
	if small == uuid.Nil || small == whole || w.loreOf(b.perspective) != small {
		t.Errorf("smaller half A-B lore = %s / %s, want one new lore", small, w.loreOf(b.perspective))
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, whole); got != 3 {
		t.Errorf("original lore member_count = %d, want 3", got)
	}
}

// Decision under test: a perspective that RecomputeArc leaves with no
// accepted link becomes a SOLO tale, outside every lore and revivable.
func TestRecomputeArcReleasesUnlinkedPerspectiveToSolo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		withdraw string
	}{
		{name: "partner deleted", withdraw: `UPDATE perspectives SET deleted_at = now() WHERE id = $1`},
		{name: "partner hidden", withdraw: `UPDATE perspectives SET hidden_at = now() WHERE id = $1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkReleasedToSolo(t, tc.withdraw)
		})
	}
}

func checkReleasedToSolo(t *testing.T, withdraw string) {
	t.Helper()
	w := newWorld(t)
	e, f := w.author("e"), w.author("f")
	g := w.author("g")
	id := w.link(e, f)
	pendingOnF := w.mustClaim(g.user, f.character)
	loreID := w.loreOf(e.perspective)

	w.exec(withdraw, f.perspective)
	w.recompute(w.arc)

	if got := w.claimStatus(id); got != claimRejected {
		t.Errorf("accepted claim touching the withdrawn perspective = %q, want REJECTED", got)
	}
	if got := w.claimStatus(pendingOnF); got != claimRejected {
		t.Errorf("pending claim on the withdrawn perspective = %q, want REJECTED", got)
	}
	if got := w.loreStatus(e.perspective); got != statusSolo {
		t.Errorf("left-behind perspective lore status = %q, want SOLO", got)
	}
	if got := w.loreOf(e.perspective); got != uuid.Nil {
		t.Errorf("left-behind perspective still a member of %s", got)
	}
	if got := w.count(`SELECT count(*) FROM perspectives WHERE id = $1 AND lore_id IS NULL`, e.perspective); got != 1 {
		t.Error("left-behind perspective still carries a lore id")
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, loreID); got != 0 {
		t.Errorf("vanished lore member_count = %d, want 0", got)
	}

	// The SOLO tale is revivable by a later accepted claim.
	w.link(e, g)
	if got := w.loreStatus(e.perspective); got != statusInLore {
		t.Errorf("revived tale lore status = %q, want IN_LORE", got)
	}
}

// ST-012, AC-023, AC-024 (lore's part), FR-058.
func TestRemoveFromLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c, d := w.author("a"), w.author("b"), w.author("c"), w.author("d")
	w.link(a, b)
	w.link(b, c)
	w.link(c, d)
	onlooker := w.author("onlooker")
	pendingOnB := w.mustClaim(onlooker.user, w.character(b.perspective, "the waiter"))
	// b's author has a pending claim on another character in the arc.
	extra := w.character(d.perspective, "the dishwasher")
	pendingByB := w.mustClaim(b.user, extra)
	whole := w.loreOf(c.perspective)
	w.clearDirty()

	if err := w.remove(b.perspective); err != nil {
		t.Fatalf("RemoveFromLore: %v", err)
	}

	if got := w.loreStatus(b.perspective); got != statusRemoved {
		t.Errorf("removed perspective lore status = %q, want REMOVED", got)
	}
	if got := w.loreOf(b.perspective); got != uuid.Nil {
		t.Errorf("removed perspective still a member of %s", got)
	}
	if got := w.count(`SELECT count(*) FROM claims c JOIN characters ch ON ch.id = c.character_id
		WHERE (ch.perspective_id = $1 OR c.claimant_user_id = $2) AND c.status <> $3`,
		b.perspective, b.user, claimRejected); got != 0 {
		t.Errorf("claims on or by the removed perspective not rejected: %d", got)
	}
	if w.claimStatus(pendingOnB) != claimRejected || w.claimStatus(pendingByB) != claimRejected {
		t.Error("pending claims involving the removed perspective not rejected")
	}
	if !w.arcDirty(w.arc) {
		t.Error("arc lore_dirty_at not set by the removal")
	}
	// A lost its only link; C-D stay linked and keep the larger share.
	if got := w.loreStatus(a.perspective); got != statusSolo {
		t.Errorf("A lore status = %q, want SOLO", got)
	}
	if w.loreOf(c.perspective) != whole || w.loreOf(d.perspective) != whole {
		t.Error("C-D no longer share the original lore")
	}
	if got := w.count(`SELECT member_count FROM lores WHERE id = $1`, whole); got != 2 {
		t.Errorf("lore member_count = %d, want 2", got)
	}

	// Idempotent on an already REMOVED perspective.
	if err := w.remove(b.perspective); err != nil {
		t.Errorf("second RemoveFromLore: %v", err)
	}
	if got := w.loreStatus(b.perspective); got != statusRemoved {
		t.Errorf("lore status after second removal = %q", got)
	}

	// REMOVED is terminal: claims on its characters and by its author fail.
	_, err := w.claim(w.author("late").user, b.character)
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = w.claim(b.user, w.author("later").character)
	wantCode(t, err, connect.CodeFailedPrecondition)
	// Removed perspectives stay readable: the row is untouched otherwise.
	if got := w.count(`SELECT count(*) FROM perspectives WHERE id = $1 AND deleted_at IS NULL AND hidden_at IS NULL`, b.perspective); got != 1 {
		t.Error("removed perspective is no longer readable")
	}
}

func TestRemoveFromLoreRefusals(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	err := w.remove(uuid.New())
	wantDomainCode(t, err, connect.CodeNotFound)

	u := w.user("drafter")
	draft := w.id(`INSERT INTO perspectives (author_id) VALUES ($1) RETURNING id`, u)
	err = w.remove(draft)
	wantDomainCode(t, err, connect.CodeFailedPrecondition)
}

// AC-025: PENDING, SOLO and REMOVED perspectives are in no lore.
func TestMembershipOfOutsideLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for _, status := range []string{statusPending, statusSolo, statusRemoved} {
		p := w.published(w.arc, w.user(strings.ToLower(status)), status)
		err := w.inTx(func(tx pgx.Tx) error {
			id, members, ok, err := w.svc.MembershipOf(context.Background(), tx, p)
			if ok || id != uuid.Nil || members != nil {
				t.Errorf("%s: MembershipOf = %s, %v, ok %v; want none", status, id, members, ok)
			}
			return err
		})
		if err != nil {
			t.Errorf("%s: MembershipOf: %v", status, err)
		}
	}
	err := w.inTx(func(tx pgx.Tx) error {
		_, _, _, err := w.svc.MembershipOf(context.Background(), tx, uuid.New())
		return err
	})
	wantDomainCode(t, err, connect.CodeNotFound)
}

func TestRecomputeArcRejectsNilArc(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	err := w.inTx(func(tx pgx.Tx) error { return w.svc.RecomputeArc(context.Background(), tx, uuid.Nil) })
	wantDomainCode(t, err, connect.CodeInvalidArgument)
}

// claimsWorld has alice with two characters: bob's PENDING claim on the
// first, carol's REJECTED claim on the second (made later), and alice's own
// PENDING claim on bob's character.
type claimsWorld struct {
	*world
	alice, bob                 author
	onFirst, onSecond, byAlice string
}

func newClaimsWorld(t *testing.T) *claimsWorld {
	t.Helper()
	w := newWorld(t)
	cw := &claimsWorld{world: w, alice: w.author("alice"), bob: w.author("bob")}
	carol := w.author("carol")
	second := w.character(cw.alice.perspective, "the cook")
	cw.onFirst = w.mustClaim(cw.bob.user, cw.alice.character)
	cw.onSecond = w.mustClaim(carol.user, second)
	if _, err := w.resolve(cw.alice.user, cw.onSecond, false); err != nil {
		t.Fatalf("reject: %v", err)
	}
	cw.byAlice = w.mustClaim(cw.alice.user, cw.bob.character)
	return cw
}

func (w *world) listClaims(
	caller uuid.UUID, dir perspectivv1.ClaimDirection, status perspectivv1.ClaimStatus, token string, size int32,
) (*perspectivv1.ListClaimsResponse, error) {
	w.t.Helper()
	resp, err := w.client.ListClaims(context.Background(), asUser(caller, &perspectivv1.ListClaimsRequest{
		Direction: dir, Status: status, PageToken: token, PageSize: size,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func claimIDs(r *perspectivv1.ListClaimsResponse) []string {
	var out []string
	for _, cl := range r.GetClaims() {
		out = append(out, cl.GetId())
	}
	return out
}

func sameIDs(got []string, want ...string) bool {
	return slices.Equal(got, want)
}

// AC-021: claims show usernames only, never an email.
func TestListClaimsIncoming(t *testing.T) {
	t.Parallel()
	w := newClaimsWorld(t)
	in, err := w.listClaims(w.alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING, 0, "", 0)
	if err != nil {
		t.Fatalf("incoming: %v", err)
	}
	if got := claimIDs(in); !sameIDs(got, w.onSecond, w.onFirst) {
		t.Fatalf("incoming = %v, want newest first [%s %s]", got, w.onSecond, w.onFirst)
	}
	first := in.GetClaims()[1]
	want := &perspectivv1.Claim{
		Id:                    w.onFirst,
		ArcId:                 w.arc.String(),
		CharacterId:           w.alice.character.String(),
		CharacterLabel:        "the one who alice saw",
		PerspectiveId:         w.alice.perspective.String(),
		Author:                &perspectivv1.UserRef{UserId: w.alice.user.String(), Username: "alice"},
		Claimant:              &perspectivv1.UserRef{UserId: w.bob.user.String(), Username: "bob"},
		ClaimantPerspectiveId: w.bob.perspective.String(),
		Status:                perspectivv1.ClaimStatus_CLAIM_STATUS_PENDING,
		CreatedAt:             first.GetCreatedAt(),
	}
	if first.GetCreatedAt() == nil || !proto.Equal(first, want) {
		t.Errorf("incoming claim = %v, want %v", first, want)
	}
	if in.GetClaims()[0].GetResolvedAt() == nil {
		t.Error("rejected claim has no resolved_at")
	}
	raw, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "@") {
		t.Errorf("claim listing leaks an email: %s", raw)
	}
}

func TestListClaimsFiltersAndDirections(t *testing.T) {
	t.Parallel()
	w := newClaimsWorld(t)
	rejected, err := w.listClaims(w.alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING,
		perspectivv1.ClaimStatus_CLAIM_STATUS_REJECTED, "", 0)
	if err != nil {
		t.Fatalf("incoming rejected: %v", err)
	}
	if got := claimIDs(rejected); !sameIDs(got, w.onSecond) {
		t.Errorf("incoming REJECTED = %v, want [%s]", got, w.onSecond)
	}
	out, err := w.listClaims(w.alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_OUTGOING, 0, "", 0)
	if err != nil {
		t.Fatalf("outgoing: %v", err)
	}
	if got := claimIDs(out); !sameIDs(got, w.byAlice) {
		t.Errorf("outgoing = %v, want [%s]", got, w.byAlice)
	}
	bobOut, err := w.listClaims(w.bob.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_OUTGOING,
		perspectivv1.ClaimStatus_CLAIM_STATUS_ACCEPTED, "", 0)
	if err != nil {
		t.Fatalf("bob outgoing accepted: %v", err)
	}
	if got := claimIDs(bobOut); len(got) != 0 {
		t.Errorf("bob outgoing ACCEPTED = %v, want none", got)
	}
}

func TestListClaimsPaging(t *testing.T) {
	t.Parallel()
	w := newClaimsWorld(t)
	var seen []string
	token := ""
	for range 3 {
		page, err := w.listClaims(w.alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING, 0, token, 1)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		seen = append(seen, claimIDs(page)...)
		token = page.GetNextPageToken()
		if token == "" {
			break
		}
	}
	if !sameIDs(seen, w.onSecond, w.onFirst) || token != "" {
		t.Errorf("paged incoming = %v (next %q), want [%s %s] then no token", seen, token, w.onSecond, w.onFirst)
	}
}

func TestListClaimsRefusals(t *testing.T) {
	t.Parallel()
	w := newClaimsWorld(t)
	incoming := perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING
	cases := []struct {
		name   string
		caller uuid.UUID
		dir    perspectivv1.ClaimDirection
		status perspectivv1.ClaimStatus
		token  string
		code   connect.Code
	}{
		{name: "no direction", caller: w.alice.user, code: connect.CodeInvalidArgument},
		{name: "unknown direction", caller: w.alice.user, dir: perspectivv1.ClaimDirection(99), code: connect.CodeInvalidArgument},
		{name: "unknown status", caller: w.alice.user, dir: incoming, status: perspectivv1.ClaimStatus(42), code: connect.CodeInvalidArgument},
		{name: "bad page token", caller: w.alice.user, dir: incoming, token: "!!", code: connect.CodeInvalidArgument},
		{name: "keyset without a separator", caller: w.alice.user, dir: incoming, token: apiutil.NextPageToken("garbage"), code: connect.CodeInvalidArgument},
		{name: "keyset with a bad time", caller: w.alice.user, dir: incoming, token: apiutil.NextPageToken("x_" + uuid.NewString()), code: connect.CodeInvalidArgument},
		{name: "keyset with a bad id", caller: w.alice.user, dir: incoming, token: apiutil.NextPageToken("12_nope"), code: connect.CodeInvalidArgument},
		{name: "signed-out", dir: incoming, code: connect.CodeUnauthenticated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := w.listClaims(tc.caller, tc.dir, tc.status, tc.token, 0)
			wantCode(t, err, tc.code)
		})
	}
}

func TestGetLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c := w.author("alice"), w.author("bob"), w.author("carol")
	ab := w.link(a, b)
	bc := w.link(b, c)
	loreID := w.loreOf(a.perspective)

	resp, err := w.client.GetLore(context.Background(), asUser(uuid.Nil, &perspectivv1.GetLoreRequest{LoreId: loreID.String()}))
	if err != nil {
		t.Fatalf("GetLore: %v", err)
	}
	l := resp.Msg.GetLore()
	if l.GetId() != loreID.String() || l.GetArcId() != w.arc.String() || l.GetMemberCount() != 3 ||
		l.GetStanding() != perspectivv1.LoreStanding_LORE_STANDING_SUBLORE {
		t.Errorf("lore = %v", l)
	}
	names := map[string]string{}
	for _, m := range l.GetMembers() {
		names[m.GetPerspectiveId()] = m.GetUser().GetUsername()
	}
	if len(names) != 3 || names[a.perspective.String()] != "alice" || names[b.perspective.String()] != "bob" ||
		names[c.perspective.String()] != "carol" {
		t.Errorf("members = %v", names)
	}
	enc := map[string]*perspectivv1.Encounter{}
	for _, e := range l.GetEncounters() {
		enc[e.GetClaimId()] = e
	}
	if len(enc) != 2 || enc[ab].GetPerspectiveId() != a.perspective.String() ||
		enc[ab].GetClaimantPerspectiveId() != b.perspective.String() || enc[bc] == nil {
		t.Errorf("encounters = %v", l.GetEncounters())
	}
	raw, _ := protojson.Marshal(resp.Msg)
	if strings.Contains(string(raw), "@") {
		t.Errorf("lore leaks an email: %s", raw)
	}

	_, err = w.client.GetLore(context.Background(), asUser(uuid.Nil, &perspectivv1.GetLoreRequest{LoreId: uuid.NewString()}))
	wantCode(t, err, connect.CodeNotFound)
	_, err = w.client.GetLore(context.Background(), asUser(uuid.Nil, &perspectivv1.GetLoreRequest{LoreId: "x"}))
	wantCode(t, err, connect.CodeInvalidArgument)
}

// Two claimants racing for one character: exactly one claim opens.
func TestConcurrentClaimsOnOneCharacter(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := w.author("alice")
	claimants := []author{w.author("b"), w.author("c"), w.author("d"), w.author("e")}
	errs := make(chan error, len(claimants))
	for _, cl := range claimants {
		go func() {
			_, err := w.client.ClaimCharacter(context.Background(),
				asUser(cl.user, &perspectivv1.ClaimCharacterRequest{CharacterId: a.character.String()}))
			errs <- err
		}()
	}
	ok, exists := 0, 0
	for range claimants {
		err := <-errs
		switch {
		case err == nil:
			ok++
		case connect.CodeOf(err) == connect.CodeAlreadyExists:
			exists++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || exists != len(claimants)-1 {
		t.Errorf("opened %d, already-exists %d; want 1 and %d", ok, exists, len(claimants)-1)
	}
}
