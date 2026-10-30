package importer

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// country is the one country the importer accepts; records for any other
// country are skipped and counted.
const country = "US"

// maxDepth bounds a parent_division_id walk, so a cycle in a bad extract is
// reported instead of looping.
const maxDepth = 32

// UnresolvedParentError reports a division whose parent place cannot be
// found in the import. Import stops at the first one; nothing is written.
type UnresolvedParentError struct {
	File     string // "divisions"
	Line     int
	SourceID string
	Name     string
	Reason   string
}

func (e *UnresolvedParentError) Error() string {
	return fmt.Sprintf("%s file line %d: %q (%s): parent not resolved: %s",
		e.File, e.Line, e.Name, e.SourceID, e.Reason)
}

// Node is one place row the import writes.
type Node struct {
	// Key identifies the node within the plan: "<source>:<source id>".
	Key      string
	Kind     perspectivv1.PlaceKind
	Source   perspectivv1.PlaceSource
	SourceID string
	Name     string
	// ParentKey is the parent node's Key, "" for a STATE.
	ParentKey string
	Timezone  string
	Lng, Lat  *float64
	// Category is what kind of place a store or mall is; UNSPECIFIED above.
	Category perspectivv1.PlaceCategory
}

// Plan is a fully parsed and resolved import: every node has its parent
// earlier in Nodes and a valid IANA zone. Building a Plan touches no
// database, so a bad file fails before anything is written.
type Plan struct {
	nodes []Node
	// merged maps the key of a record that was folded into a same-named
	// sibling to the key of the node that kept the name.
	merged map[string]string
	// Skipped counts records outside the United States.
	Skipped int
	// Unfiled counts US malls and stores left out because their address
	// names no imported city or town (an empty locality, a neighbourhood or
	// an unincorporated area) or no state.
	Unfiled int
	// Unlisted counts US places left out because they are offices, services
	// or care places nobody would write about (see visitable.go).
	Unlisted int
	// unfiled counts the Unfiled records per address locality and region.
	unfiled map[string]*LocalityCount
}

// LocalityCount is how many places named one address locality that matched
// no imported city or town.
type LocalityCount struct {
	// Locality is the address text as the first such record wrote it,
	// with its region: "St Pete, FL".
	Locality string
	Count    int
}

// unfiledAt counts one unfiled record under its locality, grouped by
// localityKey so spellings that file alike count together.
func (p *Plan) unfiledAt(locality, region string) {
	state, ok := stateCode(region)
	if !ok {
		state = strings.TrimSpace(region)
	}
	key := localityKey(locality) + "\x00" + state
	c, ok := p.unfiled[key]
	if !ok {
		label := cleanName(locality)
		if label == "" {
			label = "(no locality)"
		}
		if state != "" {
			label += ", " + state
		}
		c = &LocalityCount{Locality: label}
		p.unfiled[key] = c
	}
	c.Count++
}

