package importer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// ParseError reports a line of an input file that cannot be parsed. Import
// stops at the first one; nothing is written.
type ParseError struct {
	File string // "divisions" or "places"
	Line int    // 1-based; 0 when the file as a whole is unreadable
	Err  error
}

func (e *ParseError) Error() string {
	if e.Line == 0 {
		return fmt.Sprintf("%s file: %v", e.File, e.Err)
	}
	return fmt.Sprintf("%s file line %d: %v", e.File, e.Line, e.Err)
}

func (e *ParseError) Unwrap() error { return e.Err }

// maxLine bounds one record. A division point record is a few kilobytes; a
// line longer than this is a wrong file (a division_area polygon, say).
const maxLine = 4 << 20

// record is one input line, normalised from either accepted shape.
type record struct {
	line  int
	props json.RawMessage
	// id is a GeoJSON Feature's top-level id, used when properties has none.
	id       string
	lng, lat *float64
}

// featureEnvelope is the GeoJSON Feature shape.
type featureEnvelope struct {
	Type       string          `json:"type"`
	ID         json.RawMessage `json:"id"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties json.RawMessage `json:"properties"`
}

type pointGeometry struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"`
}

// flatCoords is where a flat record carries its coordinates: lng and lat as
// the documented extract queries name them, or Foursquare's own latitude and
// longitude columns.
type flatCoords struct {
	Lng       *float64 `json:"lng"`
	Lat       *float64 `json:"lat"`
	Longitude *float64 `json:"longitude"`
	Latitude  *float64 `json:"latitude"`
}

// readRecords reads newline-delimited JSON. Each non-blank line is one
// object: either a GeoJSON Feature (properties hold the source columns, a
// Point geometry the coordinates) or a flat object of source columns. A
// leading RFC 8142 record separator (0x1E) is ignored, so GeoJSON text
// sequences read too. fn is called per record; its error stops the read.
func readRecords(file string, r io.Reader, fn func(record) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	n := 0
	for sc.Scan() {
		n++
		raw := bytes.TrimSpace(bytes.TrimLeft(sc.Bytes(), "\x1e"))
		if len(raw) == 0 {
			continue
		}
		rec, err := decodeLine(raw)
		if err != nil {
			return &ParseError{File: file, Line: n, Err: err}
		}
		rec.line = n
		if err := fn(rec); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return &ParseError{File: file, Line: n + 1, Err: err}
	}
	return nil
}

func decodeLine(raw []byte) (record, error) {
	if raw[0] != '{' {
		return record{}, errors.New("not a JSON object")
	}
	var env featureEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return record{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if env.Type != "Feature" {
		var c flatCoords
		if err := json.Unmarshal(raw, &c); err != nil {
			return record{}, fmt.Errorf("coordinates: %w", err)
		}
		lng, lat := c.Lng, c.Lat
		if lng == nil && lat == nil {
			lng, lat = c.Longitude, c.Latitude
		}
		if err := checkCoords(lng, lat); err != nil {
			return record{}, err
		}
		return record{props: raw, lng: lng, lat: lat}, nil
	}
	return decodeFeature(env)
}

func decodeFeature(env featureEnvelope) (record, error) {
	if len(env.Properties) == 0 || env.Properties[0] != '{' {
		return record{}, errors.New("feature has no properties object")
	}
	rec := record{props: env.Properties}
	if len(env.ID) > 0 && string(env.ID) != "null" {
		if err := json.Unmarshal(env.ID, &rec.id); err != nil {
			return record{}, fmt.Errorf("feature id must be a string: %w", err)
		}
	}
	if len(env.Geometry) > 0 && string(env.Geometry) != "null" {
		var g pointGeometry
		if err := json.Unmarshal(env.Geometry, &g); err != nil {
			return record{}, fmt.Errorf("geometry: %w", err)
		}
		// Only point features are accepted: a polygon here means the wrong
		// Overture type (division_area) was extracted.
		if g.Type != "Point" || len(g.Coordinates) < 2 {
			return record{}, fmt.Errorf("geometry must be a Point, got %q", g.Type)
		}
		lng, lat := g.Coordinates[0], g.Coordinates[1]
		rec.lng, rec.lat = &lng, &lat
		if err := checkCoords(rec.lng, rec.lat); err != nil {
			return record{}, err
		}
	}
	return rec, nil
}

func checkCoords(lng, lat *float64) error {
	if (lng == nil) != (lat == nil) {
		return errors.New("coordinates need both longitude and latitude")
	}
	if lng == nil {
		return nil
	}
	if math.IsNaN(*lng) || *lng < -180 || *lng > 180 || math.IsNaN(*lat) || *lat < -90 || *lat > 90 {
		return fmt.Errorf("coordinates out of range: %v, %v", *lng, *lat)
	}
	return nil
}

// names is Overture's names struct; only the primary name is imported.
type names struct {
	Primary string `json:"primary"`
}

// cleanName collapses whitespace runs and trims, the same normalisation the
// places_parent_name_key index applies before comparing.
func cleanName(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// nameKey is the case-insensitive sibling key matching places_parent_name_key.
func nameKey(s string) string {
	return strings.ToLower(cleanName(s))
}
