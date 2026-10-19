package arc

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc/dbq"
)

// excerptLen is how many characters of a perspective's first part an arc page
// summary carries; a longer opening is cut there and ends in "…".
const excerptLen = 240

// GetArc assembles an arc page: the arc with its place chain, its canon lore,
// every other lore (highest rank first; a lore the rank job demoted stays
// listed), its tales (SOLO and REMOVED perspectives), its unlinked PENDING
// perspectives, and for a day arc its hour child arcs in ascending hour order,
// each with its own canon. Nothing is left out because of standing.
//
// An arc whose place, or any ancestor of it, moderation hid is NotFound, like
// an unknown arc: hiding a place hides its whole subtree. A
// deleted or hidden perspective stays in its place on the page as a
// placeholder (see summary).
//
// Everything is read in one REPEATABLE READ snapshot, so the page never mixes
// two runs of the rank job.
func (s *Service) GetArc(ctx context.Context, arcID uuid.UUID) (*perspectivv1.GetArcResponse, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbq.New(tx)

	arc, err := loadArc(ctx, q, arcID)
	if err != nil {
		return nil, err
	}
	content, err := loadContent(ctx, q, arcID, parseID(arc.GetCanonLoreId()))
	if err != nil {
		return nil, err
	}
	resp := &perspectivv1.GetArcResponse{
		Arc:      arc,
		Canon:    content.canon,
		Sublores: content.sublores,
		Tales:    content.tales,
		Unlinked: content.unlinked,
	}
	if arc.Hour == nil {
		if resp.Hours, err = loadHours(ctx, q, arcID, arc.GetPlaceChain()); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// loadArc reads one arc with its place chain. A missing arc, or one at a
// place moderation hid or below one, is NotFound.
func loadArc(ctx context.Context, q *dbq.Queries, arcID uuid.UUID) (*perspectivv1.Arc, error) {
	row, err := q.GetArcRow(ctx, arcID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.PlaceHidden) {
		return nil, apiutil.NotFound("arc not found")
	}
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read arc: %w", err))
	}
	chain, err := placeChain(ctx, q, row.PlaceID)
	if err != nil {
		return nil, err
	}
	return arcProto(dbq.ListHourArcsRow{
		ID: row.ID, PlaceID: row.PlaceID, LocalDate: row.LocalDate, Hour: row.Hour,
		ParentArcID: row.ParentArcID, Title: row.Title, CreatedBy: row.CreatedBy,
		CanonLoreID: row.CanonLoreID, CanonScore: row.CanonScore, MemberCount: row.MemberCount,
		CreatedAt: row.CreatedAt, CreatorUsername: row.CreatorUsername,
	}, chain), nil
}

func placeChain(ctx context.Context, q *dbq.Queries, placeID uuid.UUID) ([]*perspectivv1.PlaceRef, error) {
	rows, err := q.PlaceChain(ctx, placeID)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read place chain: %w", err))
	}
	chain := make([]*perspectivv1.PlaceRef, 0, len(rows))
	for _, r := range rows {
		kind, err := placeKindFromDB(r.Kind)
		if err != nil {
			return nil, apiutil.Internal(err)
		}
		chain = append(chain, &perspectivv1.PlaceRef{Id: r.ID.String(), Kind: kind, Name: r.Name})
	}
	return chain, nil
}

// arcProto builds the wire Arc. chain runs from the STATE to the arc's place,
// so its last element is the arc's own place.
func arcProto(r dbq.ListHourArcsRow, chain []*perspectivv1.PlaceRef) *perspectivv1.Arc {
	a := &perspectivv1.Arc{
		Id:          r.ID.String(),
		Title:       r.Title,
		PlaceChain:  chain,
		LocalDate:   dateFromDB(r.LocalDate),
		Hour:        hourFromDB(r.Hour),
		CanonScore:  r.CanonScore,
		MemberCount: r.MemberCount,
		CreatedBy:   &perspectivv1.UserRef{UserId: r.CreatedBy.String(), Username: r.CreatorUsername},
		CreatedAt:   timestampFromDB(r.CreatedAt),
	}
	if len(chain) > 0 {
		a.Place = chain[len(chain)-1]
	}
	if r.ParentArcID.Valid {
		a.ParentArcId = r.ParentArcID.UUID.String()
	}
	if r.CanonLoreID.Valid {
		a.CanonLoreId = r.CanonLoreID.UUID.String()
	}
	return a
}

// loadHours lists a day arc's hour children in ascending hour order, each with
// its canon and the number of its perspectives that are neither deleted nor
// hidden.
func loadHours(ctx context.Context, q *dbq.Queries, dayID uuid.UUID, chain []*perspectivv1.PlaceRef) ([]*perspectivv1.HourArc, error) {
	rows, err := q.ListHourArcs(ctx, uuid.NullUUID{UUID: dayID, Valid: true})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list hour arcs: %w", err))
	}
	hours := make([]*perspectivv1.HourArc, 0, len(rows))
	for _, r := range rows {
		canonID := uuid.Nil
		if r.CanonLoreID.Valid {
			canonID = r.CanonLoreID.UUID
		}
		content, err := loadContent(ctx, q, r.ID, canonID)
		if err != nil {
			return nil, err
		}
		hours = append(hours, &perspectivv1.HourArc{
			Arc:              arcProto(r, chain),
			Canon:            content.canon,
			PerspectiveCount: content.visible,
		})
	}
	return hours, nil
}

