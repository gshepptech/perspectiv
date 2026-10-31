package api_test

import (
	"context"
	"errors"
	"strings"
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
			want: []string{`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`, "<loc>https://perspectiv.example/sitemaps/places.xml</loc>",
				"<loc>https://perspectiv.example/sitemaps/days.xml</loc>", "<loc>https://perspectiv.example/sitemaps/stories.xml</loc>"},
		},
		{
			name: "a kind's pages with UTC lastmod and escaped URLs", path: "/sitemaps/stories.xml", status: 200,
			want: []string{"<loc>https://perspectiv.example/perspectives/p-1</loc><lastmod>2026-10-03T19:05:00Z</lastmod>",
				"<loc>https://perspectiv.example/perspectives/a&amp;b</loc></url>"},
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
