package importer

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place/importer/dbq"
)

// Two places with one normalised name are the same place listed twice when
// they are this close, in metres. A stadium's listings scatter across its
// lots and gates, so a sports venue gets the wider radius.
const (
	dedupeRadius      = 200.0
	venueDedupeRadius = 2000.0
	earthRadius       = 6371008.8 // mean radius, metres
)

// Why a duplicate was not merged.
const (
	SkipStoried        = "two or more places in the cluster have stories or drafts; report only"
	SkipChildLevel     = "the places under it cannot be filed under the survivor"
	SkipSurvivorInside = "the survivor is filed inside it"
	SkipChildNames     = "a place under it has the name of a place under the survivor"
	SkipGone           = "it or the survivor is no longer visible"
	SkipGainedStory    = "it gained a story or a draft since the scan"
)

// DedupeMerge is one duplicate hidden in favour of its survivor.
type DedupeMerge struct {
	Loser, Survivor         uuid.UUID
	LoserName, SurvivorName string
	// Metres between the two.
	Distance float64
	// Children counts the places moved from the loser to the survivor;
	// always 0 on a dry run.
	Children int64
}

// DedupeSkip is a duplicate, or a whole cluster, left as it is.
type DedupeSkip struct {
	Place, Survivor uuid.UUID
	Name            string
	Reason          string
}

// DedupeReport is what a dedupe found and did.
type DedupeReport struct {
	// Applied is false on a dry run, when nothing was written.
	Applied bool
	// Candidates counts the visible stores and malls with coordinates.
	Candidates int
	// Clusters counts groups of two or more places taken as one.
	Clusters int
	Merges   []DedupeMerge
	Skips    []DedupeSkip
}

// dedupePlace is one candidate, reduced to what clustering needs.
type dedupePlace struct {
	id                 uuid.UUID
	name, key          string
	lat, lng           float64
	venue              bool
	arcs, perspectives int
	// dirt counts what the name's cleanup would change; lower is cleaner.
	dirt int
}

func (p *dedupePlace) storied() bool { return p.arcs > 0 || p.perspectives > 0 }

// Dedupe finds visible stores and malls listed twice: one normalised name
// within dedupeRadius (venueDedupeRadius for a sports venue). In each
// cluster the survivor is the place with the most arcs, then the most
// perspectives, then the cleanest name, then the oldest id. Every other
// place with no arc and no perspective row is a loser: its children move to
// the survivor, its followers follow the survivor, it is hidden and an
// AUDIT_ACTION_MERGE_PLACE row records it. Nothing with a story or a draft is
// hidden, and a cluster where two places have stories is only reported. With
// apply false nothing is written. Hidden places are not candidates, so a
// second run finds nothing more to merge.
func (im *Importer) Dedupe(ctx context.Context, apply bool) (DedupeReport, error) {
	q := dbq.New(im.pool)
	rows, err := q.ListDedupeCandidates(ctx)
	if err != nil {
		return DedupeReport{}, fmt.Errorf("list dedupe candidates: %w", err)
	}
	rep := DedupeReport{Applied: apply, Candidates: len(rows)}
	places := make([]dedupePlace, len(rows))
	for i := range rows {
		places[i] = toDedupePlace(&rows[i])
	}
	for _, c := range clusterDuplicates(places) {
		rep.Clusters++
		if err := im.dedupeCluster(ctx, c, apply, &rep); err != nil {
			return rep, err
		}
	}
	im.log.Info().Bool("applied", apply).Int("candidates", rep.Candidates).Int("clusters", rep.Clusters).
		Int("merged", len(rep.Merges)).Int("skipped", len(rep.Skips)).Msg("place dedupe finished")
	return rep, nil
}

func toDedupePlace(r *dbq.ListDedupeCandidatesRow) dedupePlace {
	stripped, suffixes := stripAncestorSuffixes(r.Name, r.AncestorNames)
	dirt := suffixes
	if cleanName(r.Name) != r.Name {
		dirt++
	}
	if trimTrailingPunct(stripped) != stripped {
		dirt++
	}
	if hasLetter(r.Name) && strings.ToLower(r.Name) == r.Name {
		dirt++
	}
	return dedupePlace{
		id: r.ID, name: r.Name, key: localityKey(stripped), lat: r.Lat, lng: r.Lng,
		venue: r.Kind == kindDB(perspectivv1.PlaceKind_PLACE_KIND_MALL) &&
			r.Category == perspectivv1.PlaceCategory_PLACE_CATEGORY_SPORTS_AND_RECREATION.String(),
		arcs: int(r.Arcs), perspectives: int(r.Perspectives), dirt: dirt,
	}
}