// content is what an arc holds, sorted into the sections of its page.
type content struct {
	canon    *perspectivv1.ArcLore
	sublores []*perspectivv1.ArcLore
	tales    []*perspectivv1.PerspectiveSummary
	unlinked []*perspectivv1.PerspectiveSummary
	// visible counts published perspectives that are neither deleted nor hidden.
	visible int32
}

// loadContent sorts an arc's published perspectives into its sections.
//
// Lore membership is read from lore_members. A member sits under its lore; the
// lore named by canonID (the rank job's arcs.canon_lore_id) is the canon and
// every other lore is a sublore, in rank order. A sublore whose group has
// dissolved (no members left) has nothing to read and is not listed; the
// canon is always shown while the rank job still points at it. A perspective
// in no lore is a tale when SOLO or REMOVED and unlinked otherwise (PENDING,
// or IN_LORE before the rank job recomputes membership). Each section is in
// publish order, oldest first.
func loadContent(ctx context.Context, q *dbq.Queries, arcID, canonID uuid.UUID) (content, error) {
	var c content
	rows, err := q.ListArcPerspectives(ctx, uuid.NullUUID{UUID: arcID, Valid: true})
	if err != nil {
		return c, apiutil.Internal(fmt.Errorf("list perspectives: %w", err))
	}
	lores, err := q.ListArcLores(ctx, arcID)
	if err != nil {
		return c, apiutil.Internal(fmt.Errorf("list lores: %w", err))
	}
	encounters, err := q.ListArcEncounters(ctx, arcID)
	if err != nil {
		return c, apiutil.Internal(fmt.Errorf("list encounters: %w", err))
	}

	members := map[uuid.UUID][]*perspectivv1.PerspectiveSummary{}
	for _, r := range rows {
		sum, err := summary(arcID, r)
		if err != nil {
			return c, apiutil.Internal(err)
		}
		if !r.DeletedAt.Valid && !r.HiddenAt.Valid {
			c.visible++
		}
		switch {
		case r.MemberOf.Valid:
			members[r.MemberOf.UUID] = append(members[r.MemberOf.UUID], sum)
		case sum.GetLoreStatus() == perspectivv1.LoreStatus_LORE_STATUS_SOLO,
			sum.GetLoreStatus() == perspectivv1.LoreStatus_LORE_STATUS_REMOVED:
			c.tales = append(c.tales, sum)
		default:
			c.unlinked = append(c.unlinked, sum)
		}
	}

	for _, l := range lores {
		al, err := arcLore(arcID, l, members[l.ID], encounters)
		if err != nil {
			return c, apiutil.Internal(err)
		}
		switch {
		case l.ID == canonID:
			c.canon = al
		case len(members[l.ID]) > 0:
			c.sublores = append(c.sublores, al)
		}
	}
	return c, nil
}

func arcLore(arcID uuid.UUID, l dbq.ListArcLoresRow, members []*perspectivv1.PerspectiveSummary, encounters []dbq.ListArcEncountersRow) (*perspectivv1.ArcLore, error) {
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

// summary builds a perspective's arc page entry. A deleted or hidden
// perspective becomes a placeholder that the page renders as "[removed]": it
// keeps its id, genre, lore status, lore and publish time and its deleted or
// hidden flag, and carries no author, excerpt, part count or votes.
func summary(arcID uuid.UUID, r dbq.ListArcPerspectivesRow) (*perspectivv1.PerspectiveSummary, error) {
	genre, err := genreFromDB(r.Genre)
	if err != nil {
		return nil, err
	}
	status, err := loreStatusFromDB(r.LoreStatus)
	if err != nil {
		return nil, err
	}
	s := &perspectivv1.PerspectiveSummary{
		Id:          r.ID.String(),
		ArcId:       arcID.String(),
		Genre:       genre,
		LoreStatus:  status,
		PublishedAt: timestampFromDB(r.PublishedAt),
		Deleted:     r.DeletedAt.Valid,
		Hidden:      r.HiddenAt.Valid,
	}
	if r.MemberOf.Valid {
		s.LoreId = r.MemberOf.UUID.String()
	}
	if s.Deleted || s.Hidden {
		return s, nil
	}
	s.Author = &perspectivv1.UserRef{UserId: r.AuthorID.String(), Username: r.AuthorUsername}
	s.Excerpt = excerpt(r.Opening)
	s.PartCount = r.PartCount
	s.Upvotes = r.Upvotes
	return s, nil
}

func excerpt(opening string) string {
	if utf8.RuneCountInString(opening) <= excerptLen {
		return opening
	}
	return string([]rune(opening)[:excerptLen]) + "…"
}

// parseID reads an id this package wrote itself; empty is uuid.Nil.
func parseID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}
