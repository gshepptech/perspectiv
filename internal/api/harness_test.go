package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
)

// logSink is a goroutine-safe buffer of JSON log lines.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// lines returns every logged line whose event field equals event.
func (s *logSink) lines(t *testing.T, event string) []map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(s.buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", raw)
		}
		if m["event"] == event {
			out = append(out, m)
		}
	}
	return out
}

var (
	userID  = uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001")
	adminID = uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000002")
	unverID = uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000003")
	suspID  = uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000004")
)

// fakeAuthn maps bearer tokens to callers. "expired" is an invalid
// credential; "broken" makes the authenticator itself fail.
type fakeAuthn struct{}

func (fakeAuthn) Authenticate(_ context.Context, h http.Header) (apiutil.Caller, bool, error) {
	switch h.Get("Authorization") {
	case "":
		return apiutil.Caller{}, false, nil
	case "Bearer user":
		return apiutil.Caller{UserID: userID, Role: perspectivv1.Role_ROLE_USER, SessionID: uuid.New()}, true, nil
	case "Bearer admin":
		return apiutil.Caller{UserID: adminID, Role: perspectivv1.Role_ROLE_ADMIN, SessionID: uuid.New()}, true, nil
	case "Bearer unverified":
		return apiutil.Caller{UserID: unverID, Role: perspectivv1.Role_ROLE_USER, SessionID: uuid.New()}, true, nil
	case "Bearer suspended":
		return apiutil.Caller{UserID: suspID, Role: perspectivv1.Role_ROLE_USER, SessionID: uuid.New()}, true, nil
	case "Bearer expired":
		return apiutil.Caller{}, false, apiutil.Unauthenticated("token expired")
	default:
		return apiutil.Caller{}, false, errors.New("session store unreachable")
	}
}

// fakeGate answers the write gate by user id; standing, when set, overrides
// the answer for every user.
type fakeGate struct {
	err      error
	standing api.Standing
}

func (g fakeGate) WriteStanding(_ context.Context, id uuid.UUID) (api.Standing, error) {
	switch {
	case g.err != nil:
		return api.StandingUnverified, g.err
	case g.standing != 0:
		return g.standing, nil
	case id == unverID:
		return api.StandingUnverified, nil
	case id == suspID:
		return api.StandingSuspended, nil
	}
	return api.StandingWriter, nil
}

// fakeLimiter records every Allow call and denies the subjects in deny.
type fakeLimiter struct {
	mu    sync.Mutex
	calls []string
	deny  map[string]bool
	err   error
}

func (l *fakeLimiter) Allow(_ context.Context, action, subject string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, action+" "+subject)
	if l.err != nil {
		return false, l.err
	}
	return !l.deny[subject], nil
}

func (l *fakeLimiter) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

// stack is a running server with the full interceptor chain in front of
// test handlers that record the caller they saw.
type stack struct {
	srv  *httptest.Server
	logs *logSink
	lim  *fakeLimiter

	mu   sync.Mutex
	seen map[string]apiutil.Caller
	hits map[string]int
}

type stackOpts struct {
	gate api.Gate
	lim  *fakeLimiter
}

// testProcedures are mounted on every stack; the last is not in api.Routes.
var testProcedures = []string{
	"/perspectiv.v1.PlaceService/GetPlace",
	"/perspectiv.v1.UserService/UpdateProfile",
	"/perspectiv.v1.AuthService/SignUp",
	"/perspectiv.v1.PerspectiveService/Publish",
	"/perspectiv.v1.ModerationService/ListReports",
	"/perspectiv.v1.ModerationService/Resolve",
	"/perspectiv.v1.ModerationService/Report",
	"/perspectiv.v1.TestService/NotInRoutes",
}

func newStack(t *testing.T, o stackOpts) *stack {
	t.Helper()
	if o.gate == nil {
		o.gate = fakeGate{}
	}
	if o.lim == nil {
		o.lim = &fakeLimiter{}
	}
	s := &stack{logs: &logSink{}, lim: o.lim, seen: map[string]apiutil.Caller{}, hits: map[string]int{}}
	log := zerolog.New(s.logs)
	ics := api.Interceptors(log, fakeAuthn{}, o.gate, o.lim)

	var mounts []api.Mount
	for _, proc := range testProcedures {
		mounts = append(mounts, func(opts ...connect.HandlerOption) (string, http.Handler) {
			opts = append(opts, connect.WithInterceptors(ics...))
			return proc, connect.NewUnaryHandler(proc, func(ctx context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
				if req.Header().Get("X-Test-Panic") != "" {
					panic("handler bug")
				}
				c, _ := apiutil.CallerFrom(ctx)
				s.mu.Lock()
				s.seen[proc] = c
				s.hits[proc]++
				s.mu.Unlock()
				return connect.NewResponse(&emptypb.Empty{}), nil
			}, opts...)
		})
	}
	s.srv = httptest.NewServer(api.NewRouter(api.RouterConfig{Log: log, Handlers: mounts}))
	t.Cleanup(s.srv.Close)
	return s
}

// call invokes procedure with the given bearer token ("" for none) and
// extra headers, returning the response and error.
func (s *stack) call(t *testing.T, procedure, token string, headers map[string]string) (*connect.Response[emptypb.Empty], error) {
	t.Helper()
	client := connect.NewClient[emptypb.Empty, emptypb.Empty](s.srv.Client(), s.srv.URL+"/api"+procedure)
	req := connect.NewRequest(&emptypb.Empty{})
	if token != "" {
		req.Header().Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header().Set(k, v)
	}
	return client.CallUnary(context.Background(), req)
}

func (s *stack) handlerHits(procedure string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[procedure]
}
