package arc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc/dbq"
)

// excerptLen is how many characters of a perspective's first part an arc page
// summary carries; a longer opening is cut there and ends in "…".
const excerptLen = 240

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
		if resp.Sublores, resp.SubloresNextPageToken, err = listSublores(ctx, q, arcID, canonID, first); err != nil {
			return err
		}
		if resp.Tales, resp.TalesNextPageToken, err = listPerspectives(ctx, q, arcID, perspectivv1.ArcSection_ARC_SECTION_TALES, first); err != nil {
			return err
		}
		if resp.Unlinked, resp.UnlinkedNextPageToken, err = listPerspectives(ctx, q, arcID, perspectivv1.ArcSection_ARC_SECTION_UNLINKED, first); err != nil {
			return err
		}
		if arc.Hour == nil {
			err = dayCanonAndHours(ctx, q, resp, canonID, first)
		} else {
			resp.Canon, err = loadCanon(ctx, q, arcID, canonID)
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
// snapshot. A signed-out page of tales or unlinked perspectives is two
// statements, the arc and the list, and needs none: the arc row only decides
// NotFound and the list reads nothing from it, so a hide or a new story
// landing between the two shows the reader what one request a moment
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
	arc, err := loadArc(ctx, q, arcID)
	if err != nil {
		return err
	}
	switch section {
	case perspectivv1.ArcSection_ARC_SECTION_SUBLORES:
		resp.Sublores, resp.NextPageToken, err = listSublores(ctx, q, arcID, parseID(arc.GetCanonLoreId()), page)
	case perspectivv1.ArcSection_ARC_SECTION_TALES, perspectivv1.ArcSection_ARC_SECTION_UNLINKED:
		resp.Perspectives, resp.NextPageToken, err = listPerspectives(ctx, q, arcID, section, page)
	case perspectivv1.ArcSection_ARC_SECTION_HOURS:
		if arc.Hour == nil {
			resp.Hours, resp.NextPageToken, err = listHours(ctx, q, arcID, arc.GetPlaceChain(), page)
		}
	case perspectivv1.ArcSection_ARC_SECTION_UNSPECIFIED:
		err = apiutil.InvalidArg("section is required")
	default:
		err = apiutil.InvalidArg("section is not a known arc page list")
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

// listHours pages a day arc's hour children in hour order, each with its
// canon and the number of its perspectives that are neither deleted nor
// hidden. A page of hours is at most four statements however many hours it
// holds: the hours with their counts, then their canons, the canons'
// members and the canons' encounters.
func listHours(ctx context.Context, q *dbq.Queries, dayID uuid.UUID, chain []*perspectivv1.PlaceRef, page apiutil.PageToken) ([]*perspectivv1.HourArc, string, error) {
	rows, next, err := pageHours(ctx, q, dayID, page)
	if err != nil {
		return nil, "", err
	}
	canons, err := loadCanons(ctx, q, hourOwners(nil, rows))
	if err != nil {
		return nil, "", err
	}
	return hourArcs(rows, chain, canons), next, nil
}

// dayCanonAndHours fills a day arc page's canon and its first page of hours.
// The day's canon is read in the same batch as its hours' canons, so the day
// costs no statements beyond listHours'.
func dayCanonAndHours(ctx context.Context, q *dbq.Queries, resp *perspectivv1.GetArcResponse, canonID uuid.UUID, page apiutil.PageToken) error {
	day := resp.GetArc()
	dayID := parseID(day.GetId())
	rows, next, err := pageHours(ctx, q, dayID, page)
	if err != nil {
		return err
	}
	canons, err := loadCanons(ctx, q, hourOwners([]canonOwner{{arcID: dayID, canonID: canonID}}, rows))
	if err != nil {
		return err
	}
	resp.Canon = canons[0]
	resp.Hours = hourArcs(rows, day.GetPlaceChain(), canons[1:])
	resp.HoursNextPageToken = next
	return nil
}

// pageHours reads one page of a day arc's hour children with their story
// counts. The cursor is the last hour listed.
func pageHours(ctx context.Context, q *dbq.Queries, dayID uuid.UUID, page apiutil.PageToken) ([]dbq.ListHourArcsRow, string, error) {
	params := dbq.ListHourArcsParams{ParentArcID: dayID, RowLimit: page.Limit + 1}
	if page.After != "" {
		h, err := strconv.ParseInt(page.After, 10, 16)
		if err != nil || h < 0 || h > 23 {
			return nil, "", errBadToken
		}
		params.AfterHour = pgtype.Int2{Int16: int16(h), Valid: true}
	}
	rows, err := q.ListHourArcs(ctx, params)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list hour arcs: %w", err))
	}
	next := ""
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		next = apiutil.NextPageToken(strconv.Itoa(int(rows[len(rows)-1].Hour.Int16)))
	}
	return rows, next, nil
}

