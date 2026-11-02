package main

import (
	"context"
	"fmt"
	"time"

	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/place"
)

type sitemapList func(ctx context.Context) ([]apiutil.SitemapEntry, error)

// guidePages are the fixed reading pages the "pages" file lists ahead of the
// best-days pages, each with the day its content last changed. A page's date
// moves when its text does.
var guidePages = []api.SitemapPage{
	{Path: "/how-it-works", LastMod: time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)},
}

// sitemap maps each sitemap file to the domain that lists its pages and the
// path the SPA reads them at. The "pages" file is guidePages, then the best
// days at each kind of place, /best/<slug>, which categories lists.
type sitemap struct {
	places, days, stories sitemapList
	categories            func(ctx context.Context) ([]place.CategoryPage, error)
}

func (s sitemap) pages(ctx context.Context, kind string) ([]api.SitemapPage, error) {
	var (
		list   sitemapList
		prefix string
	)
	switch kind {
	case "pages":
		return s.fixedPages(ctx)
	case "places":
		list, prefix = s.places, "/places/"
	case "days":
		list, prefix = s.days, "/arcs/"
	case "stories":
		list, prefix = s.stories, "/perspectives/"
	default:
		return nil, fmt.Errorf("unknown sitemap %q", kind)
	}
	entries, err := list(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.SitemapPage, len(entries))
	for i, e := range entries {
		out[i] = api.SitemapPage{Path: prefix + e.ID.String(), LastMod: e.LastMod}
	}
	return out, nil
}

func (s sitemap) fixedPages(ctx context.Context) ([]api.SitemapPage, error) {
	cats, err := s.categories(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.SitemapPage, 0, len(guidePages)+len(cats))
	out = append(out, guidePages...)
	for _, c := range cats {
		out = append(out, api.SitemapPage{Path: "/best/" + place.CategorySlug(c.Category), LastMod: c.LastMod})
	}
	return out, nil
}
