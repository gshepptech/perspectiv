//go:build integration

package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/auth"
	"perspectiv/internal/jobs"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

const (
	jwtSecret    = "integration-secret-0123456789abcdef"
	baseURL      = "https://perspectiv.example"
	goodPassword = "correct horse battery staple"
)

// syncBuffer collects the server's log lines; the interceptors and the
// service write to it from request goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// env is one test's server: a fresh database, the auth service and limiter
// wired into the production interceptor chain and router, the auth handler,
// and Unimplemented handlers for every other service so the interceptor's
// decision on any route can be observed: a call that passes authorization
// reaches the handler and answers Unimplemented.
type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	svc  *auth.Service
	srv  *httptest.Server
	logs *syncBuffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testdb.New(t)
	return newEnvOn(t, pool)
}

// newEnvOn starts a server on an existing database, which is how a test
// restarts the server: a fresh process with fresh memory, same Postgres.
func newEnvOn(t *testing.T, pool *pgxpool.Pool) *env {
	t.Helper()
	logs := &syncBuffer{}
	log := zerolog.New(logs)
	river, err := jobs.NewClient(pool, log, metrics.New(prometheus.NewRegistry()), nil, nil)
	if err != nil {
		t.Fatalf("jobs.NewClient: %v", err)
	}
	lim := auth.NewLimiter(pool)
	svc, err := auth.NewService(pool, jobs.NewEnqueuer(river), lim, auth.Config{
		JWTSecret:     jwtSecret,
		PublicBaseURL: baseURL,
		CookieSecure:  true,
		BcryptCost:    bcrypt.MinCost,
	}, log)
	if err != nil {
		t.Fatalf("auth.NewService: %v", err)
	}
	ics := connect.WithInterceptors(api.Interceptors(log, svc, svc, lim)...)
	mounts := []api.Mount{
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewAuthServiceHandler(auth.NewHandler(svc), append(o, ics)...)
		},
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewPlaceServiceHandler(perspectivv1connect.UnimplementedPlaceServiceHandler{}, append(o, ics)...)
		},
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewPerspectiveServiceHandler(perspectivv1connect.UnimplementedPerspectiveServiceHandler{}, append(o, ics)...)
		},
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewLoreServiceHandler(perspectivv1connect.UnimplementedLoreServiceHandler{}, append(o, ics)...)
		},
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewVoteServiceHandler(perspectivv1connect.UnimplementedVoteServiceHandler{}, append(o, ics)...)
		},
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewReactServiceHandler(perspectivv1connect.UnimplementedReactServiceHandler{}, append(o, ics)...)
		},
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewModerationServiceHandler(perspectivv1connect.UnimplementedModerationServiceHandler{}, append(o, ics)...)
		},
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewNotifyServiceHandler(perspectivv1connect.UnimplementedNotifyServiceHandler{}, append(o, ics)...)
		},
	}
	srv := httptest.NewServer(api.NewRouter(api.RouterConfig{Log: log, Handlers: mounts}))
	t.Cleanup(srv.Close)
	return &env{t: t, pool: pool, svc: svc, srv: srv, logs: logs}
}

func (e *env) auth() perspectivv1connect.AuthServiceClient {
	return perspectivv1connect.NewAuthServiceClient(e.srv.Client(), e.srv.URL+"/api", clientOpts("")...)
}

// clientOpts sends access as a bearer token when it is not empty. Every call
// comes from its own client address, as the proxy in front of the server
// reports it in X-Forwarded-For, so the per-IP limits of one test do not
// spill into the next request; the rate-limit tests pin the address.
func clientOpts(access string) []connect.ClientOption {
	ics := []connect.Interceptor{forwardedFor("")}
	if access != "" {
		ics = append(ics, bearer(access))
	}
	return []connect.ClientOption{connect.WithInterceptors(ics...)}
}

// forwardedFor sets X-Forwarded-For to ip, or to a fresh documentation-range
// address per call when ip is empty.
type forwardedFor string

func (f forwardedFor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ip := string(f)
		if ip == "" {
			b := uuid.New()
			ip = fmt.Sprintf("2001:db8::%x:%x", uint16(b[0])<<8|uint16(b[1]), uint16(b[2])<<8|uint16(b[3]))
		}
		req.Header().Set("X-Forwarded-For", ip)
		return next(ctx, req)
	}
}

