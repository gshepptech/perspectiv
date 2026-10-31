package arc

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc/dbq"
)

// loreRow is one lore on an arc page.
type loreRow = dbq.ListArcSubloresRow

// canonOwner is an arc and the lore the rank job made its canon, uuid.Nil
// when it has none.
type canonOwner struct {
	arcID, canonID uuid.UUID
}

// loadCanon reads the lore the rank job made the arc's canon, nil when
// canonID is uuid.Nil or names no lore, and builds sublores (a page from
// pageSublores) in the same batch: the members and encounters of both are
// one query each. A day arc's canon is read with its hours' instead
// (dayCanonAndHours).
func loadCanon(ctx context.Context, q *dbq.Queries, arcID, canonID uuid.UUID, sublores []shownLore) (*perspectivv1.ArcLore, []*perspectivv1.ArcLore, error) {
	canons, subs, err := loadCanons(ctx, q, []canonOwner{{arcID: arcID, canonID: canonID}}, sublores)
	if err != nil {
		return nil, nil, err
	}
	return canons[0], subs, nil
}

// loadCanons reads the canon of each arc, with its members and encounters,
// in three statements however many arcs there are, and returns them in the
// order of owners. An arc's entry is nil when it has no canon or its canon
// names no lore. A canon is shown while the rank job points at it, even
// with no members left. The lores in also are built in the same batch and
// returned second, in their order, so an arc page's sublores cost no
// member or encounter query of their own.
func loadCanons(ctx context.Context, q *dbq.Queries, owners []canonOwner, also []shownLore) ([]*perspectivv1.ArcLore, []*perspectivv1.ArcLore, error) {
	shown, at, err := canonRows(ctx, q, owners)
	if err != nil {
		return nil, nil, err
	}
	lores, err := withMembers(ctx, q, append(shown, also...))
	if err != nil {
		return nil, nil, err
	}
	out := make([]*perspectivv1.ArcLore, len(owners))
	for j, i := range at {
		out[i] = lores[j]
	}
	return out, lores[len(at):], nil
}

// canonRows reads the canon lore of each owner that has one, in one query,
// and returns them in the order of owners with each one's index in owners.
func canonRows(ctx context.Context, q *dbq.Queries, owners []canonOwner) ([]shownLore, []int, error) {
	var ids []uuid.UUID
	for _, o := range owners {
		if o.canonID != uuid.Nil {
			ids = append(ids, o.canonID)
		}
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	rows, err := q.ListLoresByID(ctx, ids)
	if err != nil {
		return nil, nil, apiutil.Internal(fmt.Errorf("read canon lores: %w", err))
	}
	byID := make(map[uuid.UUID]loreRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = loreRow(r)
	}
	var shown []shownLore
	var at []int
	for i, o := range owners {
		if r, ok := byID[o.canonID]; ok {
			shown = append(shown, shownLore{arcID: o.arcID, row: r})
			at = append(at, i)
		}
	}
	return shown, at, nil
}

// shownLore is a lore and the arc whose page shows it: the arc's own lores,
// or an hour's canon on its day's page.
type shownLore struct {
	arcID uuid.UUID
	row   loreRow
}

// listSublores pages every lore but the canon that has a member, highest
// rank first, with their members and encounters.
func listSublores(ctx context.Context, q *dbq.Queries, arcID, canonID uuid.UUID, page apiutil.PageToken) ([]*perspectivv1.ArcLore, string, error) {
	shown, next, err := pageSublores(ctx, q, arcID, canonID, page)
	if err != nil {
		return nil, "", err
	}
	lores, err := withMembers(ctx, q, shown)
	if err != nil {
		return nil, "", err
	}
	return lores, next, nil
}

// pageSublores reads one page of the sublores' rows, without their members.
// The cursor is the last lore's (rank score, id).
func pageSublores(ctx context.Context, q *dbq.Queries, arcID, canonID uuid.UUID, page apiutil.PageToken) ([]shownLore, string, error) {
	params := dbq.ListArcSubloresParams{ArcID: arcID, RowLimit: page.Limit + 1}
	if canonID != uuid.Nil {
		params.CanonID = uuid.NullUUID{UUID: canonID, Valid: true}
	}
	if page.After != "" {
		scoreText, idText, ok := strings.Cut(page.After, "|")
		score, serr := strconv.ParseFloat(scoreText, 64)
		id, ierr := uuid.Parse(idText)
		if !ok || serr != nil || ierr != nil {
			return nil, "", errBadToken
		}
		params.AfterScore = pgtype.Float8{Float64: score, Valid: true}
		params.AfterID = uuid.NullUUID{UUID: id, Valid: true}
	}
	rows, err := q.ListArcSublores(ctx, params)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list sublores: %w", err))
	}
	next := ""
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		last := rows[len(rows)-1]
		next = apiutil.NextPageToken(strconv.FormatFloat(last.RankScore, 'g', -1, 64) + "|" + last.ID.String())
	}
	shown := make([]shownLore, len(rows))
	for i, r := range rows {
		shown[i] = shownLore{arcID: arcID, row: r}
	}
	return shown, next, nil
}

