package place

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/place/dbq"
	"perspectiv/internal/place/importer"
)

// twinReach is the widest distance a twin may lie at, as degrees of
// latitude: the box the candidate query reads before distances are checked.
var twinReach = importer.VenueDedupeRadius / earthRadius * 180 / math.Pi

// earthRadius is the mean radius of the earth in metres, as dedupe uses.
const earthRadius = 6371008.8

// twinCandidate is a storied place that may be another listing of a
// storyless one, with what the dedupe rule compares.
type twinCandidate struct {
	ref        Ref
	key        string
	venue      bool
	sameParent bool
	stories    int32
	// metres from the storyless place.
	distance float64
}

// twinOrigin is the storyless place the candidates are compared with.
type twinOrigin struct {
	key   string
	venue bool
}

// twinCandidates returns the storied stores and malls that may be another
// listing of p, and what p itself is compared by; see the TwinCandidates
// query. None means p is not a storyless store or mall with coordinates,
// or nothing storied lies near it.
func (s *Store) twinCandidates(ctx context.Context, p Place) ([]twinCandidate, twinOrigin, error) {
	rows, err := s.q.TwinCandidates(ctx, dbq.TwinCandidatesParams{ID: p.ID, Reach: twinReach})
	if err != nil {
		return nil, twinOrigin{}, fmt.Errorf("twin candidates: %w", err)
	}
	if len(rows) == 0 {
		return nil, twinOrigin{}, nil
	}
	origin := twinOrigin{
		key:   importer.DuplicateKey(p.Name, rows[0].MyAncestorNames),
		venue: importer.IsVenue(p.Kind.String(), p.Category.String()),
	}
	out := make([]twinCandidate, len(rows))
	for i, r := range rows {
		out[i] = twinCandidate{
			ref:        Ref{ID: r.ID, Kind: kindOf(r.Kind), Name: r.Name},
			key:        importer.DuplicateKey(r.Name, r.AncestorNames),
			venue:      importer.IsVenue(r.Kind, r.Category),
			sameParent: r.SameParent,
			stories:    r.Stories,
			distance:   importer.Distance(r.MyLat, r.MyLng, r.Lat, r.Lng),
		}
	}
	return out, origin, nil
}

// isTwin applies placectl dedupe's rule: one name key, and within dedupe's
// radius (200 m, 2 km when either is a sports venue). Two listings filed
// under one parent may lie up to the venue radius apart: a stadium filed
// as a store keeps listings on opposite sides of its lots. Chain branches
// cannot match this way: names are unique under a parent, so two branches
// in one town are spelled apart ("Starbucks", "Starbucks Coffee Company")
// and their name keys differ.
func (c *twinCandidate) isTwin(o twinOrigin) bool {
	if c.key == "" || c.key != o.key {
		return false
	}
	if c.sameParent && c.distance <= importer.VenueDedupeRadius {
		return true
	}
	return c.distance <= importer.DedupeRadius(c.venue || o.venue)
}

// Twin returns the place readers write about instead of p, when p is a
// store or mall with no readable story listed a second time; ok is false
// when there is none. Of several twins the one with the most stories wins,
// then the nearest, then the lowest id.
func (s *Service) Twin(ctx context.Context, p Place) (Ref, bool, error) {
	if p.Kind != perspectivv1.PlaceKind_PLACE_KIND_STORE && p.Kind != perspectivv1.PlaceKind_PLACE_KIND_MALL {
		return Ref{}, false, nil
	}
	cands, origin, err := s.store.twinCandidates(ctx, p)
	if err != nil {
		return Ref{}, false, apiutil.Internal(err)
	}
	twins := slices.DeleteFunc(cands, func(c twinCandidate) bool { return !c.isTwin(origin) })
	if len(twins) == 0 {
		return Ref{}, false, nil
	}
	best := slices.MinFunc(twins, func(a, b twinCandidate) int {
		if c := cmp.Compare(b.stories, a.stories); c != 0 {
			return c
		}
		if c := cmp.Compare(a.distance, b.distance); c != 0 {
			return c
		}
		return slices.Compare(a.ref.ID[:], b.ref.ID[:])
	})
	return best.ref, true, nil
}