// stripAncestorSuffixes removes every trailing ", <name>" that names a place
// above it ("Starbucks, Tampa"), or its state's code ("Starbucks, FL"), and
// counts them.
func stripAncestorSuffixes(name string, ancestors []string) (string, int) {
	keys := make(map[string]bool, len(ancestors)+1)
	for _, a := range ancestors {
		keys[localityKey(a)] = true
		if code, ok := stateNames[nameKey(a)]; ok {
			keys[localityKey(code)] = true
		}
	}
	n := 0
	for {
		i := strings.LastIndex(name, ",")
		if i <= 0 || !keys[localityKey(name[i+1:])] || !hasLetter(name[:i]) {
			return name, n
		}
		name = strings.TrimSpace(name[:i])
		n++
	}
}

// survivorFirst orders places best survivor first: most arcs, most
// perspectives, cleanest name, oldest id (ids are UUIDv7, so time ordered).
func survivorFirst(a, b dedupePlace) int {
	if c := cmp.Compare(b.arcs, a.arcs); c != 0 {
		return c
	}
	if c := cmp.Compare(b.perspectives, a.perspectives); c != 0 {
		return c
	}
	if c := cmp.Compare(a.dirt, b.dirt); c != 0 {
		return c
	}
	return bytes.Compare(a.id[:], b.id[:])
}

// clusterDuplicates groups places by normalised name, then, best survivor
// first, gathers every unclaimed same-named place within reach of it. Each
// cluster's first place is its survivor, and every other place lies within
// the radius of the survivor itself, so a chain of shops along a street
// never merges end to end. Only clusters of two or more are returned.
func clusterDuplicates(places []dedupePlace) [][]dedupePlace {
	groups := map[string][]dedupePlace{}
	for _, p := range places {
		if p.key != "" {
			groups[p.key] = append(groups[p.key], p)
		}
	}
	keys := make([]string, 0, len(groups))
	for k, g := range groups {
		if len(g) > 1 {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var out [][]dedupePlace
	for _, k := range keys {
		out = append(out, clusterGroup(groups[k])...)
	}
	return out
}

// latReach is the widest radius as degrees of latitude.
var latReach = venueDedupeRadius / earthRadius * 180 / math.Pi

func clusterGroup(g []dedupePlace) [][]dedupePlace {
	slices.SortFunc(g, survivorFirst)
	byLat := make([]int, len(g))
	for i := range byLat {
		byLat[i] = i
	}
	slices.SortFunc(byLat, func(a, b int) int { return cmp.Compare(g[a].lat, g[b].lat) })
	claimed := make([]bool, len(g))
	var out [][]dedupePlace
	for i := range g {
		if claimed[i] {
			continue
		}
		claimed[i] = true
		c := []dedupePlace{g[i]}
		start, _ := slices.BinarySearchFunc(byLat, g[i].lat-latReach, func(j int, lat float64) int {
			return cmp.Compare(g[j].lat, lat)
		})
		for _, j := range byLat[start:] {
			if g[j].lat > g[i].lat+latReach {
				break
			}
			if !claimed[j] && near(&g[i], &g[j]) {
				claimed[j] = true
				c = append(c, g[j])
			}
		}
		if len(c) > 1 {
			slices.SortFunc(c[1:], survivorFirst)
			out = append(out, c)
		}
	}
	return out
}

func near(a, b *dedupePlace) bool {
	radius := dedupeRadius
	if a.venue || b.venue {
		radius = venueDedupeRadius
	}
	return haversine(a.lat, a.lng, b.lat, b.lng) <= radius
}

// haversine is the great-circle distance in metres.
func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	dLat, dLng := (lat2-lat1)*rad, (lng2-lng1)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(h)))
}

