//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// syncBuffer is a bytes.Buffer safe to write from the server's goroutines
// while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// freeAddr returns a loopback address with a port nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// httpGet issues a GET bound to the test's lifetime.
func httpGet(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return http.DefaultClient.Do(req)
}

// waitHealthy polls /healthz at base until it answers 200 or done yields an
// early exit code.
func waitHealthy(t *testing.T, base string, done <-chan int, stderr *syncBuffer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		select {
		case code := <-done:
			t.Fatalf("exited early with %d; stderr:\n%s", code, stderr.String())
		default:
		}
		resp, err := httpGet(t, base+"/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("never became healthy (last err %v); stderr:\n%s", err, stderr.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// server is one perspectiv process started through run.
type server struct {
	base   string
	stderr *syncBuffer
}

// startServer runs the binary's serve path with ROLE=role against pool until
// the test ends, and waits for /healthz to answer 200. Cleanup cancels it and
// requires a clean exit.
func startServer(t *testing.T, pool *pgxpool.Pool, role string) *server {
	t.Helper()
	addr := freeAddr(t)
	e := with(validEnv(),
		"ROLE", role,
		"DATABASE_URL", pool.Config().ConnString(),
		"HTTP_ADDR", addr,
		"PUBLIC_BASE_URL", "http://localhost:5173",
		"COOKIE_SECURE", "false",
		"SMTP_ADDR", "127.0.0.1:1",
		"AVATAR_DIR", t.TempDir(),
		"RANK_INTERVAL", "1h",
		"LOG_LEVEL", "debug",
	)
	ctx, cancel := context.WithCancel(context.Background())
	s := &server{base: "http://" + addr, stderr: &syncBuffer{}}
	done := make(chan int, 1)
	go func() { done <- run(ctx, nil, env(e), s.stderr) }()
	t.Cleanup(func() {
		cancel()
		select {
		case code := <-done:
			if code != exitOK {
				t.Errorf("ROLE=%s exit = %d after shutdown; stderr:\n%s", role, code, s.stderr.String())
			}
		case <-time.After(30 * time.Second):
			t.Errorf("ROLE=%s did not stop within 30s", role)
		}
	})

	waitHealthy(t, s.base, done, s.stderr, 30*time.Second)
	return s
}

func (s *server) get(t *testing.T, path string) (int, string) {
	t.Helper()
	resp, err := httpGet(t, s.base+path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

// servesAPI reports whether a public Connect procedure is mounted. A missing
// procedure is HTTP 404, which Connect reports as Unimplemented.
func (s *server) servesAPI(t *testing.T) bool {
	t.Helper()
	client := perspectivv1connect.NewPlaceServiceClient(http.DefaultClient, s.base+"/api")
	_, err := client.SearchPlaces(context.Background(), connect.NewRequest(&perspectivv1.SearchPlacesRequest{Query: "Austin"}))
	if err == nil {
		return true
	}
	if connect.CodeOf(err) == connect.CodeUnimplemented {
		return false
	}
	t.Fatalf("SearchPlaces: unexpected error %v", err)
	return false
}

// insertRankJob inserts one lore_rank job from outside the server, the way
// any producer would, and returns its id.
func insertRankJob(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	client, err := jobs.NewClient(pool, zerolog.Nop(), metrics.New(prometheus.NewRegistry()), nil, nil)
	if err != nil {
		t.Fatalf("insert-only client: %v", err)
	}
	res, err := client.Insert(context.Background(), jobs.RankArgs{}, nil)
	if err != nil {
		t.Fatalf("insert rank job: %v", err)
	}
	return res.Job.ID
}

func jobState(t *testing.T, pool *pgxpool.Pool, id int64) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(context.Background(), `SELECT state::text FROM river_job WHERE id = $1`, id).Scan(&state); err != nil {
		t.Fatalf("job %d state: %v", id, err)
	}
	return state
}

// waitJobState polls until job id reaches want or the timeout passes, and
// returns the last state seen.
func waitJobState(t *testing.T, pool *pgxpool.Pool, id int64, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		state := jobState(t, pool, id)
		if state == want || time.Now().After(deadline) {
			return state
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// OT-044, GI-005, FR-059, AC-044: ROLE=api serves the API and runs no River
// workers; ROLE=worker runs the workers and serves no API, but still answers
// /healthz and /metrics; ROLE=all does both.
func TestServeByRole(t *testing.T) {
	t.Parallel()
	cases := []struct {
		role           string
		api, workers   bool
		wantComponents []string
	}{
		{"api", true, false, []string{"database"}},
		{"worker", false, true, []string{"database", "jobs"}},
		{"all", true, true, []string{"database", "jobs"}},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			t.Parallel()
			pool := testdb.New(t)
			s := startServer(t, pool, tc.role)

			checkHealth(t, s, tc.wantComponents)
			checkMetrics(t, s)
			if got := s.servesAPI(t); got != tc.api {
				t.Errorf("serves the Connect API = %v, want %v", got, tc.api)
			}
			checkSPA(t, s, tc.api)
			checkJobsWorked(t, pool, tc.workers)
		})
	}
}

func checkHealth(t *testing.T, s *server, want []string) {
	t.Helper()
	code, body := s.get(t, "/healthz")
	var health struct {
		Status     string            `json:"status"`
		Components map[string]string `json:"components"`
	}
	if err := json.Unmarshal([]byte(body), &health); err != nil || code != http.StatusOK || health.Status != "ok" {
		t.Fatalf("/healthz = %d %s (%v)", code, body, err)
	}
	if len(health.Components) != len(want) {
		t.Errorf("/healthz components = %v, want %v", health.Components, want)
	}
	for _, c := range want {
		if health.Components[c] != "ok" {
			t.Errorf("/healthz component %s = %q", c, health.Components[c])
		}
	}
}

func checkMetrics(t *testing.T, s *server) {
	t.Helper()
	code, body := s.get(t, "/metrics")
	if code != http.StatusOK || !strings.Contains(body, "job_failures_total") || !strings.Contains(body, "perspectives_published_total") {
		t.Errorf("/metrics = %d without the domain counters", code)
	}
}

// checkSPA: the embedded index.html answers / and a client-side route
// exactly where the API runs.
func checkSPA(t *testing.T, s *server, want bool) {
	t.Helper()
	for _, path := range []string{"/", "/places/abc"} {
		code, body := s.get(t, path)
		got := code == http.StatusOK && strings.Contains(strings.ToLower(body), "<!doctype html")
		if got != want {
			t.Errorf("GET %s = %d (SPA %v), want SPA %v", path, code, got, want)
		}
	}
}

// checkJobsWorked inserts a rank job and checks whether this process works it.
func checkJobsWorked(t *testing.T, pool *pgxpool.Pool, workers bool) {
	t.Helper()
	id := insertRankJob(t, pool)
	if workers {
		if state := waitJobState(t, pool, id, "completed", 20*time.Second); state != "completed" {
			t.Errorf("rank job state = %s, want completed by the worker", state)
		}
		return
	}
	// River workers pick up an available job within a second (LISTEN/NOTIFY,
	// else a one-second poll). Three seconds with the job untouched shows
	// nothing is working it.
	if state := waitJobState(t, pool, id, "completed", 3*time.Second); state != "available" {
		t.Errorf("rank job state = %s without workers, want available", state)
	}
	var periodic int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'lore_rank' AND id <> $1`, id).Scan(&periodic); err != nil {
		t.Fatal(err)
	}
	if periodic != 0 {
		t.Errorf("a process without workers inserted %d periodic rank jobs", periodic)
	}
}

// The serve path applies migrations first when MIGRATE_ON_START is true.
func TestServeMigrateOnStart(t *testing.T) {
	t.Parallel()
	pool := testdb.NewEmpty(t)
	addr := freeAddr(t)
	e := with(validEnv(),
		"ROLE", "api",
		"DATABASE_URL", pool.Config().ConnString(),
		"HTTP_ADDR", addr,
		"AVATAR_DIR", t.TempDir(),
		"MIGRATE_ON_START", "true",
	)
	ctx, cancel := context.WithCancel(context.Background())
	var stderr syncBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, nil, env(e), &stderr) }()
	defer func() {
		cancel()
		if code := <-done; code != exitOK {
			t.Errorf("exit = %d; stderr:\n%s", code, stderr.String())
		}
	}()
	waitHealthy(t, "http://"+addr, done, &stderr, 60*time.Second)
	var users bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&users); err != nil || !users {
		t.Fatalf("users table after MIGRATE_ON_START: %v %v", users, err)
	}
}

// AC-044: perspectiv migrate applies the goose and River migrations to an
// empty database and exits 0; running it again is a no-op that also exits 0.
func TestMigrateSubcommand(t *testing.T) {
	t.Parallel()
	pool := testdb.NewEmpty(t)
	e := map[string]string{"DATABASE_URL": pool.Config().ConnString(), "LOG_LEVEL": "info"}
	for i := range 2 {
		var stderr bytes.Buffer
		if code := run(context.Background(), []string{"migrate"}, env(e), &stderr); code != exitOK {
			t.Fatalf("migrate run %d exit = %d; stderr:\n%s", i+1, code, stderr.String())
		}
	}
	var users, riverJob bool
	err := pool.QueryRow(context.Background(),
		`SELECT to_regclass('public.users') IS NOT NULL, to_regclass('public.river_job') IS NOT NULL`).Scan(&users, &riverJob)
	if err != nil || !users || !riverJob {
		t.Fatalf("after migrate: users %v river_job %v err %v", users, riverJob, err)
	}
}

// CT-015, OT-006, AC-039: perspectiv admin grant <username> sets the account's
// role to ADMIN and writes an audit row; an unknown username exits non-zero
// and changes nothing.
func TestAdminGrant(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	ctx := context.Background()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, email_verified_at, password_hash, username, age_confirmed_at)
		VALUES ('founder@example.test', now(), 'x', 'Founder', now()) RETURNING id::text`).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	e := map[string]string{"DATABASE_URL": pool.Config().ConnString()}

	var stderr bytes.Buffer
	if code := run(ctx, []string{"admin", "grant", "Founder"}, env(e), &stderr); code != exitOK {
		t.Fatalf("grant exit = %d; stderr:\n%s", code, stderr.String())
	}
	var role string
	var audits int
	if err := pool.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, id).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM moderation_audit WHERE action = 'ADMIN_GRANT' AND target_id = $1`, id).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if role != perspectivv1.Role_ROLE_ADMIN.String() || audits != 1 {
		t.Fatalf("after grant: role %s, audit rows %d; want ROLE_ADMIN, 1", role, audits)
	}

	stderr.Reset()
	code := run(ctx, []string{"admin", "grant", "nobody_here"}, env(e), &stderr)
	if code == exitOK {
		t.Fatal("grant to an unknown username exited 0")
	}
	if !strings.Contains(stderr.String(), "nobody_here") {
		t.Errorf("stderr does not name the unknown username:\n%s", stderr.String())
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM moderation_audit`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("audit rows after failed grant = %d, want 1", total)
	}
}
