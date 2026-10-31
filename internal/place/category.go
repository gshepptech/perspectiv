package place

import (
	"strings"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// Categories lists every place category a place can carry, in enum order:
// every PlaceCategory value but UNSPECIFIED.
func Categories() []perspectivv1.PlaceCategory {
	values := perspectivv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED.Descriptor().Values()
	out := make([]perspectivv1.PlaceCategory, 0, values.Len())
	for i := range values.Len() {
		c := perspectivv1.PlaceCategory(values.Get(i).Number())
		if c != perspectivv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED {
			out = append(out, c)
		}
	}
	return out
}

// CategorySlug is a category's URL slug, its enum name without the prefix
// in lowercase with hyphens: PLACE_CATEGORY_SPORTS_AND_RECREATION is
// "sports-and-recreation". The web app derives the same slug from the same
// enum (categorySlug in web/src/components/reader/format.ts) for the
// /best/<slug> pages.
func CategorySlug(c perspectivv1.PlaceCategory) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(c.String(), "PLACE_CATEGORY_")), "_", "-")
}