// hourOwners appends each hour's canon owner to owners, in hour order.
func hourOwners(owners []canonOwner, rows []dbq.ListHourArcsRow) []canonOwner {
	for _, r := range rows {
		owners = append(owners, canonOwner{arcID: r.ID, canonID: r.CanonLoreID.UUID})
	}
	return owners
}

// hourArcs builds the wire hours; canons[i] is the canon of rows[i].
func hourArcs(rows []dbq.ListHourArcsRow, chain []*perspectivv1.PlaceRef, canons []*perspectivv1.ArcLore) []*perspectivv1.HourArc {
	hours := make([]*perspectivv1.HourArc, 0, len(rows))
	for i, r := range rows {
		hours = append(hours, &perspectivv1.HourArc{Arc: arcProto(r, chain), Canon: canons[i], PerspectiveCount: r.PerspectiveCount})
	}
	return hours
}

// errBadToken answers a page token this package did not write.
var errBadToken = apiutil.InvalidArg("invalid page token")

// perspectiveRow is one perspective on an arc page; every arc page
// perspective query returns these columns.
type perspectiveRow = dbq.ListArcTalesRow

// loreRow is one lore on an arc page.
type loreRow = dbq.ListArcSubloresRow

// loadCanon reads the lore the rank job made the arc's canon, with its
// members and encounters; nil when canonID is uuid.Nil or names no lore. A
// day arc's canon is read with its hours' instead (dayCanonAndHours).
func loadCanon(ctx context.Context, q *dbq.Queries, arcID, canonID uuid.UUID) (*perspectivv1.ArcLore, error) {
	canons, err := loadCanons(ctx, q, []canonOwner{{arcID: arcID, canonID: canonID}})
	if err != nil {
		return nil, err
	}
	return canons[0], nil
}

// canonOwner is an arc and the lore the rank job made its canon, uuid.Nil
// when it has none.
type canonOwner struct {
	arcID, canonID uuid.UUID
}

