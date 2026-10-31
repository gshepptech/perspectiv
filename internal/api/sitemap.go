package api

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

// SitemapPage is one public page the sitemap lists: its path, such as
// /places/<id>, and when its content last changed.
type SitemapPage struct {
	Path    string
	LastMod time.Time
}

// SitemapSource lists the pages of one kind ("places", "days" or
// "stories") that a signed-out reader may open, at most 50,000.
type SitemapSource func(ctx context.Context, kind string) ([]SitemapPage, error)

// sitemapKinds are the sitemap files /sitemap.xml indexes, in order.
var sitemapKinds = []string{"places", "days", "stories"}

// sitemapTimeout bounds building one sitemap file.
const sitemapTimeout = 10 * time.Second

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

// sitemapHandler serves /sitemaps/{kind}.xml from src. A failed read is a
// 503 so a crawler retries later rather than dropping every page.
func sitemapHandler(log zerolog.Logger, baseURL string, src SitemapSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := strings.TrimSuffix(chi.URLParam(r, "file"), ".xml")
		if !strings.HasSuffix(chi.URLParam(r, "file"), ".xml") || !known(kind) {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), sitemapTimeout)
		defer cancel()
		pages, err := src(ctx, kind)
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
