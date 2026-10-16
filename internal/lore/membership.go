package lore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/lore/dbq"
)

// Member is one perspective in a lore and its author.
type Member struct {
	PerspectiveID uuid.UUID
	UserID        uuid.UUID
}

// MembershipOf returns the lore the perspective belongs to and every member
// of that lore, ordered by perspective id. ok is false, with no lore and no
// members, when the perspective is not IN_LORE. An unknown perspective is
// NotFound.
func (s *Service) MembershipOf(ctx context.Context, tx pgx.Tx, perspectiveID uuid.UUID) (uuid.UUID, []Member, bool, error) {
	q := dbq.New(tx)
	row, err := q.GetPerspectiveLore(ctx, perspectiveID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil, false, apiutil.NotFound("perspective not found")
	}
	if err != nil {
		return uuid.Nil, nil, false, apiutil.Internal(fmt.Errorf("read perspective lore: %w", err))
	}
	if row.LoreStatus.String != loreStatusInLore || !row.LoreID.Valid {
		return uuid.Nil, nil, false, nil
	}
	rows, err := q.ListLoreMembers(ctx, row.LoreID.UUID)
	if err != nil {
		return uuid.Nil, nil, false, apiutil.Internal(fmt.Errorf("list lore members: %w", err))
	}
	members := make([]Member, 0, len(rows))
	for _, r := range rows {
		members = append(members, Member{PerspectiveID: r.PerspectiveID, UserID: r.AuthorID})
	}
	return row.LoreID.UUID, members, true, nil
}

// RecomputeArc rebuilds lore membership in one arc from its accepted claims.
//
// It first rejects every PENDING or ACCEPTED claim touching a deleted or
// hidden perspective, then groups the arc's perspectives into connected
// components of the remaining accepted claims (see the ArcGroups query) and
// stores one lore per group:
//
//   - Each group keeps the id of an existing lore. Candidate (group, lore)
//     pairs are taken greedily by how many of the group's members the lore
//     holds, most first, ties to the lowest lore id; each lore and each group
//     is used once. So a group keeps the lore holding most of its members, and
//     when a lore splits, the half holding more of it keeps the id. A group
//     left without an existing lore gets a new one.
//   - Members are IN_LORE with their lore id set; a SOLO tale that gained an
//     accepted link is revived this way. REMOVED perspectives never appear:
//     RemoveFromLore rejected their claims.
//   - A perspective that was IN_LORE and is in no group any more becomes
//     SOLO with no lore. It is not set back to PENDING: its expiry job may
//     already have run and is a no-op for anything but PENDING, so PENDING
//     could strand it outside lore for good, while a SOLO tale stays readable
//     and is revived by its next accepted claim.
//   - Every lore in the arc gets its member count; a lore whose group vanished
//     keeps its row with member_count 0.
//
// It never writes lores.status, lores.rank_score or arcs.canon_lore_id, and
// it does not set arcs.lore_dirty_at: the rank job calls it while clearing
// that mark, and request-path callers mark the arc themselves.
func (s *Service) RecomputeArc(ctx context.Context, tx pgx.Tx, arcID uuid.UUID) error {
	if arcID == uuid.Nil {
		return apiutil.InvalidArg("arc is required")
	}
	q := dbq.New(tx)
	if err := q.LockArcLore(ctx, arcID); err != nil {
		return apiutil.Internal(fmt.Errorf("lock arc: %w", err))
	}
	if err := q.RejectClaimsOnWithdrawn(ctx, dbq.RejectClaimsOnWithdrawnParams{
		ArcID: arcID, Rejected: claimRejected, ActiveStatuses: activeClaims,
	}); err != nil {
		return apiutil.Internal(fmt.Errorf("reject claims on withdrawn perspectives: %w", err))
	}
	groupRows, err := q.ArcGroups(ctx, dbq.ArcGroupsParams{ArcID: arcID, Accepted: claimAccepted})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("compute groups: %w", err))
	}
	current, err := q.ArcMemberships(ctx, arcID)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read memberships: %w", err))
	}

	groups := make(map[uuid.UUID][]uuid.UUID)
	for _, r := range groupRows {
		groups[r.GroupKey] = append(groups[r.GroupKey], r.PerspectiveID)
	}
	was := make(map[uuid.UUID]uuid.UUID, len(current))
	for _, r := range current {
		was[r.PerspectiveID] = r.LoreID
	}
	assigned := assignLores(groups, was)

	var loreIDs, perspectiveIDs []uuid.UUID
	for _, key := range sortedKeys(groups) {
		id, ok := assigned[key]
		if !ok {
			if id, err = q.InsertLore(ctx, arcID); err != nil {
				return apiutil.Internal(fmt.Errorf("insert lore: %w", err))
			}
		}
		for _, p := range groups[key] {
			loreIDs = append(loreIDs, id)
			perspectiveIDs = append(perspectiveIDs, p)
		}
	}
	return storeMembership(ctx, q, arcID, loreIDs, perspectiveIDs)
}

