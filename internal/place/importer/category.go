package importer

import (
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// overtureGroups maps Overture's top-level taxonomy groups onto the
// categories a reader filters places by.
var overtureGroups = map[string]perspectivv1.PlaceCategory{
	"food_and_drink":            perspectivv1.PlaceCategory_PLACE_CATEGORY_FOOD_AND_DRINK,
	"shopping":                  perspectivv1.PlaceCategory_PLACE_CATEGORY_SHOPPING,
	"geographic_entities":       perspectivv1.PlaceCategory_PLACE_CATEGORY_OUTDOORS,
	"arts_and_entertainment":    perspectivv1.PlaceCategory_PLACE_CATEGORY_ARTS_AND_ENTERTAINMENT,
	"sports_and_recreation":     perspectivv1.PlaceCategory_PLACE_CATEGORY_SPORTS_AND_RECREATION,
	"cultural_and_historic":     perspectivv1.PlaceCategory_PLACE_CATEGORY_CULTURE_AND_HISTORY,
	"education":                 perspectivv1.PlaceCategory_PLACE_CATEGORY_EDUCATION,
	"community_and_government":  perspectivv1.PlaceCategory_PLACE_CATEGORY_COMMUNITY,
	"travel_and_transportation": perspectivv1.PlaceCategory_PLACE_CATEGORY_TRAVEL,
	"lodging":                   perspectivv1.PlaceCategory_PLACE_CATEGORY_LODGING,
	"lifestyle_services":        perspectivv1.PlaceCategory_PLACE_CATEGORY_SERVICES,
	"services_and_business":     perspectivv1.PlaceCategory_PLACE_CATEGORY_SERVICES,
	"health_care":               perspectivv1.PlaceCategory_PLACE_CATEGORY_HEALTH,
}

// nightlifeCategories are the Overture categories filed under nightlife,
// whatever group they sit in: where people go out at night.
var nightlifeCategories = map[string]bool{
	"bar": true, "pub": true, "nightclub": true, "lounge": true, "cocktail_bar": true, "wine_bar": true,
	"sports_bar": true, "beer_bar": true, "brewery": true, "dive_bar": true, "karaoke": true,
}

// parks and other outdoor places Overture files under sports and recreation.
var outdoorCategories = map[string]bool{
	"park": true, "beach": true, "national_park": true, "state_park": true, "dog_park": true,
	"nature_reserve": true, "hiking_trail": true, "garden": true, "botanical_garden": true,
}

func overtureCategory(p *placeRecord) perspectivv1.PlaceCategory {
	cat := p.BasicCategory
	var hierarchy []string
	if p.Taxonomy != nil {
		hierarchy = p.Taxonomy.Hierarchy
	}
	for _, c := range append([]string{cat}, hierarchy...) {
		if nightlifeCategories[c] {
			return perspectivv1.PlaceCategory_PLACE_CATEGORY_NIGHTLIFE
		}
		if outdoorCategories[c] {
			return perspectivv1.PlaceCategory_PLACE_CATEGORY_OUTDOORS
		}
	}
	if len(hierarchy) > 0 {
		return overtureGroups[hierarchy[0]]
	}
	return perspectivv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED
}

// fsqGroups maps the first segment of a Foursquare category label.
var fsqGroups = map[string]perspectivv1.PlaceCategory{
	"dining and drinking":                perspectivv1.PlaceCategory_PLACE_CATEGORY_FOOD_AND_DRINK,
	"retail":                             perspectivv1.PlaceCategory_PLACE_CATEGORY_SHOPPING,
	"landmarks and outdoors":             perspectivv1.PlaceCategory_PLACE_CATEGORY_OUTDOORS,
	"arts and entertainment":             perspectivv1.PlaceCategory_PLACE_CATEGORY_ARTS_AND_ENTERTAINMENT,
	"event":                              perspectivv1.PlaceCategory_PLACE_CATEGORY_ARTS_AND_ENTERTAINMENT,
	"sports and recreation":              perspectivv1.PlaceCategory_PLACE_CATEGORY_SPORTS_AND_RECREATION,
	"travel and transportation":          perspectivv1.PlaceCategory_PLACE_CATEGORY_TRAVEL,
	"community and government":           perspectivv1.PlaceCategory_PLACE_CATEGORY_COMMUNITY,
	"business and professional services": perspectivv1.PlaceCategory_PLACE_CATEGORY_SERVICES,
	"health and medicine":                perspectivv1.PlaceCategory_PLACE_CATEGORY_HEALTH,
}

func fsqCategory(labels []string) perspectivv1.PlaceCategory {
	for _, l := range labels {
		parts := strings.Split(l, ">")
		last := nameKey(parts[len(parts)-1])
		if strings.Contains(last, "bar") || strings.Contains(last, "pub") || strings.Contains(last, "nightclub") || strings.Contains(last, "brewery") {
			return perspectivv1.PlaceCategory_PLACE_CATEGORY_NIGHTLIFE
		}
		if c, ok := fsqGroups[nameKey(parts[0])]; ok {
			return c
		}
	}
	return perspectivv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED
}

// categoryDB is the stored name of a category; NULL for UNSPECIFIED.
func categoryDB(c perspectivv1.PlaceCategory) pgtype.Text {
	if c == perspectivv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED {
		return pgtype.Text{}
	}
	return pgtype.Text{String: c.String(), Valid: true}
}
