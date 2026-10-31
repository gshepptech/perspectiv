package api

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"
)

// SitemapPage is one public page the sitemap lists: its path, such as
// /places/<id>, and when its content last changed.
type SitemapPage struct {
	Path    string
	LastMod time.Time
}

// SitemapSource lists the pages of one kind ("pages", "places", "days" or
// "stories") that a signed-out reader may open, at most 50,000.
type SitemapSource func(ctx context.Context, kind string) ([]SitemapPage, error)

// sitemapKinds are the sitemap files /sitemap.xml indexes, in order. "pages"
// holds the listing pages that are not one place, day or story, such as the
// best days at each kind of place.
var sitemapKinds = []string{"pages", "places", "days", "stories"}

// sitemapTimeout bounds building one sitemap file.
const sitemapTimeout = 10 * time.Second

// sitemapTTL is how long a built sitemap file is served before it is built
// again. Each build is an aggregate over every readable story; crawlers
// re-read a sitemap hours apart, so half an hour stale costs nothing, while
// building per request let a burst of requests hold the database pool.
const sitemapTTL = 30 * time.Minute

// sitemapCache keeps each built sitemap file for sitemapTTL. Concurrent
// misses for one kind share a single build; a failed build is not kept, so
// the next request tries again.
type sitemapCache struct {
	src   SitemapSource
	ttl   time.Duration
	now   func() time.Time
	group singleflight.Group

	mu    sync.Mutex
	built map[string]builtSitemap
}

type builtSitemap struct {
	pages []SitemapPage
	at    time.Time
}

func newSitemapCache(src SitemapSource, ttl time.Duration, now func() time.Time) *sitemapCache {
	return &sitemapCache{src: src, ttl: ttl, now: now, built: map[string]builtSitemap{}}
}

// pages returns kind's pages, built at most once per ttl. The build runs
// detached from the request that started it, so one crawler hanging up does
// not fail the others waiting on the same build.
func (c *sitemapCache) pages(ctx context.Context, kind string) ([]SitemapPage, error) {
	c.mu.Lock()
	b, ok := c.built[kind]
	c.mu.Unlock()
	if ok && c.now().Sub(b.at) < c.ttl {
		return b.pages, nil
	}
	ch := c.group.DoChan(kind, func() (any, error) {
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sitemapTimeout)
		defer cancel()
		pages, err := c.src(bctx, kind)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.built[kind] = builtSitemap{pages: pages, at: c.now()}
		c.mu.Unlock()
		return pages, nil
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		pages, _ := res.Val.([]SitemapPage)
		return pages, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// crawlerOff are the paths robots.txt asks crawlers to skip: the API, the
// account, writing and admin pages, and one-time links. Reading pages stay
// open.
var crawlerOff = []string{
	"/api/", "/admin/", "/settings", "/compose", "/inbox", "/claims", "/saved",
	"/login", "/signup", "/i/", "/unsubscribe", "/verify-email", "/confirm-email",
	"/forgot-password", "/reset-password",
}

// robotsHandler serves robots.txt, naming the sitemap when there is one.
func robotsHandler(baseURL string, withSitemap bool) http.HandlerFunc {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	for _, p := range crawlerOff {
		b.WriteString("Disallow: " + p + "\n")
	}
	if withSitemap && baseURL != "" {
		b.WriteString("\nSitemap: " + baseURL + "/sitemap.xml\n")
	}
	body := b.String()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte(body))
	}
}

type sitemapIndex struct {
	XMLName  xml.Name     `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 sitemapindex"`
	Sitemaps []sitemapLoc `xml:"sitemap"`
}

type sitemapLoc struct {
	Loc string `xml:"loc"`
}

type urlSet struct {
	XMLName xml.Name   `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []urlEntry `xml:"url"`
}

type urlEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// sitemapIndexHandler serves /sitemap.xml, the index of the per-kind files.
func sitemapIndexHandler(baseURL string) http.HandlerFunc {
	idx := sitemapIndex{}
	for _, k := range sitemapKinds {
		idx.Sitemaps = append(idx.Sitemaps, sitemapLoc{Loc: baseURL + "/sitemaps/" + k + ".xml"})
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		writeXML(w, idx)
	}
}

// sitemapHandler serves /sitemaps/{kind}.xml from src through a
// sitemapCache. A failed read is a 503 so a crawler retries later rather
// than dropping every page.
func sitemapHandler(log zerolog.Logger, baseURL string, src SitemapSource) http.HandlerFunc {
	cache := newSitemapCache(src, sitemapTTL, time.Now)
	return func(w http.ResponseWriter, r *http.Request) {
		kind := strings.TrimSuffix(chi.URLParam(r, "file"), ".xml")
		if !strings.HasSuffix(chi.URLParam(r, "file"), ".xml") || !known(kind) {
			http.NotFound(w, r)
			return
		}
		pages, err := cache.pages(r.Context(), kind)
		if err != nil {
			log.Error().Err(err).Str("kind", kind).Msg("sitemap")
			http.Error(w, "sitemap unavailable", http.StatusServiceUnavailable)
			return
		}
		set := urlSet{URLs: make([]urlEntry, len(pages))}
		for i, p := range pages {
			e := urlEntry{Loc: baseURL + p.Path}
			if !p.LastMod.IsZero() {
				e.LastMod = p.LastMod.UTC().Format(time.RFC3339)
			}
			set.URLs[i] = e
		}
		writeXML(w, set)
	}
}

func known(kind string) bool {
	for _, k := range sitemapKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func writeXML(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(v)
}
