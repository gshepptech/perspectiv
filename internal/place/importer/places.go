package importer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// placeRecord is one row of the places file: a Foursquare OS Places row
// (fsq_place_id, name, locality, region, country, fsq_category_labels) or an
// Overture place (id, names, addresses, basic_category, taxonomy,
// categories). The two share no required column, so each line says which it
// is by carrying fsq_place_id or not.
type placeRecord struct {
	// Foursquare OS Places columns.
	FsqPlaceID     string   `json:"fsq_place_id"`
	Name           string   `json:"name"`
	Locality       string   `json:"locality"`
	Region         string   `json:"region"`
	Country        string   `json:"country"`
	CategoryLabels []string `json:"fsq_category_labels"`

	// Overture places columns.
	ID            string `json:"id"`
	Names         names  `json:"names"`
	BasicCategory string `json:"basic_category"`
	Taxonomy      *struct {
		Primary   string   `json:"primary"`
		Hierarchy []string `json:"hierarchy"`
	} `json:"taxonomy"`
	Categories *struct {
		Primary string `json:"primary"`
	} `json:"categories"`
	Addresses []struct {
		Locality string `json:"locality"`
		Region   string `json:"region"`
		Country  string `json:"country"`
	} `json:"addresses"`
}

// overtureMallCategories are the Overture category ids that make a place a
// MALL, matched against basic_category, taxonomy.primary, any
// taxonomy.hierarchy entry and the older categories.primary.
var overtureMallCategories = map[string]bool{
	"shopping_mall":   true,
	"shopping_center": true,
	"outlet_mall":     true,
}

// fsqMallLabels are the last segments of a Foursquare category label
// ("Retail > Shopping Mall") that make a place a MALL.
var fsqMallLabels = map[string]bool{
	"shopping mall": true,
	"outlet mall":   true,
}

// parsedPlace is a places-file row reduced to what the import needs.
type parsedPlace struct {
	line     int
	source   perspectivv1.PlaceSource
	sourceID string
	name     string
	locality string
	region   string
	country  string
	kind     perspectivv1.PlaceKind
	lng, lat *float64
}

func parsePlace(rec record) (parsedPlace, error) {
	var p placeRecord
	if err := json.Unmarshal(rec.props, &p); err != nil {
		return parsedPlace{}, fmt.Errorf("place properties: %w", err)
	}
	out := parsedPlace{line: rec.line, lng: rec.lng, lat: rec.lat, kind: perspectivv1.PlaceKind_PLACE_KIND_STORE}
	switch {
	case p.FsqPlaceID != "":
		out.source = perspectivv1.PlaceSource_PLACE_SOURCE_FOURSQUARE
		out.sourceID = p.FsqPlaceID
		out.name = p.Name
		out.locality, out.region, out.country = p.Locality, p.Region, p.Country
		for _, l := range p.CategoryLabels {
			parts := strings.Split(l, ">")
			if fsqMallLabels[nameKey(parts[len(parts)-1])] {
				out.kind = perspectivv1.PlaceKind_PLACE_KIND_MALL
			}
		}
	default:
		id := p.ID
		if id == "" {
			id = rec.id
		}
		if id == "" {
			return parsedPlace{}, errors.New("neither fsq_place_id nor id is set")
		}
		out.source = perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE
		out.sourceID = id
		out.name = p.Names.Primary
		if len(p.Addresses) > 0 {
			a := p.Addresses[0]
			out.locality, out.region, out.country = a.Locality, a.Region, a.Country
		}
		if overtureIsMall(&p) {
			out.kind = perspectivv1.PlaceKind_PLACE_KIND_MALL
		}
	}
	out.name = cleanName(out.name)
	switch {
	case out.name == "":
		return parsedPlace{}, errors.New("name is required")
	case out.country == "":
		return parsedPlace{}, errors.New("country is required")
	}
	return out, nil
}

func overtureIsMall(p *placeRecord) bool {
	if overtureMallCategories[p.BasicCategory] {
		return true
	}
	if p.Categories != nil && overtureMallCategories[p.Categories.Primary] {
		return true
	}
	if p.Taxonomy == nil {
		return false
	}
	if overtureMallCategories[p.Taxonomy.Primary] {
		return true
	}
	for _, h := range p.Taxonomy.Hierarchy {
		if overtureMallCategories[h] {
			return true
		}
	}
	return false
}

// addPlaces adds every US mall, then every US store. Neither dataset links a
// store to the mall it is in, so both are filed under the CITY or TOWN their
// address names (locality and region); a user files a store under a mall by
// hand. The zone is inherited from that locality.
func (b *builder) addPlaces(r io.Reader, locs localityIndex) error {
	var malls, stores []parsedPlace
	err := readRecords("places", r, func(rec record) error {
		p, err := parsePlace(rec)
		if err != nil {
			return &ParseError{File: "places", Line: rec.line, Err: err}
		}
		switch {
		case strings.ToUpper(p.country) != country:
			b.plan.Skipped++
		case p.kind == perspectivv1.PlaceKind_PLACE_KIND_MALL:
			malls = append(malls, p)
		default:
			stores = append(stores, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range append(malls, stores...) {
		unresolved := func(reason string) error {
			return &UnresolvedParentError{File: "places", Line: p.line, SourceID: p.sourceID, Name: p.name, Reason: reason}
		}
		state, ok := stateCode(p.region)
		if !ok {
			return unresolved(fmt.Sprintf("region %q is not a US state", p.region))
		}
		parentKey, ok := locs[state][nameKey(p.locality)]
		if !ok || p.locality == "" {
			return unresolved(fmt.Sprintf("no city or town %q in %s", p.locality, state))
		}
		parent := b.node(parentKey)
		b.add(Node{
			Key:       sourcePrefix(p.source) + p.sourceID,
			Kind:      p.kind,
			Source:    p.source,
			SourceID:  p.sourceID,
			Name:      p.name,
			ParentKey: parent.Key,
			Timezone:  parent.Timezone,
			Lng:       p.lng,
			Lat:       p.lat,
		}, state)
	}
	return nil
}

func sourcePrefix(s perspectivv1.PlaceSource) string {
	if s == perspectivv1.PlaceSource_PLACE_SOURCE_FOURSQUARE {
		return "foursquare:"
	}
	return "overture:"
}