func (f forwardedFor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (f forwardedFor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
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

// signUp creates an account through the API and returns its id.
func (e *env) signUp(username string) uuid.UUID {
	e.t.Helper()
	res, err := e.auth().SignUp(context.Background(), connect.NewRequest(&perspectivv1.SignUpRequest{
		Email: emailOf(username), Password: goodPassword, Username: username, AgeConfirmed: true,
	}))
	if err != nil {
		e.t.Fatalf("SignUp(%s): %v", username, err)
	}
	id, err := uuid.Parse(res.Msg.GetUserId())
	if err != nil {
		e.t.Fatalf("SignUp user_id %q: %v", res.Msg.GetUserId(), err)
	}
	return id
}

func emailOf(username string) string { return strings.ToLower(username) + "@example.test" }

// login signs in and returns the response with the refresh cookie value.
func (e *env) login(username, password string) (*connect.Response[perspectivv1.LoginResponse], string, error) {
	e.t.Helper()
	res, err := e.auth().Login(context.Background(), connect.NewRequest(&perspectivv1.LoginRequest{
		Email: emailOf(username), Password: password,
	}))
	if err != nil {
		return nil, "", err
	}
	return res, setCookieValue(e.t, res.Header()), nil
}

func (e *env) mustLogin(username string) (access, refresh string) {
	e.t.Helper()
	res, cookie, err := e.login(username, goodPassword)
	if err != nil {
		e.t.Fatalf("Login(%s): %v", username, err)
	}
	return res.Msg.GetAccessToken(), cookie
}

// refresh calls Refresh presenting cookie (none when empty) and an optional
// stale bearer token, and returns the response and its new cookie value.
func (e *env) refresh(cookie, staleAccess string) (*connect.Response[perspectivv1.RefreshResponse], string, error) {
	e.t.Helper()
	req := connect.NewRequest(&perspectivv1.RefreshRequest{})
	if cookie != "" {
		req.Header().Set("Cookie", auth.RefreshCookieName+"="+cookie)
	}
	client := perspectivv1connect.NewAuthServiceClient(e.srv.Client(), e.srv.URL+"/api", clientOpts(staleAccess)...)
	res, err := client.Refresh(context.Background(), req)
	if err != nil {
		return nil, "", err
	}
	return res, setCookieValue(e.t, res.Header()), nil
}

// setCookieValue returns the refresh cookie a response set, checking the
// attributes every refresh cookie carries.
func setCookieValue(t *testing.T, h http.Header) string {
	t.Helper()
	for _, line := range h.Values("Set-Cookie") {
		c, err := http.ParseSetCookie(line)
		if err != nil || c.Name != auth.RefreshCookieName {
			continue
		}
		if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
			t.Fatalf("refresh cookie %q is not HttpOnly, Secure and SameSite=Strict", line)
		}
		return c.Value
	}
	t.Fatalf("no %s cookie in %v", auth.RefreshCookieName, h.Values("Set-Cookie"))
	return ""
}

// emailJob is one queued email job's arguments.
type emailJob struct {
	To       string            `json:"to"`
	Template string            `json:"template"`
	Data     map[string]string `json:"data"`
}

// emailJobs returns the queued email jobs to address, oldest first.
func (e *env) emailJobs(to string) []emailJob {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(),
		`SELECT args FROM river_job WHERE kind = 'email' ORDER BY id`)
	if err != nil {
		e.t.Fatalf("query river_job: %v", err)
	}
	defer rows.Close()
	var out []emailJob
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			e.t.Fatalf("scan: %v", err)
		}
		var j emailJob
		if err := json.Unmarshal(raw, &j); err != nil {
			e.t.Fatalf("decode %s: %v", raw, err)
		}
		if strings.EqualFold(j.To, to) {
			out = append(out, j)
		}
	}
	if err := rows.Err(); err != nil {
		e.t.Fatalf("rows: %v", err)
	}
	return out
}

// linkToken returns the token in the newest email of template sent to to,
// checking the link points at page under the public base URL.
func (e *env) linkToken(to, template, page string) string {
	e.t.Helper()
	jobs := e.emailJobs(to)
	for i := len(jobs) - 1; i >= 0; i-- {
		if jobs[i].Template != template {
			continue
		}
		u, err := url.Parse(jobs[i].Data["link"])
		if err != nil {
			e.t.Fatalf("link %q: %v", jobs[i].Data["link"], err)
		}
		if got := u.Scheme + "://" + u.Host + u.Path; got != baseURL+page {
			e.t.Fatalf("link %q does not point at %s%s", jobs[i].Data["link"], baseURL, page)
		}
		tok := u.Query().Get("token")
		if tok == "" {
			e.t.Fatalf("link %q has no token", jobs[i].Data["link"])
		}
		return tok
	}
	e.t.Fatalf("no %s email to %s", template, to)
	return ""
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
