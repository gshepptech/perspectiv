//go:build integration

package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/notify"
	"perspectiv/internal/notify/mailer"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

const waitTimeout = 45 * time.Second

// env is one test's world: a fresh database, the NotifyService mounted on the
// production router behind the production interceptors, and a River client
// working notify and email jobs with the given mailer.
type env struct {
	t       *testing.T
	pool    *pgxpool.Pool
	srv     *httptest.Server
	metrics *metrics.Metrics
	river   *river.Client[pgx.Tx]
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return newEnvWithMailer(t, &recorder{})
}

func newEnvWithMailer(t *testing.T, m mailer.Mailer) *env {
	t.Helper()
	pool := testdb.New(t)
	log := zerolog.New(io.Discard)
	met := metrics.New(prometheus.NewRegistry())

	h := notify.NewHandler(notify.NewService(pool, unsub))
	ics := connect.WithInterceptors(api.Interceptors(log, bearerAuthn{}, allowAll{}, allowAll{})...)
	mount := func(opts ...connect.HandlerOption) (string, http.Handler) {
		return perspectivv1connect.NewNotifyServiceHandler(h, append(opts, ics)...)
	}
	srv := httptest.NewServer(api.NewRouter(api.RouterConfig{Log: log, Handlers: []api.Mount{mount, h.OneClickMount}}))
	t.Cleanup(srv.Close)

	workers := river.NewWorkers()
	notify.AddWorkers(workers, pool, m, met, log, baseURL, unsub)
	client, err := jobs.NewClient(pool, log, met, workers, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := client.Start(ctx); err != nil {
		cancel()
		t.Fatalf("start river: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = client.Stop(stopCtx)
		cancel()
	})
	return &env{t: t, pool: pool, srv: srv, metrics: met, river: client}
}

// bearerAuthn treats "Bearer <uuid>" as a signed-in caller with that id; the
// real authenticator belongs to auth.
type bearerAuthn struct{}

func (bearerAuthn) Authenticate(_ context.Context, h http.Header) (apiutil.Caller, bool, error) {
	v := h.Get("Authorization")
	if v == "" {
		return apiutil.Caller{}, false, nil
	}
	id, err := uuid.Parse(strings.TrimPrefix(v, "Bearer "))
	if err != nil {
		return apiutil.Caller{}, false, apiutil.Unauthenticated("bad token")
	}
	return apiutil.Caller{UserID: id, Role: perspectivv1.Role_ROLE_USER, SessionID: uuid.New()}, true, nil
}

type allowAll struct{}

func (allowAll) WriteStanding(context.Context, uuid.UUID) (api.Standing, error) {
	return api.StandingWriter, nil
}
func (allowAll) Allow(context.Context, string, string) (bool, error) { return true, nil }

type bearer uuid.UUID

func (b bearer) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+uuid.UUID(b).String())
		return next(ctx, req)
	}
}

func (b bearer) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (b bearer) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// client calls NotifyService as the given user; uuid.Nil is anonymous.
func (e *env) client(as uuid.UUID) perspectivv1connect.NotifyServiceClient {
	var opts []connect.ClientOption
	if as != uuid.Nil {
		opts = append(opts, connect.WithInterceptors(bearer(as)))
	}
	return perspectivv1connect.NewNotifyServiceClient(e.srv.Client(), e.srv.URL+"/api", opts...)
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

func (e *env) user(name string) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO users (email, email_verified_at, password_hash, username, age_confirmed_at)
		VALUES ($1, now(), 'x', $2, now()) RETURNING id`, name+"@example.test", name)
}

func (e *env) arc(title string) uuid.UUID {
	e.t.Helper()
	creator := e.user("creator" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
	place := e.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, "State "+uuid.NewString())
	return e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-11', $2, $3) RETURNING id`, place, title, creator)
}

func (e *env) published(arc, author uuid.UUID) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())
		RETURNING id`, arc, author)
}

// claim makes a character on perspective and a claim on it from the author of
// claimantPerspective, with status.
func (e *env) claim(perspective, claimantPerspective uuid.UUID, status string) uuid.UUID {
	e.t.Helper()
	ch := e.id(`INSERT INTO characters (perspective_id, label, position)
		VALUES ($1, 'the barista', (SELECT count(*) + 1 FROM characters WHERE perspective_id = $1)) RETURNING id`, perspective)
	return e.id(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status)
		SELECT $1, author_id, id, $3 FROM perspectives WHERE id = $2 RETURNING id`, ch, claimantPerspective, status)
}

