//go:build integration

package user_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/testdb"
	"perspectiv/internal/user"
)

func TestMain(m *testing.M) { testdb.Main(m) }

const goodPassword = "correct horse battery staple"

// bcryptVerifier checks bcrypt hashes. The service takes its verifier from
// the composition root; the tests seed bcrypt hashes and check them here.
type bcryptVerifier struct{}

func (bcryptVerifier) VerifyPassword(hash, password string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return false, nil
	}
	return false, err
}

// bearerAuthn treats "Bearer <uuid>" as a signed-in caller with that id. The
// real authenticator (sessions and JWTs) belongs to auth; these tests are
// about what the user service does for a given caller.
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

func (allowAll) EmailVerified(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (allowAll) Allow(context.Context, string, string) (bool, error)    { return true, nil }

// env is one test's server: a fresh database, an avatar directory, and the
// user service mounted on the production router behind the interceptors.
type env struct {
	t         *testing.T
	pool      *pgxpool.Pool
	avatarDir string
	srv       *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testdb.New(t)
	dir := t.TempDir()
	avatars, err := user.NewAvatarStore(dir)
	if err != nil {
		t.Fatalf("NewAvatarStore: %v", err)
	}
	log := zerolog.New(io.Discard)
	svc := user.NewService(pool, avatars, bcryptVerifier{}, log)
	h := user.NewHandler(svc)
	ics := connect.WithInterceptors(api.Interceptors(log, bearerAuthn{}, allowAll{}, allowAll{})...)
	mount := func(opts ...connect.HandlerOption) (string, http.Handler) {
		return perspectivv1connect.NewUserServiceHandler(h, append(opts, ics)...)
	}
	srv := httptest.NewServer(api.NewRouter(api.RouterConfig{
		Log:      log,
		Handlers: []api.Mount{mount, avatars.Mount},
	}))
	t.Cleanup(srv.Close)
	return &env{t: t, pool: pool, avatarDir: dir, srv: srv}
}

// client calls the API as the given user; uuid.Nil is anonymous.
func (e *env) client(as uuid.UUID) perspectivv1connect.UserServiceClient {
	var opts []connect.ClientOption
	if as != uuid.Nil {
		opts = append(opts, connect.WithInterceptors(bearer(as)))
	}
	return perspectivv1connect.NewUserServiceClient(e.srv.Client(), e.srv.URL+"/api", opts...)
}

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

// user inserts a verified account whose password is goodPassword.
func (e *env) user(name string) uuid.UUID {
	e.t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(goodPassword), bcrypt.MinCost)
	if err != nil {
		e.t.Fatalf("bcrypt: %v", err)
	}
	return e.id(`INSERT INTO users (email, email_verified_at, password_hash, username, age_confirmed_at)
		VALUES ($1, now(), $2, $3, now()) RETURNING id`, name+"@example.test", string(hash), name)
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

// store returns a store place under a fresh state and city.
func (e *env) store() uuid.UUID {
	e.t.Helper()
	state := e.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, "State "+uuid.NewString())
	return e.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_STORE', 'Corner Deli', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, state)
}

func (e *env) dayArc(place, by uuid.UUID) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-11', 'A real title', $2) RETURNING id`, place, by)
}

func (e *env) published(arc, author uuid.UUID) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())
		RETURNING id`, arc, author)
}

func (e *env) draft(author uuid.UUID) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO perspectives (author_id) VALUES ($1) RETURNING id`, author)
}

// get fetches a path from the test server and returns its status, content
// type and body.
func (e *env) get(path string) (int, string, []byte) {
	e.t.Helper()
	return e.do(http.MethodGet, path)
}

func (e *env) do(method, path string) (int, string, []byte) {
	e.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, e.srv.URL+path, nil)
	if err != nil {
		e.t.Fatalf("request %s: %v", path, err)
	}
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), body
}

// timeCol reads one nullable timestamptz column of the row with id.
func (e *env) timeCol(query string, id uuid.UUID) *time.Time {
	e.t.Helper()
	var at *time.Time
	e.scan(query, []any{id}, &at)
	return at
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
