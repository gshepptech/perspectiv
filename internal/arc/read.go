package arc

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc/dbq"
)

// GetArc assembles the first screen of an arc page: the arc with its place
// chain, its canon lore, the first page of each list (every other lore that
// has a member, highest rank first, a lore the rank job demoted included; the
// tales, SOLO and REMOVED; the PENDING perspectives not yet linked; and for a
// day arc its hour children in hour order, each with its own canon), a next
// page token for each list, and for a signed-in viewer where they stand in
// the arc. ListArcSection serves the later pages. viewer is uuid.Nil for a
// signed-out reader.
//
// An arc whose place, or any ancestor of it, moderation hid is NotFound, like
// an unknown arc: hiding a place hides its whole subtree. A deleted or hidden
// perspective stays in its place on the page as a placeholder (see summary).
//
// Everything is read in one REPEATABLE READ snapshot, so the page never mixes
// two runs of the rank job.
func (s *Service) GetArc(ctx context.Context, viewer, arcID uuid.UUID, pageSize int32) (*perspectivv1.GetArcResponse, error) {
	first, err := apiutil.ParsePageToken("", pageSize)
	if err != nil {
		return nil, err
	}
	var resp *perspectivv1.GetArcResponse
	err = s.readTx(ctx, func(q *dbq.Queries) error {
		arc, err := loadArc(ctx, q, arcID)
		if err != nil {
			return err
		}
		canonID := parseID(arc.GetCanonLoreId())
		resp = &perspectivv1.GetArcResponse{Arc: arc}
		sublores, next, err := pageSublores(ctx, q, arcID, canonID, first)
		if err != nil {
			return err
		}
		resp.SubloresNextPageToken = next
		if err := firstPerspectives(ctx, q, resp, first); err != nil {
			return err
		}
		// The sublores' members and encounters are read in the canon batch.
		if arc.Hour == nil {
			err = dayCanonAndHours(ctx, q, resp, canonID, sublores, first)
		} else {
			resp.Canon, resp.Sublores, err = loadCanon(ctx, q, arcID, canonID, sublores)
		}
		if err != nil {
			return err
		}
		return forViewer(ctx, q, resp, viewer)
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ListArcSection serves the next page of one list on an arc page, from a
// token GetArc or an earlier call returned. An hour arc has no hours list,
// so asking for it there returns an empty page.
func (s *Service) ListArcSection(ctx context.Context, viewer, arcID uuid.UUID, section perspectivv1.ArcSection, pageToken string, pageSize int32) (*perspectivv1.ListArcSectionResponse, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return nil, err
	}
	resp := &perspectivv1.ListArcSectionResponse{}
	read := func(q *dbq.Queries) error { return listSection(ctx, q, viewer, arcID, section, page, resp) }
	if needsSnapshot(viewer, section) {
		err = s.readTx(ctx, read)
	} else {
		err = read(dbq.New(s.pool))
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// needsSnapshot reports whether a ListArcSection page must be read in one
// snapshot. A signed-out page of tales or unlinked perspectives is the list
// alone, which reads the arc's visibility itself, and the arc row only when
// the list is empty (sectionPerspectives), and needs none: the arc row only
// decides NotFound and the list reads nothing from it, so a hide or a new
// story landing between the two shows the reader what one request a moment
// earlier or later would have. Every other page has statements a reader
// could catch disagreeing: a lore's member count against the members listed
// under it and its encounters, an hour's canon against its story count, or
// a signed-in viewer's upvote mark against the vote count beside it.
func needsSnapshot(viewer uuid.UUID, section perspectivv1.ArcSection) bool {
	switch section {
	case perspectivv1.ArcSection_ARC_SECTION_TALES, perspectivv1.ArcSection_ARC_SECTION_UNLINKED:
		return viewer != uuid.Nil
	default:
		return true
	}
}

// listSection reads one ListArcSection page into resp.
func listSection(ctx context.Context, q *dbq.Queries, viewer, arcID uuid.UUID, section perspectivv1.ArcSection, page apiutil.PageToken, resp *perspectivv1.ListArcSectionResponse) error {
	var err error
	switch section {
	case perspectivv1.ArcSection_ARC_SECTION_TALES, perspectivv1.ArcSection_ARC_SECTION_UNLINKED:
		// The list reads the arc's visibility itself (sectionPerspectives).
		resp.Perspectives, resp.NextPageToken, err = sectionPerspectives(ctx, q, arcID, section, page)
	default:
		err = arcSection(ctx, q, arcID, section, page, resp)
	}
	if err != nil || viewer == uuid.Nil {
		return err
	}
	lists := [][]*perspectivv1.PerspectiveSummary{resp.Perspectives}
	for _, l := range resp.Sublores {
		lists = append(lists, l.GetPerspectives())
	}
	return markVoted(ctx, q, viewer, lists...)
}

// arcSection reads a ListArcSection page that needs the arc row first: the
// sublores leave out its canon, and only a day has hours. A missing arc, or
// one at or under a hidden place, is NotFound before the section is checked.
func arcSection(ctx context.Context, q *dbq.Queries, arcID uuid.UUID, section perspectivv1.ArcSection, page apiutil.PageToken, resp *perspectivv1.ListArcSectionResponse) error {
	arc, err := loadArc(ctx, q, arcID)
	if err != nil {
		return err
	}
	switch section {
	case perspectivv1.ArcSection_ARC_SECTION_SUBLORES:
		resp.Sublores, resp.NextPageToken, err = listSublores(ctx, q, arcID, parseID(arc.GetCanonLoreId()), page)
	case perspectivv1.ArcSection_ARC_SECTION_HOURS:
		if arc.Hour == nil {
			resp.Hours, resp.NextPageToken, err = listHours(ctx, q, arcID, arc.GetPlaceChain(), page)
		}
	case perspectivv1.ArcSection_ARC_SECTION_TALES, perspectivv1.ArcSection_ARC_SECTION_UNLINKED:
		resp.Perspectives, resp.NextPageToken, err = listPerspectives(ctx, q, arcID, section, page)
	case perspectivv1.ArcSection_ARC_SECTION_UNSPECIFIED:
		err = apiutil.InvalidArg("section is required")
	default:
		err = apiutil.InvalidArg("section is not a known arc page list")
	}
	return err
}

// forViewer adds what only a signed-in viewer sees: where they stand in the
// arc and which stories they upvoted. A signed-out response is left as is.
func forViewer(ctx context.Context, q *dbq.Queries, resp *perspectivv1.GetArcResponse, viewer uuid.UUID) error {
	if viewer == uuid.Nil {
		return nil
	}
	var err error
	if resp.Viewer, err = viewerIn(ctx, q, parseID(resp.GetArc().GetId()), viewer); err != nil {
		return err
	}
	lists := [][]*perspectivv1.PerspectiveSummary{resp.Tales, resp.Unlinked}
	for _, l := range append([]*perspectivv1.ArcLore{resp.Canon}, resp.Sublores...) {
		lists = append(lists, l.GetPerspectives())
	}
	return markVoted(ctx, q, viewer, lists...)
}

// markVoted sets Voted on every summary the viewer has upvoted, with one
// query, so the arc page needs no read per story to draw its vote rail.
func markVoted(ctx context.Context, q *dbq.Queries, viewer uuid.UUID, lists ...[]*perspectivv1.PerspectiveSummary) error {
	byID := map[uuid.UUID][]*perspectivv1.PerspectiveSummary{}
	var ids []uuid.UUID
	for _, l := range lists {
		for _, p := range l {
			id, err := uuid.Parse(p.GetId())
			if err != nil {
				continue
			}
			if _, seen := byID[id]; !seen {
				ids = append(ids, id)
			}
			byID[id] = append(byID[id], p)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	voted, err := q.ViewerVotes(ctx, dbq.ViewerVotesParams{UserID: viewer, Ids: ids})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read viewer votes: %w", err))
	}
	for _, id := range voted {
		for _, p := range byID[id] {
			p.Voted = true
		}
	}
	return nil
}

// readTx runs fn in one read-only REPEATABLE READ snapshot. BEGIN and
// ROLLBACK are two round trips of their own, so a read that sends only
// statements that cannot disagree goes straight to the pool instead (see
// needsSnapshot and ListOpenCharacters).
func (s *Service) readTx(ctx context.Context, fn func(q *dbq.Queries) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return fn(dbq.New(tx))
}

// loadArc reads one arc with its place chain. A missing arc, or one at a
// place moderation hid or below one, is NotFound.
func loadArc(ctx context.Context, q *dbq.Queries, arcID uuid.UUID) (*perspectivv1.Arc, error) {
	row, err := q.GetArcRow(ctx, arcID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.PlaceHidden) {
		return nil, apiutil.NotFound("day not found")
	}
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read arc: %w", err))
	}
	chain, err := placeChain(row)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	return arcProto(dbq.ListHourArcsRow{
		ID: row.ID, PlaceID: row.PlaceID, LocalDate: row.LocalDate, Hour: row.Hour,
		ParentArcID: row.ParentArcID, Title: row.Title, CreatedBy: row.CreatedBy,
		CanonLoreID: row.CanonLoreID, CanonScore: row.CanonScore, MemberCount: row.MemberCount,
		CreatedAt: row.CreatedAt, CreatorUsername: row.CreatorUsername,
	}, chain), nil
}

// placeChain builds the wire chain from GetArcRow's three parallel arrays,
// root (STATE) first.
func placeChain(row dbq.GetArcRowRow) ([]*perspectivv1.PlaceRef, error) {
	if len(row.ChainKinds) != len(row.ChainIds) || len(row.ChainNames) != len(row.ChainIds) {
		return nil, fmt.Errorf("place chain of arc %s: %d ids, %d kinds, %d names",
			row.ID, len(row.ChainIds), len(row.ChainKinds), len(row.ChainNames))
	}
	chain := make([]*perspectivv1.PlaceRef, len(row.ChainIds))
	for i, id := range row.ChainIds {
		kind, err := placeKindFromDB(row.ChainKinds[i])
		if err != nil {
			return nil, err
		}
		chain[i] = &perspectivv1.PlaceRef{Id: id.String(), Kind: kind, Name: row.ChainNames[i]}
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

// viewerIn is where the viewer stands in the arc: their readable published
// perspective there and its lore, both empty when they have none.
func viewerIn(ctx context.Context, q *dbq.Queries, arcID, viewer uuid.UUID) (*perspectivv1.ArcViewer, error) {
	row, err := q.ViewerInArc(ctx, dbq.ViewerInArcParams{ArcID: arcID, AuthorID: viewer})
	if errors.Is(err, pgx.ErrNoRows) {
		return &perspectivv1.ArcViewer{}, nil
	}
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read viewer: %w", err))
	}
	v := &perspectivv1.ArcViewer{PerspectiveId: row.ID.String()}
	if row.MemberOf.Valid {
		v.LoreId = row.MemberOf.UUID.String()
	}
	return v, nil
}

// errBadToken answers a page token this package did not write.
var errBadToken = apiutil.InvalidArg("invalid page token")

// parseID reads an id this package wrote itself; empty is uuid.Nil.
func parseID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// Sitemap lists up to apiutil.SitemapLimit days and hours with a readable
// story, not under a hidden place, most recently written first.
func (s *Service) Sitemap(ctx context.Context) ([]apiutil.SitemapEntry, error) {
	rows, err := dbq.New(s.pool).SitemapArcs(ctx, apiutil.SitemapLimit)
	if err != nil {
		return nil, fmt.Errorf("sitemap arcs: %w", err)
	}
	out := make([]apiutil.SitemapEntry, len(rows))
	for i, r := range rows {
		out[i] = apiutil.SitemapEntry{ID: r.ID, LastMod: r.LastMod.Time}
	}
	return out, nil
}