// UnfiledLocalities returns the n address localities that left the most
// places unfiled, most first, so a city missing from the divisions file
// shows up by name.
func (p *Plan) UnfiledLocalities(n int) []LocalityCount {
	out := make([]LocalityCount, 0, len(p.unfiled))
	for _, c := range p.unfiled {
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b LocalityCount) int {
		if a.Count != b.Count {
			return cmp.Compare(b.Count, a.Count)
		}
		return strings.Compare(a.Locality, b.Locality)
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Nodes returns the nodes in write order: states, cities, towns, malls,
// stores.
func (p *Plan) Nodes() []Node { return p.nodes }

// Merged returns, for each record folded into a same-named sibling, the key
// of the node it was folded into. The places_parent_name_key index allows one
// name per parent, so two records with one name under one parent (two
// "Starbucks" in a town) become one place.
func (p *Plan) Merged() map[string]string { return p.merged }

// builder accumulates nodes, folding same-named siblings.
type builder struct {
	plan     *Plan
	siblings map[string]string // canonical parent key + "\x00" + name key -> node key
	byKey    map[string]int    // node key -> index in plan.nodes
	state    map[string]string // node key -> state code
}

func newBuilder() *builder {
	return &builder{
		plan:     &Plan{merged: map[string]string{}, unfiled: map[string]*LocalityCount{}},
		siblings: map[string]string{},
		byKey:    map[string]int{},
		state:    map[string]string{},
	}
}

// canonical follows merges to the node that holds the row.
func (b *builder) canonical(key string) string {
	for {
		next, ok := b.plan.merged[key]
		if !ok {
			return key
		}
		key = next
	}
}

// add appends n, or folds it into an existing sibling with the same name. It
// returns the key of the node that holds the row.
func (b *builder) add(n Node, state string) string {
	if n.ParentKey != "" {
		n.ParentKey = b.canonical(n.ParentKey)
	}
	sib := n.ParentKey + "\x00" + nameKey(n.Name)
	if existing, ok := b.siblings[sib]; ok {
		b.plan.merged[n.Key] = existing
		return existing
	}
	b.siblings[sib] = n.Key
	b.byKey[n.Key] = len(b.plan.nodes)
	b.state[n.Key] = state
	b.plan.nodes = append(b.plan.nodes, n)
	return n.Key
}

func (b *builder) node(key string) Node { return b.plan.nodes[b.byKey[b.canonical(key)]] }

// Prepare reads the divisions file and the places file and resolves every
// record's kind, parent and time zone. The file formats are described in the
// package documentation.
func Prepare(divisions, places io.Reader) (*Plan, error) {
	b := newBuilder()
	locs, err := b.addDivisions(divisions)
	if err != nil {
		return nil, err
	}
	if err := b.addPlaces(places, locs); err != nil {
		return nil, err
	}
	return b.plan, nil
}

// division is one Overture division record.
type division struct {
	ID       string `json:"id"`
	Subtype  string `json:"subtype"`
	Class    string `json:"class"`
	Country  string `json:"country"`
	Region   string `json:"region"`
	Names    names  `json:"names"`
	ParentID string `json:"parent_division_id"`

	line     int
	lng, lat *float64
	kind     perspectivv1.PlaceKind // UNSPECIFIED for a division not imported
}

// divisionKind maps an Overture division to a place kind. A region is a
// STATE. A locality is a CITY when its class is megacity or city (or absent)
// and a TOWN when it is town, village or hamlet. Counties, local admin areas,
// neighborhoods and the rest are not imported; counties are still read for
// the time zone.
func divisionKind(d *division) (perspectivv1.PlaceKind, error) {
	switch d.Subtype {
	case "region":
		return perspectivv1.PlaceKind_PLACE_KIND_STATE, nil
	case "locality":
		switch d.Class {
		case "", "megacity", "city":
			return perspectivv1.PlaceKind_PLACE_KIND_CITY, nil
		case "town", "village", "hamlet":
			return perspectivv1.PlaceKind_PLACE_KIND_TOWN, nil
		default:
			return 0, fmt.Errorf("unknown locality class %q", d.Class)
		}
	default:
		return perspectivv1.PlaceKind_PLACE_KIND_UNSPECIFIED, nil
	}
}

func parseDivision(rec record) (*division, error) {
	var d division
	if err := json.Unmarshal(rec.props, &d); err != nil {
		return nil, fmt.Errorf("division properties: %w", err)
	}
	if d.ID == "" {
		d.ID = rec.id
	}
	d.Names.Primary = cleanName(d.Names.Primary)
	switch {
	case d.ID == "":
		return nil, errors.New("id is required")
	case d.Subtype == "":
		return nil, errors.New("subtype is required")
	case d.Country == "":
		return nil, errors.New("country is required")
	case d.Subtype != "country" && d.ParentID == "":
		return nil, errors.New("parent_division_id is required below country")
	case d.Subtype != "country" && d.Names.Primary == "":
		return nil, errors.New("names.primary is required")
	}
	kind, err := divisionKind(&d)
	if err != nil {
		return nil, err
	}
	d.kind = kind
	if kind == perspectivv1.PlaceKind_PLACE_KIND_STATE && d.Country == country {
		if _, ok := stateCode(d.Region); !ok {
			return nil, fmt.Errorf("region code %q is not a US state code", d.Region)
		}
	}
	d.line, d.lng, d.lat = rec.line, rec.lng, rec.lat
	return &d, nil
}

// localityIndex finds a CITY or TOWN by state code and localityKey.
type localityIndex map[string]map[string]string

func (b *builder) addDivisions(r io.Reader) (localityIndex, error) {
	byID := map[string]*division{}
	var order []*division
	err := readRecords("divisions", r, func(rec record) error {
		d, err := parseDivision(rec)
		if err != nil {
			return &ParseError{File: "divisions", Line: rec.line, Err: err}
		}
		if _, dup := byID[d.ID]; dup {
			return &ParseError{File: "divisions", Line: rec.line, Err: fmt.Errorf("duplicate id %q", d.ID)}
		}
		byID[d.ID] = d
		order = append(order, d)
		return nil
	})
	if err != nil {
		return nil, err
	}

	keys := map[string]string{} // division id -> node key holding it
	locs := localityIndex{}
	for _, kind := range []perspectivv1.PlaceKind{
		perspectivv1.PlaceKind_PLACE_KIND_STATE,
		perspectivv1.PlaceKind_PLACE_KIND_CITY,
		perspectivv1.PlaceKind_PLACE_KIND_TOWN,
	} {
		for _, d := range order {
			if d.kind != kind {
				continue
			}
			if d.Country != country {
				b.plan.Skipped++
				continue
			}
			n, state, err := b.resolveDivision(d, byID, keys)
			if err != nil {
				return nil, err
			}
			held := b.add(n, state)
			keys[d.ID] = held
			if kind != perspectivv1.PlaceKind_PLACE_KIND_STATE {
				locs.put(state, d.Names.Primary, held)
			}
		}
	}
	return locs, nil
}

// put records the first CITY or TOWN with a name in a state; cities are added
// before towns, so a city wins a name it shares with a town.
func (l localityIndex) put(state, name, key string) {
	m, ok := l[state]
	if !ok {
		m = map[string]string{}
		l[state] = m
	}
	if _, taken := m[localityKey(name)]; !taken {
		m[localityKey(name)] = key
	}
}

// resolveDivision finds d's parent node and time zone by walking the
// parent_division_id chain. A STATE has no parent. A CITY's parent is the
// nearest STATE above it. A TOWN's parent is the nearest CITY or STATE above
// it: a US town usually sits under a county, so its parent is its state, and
// a village Overture files inside a city sits under that city.
func (b *builder) resolveDivision(d *division, byID map[string]*division, keys map[string]string) (Node, string, error) {
	n := Node{
		Key:      "overture:" + d.ID,
		Kind:     d.kind,
		Source:   perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE,
		SourceID: d.ID,
		Name:     d.Names.Primary,
		Lng:      d.lng,
		Lat:      d.lat,
	}
	unresolved := func(reason string) error {
		return &UnresolvedParentError{File: "divisions", Line: d.line, SourceID: d.ID, Name: d.Names.Primary, Reason: reason}
	}
	if d.kind == perspectivv1.PlaceKind_PLACE_KIND_STATE {
		state, _ := stateCode(d.Region)
		zone, err := resolveZone(state, "", "")
		if err != nil {
			return Node{}, "", unresolved(err.Error())
		}
		n.Timezone = zone
		return n, state, nil
	}

	chain, reason := walkChain(d, byID)
	if reason != "" {
		return Node{}, "", unresolved(reason)
	}
	if chain.parent.Country != country {
		return Node{}, "", unresolved(fmt.Sprintf("parent division %q is outside the United States", chain.parent.ID))
	}
	n.ParentKey = keys[chain.parent.ID]
	if n.ParentKey == "" {
		return Node{}, "", unresolved(fmt.Sprintf("parent division %q was not imported", chain.parent.ID))
	}
	state, _ := stateCode(chain.state.Region)
	zone, err := resolveZone(state, chain.county, d.Names.Primary)
	if err != nil {
		return Node{}, "", unresolved(err.Error())
	}
	n.Timezone = zone
	return n, state, nil
}

// chain is what a walk up the parent_division_id chain finds: the nearest
// division that may parent the walker, the first county on the way and the
// state at the top.
type chain struct {
	parent, state *division
	county        string
}

// walkChain walks from d to its state. A non-empty reason says why the walk
// failed.
func walkChain(d *division, byID map[string]*division) (chain, string) {
	var c chain
	id := d.ParentID
	for depth := 0; depth < maxDepth; depth++ {
		p, ok := byID[id]
		if !ok {
			return chain{}, fmt.Sprintf("parent division %q is not in the divisions file", id)
		}
		if p.Subtype == "county" && c.county == "" {
			c.county = p.Names.Primary
		}
		if c.parent == nil && acceptsChild(p.kind, d.kind) {
			c.parent = p
		}
		if p.kind == perspectivv1.PlaceKind_PLACE_KIND_STATE {
			c.state = p
			return c, ""
		}
		if p.ParentID == "" {
			return chain{}, "no state above it in the parent_division_id chain"
		}
		id = p.ParentID
	}
	return chain{}, "parent_division_id chain is cyclic or too deep"
}

// acceptsChild reports whether a node of kind parent may be the parent of a
// division of kind child.
func acceptsChild(parent, child perspectivv1.PlaceKind) bool {
	switch child {
	case perspectivv1.PlaceKind_PLACE_KIND_CITY:
		return parent == perspectivv1.PlaceKind_PLACE_KIND_STATE
	case perspectivv1.PlaceKind_PLACE_KIND_TOWN:
		return parent == perspectivv1.PlaceKind_PLACE_KIND_STATE || parent == perspectivv1.PlaceKind_PLACE_KIND_CITY
	default:
		return false
	}
}

// stateCode turns "US-TX", "TX" or "Texas" into "TX".
func stateCode(region string) (string, bool) {
	r := strings.ToUpper(strings.TrimSpace(region))
	r = strings.TrimPrefix(r, "US-")
	if _, ok := stateZones[r]; ok {
		return r, true
	}
	code, ok := stateNames[nameKey(region)]
	return code, ok
}

// stateNames maps full state names to codes, for place records whose region
// column holds the name.
var stateNames = map[string]string{
	"alabama": "AL", "alaska": "AK", "arizona": "AZ", "arkansas": "AR",
	"california": "CA", "colorado": "CO", "connecticut": "CT", "delaware": "DE",
	"district of columbia": "DC", "florida": "FL", "georgia": "GA", "hawaii": "HI",
	"idaho": "ID", "illinois": "IL", "indiana": "IN", "iowa": "IA", "kansas": "KS",
	"kentucky": "KY", "louisiana": "LA", "maine": "ME", "maryland": "MD",
	"massachusetts": "MA", "michigan": "MI", "minnesota": "MN", "mississippi": "MS",
	"missouri": "MO", "montana": "MT", "nebraska": "NE", "nevada": "NV",
	"new hampshire": "NH", "new jersey": "NJ", "new mexico": "NM", "new york": "NY",
	"north carolina": "NC", "north dakota": "ND", "ohio": "OH", "oklahoma": "OK",
	"oregon": "OR", "pennsylvania": "PA", "rhode island": "RI", "south carolina": "SC",
	"south dakota": "SD", "tennessee": "TN", "texas": "TX", "utah": "UT",
	"vermont": "VT", "virginia": "VA", "washington": "WA", "west virginia": "WV",
	"wisconsin": "WI", "wyoming": "WY", "puerto rico": "PR", "guam": "GU",
	"u.s. virgin islands": "VI", "northern mariana islands": "MP", "american samoa": "AS",
}
