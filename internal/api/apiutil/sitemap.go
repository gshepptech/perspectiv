package apiutil

import (
	"time"

	"github.com/google/uuid"
)

// SitemapLimit is the most URLs one sitemap file may list (sitemaps.org).
const SitemapLimit = 50000

// SitemapEntry is one public page a domain lists in the sitemap: the id the
// page's path is built from, and when its content last changed.
type SitemapEntry struct {
	ID      uuid.UUID
	LastMod time.Time
}