// loadCanons reads the canon of each arc, with its members and encounters,
// in three statements however many arcs there are, and returns them in the
// order of owners. An arc's entry is nil when it has no canon or its canon
// names no lore. A canon is shown while the rank job points at it, even
// with no members left.
func loadCanons(ctx context.Context, q *dbq.Queries, owners []canonOwner) ([]*perspectivv1.ArcLore, error) {
	out := make([]*perspectivv1.ArcLore, len(owners))
	var ids []uuid.UUID
	for _, o := range owners {
		if o.canonID != uuid.Nil {
			ids = append(ids, o.canonID)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.ListLoresByID(ctx, ids)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read canon lores: %w", err))
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
	lores, err := withMembers(ctx, q, shown)
	if err != nil {
		return nil, err
	}
	for j, l := range lores {
		out[at[j]] = l
	}
	return out, nil
}

// listSublores pages every lore but the canon that has a member, highest
// rank first. The cursor is the last lore's (rank score, id).
func listSublores(ctx context.Context, q *dbq.Queries, arcID, canonID uuid.UUID, page apiutil.PageToken) ([]*perspectivv1.ArcLore, string, error) {
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
	lores, err := withMembers(ctx, q, shown)
	if err != nil {
		return nil, "", err
	}
	return lores, next, nil
}

// shownLore is a lore and the arc whose page shows it: the arc's own lores,
// or an hour's canon on its day's page.
type shownLore struct {
	arcID uuid.UUID
	row   loreRow
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

// listPerspectives pages the tales or the unlinked perspectives, oldest
// first. The cursor is the last perspective's (published_at, id).
func listPerspectives(ctx context.Context, q *dbq.Queries, arcID uuid.UUID, section perspectivv1.ArcSection, page apiutil.PageToken) ([]*perspectivv1.PerspectiveSummary, string, error) {
	params := dbq.ListArcTalesParams{ArcID: arcID, RowLimit: page.Limit + 1}
	if page.After != "" {
		atText, idText, ok := strings.Cut(page.After, "|")
		at, terr := time.Parse(time.RFC3339Nano, atText)
		id, ierr := uuid.Parse(idText)
		if !ok || terr != nil || ierr != nil {
			return nil, "", errBadToken
		}
		params.AfterAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.AfterID = uuid.NullUUID{UUID: id, Valid: true}
	}
	var rows []perspectiveRow
	if section == perspectivv1.ArcSection_ARC_SECTION_TALES {
		tales, err := q.ListArcTales(ctx, params)
		if err != nil {
			return nil, "", apiutil.Internal(fmt.Errorf("list tales: %w", err))
		}
		rows = tales
	} else {
		unlinked, err := q.ListArcUnlinked(ctx, dbq.ListArcUnlinkedParams(params))
		if err != nil {
			return nil, "", apiutil.Internal(fmt.Errorf("list unlinked perspectives: %w", err))
		}
		for _, r := range unlinked {
			rows = append(rows, perspectiveRow(r))
		}
	}
	next := ""
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		last := rows[len(rows)-1]
		next = apiutil.NextPageToken(last.PublishedAt.Time.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String())
	}
	out := make([]*perspectivv1.PerspectiveSummary, 0, len(rows))
	for _, r := range rows {
		sum, err := summary(arcID, r)
		if err != nil {
			return nil, "", apiutil.Internal(err)
		}
		out = append(out, sum)
	}
	return out, next, nil
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

// summary builds a perspective's arc page entry. A deleted or hidden
// perspective becomes a placeholder that the page renders as "[removed]": it
// keeps its id, genre, lore status, lore and publish time and its deleted or
// hidden flag, and carries no author, excerpt, part count or votes.
func summary(arcID uuid.UUID, r perspectiveRow) (*perspectivv1.PerspectiveSummary, error) {
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

// excerpt cuts an opening at the last word that fits and marks the cut with
// an ellipsis, so a row never quotes half a word.
func excerpt(opening string) string {
	if utf8.RuneCountInString(opening) <= excerptLen {
		return opening
	}
	r := []rune(opening)
	cut := excerptLen
	for i := excerptLen; i > excerptLen/2; i-- {
		if unicode.IsSpace(r[i]) {
			cut = i
			break
		}
	}
	return strings.TrimRightFunc(string(r[:cut]), func(c rune) bool { return unicode.IsSpace(c) || c == ',' || c == ';' || c == ':' }) + "…"
}

// parseID reads an id this package wrote itself; empty is uuid.Nil.
func parseID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// ListOpenCharacters lists the characters in the arc's readable stories that
// nobody holds yet, for the arc page's "Is this you?" list. A missing arc,
// or one at or under a hidden place, is NotFound.
//
// The two statements read through the pool, not in a snapshot: the arc row
// only decides NotFound and the list reads nothing from it, so a hide or a
// claim landing between them shows what a request a moment earlier or later
// would have.
func (s *Service) ListOpenCharacters(ctx context.Context, arcID uuid.UUID) ([]*perspectivv1.OpenCharacter, error) {
	q := dbq.New(s.pool)
	if _, err := loadArc(ctx, q, arcID); err != nil {
		return nil, err
	}
	rows, err := q.ListOpenCharacters(ctx, arcID)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list open characters: %w", err))
	}
	out := make([]*perspectivv1.OpenCharacter, len(rows))
	for i, r := range rows {
		out[i] = &perspectivv1.OpenCharacter{
			CharacterId: r.ID.String(), Label: r.Label, PerspectiveId: r.PerspectiveID.String(),
			Author: &perspectivv1.UserRef{UserId: r.AuthorID.String(), Username: r.Username},
		}
	}
	return out, nil
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