// withMembers builds the wire lores for lores, in order, reading the member
// perspectives and encounters of all of them in one query each, whichever
// arcs they belong to.
func withMembers(ctx context.Context, q *dbq.Queries, lores []shownLore) ([]*perspectivv1.ArcLore, error) {
	out := make([]*perspectivv1.ArcLore, 0, len(lores))
	if len(lores) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(lores))
	for i, l := range lores {
		ids[i] = l.row.ID
	}
	memberRows, err := q.ListLorePerspectives(ctx, ids)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list lore members: %w", err))
	}
	members := map[uuid.UUID][]perspectiveRow{}
	for _, m := range memberRows {
		members[m.MemberOf] = append(members[m.MemberOf], memberRow(m))
	}
	encounters, err := q.ListLoreEncounters(ctx, ids)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list encounters: %w", err))
	}
	for _, l := range lores {
		var sums []*perspectivv1.PerspectiveSummary
		for _, m := range members[l.row.ID] {
			sum, err := summary(l.arcID, m)
			if err != nil {
				return nil, apiutil.Internal(err)
			}
			sums = append(sums, sum)
		}
		al, err := arcLore(l.arcID, l.row, sums, encounters)
		if err != nil {
			return nil, apiutil.Internal(err)
		}
		out = append(out, al)
	}
	return out, nil
}

func memberRow(m dbq.ListLorePerspectivesRow) perspectiveRow {
	return perspectiveRow{
		ID: m.ID, AuthorID: m.AuthorID, AuthorUsername: m.AuthorUsername, Genre: m.Genre, LoreStatus: m.LoreStatus,
		MemberOf: uuid.NullUUID{UUID: m.MemberOf, Valid: true}, PublishedAt: m.PublishedAt, DeletedAt: m.DeletedAt,
		HiddenAt: m.HiddenAt, Opening: m.Opening, PartCount: m.PartCount, Upvotes: m.Upvotes,
	}
}

func arcLore(arcID uuid.UUID, l loreRow, members []*perspectivv1.PerspectiveSummary, encounters []dbq.ListLoreEncountersRow) (*perspectivv1.ArcLore, error) {
	standing, err := loreStandingFromDB(l.Status)
	if err != nil {
		return nil, err
	}
	lore := &perspectivv1.Lore{
		Id:          l.ID.String(),
		ArcId:       arcID.String(),
		Standing:    standing,
		MemberCount: l.MemberCount,
		Upvotes:     l.Upvotes,
		RankScore:   l.RankScore,
		ComputedAt:  timestampFromDB(l.ComputedAt),
		Name:        l.Name,
	}
	if l.NamedBy.Valid && l.NamedByUsername != "" {
		lore.NamedBy = &perspectivv1.UserRef{UserId: l.NamedBy.UUID.String(), Username: l.NamedByUsername}
	}
	for _, m := range members {
		lore.Members = append(lore.Members, &perspectivv1.LoreMember{PerspectiveId: m.GetId(), User: m.GetAuthor()})
	}
	for _, e := range encounters {
		if e.LoreID == l.ID {
			lore.Encounters = append(lore.Encounters, &perspectivv1.Encounter{
				ClaimId:               e.ClaimID.String(),
				PerspectiveId:         e.PerspectiveID.String(),
				ClaimantPerspectiveId: e.ClaimantPerspectiveID.String(),
			})
		}
	}
	return &perspectivv1.ArcLore{Lore: lore, Perspectives: members}, nil
}
