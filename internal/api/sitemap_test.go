package api_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"perspectiv/internal/api"
)

func sitemapRouter(src api.SitemapSource, base string) api.RouterConfig {
	return api.RouterConfig{Log: zerolog.Nop(), PublicBaseURL: base, Sitemap: src}
}

func TestRobotsAndSitemap(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 3, 14, 5, 0, 0, time.FixedZone("CDT", -5*3600))
	src := func(_ context.Context, kind string) ([]api.SitemapPage, error) {
		switch kind {
		case "stories":
			return []api.SitemapPage{{Path: "/perspectives/p-1", LastMod: at}, {Path: "/perspectives/a&b"}}, nil
		case "pages":
			return []api.SitemapPage{{Path: "/best/sports-and-recreation", LastMod: at}}, nil
		case "days":
			return nil, errors.New("database down")
		}
		return nil, nil
	}
	h := api.NewRouter(sitemapRouter(src, "https://perspectiv.example/"))

	tests := []struct {
		name, path string
		status     int
		want       []string
		absent     []string
	}{
		{
			name: "robots names the sitemap and keeps crawlers off account pages", path: "/robots.txt", status: 200,
			want:   []string{"User-agent: *", "Disallow: /api/", "Disallow: /admin/", "Disallow: /settings", "Sitemap: https://perspectiv.example/sitemap.xml"},
			absent: []string{"Disallow: /places", "Disallow: /perspectives", "Disallow: /arcs"},
		},
		{
			name: "index lists each kind", path: "/sitemap.xml", status: 200,
			want: []string{`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`,
				"<loc>https://perspectiv.example/sitemaps/pages.xml</loc>", "<loc>https://perspectiv.example/sitemaps/places.xml</loc>",
				"<loc>https://perspectiv.example/sitemaps/days.xml</loc>", "<loc>https://perspectiv.example/sitemaps/stories.xml</loc>"},
		},
		{
			name: "a kind's pages with UTC lastmod and escaped URLs", path: "/sitemaps/stories.xml", status: 200,
			want: []string{"<loc>https://perspectiv.example/perspectives/p-1</loc><lastmod>2026-10-03T19:05:00Z</lastmod>",
				"<loc>https://perspectiv.example/perspectives/a&amp;b</loc></url>"},
		},
		{
			name: "the listing pages", path: "/sitemaps/pages.xml", status: 200,
			want: []string{"<loc>https://perspectiv.example/best/sports-and-recreation</loc><lastmod>2026-10-03T19:05:00Z</lastmod>"},
		},
		{name: "an empty kind is an empty set", path: "/sitemaps/places.xml", status: 200, want: []string{"<urlset"}},
		{name: "a failed read asks the crawler to come back", path: "/sitemaps/days.xml", status: 503},
		{name: "an unknown kind", path: "/sitemaps/users.xml", status: 404},
		{name: "not an xml file", path: "/sitemaps/places", status: 404},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, body := get(t, h, tc.path)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d; body %q", resp.StatusCode, tc.status, body)
			}
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("body lacks %q:\n%s", w, body)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(body, a) {
					t.Errorf("body has %q", a)
				}
			}
		})
	}
}

// Without a sitemap source or a base URL, robots.txt still answers and
// names no sitemap, and /sitemap.xml falls through to the SPA's 404.
func TestRobotsWithoutSitemap(t *testing.T) {
	t.Parallel()
	for _, cfg := range []api.RouterConfig{sitemapRouter(nil, "https://perspectiv.example"), sitemapRouter(func(context.Context, string) ([]api.SitemapPage, error) { return nil, nil }, "")} {
		h := api.NewRouter(cfg)
		resp, body := get(t, h, "/robots.txt")
		if resp.StatusCode != 200 || strings.Contains(body, "Sitemap:") || !strings.Contains(body, "Disallow: /api/") {
			t.Fatalf("robots = %d %q", resp.StatusCode, body)
		}
		if resp, _ := get(t, h, "/sitemap.xml"); resp.StatusCode == 200 {
			t.Fatal("sitemap served without a source and a base URL")
		}
	}
}

// fetch GETs path from srv and returns the status and body. It reports
// failures with t.Error, so it is safe from any goroutine.
func fetch(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Error(err)
		return 0, ""
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Errorf("GET %s: %v", path, err)
		return 0, ""
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("read %s: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

// A sitemap file is built once and served from memory: concurrent requests
// share one build, whether they join it in flight or read its result after.
func TestSitemapConcurrentRequestsBuildOnce(t *testing.T) {
	t.Parallel()
	const readers = 25
	var builds atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	src := func(_ context.Context, kind string) ([]api.SitemapPage, error) {
		builds.Add(1)
		once.Do(func() { close(started) })
		<-release
		return []api.SitemapPage{{Path: "/places/" + kind}}, nil
	}
	srv := httptest.NewServer(api.NewRouter(sitemapRouter(src, "https://perspectiv.example")))
	t.Cleanup(srv.Close)

	var wg sync.WaitGroup
	codes := make([]int, readers)
	for i := range readers {
		wg.Go(func() { codes[i], _ = fetch(t, srv, "/sitemaps/places.xml") })
	}
	<-started
	close(release)
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusOK {
			t.Errorf("request %d = %d, want 200", i, c)
		}
	}
	if code, body := fetch(t, srv, "/sitemaps/places.xml"); code != http.StatusOK || !strings.Contains(body, "/places/places</loc>") {
		t.Fatalf("a later request = %d %q", code, body)
	}
	if got := builds.Load(); got != 1 {
		t.Fatalf("%d requests built the file %d times, want 1", readers+1, got)
	}
}

// A failed build is not kept: the request after it builds again, and that
// result is kept.
func TestSitemapFailedBuildIsNotKept(t *testing.T) {
	t.Parallel()
	var builds atomic.Int32
	src := func(_ context.Context, kind string) ([]api.SitemapPage, error) {
		if builds.Add(1) == 1 {
			return nil, errors.New("database down")
		}
		return []api.SitemapPage{{Path: "/places/" + kind}}, nil
	}
	srv := httptest.NewServer(api.NewRouter(sitemapRouter(src, "https://perspectiv.example")))
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		code       int
		wantBuilds int32
	}{
		{name: "the failure is a 503", code: http.StatusServiceUnavailable, wantBuilds: 1},
		{name: "the next request builds again", code: http.StatusOK, wantBuilds: 2},
		{name: "and that build is kept", code: http.StatusOK, wantBuilds: 2},
	}
	// Serial: each case depends on the requests before it.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if code, body := fetch(t, srv, "/sitemaps/places.xml"); code != tc.code {
				t.Fatalf("status = %d, want %d; body %q", code, tc.code, body)
			}
			if got := builds.Load(); got != tc.wantBuilds {
				t.Fatalf("built %d times, want %d", got, tc.wantBuilds)
			}
		})
	}
}
