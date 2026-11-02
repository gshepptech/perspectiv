package main

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place"
)

// The "pages" sitemap lists the guide ahead of the best-days pages, each
// with a lastmod, and fails when the category list does.
func TestSitemapFixedPages(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, time.October, 3, 0, 0, 0, 0, time.UTC)
	errCategories := errors.New("categories down")
	cases := []struct {
		name    string
		cats    []place.CategoryPage
		err     error
		want    []string
		wantErr error
	}{
		{name: "no categories", want: []string{"/how-it-works"}},
		{
			name: "guide then categories",
			cats: []place.CategoryPage{{Category: perspectivv1.PlaceCategory_PLACE_CATEGORY_SHOPPING, LastMod: at}},
			want: []string{"/how-it-works", "/best/shopping"},
		},
		{name: "categories fail", err: errCategories, wantErr: errCategories},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sm := sitemap{categories: func(context.Context) ([]place.CategoryPage, error) { return tc.cats, tc.err }}
			pages, err := sm.pages(context.Background(), "pages")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("pages error = %v, want %v", err, tc.wantErr)
			}
			got := make([]string, len(pages))
			for i, p := range pages {
				got[i] = p.Path
				if p.LastMod.IsZero() {
					t.Errorf("%s has no lastmod", p.Path)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("pages = %v, want %v", got, tc.want)
			}
		})
	}
}
