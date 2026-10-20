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
	// listed is false for an office, service or care place left out; see
	// visitable.go.
	listed bool
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
		out.listed = fsqListed(p.CategoryLabels)
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
		out.listed = overtureListed(&p)
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
//
// The address is free text, not a link to a division: the US releases carry
// POIs whose locality is empty or names a neighbourhood, borough or
// unincorporated area that is no imported CITY or TOWN, or whose region is no
// state. Such a POI has no authoritative parent, so it is left out and counted
// in Plan.Unfiled rather than failing the import; filing it under its state
// instead would give it the state's default zone, which is wrong in a state
// that spans two zones. A division whose parent cannot be resolved is still
// an *UnresolvedParentError, because parent_division_id is authoritative.
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
		case !p.listed && p.kind != perspectivv1.PlaceKind_PLACE_KIND_MALL, !hasLetter(p.name):
			b.plan.Unlisted++
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
		parentKey, state, ok := locs.lookup(p.region, p.locality)
		if !ok {
			b.plan.Unfiled++
			continue
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

// lookup finds the imported CITY or TOWN an address names, and its state.
func (l localityIndex) lookup(region, locality string) (key, state string, ok bool) {
	state, ok = stateCode(region)
	if !ok || strings.TrimSpace(locality) == "" {
		return "", "", false
	}
	key, ok = l[state][nameKey(locality)]
	return key, state, ok
}

func sourcePrefix(s perspectivv1.PlaceSource) string {
	if s == perspectivv1.PlaceSource_PLACE_SOURCE_FOURSQUARE {
		return "foursquare:"
	}
	return "overture:"
}
