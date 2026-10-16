package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"

	"perspectiv/internal/api"
	"perspectiv/internal/api/buildinfo"
	"perspectiv/internal/api/metrics"
)

// response is what a GET returned, with the body already read and closed.
type response struct {
	StatusCode int
	Header     http.Header
}

func get(t *testing.T, h http.Handler, path string) (response, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	body, err := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	return response{StatusCode: resp.StatusCode, Header: resp.Header}, string(body)
}

// checkHealth asserts a /healthz body.
func checkHealth(t *testing.T, body, wantStatus string, wantComps map[string]string) {
	t.Helper()
	var got api.HealthStatus
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if got.Status != wantStatus || len(got.Components) != len(wantComps) {
		t.Fatalf("health = %+v, want status %q components %v", got, wantStatus, wantComps)
	}
	for k, v := range wantComps {
		if got.Components[k] != v {
			t.Fatalf("component %s = %q, want %q", k, got.Components[k], v)
		}
	}
	if strings.Contains(body, "10.0.0.5") {
		t.Fatalf("error detail leaked: %s", body)
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		health     func(context.Context) map[string]error
		wantCode   int
		wantStatus string
		wantComps  map[string]string
		wantLogged int
	}{
		{name: "no checks means the process is up", wantCode: 200, wantStatus: "ok", wantComps: map[string]string{}},
		{
			name:     "all healthy",
			health:   api.HealthChecks(map[string]func(context.Context) error{"database": func(context.Context) error { return nil }}),
			wantCode: 200, wantStatus: "ok", wantComps: map[string]string{"database": "ok"},
		},
		{
			name: "one failing component is 503 and hides the error",
			health: api.HealthChecks(map[string]func(context.Context) error{
				"database": func(context.Context) error { return errors.New("dial tcp 10.0.0.5:5432: refused") },
				"jobs":     func(context.Context) error { return nil },
			}),
			wantCode: 503, wantStatus: "unavailable", wantComps: map[string]string{"database": "unavailable", "jobs": "ok"},
			wantLogged: 1,
		},
		{
			name: "a hung check times out as unavailable",
			health: api.HealthChecks(map[string]func(context.Context) error{
				"database": func(context.Context) error { time.Sleep(10 * time.Second); return nil },
			}),
			wantCode: 503, wantStatus: "unavailable", wantComps: map[string]string{"database": "unavailable"},
			wantLogged: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logs := &logSink{}
			h := api.NewRouter(api.RouterConfig{Log: zerolog.New(logs), Health: tc.health})
			started := time.Now()
			resp, body := get(t, h, "/healthz")
			if time.Since(started) > 5*time.Second {
				t.Fatal("/healthz did not bound a hung check")
			}
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", resp.StatusCode, tc.wantCode, body)
			}
			checkHealth(t, body, tc.wantStatus, tc.wantComps)
			if n := len(logs.lines(t, "health")); n != tc.wantLogged {
				t.Fatalf("logged %d health lines, want %d", n, tc.wantLogged)
			}
		})
	}
}

func TestMetricsEndpointExposesDomainCounters(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry()
	m := metrics.New(reg)
	m.PerspectivesPublished.Inc()
	m.EmailSendFailures.Add(2)
	resp, body := get(t, api.NewRouter(api.RouterConfig{Log: zerolog.Nop(), Registry: reg}), "/metrics")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for _, want := range []string{
		"perspectives_published_total 1",
		"claims_accepted_total 0",
		"canon_changes_total 0",
		"job_failures_total 0",
		"email_send_failures_total 2",
		"go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics lacks %q", want)
		}
	}
}

func TestAbout(t *testing.T) {
	t.Parallel()
	resp, body := get(t, api.NewRouter(api.RouterConfig{Log: zerolog.Nop()}), "/about")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, content type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var got buildinfo.Info
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if got != buildinfo.Get() {
		t.Fatalf("/about = %+v, want %+v", got, buildinfo.Get())
	}
}

func TestSPA(t *testing.T) {
	t.Parallel()
	static := fstest.MapFS{
		"index.html":      {Data: []byte("<!doctype html><title>app</title>")},
		"assets/app-1.js": {Data: []byte("console.log(1)")},
		"favicon.svg":     {Data: []byte("<svg/>")},
	}
	h := api.NewRouter(api.RouterConfig{Log: zerolog.Nop(), Static: static})

	tests := []struct {
		name      string
		path      string
		wantCode  int
		wantBody  string
		wantCache string
	}{
		{name: "root serves index", path: "/", wantCode: 200, wantBody: "<title>app</title>", wantCache: "no-cache"},
		{name: "client route falls back to index", path: "/places/123", wantCode: 200, wantBody: "<title>app</title>", wantCache: "no-cache"},
		{name: "index.html itself", path: "/index.html", wantCode: 200, wantBody: "<title>app</title>"},
		{name: "hashed asset is immutable", path: "/assets/app-1.js", wantCode: 200, wantBody: "console.log(1)", wantCache: "public, max-age=31536000, immutable"},
		{name: "root file", path: "/favicon.svg", wantCode: 200, wantBody: "<svg/>"},
		{name: "missing asset is 404, not index", path: "/assets/app-0.js", wantCode: 404},
		{name: "unknown api path is 404, not index", path: "/api/perspectiv.v1.Nope/Nope", wantCode: 404},
		{name: "traversal stays inside the bundle", path: "/../../etc/passwd", wantCode: 200, wantBody: "<title>app</title>"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, body := get(t, h, tc.path)
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tc.wantCode, body)
			}
			if !strings.Contains(body, tc.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", body, tc.wantBody)
			}
			if tc.wantCache != "" && resp.Header.Get("Cache-Control") != tc.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", resp.Header.Get("Cache-Control"), tc.wantCache)
			}
			if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("nosniff header missing")
			}
		})
	}
}

func TestNoStaticServesNoSPA(t *testing.T) {
	t.Parallel()
	resp, _ := get(t, api.NewRouter(api.RouterConfig{Log: zerolog.Nop()}), "/places/1")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