func (im *Importer) dedupeCluster(ctx context.Context, c []dedupePlace, apply bool, rep *DedupeReport) error {
	survivor := &c[0]
	if c[1].storied() {
		rep.Skips = append(rep.Skips, DedupeSkip{Place: survivor.id, Survivor: survivor.id, Name: survivor.name, Reason: SkipStoried})
		return nil
	}
	for i := 1; i < len(c); i++ {
		loser := &c[i]
		merge := DedupeMerge{
			Loser: loser.id, Survivor: survivor.id, LoserName: loser.name, SurvivorName: survivor.name,
			Distance: haversine(loser.lat, loser.lng, survivor.lat, survivor.lng),
		}
		var reason string
		var err error
		if apply {
			reason, err = im.applyMerge(ctx, &merge)
		} else {
			reason, err = moveBlocked(ctx, dbq.New(im.pool), loser.id, survivor.id)
		}
		if err != nil {
			return err
		}
		if reason != "" {
			rep.Skips = append(rep.Skips, DedupeSkip{Place: loser.id, Survivor: survivor.id, Name: loser.name, Reason: reason})
			continue
		}
		rep.Merges = append(rep.Merges, merge)
	}
	return nil
}

// moveBlocked says why the loser's children cannot move under the
// survivor, or "" when they can.
func moveBlocked(ctx context.Context, q *dbq.Queries, loser, survivor uuid.UUID) (string, error) {
	chk, err := q.CheckDedupeMove(ctx, dbq.CheckDedupeMoveParams{LoserID: loser, SurvivorID: survivor})
	if err != nil {
		return "", fmt.Errorf("check merge of %s into %s: %w", loser, survivor, err)
	}
	switch {
	case chk.SurvivorInside:
		return SkipSurvivorInside, nil
	case chk.ChildLevel > 0 && chk.ChildLevel <= chk.SurvivorLevel:
		return SkipChildLevel, nil
	case chk.NameClashes > 0:
		return SkipChildNames, nil
	}
	return "", nil
}

// applyMerge hides one loser in its own transaction, with both rows locked
// so no arc or draft can land on the loser part way. A non-empty reason
// means nothing was written.
func (im *Importer) applyMerge(ctx context.Context, m *DedupeMerge) (string, error) {
	var reason string
	err := pgx.BeginFunc(ctx, im.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		locked, err := q.LockDedupePlaces(ctx, []uuid.UUID{m.Loser, m.Survivor})
		if err != nil {
			return fmt.Errorf("lock %s and %s: %w", m.Loser, m.Survivor, err)
		}
		if len(locked) != 2 {
			reason = SkipGone
			return nil
		}
		if reason, err = moveBlocked(ctx, q, m.Loser, m.Survivor); err != nil || reason != "" {
			return err
		}
		hidden, err := q.HideDedupeLoser(ctx, m.Loser)
		if err != nil {
			return fmt.Errorf("hide %s: %w", m.Loser, err)
		}
		if hidden == 0 {
			reason = SkipGainedStory
			return nil
		}
		return im.moveToSurvivor(ctx, q, m)
	})
	return reason, err
}

func (im *Importer) moveToSurvivor(ctx context.Context, q *dbq.Queries, m *DedupeMerge) error {
	moved := dbq.MoveDedupeChildrenParams{IntoID: m.Survivor, FromID: m.Loser}
	n, err := q.MoveDedupeChildren(ctx, moved)
	if err != nil {
		return fmt.Errorf("move places under %s: %w", m.Loser, err)
	}
	m.Children = n
	if err := q.MoveDedupeFollows(ctx, dbq.MoveDedupeFollowsParams{IntoID: m.Survivor, FromID: m.Loser}); err != nil {
		return fmt.Errorf("move follows of %s: %w", m.Loser, err)
	}
	if err := q.InsertDedupeAudit(ctx, dbq.InsertDedupeAuditParams{
		Action:     perspectivv1.AuditAction_AUDIT_ACTION_MERGE_PLACE.String(),
		TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PLACE.String(),
		TargetID:   m.Loser,
		Reason:     "deduplicated by placectl into " + m.Survivor.String(),
	}); err != nil {
		return fmt.Errorf("audit merge of %s: %w", m.Loser, err)
	}
	im.log.Info().Str("event", "place_deduplicated").Str("place", m.Loser.String()).
		Str("into", m.Survivor.String()).Int64("children", n).Msg("duplicate place hidden")
	return nil
}