// storeMembership replaces the arc's lore_members with the given pairs and
// brings perspectives and lore counts in line with them.
func storeMembership(ctx context.Context, q *dbq.Queries, arcID uuid.UUID, loreIDs, perspectiveIDs []uuid.UUID) error {
	if err := q.DeleteArcMembers(ctx, arcID); err != nil {
		return apiutil.Internal(fmt.Errorf("clear members: %w", err))
	}
	if len(loreIDs) > 0 {
		if err := q.InsertMembers(ctx, dbq.InsertMembersParams{LoreIds: loreIDs, PerspectiveIds: perspectiveIDs}); err != nil {
			return apiutil.Internal(fmt.Errorf("insert members: %w", err))
		}
	}
	if err := q.MarkMembersInLore(ctx, dbq.MarkMembersInLoreParams{ArcID: arcID, InLore: loreStatusInLore}); err != nil {
		return apiutil.Internal(fmt.Errorf("mark members in lore: %w", err))
	}
	if err := q.ReleaseNonMembers(ctx, dbq.ReleaseNonMembersParams{
		ArcID: arcID, InLore: loreStatusInLore, ReleasedStatus: loreStatusSolo,
	}); err != nil {
		return apiutil.Internal(fmt.Errorf("release non-members: %w", err))
	}
	if err := q.SetArcLoreCounts(ctx, arcID); err != nil {
		return apiutil.Internal(fmt.Errorf("set lore counts: %w", err))
	}
	return nil
}

// assignLores maps group keys to the existing lore each keeps; see
// RecomputeArc for the rule. Groups missing from the result need a new lore.
func assignLores(groups map[uuid.UUID][]uuid.UUID, was map[uuid.UUID]uuid.UUID) map[uuid.UUID]uuid.UUID {
	type candidate struct {
		group, lore uuid.UUID
		overlap     int
	}
	var candidates []candidate
	for key, members := range groups {
		counts := make(map[uuid.UUID]int)
		for _, p := range members {
			if l, ok := was[p]; ok {
				counts[l]++
			}
		}
		for l, n := range counts {
			candidates = append(candidates, candidate{group: key, lore: l, overlap: n})
		}
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.overlap != b.overlap {
			return b.overlap - a.overlap
		}
		if c := bytes.Compare(a.lore[:], b.lore[:]); c != 0 {
			return c
		}
		return bytes.Compare(a.group[:], b.group[:])
	})
	assigned := make(map[uuid.UUID]uuid.UUID)
	taken := make(map[uuid.UUID]bool)
	for _, c := range candidates {
		if _, done := assigned[c.group]; done || taken[c.lore] {
			continue
		}
		assigned[c.group] = c.lore
		taken[c.lore] = true
	}
	return assigned
}

func sortedKeys(groups map[uuid.UUID][]uuid.UUID) []uuid.UUID {
	keys := make([]uuid.UUID, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return keys
}

// RemoveFromLore takes a published perspective out of lore for good: it sets
// lore_status REMOVED, rejects every PENDING or ACCEPTED claim on its
// characters and every one its author made in that arc, removes its
// membership, recomputes the arc's remaining groups and marks the arc
// lore-dirty, all inside tx. The perspective stays readable. It is a no-op on
// a perspective that is already REMOVED. An unknown perspective is NotFound
// and a draft, which is in no arc, is FailedPrecondition.
func (s *Service) RemoveFromLore(ctx context.Context, tx pgx.Tx, perspectiveID uuid.UUID) error {
	q := dbq.New(tx)
	arc, err := q.GetPerspectiveArc(ctx, perspectiveID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apiutil.NotFound("perspective not found")
	}
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read perspective arc: %w", err))
	}
	if !arc.Valid {
		return apiutil.FailedPrecondition("perspective is not published")
	}
	// A published perspective never changes arc, so the arc read before the
	// locks is the arc it is in.
	arcID := arc.UUID
	if err := q.LockArcLore(ctx, arcID); err != nil {
		return apiutil.Internal(fmt.Errorf("lock arc: %w", err))
	}
	p, _, err := lockPair(ctx, q, perspectiveID, perspectiveID)
	if err != nil {
		return err
	}
	if removed(p) {
		return nil
	}
	if err := q.RejectClaimsOfPerspective(ctx, dbq.RejectClaimsOfPerspectiveParams{
		Rejected: claimRejected, ActiveStatuses: activeClaims,
		PerspectiveID: perspectiveID, AuthorID: p.AuthorID, ArcID: arcID,
	}); err != nil {
		return apiutil.Internal(fmt.Errorf("reject claims: %w", err))
	}
	if err := q.SetPerspectiveRemoved(ctx, dbq.SetPerspectiveRemovedParams{ID: perspectiveID, Removed: loreStatusRemoved}); err != nil {
		return apiutil.Internal(fmt.Errorf("set removed: %w", err))
	}
	if err := q.DeleteMemberOf(ctx, perspectiveID); err != nil {
		return apiutil.Internal(fmt.Errorf("remove membership: %w", err))
	}
	if err := s.RecomputeArc(ctx, tx, arcID); err != nil {
		return err
	}
	if err := q.MarkArcLoreDirty(ctx, arcID); err != nil {
		return apiutil.Internal(fmt.Errorf("mark arc lore dirty: %w", err))
	}
	return nil
}
