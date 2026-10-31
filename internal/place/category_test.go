package place_test

import (
	"regexp"
	"testing"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place"
)

// The /best/<slug> addresses: one lowercase hyphenated slug per category,
// the same the web app derives from the same enum.
func TestCategorySlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		category perspectivv1.PlaceCategory
		want     string
	}{
		{perspectivv1.PlaceCategory_PLACE_CATEGORY_SPORTS_AND_RECREATION, "sports-and-recreation"},
		{perspectivv1.PlaceCategory_PLACE_CATEGORY_FOOD_AND_DRINK, "food-and-drink"},
		{perspectivv1.PlaceCategory_PLACE_CATEGORY_NIGHTLIFE, "nightlife"},
		{perspectivv1.PlaceCategory_PLACE_CATEGORY_HEALTH, "health"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := place.CategorySlug(tc.category); got != tc.want {
				t.Fatalf("CategorySlug(%v) = %q, want %q", tc.category, got, tc.want)
			}
		})
	}

	t.Run("every category has its own slug", func(t *testing.T) {
		t.Parallel()
		cats := place.Categories()
		if len(cats) != len(perspectivv1.PlaceCategory_name)-1 {
			t.Fatalf("Categories() has %d, want every value but UNSPECIFIED", len(cats))
		}
		shape := regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
		seen := map[string]bool{}
		for _, c := range cats {
			s := place.CategorySlug(c)
			if !shape.MatchString(s) || seen[s] {
				t.Errorf("slug %q for %v is malformed or repeated", s, c)
			}
			seen[s] = true
		}
	})
}
