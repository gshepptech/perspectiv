package main

import (
	"context"
	"fmt"

	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/place"
)

type sitemapList func(ctx context.Context) ([]apiutil.SitemapEntry, error)

// sitemap maps each sitemap file to the domain that lists its pages and the
// path the SPA reads them at. categories lists the "pages" file: the best
// days at each kind of place, /best/<slug>.
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
		return s.categoryPages(ctx)
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

func (s sitemap) categoryPages(ctx context.Context) ([]api.SitemapPage, error) {
	cats, err := s.categories(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.SitemapPage, len(cats))
	for i, c := range cats {
		out[i] = api.SitemapPage{Path: "/best/" + place.CategorySlug(c.Category), LastMod: c.LastMod}
	}
	return out, nil
}