// lore puts the perspectives in one lore in arc.
func (e *env) lore(arc uuid.UUID, members ...uuid.UUID) uuid.UUID {
	e.t.Helper()
	l := e.id(`INSERT INTO lores (arc_id, member_count) VALUES ($1, $2) RETURNING id`, arc, len(members))
	for _, p := range members {
		e.exec(`INSERT INTO lore_members (lore_id, perspective_id) VALUES ($1, $2)`, l, p)
		e.exec(`UPDATE perspectives SET lore_status = 'LORE_STATUS_IN_LORE', lore_id = $1 WHERE id = $2`, l, p)
	}
	return l
}

func (e *env) block(blocker, blocked uuid.UUID) {
	e.t.Helper()
	e.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, blocker, blocked)
}

func (e *env) pref(user uuid.UUID, kind perspectivv1.NotificationKind, inApp, email bool) {
	e.t.Helper()
	e.exec(`INSERT INTO notification_preferences (user_id, kind, in_app, email) VALUES ($1, $2, $3, $4)`,
		user, kind.String(), inApp, email)
}

// notifyNow enqueues a as a producer does, inside its own committed write
// transaction, and waits for the notify job to finish.
func (e *env) notifyNow(a jobs.NotifyArgs) {
	e.t.Helper()
	ctx := context.Background()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		e.t.Fatalf("begin: %v", err)
	}
	if err := jobs.NewEnqueuer(e.river).NotifyTx(ctx, tx, a); err != nil {
		_ = tx.Rollback(ctx)
		e.t.Fatalf("NotifyTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		e.t.Fatalf("commit: %v", err)
	}
	var jobID int64
	if err := e.pool.QueryRow(ctx, `SELECT max(id) FROM river_job WHERE kind = 'notify'`).Scan(&jobID); err != nil {
		e.t.Fatalf("find notify job: %v", err)
	}
	if state := e.waitFinal(jobID); state != "completed" {
		e.t.Fatalf("notify job state = %s, want completed", state)
	}
}

// waitFinal waits for job id to reach a final state and returns it.
func (e *env) waitFinal(id int64) string {
	e.t.Helper()
	return e.waitState(id, "completed", "cancelled", "discarded")
}

// waitState waits until job id is in one of states and returns the state.
func (e *env) waitState(id int64, states ...string) string {
	e.t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		var state string
		if err := e.pool.QueryRow(context.Background(), `SELECT state::text FROM river_job WHERE id = $1`, id).Scan(&state); err != nil {
			e.t.Fatalf("read job %d: %v", id, err)
		}
		for _, s := range states {
			if state == s {
				return state
			}
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("job %d still %s after %v, want one of %v", id, state, waitTimeout, states)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// inboxRow is one notifications row as stored.
type inboxRow struct {
	kind    string
	payload map[string]string
}

func (e *env) inbox(user uuid.UUID) []inboxRow {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(),
		`SELECT kind, payload FROM notifications WHERE user_id = $1 ORDER BY created_at, id`, user)
	if err != nil {
		e.t.Fatalf("read inbox: %v", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (inboxRow, error) {
		var row inboxRow
		var raw []byte
		if err := r.Scan(&row.kind, &raw); err != nil {
			return row, err
		}
		return row, json.Unmarshal(raw, &row.payload)
	})
	if err != nil {
		e.t.Fatalf("scan inbox: %v", err)
	}
	return out
}

// storedEmail is one email job's args as stored by River.
type storedEmail struct {
	id   int64
	args jobs.EmailArgs
}

func (e *env) emailJobs(to string) []storedEmail {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(),
		`SELECT id, args FROM river_job WHERE kind = 'email' AND args->>'To' = $1 ORDER BY id`, to)
	if err != nil {
		e.t.Fatalf("read email jobs: %v", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (storedEmail, error) {
		var j storedEmail
		var raw []byte
		if err := r.Scan(&j.id, &raw); err != nil {
			return j, err
		}
		return j, json.Unmarshal(raw, &j.args)
	})
	if err != nil {
		e.t.Fatalf("scan email jobs: %v", err)
	}
	return out
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
