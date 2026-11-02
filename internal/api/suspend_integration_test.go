//go:build integration

package api_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"perspectiv/pkg/crypto"
)

func TestMain(m *testing.M) { testdb.Main(m) }

const suspendPassword = "correct horse battery staple"

// gateServer is the production interceptor chain and router in front of the
// real auth service on a fresh database. Every other service answers
// Unimplemented, so a call that passes authorization is seen to reach its
// handler, and a call the gate refuses never does.
type gateServer struct {
	t    *testing.T
	pool *pgxpool.Pool
	srv  *httptest.Server
}

func newGateServer(t *testing.T) *gateServer {
	t.Helper()
	pool := testdb.New(t)
	log := zerolog.New(io.Discard)
	river, err := jobs.NewClient(pool, log, metrics.New(prometheus.NewRegistry()), nil, nil)
	if err != nil {
		t.Fatalf("jobs.NewClient: %v", err)
	}
	lim := auth.NewLimiter(pool)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("crypto.GenerateKey: %v", err)
	}
	enc, err := crypto.NewEncryptor(key)
	if err != nil {
		t.Fatalf("crypto.NewEncryptor: %v", err)
	}
	svc, err := auth.NewService(pool, jobs.NewEnqueuer(river), lim, enc, auth.Config{
		JWTSecret:     "integration-secret-0123456789abcdef",
		PublicBaseURL: "https://perspectiv.example",
		CookieSecure:  true,
		BcryptCost:    bcrypt.MinCost,
		SignUpMode:    auth.SignUpOpen,
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
			return perspectivv1connect.NewPerspectiveServiceHandler(perspectivv1connect.UnimplementedPerspectiveServiceHandler{}, append(o, ics)...)
		},
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewArcServiceHandler(perspectivv1connect.UnimplementedArcServiceHandler{}, append(o, ics)...)
		},
		func(o ...connect.HandlerOption) (string, http.Handler) {
			return perspectivv1connect.NewLoreServiceHandler(perspectivv1connect.UnimplementedLoreServiceHandler{}, append(o, ics)...)
		},
	}
	srv := httptest.NewServer(api.NewRouter(api.RouterConfig{Log: log, Handlers: mounts}))
	t.Cleanup(srv.Close)
	return &gateServer{t: t, pool: pool, srv: srv}
}

// clientAddr gives every call its own documentation-range client address,
// so the per-IP signup and login limits never decide these tests.
type clientAddr struct{ bearer string }

func (c clientAddr) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		b := uuid.New()
		req.Header().Set("X-Forwarded-For", fmt.Sprintf("2001:db8::%x:%x", uint16(b[0])<<8|uint16(b[1]), uint16(b[2])<<8|uint16(b[3])))
		if c.bearer != "" {
			req.Header().Set("Authorization", "Bearer "+c.bearer)
		}
		return next(ctx, req)
	}
}

func (clientAddr) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (clientAddr) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func (g *gateServer) opts(access string) []connect.ClientOption {
	return []connect.ClientOption{connect.WithInterceptors(clientAddr{bearer: access})}
}

// verifiedAuthor signs up username, marks its email verified and returns
// its id.
func (g *gateServer) verifiedAuthor(username string) uuid.UUID {
	g.t.Helper()
	res, err := perspectivv1connect.NewAuthServiceClient(g.srv.Client(), g.srv.URL+"/api", g.opts("")...).
		SignUp(context.Background(), connect.NewRequest(&perspectivv1.SignUpRequest{
			Email: username + "@example.test", Password: suspendPassword, Username: username, AgeConfirmed: true,
		}))
	if err != nil {
		g.t.Fatalf("SignUp(%s): %v", username, err)
	}
	id := uuid.MustParse(res.Msg.GetUserId())
	g.exec(`UPDATE users SET email_verified_at = now() WHERE id = $1`, id)
	return id
}

func (g *gateServer) login(username string) string {
	g.t.Helper()
	res, err := perspectivv1connect.NewAuthServiceClient(g.srv.Client(), g.srv.URL+"/api", g.opts("")...).
		Login(context.Background(), connect.NewRequest(&perspectivv1.LoginRequest{Email: username + "@example.test", Password: suspendPassword}))
	if err != nil {
		g.t.Fatalf("Login(%s): %v", username, err)
	}
	return res.Msg.GetAccessToken()
}

func (g *gateServer) exec(query string, args ...any) {
	g.t.Helper()
	if _, err := g.pool.Exec(context.Background(), query, args...); err != nil {
		g.t.Fatalf("%s: %v", query, err)
	}
}

// authorWrites makes each write an author performs on text already
// published, other than Publish itself, as the holder of access.
func (g *gateServer) authorWrites(access string) map[string]error {
	g.t.Helper()
	ctx := context.Background()
	h, base, o := g.srv.Client(), g.srv.URL+"/api", g.opts(access)
	out := map[string]error{}
	_, out["PerspectiveService/Update"] = perspectivv1connect.NewPerspectiveServiceClient(h, base, o...).
		Update(ctx, connect.NewRequest(&perspectivv1.PerspectiveServiceUpdateRequest{}))
	_, out["ArcService/RetitleHourArc"] = perspectivv1connect.NewArcServiceClient(h, base, o...).
		RetitleHourArc(ctx, connect.NewRequest(&perspectivv1.RetitleHourArcRequest{}))
	_, out["LoreService/ResolveClaim"] = perspectivv1connect.NewLoreServiceClient(h, base, o...).
		ResolveClaim(ctx, connect.NewRequest(&perspectivv1.ResolveClaimRequest{}))
	return out
}

// A suspended author cannot rewrite its published text, retitle its hour
// arc or accept a claim on its characters: the shared write gate refuses
// each before the handler runs, with the token it held before the
// suspension and with one from a fresh login. An author in good standing
// reaches the handler on the same calls.
func TestSuspendedAuthorCannotEditPublishedWork(t *testing.T) {
	t.Parallel()
	g := newGateServer(t)
	badID := g.verifiedAuthor("suspended_author")
	g.verifiedAuthor("good_author")
	before := g.login("suspended_author")

	// The write moderation's SUSPEND_USER action makes.
	g.exec(`UPDATE users SET suspended_at = now() WHERE id = $1`, badID)

	tokens := []struct{ name, access string }{
		{"token from before the suspension", before},
		{"fresh login after the suspension", g.login("suspended_author")},
	}
	for _, tok := range tokens {
		for rpc, err := range g.authorWrites(tok.access) {
			if connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), "your account is suspended") {
				t.Errorf("suspended, %s: %s = %v, want PermissionDenied naming the suspension before the handler", tok.name, rpc, err)
			}
		}
	}
	for rpc, err := range g.authorWrites(g.login("good_author")) {
		if connect.CodeOf(err) != connect.CodeUnimplemented {
			t.Errorf("author in good standing: %s = %v, want it to reach the handler", rpc, err)
		}
	}
}
