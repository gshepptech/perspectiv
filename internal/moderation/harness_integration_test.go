//go:build integration

package moderation_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/moderation"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// tokenAuthn treats "Bearer <uuid>" as a signed-in USER and
// "Bearer <uuid>:admin" as a caller whose token claims ADMIN. Sessions and
// JWTs belong to auth; these tests are about what moderation does for a given
// caller, including a token whose role no longer matches the users row.
type tokenAuthn struct{}

func (tokenAuthn) Authenticate(_ context.Context, h http.Header) (apiutil.Caller, bool, error) {
	v := h.Get("Authorization")
	if v == "" {
		return apiutil.Caller{}, false, nil
	}
	raw, admin := strings.CutSuffix(strings.TrimPrefix(v, "Bearer "), ":admin")
	id, err := uuid.Parse(raw)
	if err != nil {
		return apiutil.Caller{}, false, apiutil.Unauthenticated("bad token")
	}
	role := perspectivv1.Role_ROLE_USER
	if admin {
		role = perspectivv1.Role_ROLE_ADMIN
	}
	return apiutil.Caller{UserID: id, Role: role, SessionID: uuid.New()}, true, nil
}

// dbGate answers the write gate from the users row. It leaves deleted
// accounts to the handler: in production auth's Authenticate refuses them
// before the gate runs, and tokenAuthn does not, so these tests can reach
// moderation's own check for an account deleted after its token was minted.
type dbGate struct{ pool *pgxpool.Pool }

func (g dbGate) WriteStanding(ctx context.Context, id uuid.UUID) (api.Standing, error) {
	var verified, suspended bool
	err := g.pool.QueryRow(ctx, `SELECT email_verified_at IS NOT NULL, suspended_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&verified, &suspended)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return api.StandingUnverified, nil
	case err != nil:
		return api.StandingUnverified, err
	case suspended:
		return api.StandingSuspended, nil
	case !verified:
		return api.StandingUnverified, nil
	}
	return api.StandingWriter, nil
}

// switchLimiter allows every call until exhausted is set. The Postgres
// counters belong to auth; here only the interceptor's answer matters.
type switchLimiter struct{ exhausted atomic.Bool }

func (l *switchLimiter) Allow(context.Context, string, string) (bool, error) {
	return !l.exhausted.Load(), nil
}

// logBuffer collects the JSON log lines the server writes.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) lines() []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(b.buf.Bytes()))
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// denials returns the authz_denied lines for a caller and method.
func (b *logBuffer) denials(sub uuid.UUID, method string) []map[string]any {
	var out []map[string]any
	for _, l := range b.lines() {
		if l["event"] == "authz_denied" && l["sub"] == sub.String() && l["method"] == method {
			out = append(out, l)
		}
	}
	return out
}

// env is one test's server: a fresh database and the moderation service on
// the production router behind the production interceptor chain, with a
// real insert-only River client for the notifications it enqueues.
type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	svc  *moderation.Service
	srv  *httptest.Server
	logs *logBuffer
	lim  *switchLimiter
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testdb.New(t)
	logs := &logBuffer{}
	log := zerolog.New(logs)
	client, err := jobs.NewClient(pool, log, metrics.New(prometheus.NewRegistry()), nil, nil)
	if err != nil {
		t.Fatalf("jobs.NewClient: %v", err)
	}
	svc := moderation.NewService(pool, jobs.NewEnqueuer(client), log)
	h := moderation.NewHandler(svc)
	lim := &switchLimiter{}
	ics := connect.WithInterceptors(api.Interceptors(log, tokenAuthn{}, dbGate{pool: pool}, lim)...)
	mount := func(opts ...connect.HandlerOption) (string, http.Handler) {
		return h.Mount(append(opts, ics)...)
	}
	srv := httptest.NewServer(api.NewRouter(api.RouterConfig{Log: log, Handlers: []api.Mount{mount}}))
	t.Cleanup(srv.Close)
	return &env{t: t, pool: pool, svc: svc, srv: srv, logs: logs, lim: lim}
}

// client calls the API as a USER token; uuid.Nil is anonymous.
func (e *env) client(as uuid.UUID) perspectivv1connect.ModerationServiceClient {
	return e.clientWith(as, "")
}

// admin calls the API with a token claiming ADMIN.
func (e *env) admin(as uuid.UUID) perspectivv1connect.ModerationServiceClient {
	return e.clientWith(as, ":admin")
}

func (e *env) clientWith(as uuid.UUID, suffix string) perspectivv1connect.ModerationServiceClient {
	var opts []connect.ClientOption
	if as != uuid.Nil {
		opts = append(opts, connect.WithInterceptors(bearer(as.String()+suffix)))
	}
	return perspectivv1connect.NewModerationServiceClient(e.srv.Client(), e.srv.URL+"/api", opts...)
}

type bearer string

func (b bearer) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+string(b))
		return next(ctx, req)
	}
}

func (b bearer) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (b bearer) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// user inserts a verified account.
func (e *env) user(name string) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO users (email, email_verified_at, password_hash, username, age_confirmed_at)
		VALUES ($1, now(), 'x', $2, now()) RETURNING id`, name+"@example.test", name)
}

// adminUser inserts a verified account whose stored role is ADMIN.
func (e *env) adminUser(name string) uuid.UUID {
	e.t.Helper()
	id := e.user(name)
	e.exec(`UPDATE users SET role = 'ROLE_ADMIN' WHERE id = $1`, id)
	return id
}

func (e *env) id(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&id); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return id
}

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), query, args...); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
}

func (e *env) scan(query string, args []any, dest ...any) {
	e.t.Helper()
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(dest...); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	e.scan(query, args, &n)
	return n
}

// world is a small published scene: a store under a state, a day arc and one
// published perspective by its author.
type world struct {
	author      uuid.UUID
	state       uuid.UUID
	store       uuid.UUID
	arc         uuid.UUID
	perspective uuid.UUID
}

func (e *env) world(authorName string) world {
	e.t.Helper()
	w := world{author: e.user(authorName)}
	w.state = e.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, "State "+uuid.NewString())
	w.store = e.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_STORE', 'Corner Deli', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, w.state)
	w.arc = e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-11', 'Rain at the deli', $2) RETURNING id`, w.store, w.author)
	w.perspective = e.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())
		RETURNING id`, w.arc, w.author)
	return w
}

// report files a report through the API and returns its id.
func (e *env) report(as uuid.UUID, kind perspectivv1.ReportTargetKind, target uuid.UUID) uuid.UUID {
	e.t.Helper()
	res, err := e.client(as).Report(context.Background(), connect.NewRequest(&perspectivv1.ReportRequest{
		TargetKind: kind,
		TargetId:   target.String(),
		Reason:     perspectivv1.ReportReason_REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER,
		Details:    "names the cashier",
	}))
	if err != nil {
		e.t.Fatalf("Report: %v", err)
	}
	id, err := uuid.Parse(res.Msg.GetReportId())
	if err != nil {
		e.t.Fatalf("report id %q: %v", res.Msg.GetReportId(), err)
	}
	return id
}

// wantCode fails the test unless err is a Connect error with code.
func wantCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want %v", code)
	}
	if got := connect.CodeOf(err); got != code {
		t.Fatalf("code = %v (%v), want %v", got, err, code)
	}
}

func pageOf(limit int32) apiutil.PageToken { return apiutil.PageToken{Limit: limit} }
