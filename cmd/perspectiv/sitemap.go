package main

import (
	"context"
	"fmt"

	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
)

type sitemapList func(ctx context.Context) ([]apiutil.SitemapEntry, error)

// sitemap maps each sitemap file to the domain that lists its pages and the
// path the SPA reads them at.
type sitemap struct {
	places, days, stories sitemapList
}

func (s sitemap) pages(ctx context.Context, kind string) ([]api.SitemapPage, error) {
	var (
		list   sitemapList
		prefix string
	)
	switch kind {
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
