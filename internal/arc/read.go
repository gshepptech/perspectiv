package arc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
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
		if resp.Canon, err = loadCanon(ctx, q, arcID, canonID); err != nil {
			return err
		}
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
			if resp.Hours, resp.HoursNextPageToken, err = listHours(ctx, q, arcID, arc.GetPlaceChain(), first); err != nil {
				return err
			}
		}
		if viewer != uuid.Nil {
			resp.Viewer, err = viewerIn(ctx, q, arcID, viewer)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ListArcSection serves the next page of one list on an arc page, from a
// token GetArc or an earlier call returned. An hour arc has no hours list,
// so asking for it there returns an empty page.
func (s *Service) ListArcSection(ctx context.Context, arcID uuid.UUID, section perspectivv1.ArcSection, pageToken string, pageSize int32) (*perspectivv1.ListArcSectionResponse, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return nil, err
	}
	resp := &perspectivv1.ListArcSectionResponse{}
	err = s.readTx(ctx, func(q *dbq.Queries) error {
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
		return err
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// readTx runs fn in one read-only REPEATABLE READ snapshot.
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

// listHours pages a day arc's hour children in hour order, each with its
// canon and the number of its perspectives that are neither deleted nor
// hidden. The cursor is the last hour listed.
func listHours(ctx context.Context, q *dbq.Queries, dayID uuid.UUID, chain []*perspectivv1.PlaceRef, page apiutil.PageToken) ([]*perspectivv1.HourArc, string, error) {
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
	hours := make([]*perspectivv1.HourArc, 0, len(rows))
	for _, r := range rows {
		canon, err := loadCanon(ctx, q, r.ID, r.CanonLoreID.UUID)
		if err != nil {
			return nil, "", err
		}
		visible, err := q.CountVisiblePerspectives(ctx, uuid.NullUUID{UUID: r.ID, Valid: true})
		if err != nil {
			return nil, "", apiutil.Internal(fmt.Errorf("count perspectives: %w", err))
		}
		hours = append(hours, &perspectivv1.HourArc{Arc: arcProto(r, chain), Canon: canon, PerspectiveCount: visible})
	}
	return hours, next, nil
}

// errBadToken answers a page token this package did not write.
var errBadToken = apiutil.InvalidArg("invalid page token")

// perspectiveRow is one perspective on an arc page; every arc page
// perspective query returns these columns.
type perspectiveRow = dbq.ListArcTalesRow

// loreRow is one lore on an arc page.
type loreRow = dbq.ListArcSubloresRow

// loadCanon reads the lore the rank job made canon, with its members and
// encounters; nil when canonID is uuid.Nil or names no lore. The canon is
// shown while the rank job points at it, even with no members left.
func loadCanon(ctx context.Context, q *dbq.Queries, arcID, canonID uuid.UUID) (*perspectivv1.ArcLore, error) {
	if canonID == uuid.Nil {
		return nil, nil
	}
	row, err := q.GetArcLore(ctx, canonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read canon lore: %w", err))
	}
	lores, err := withMembers(ctx, q, arcID, []loreRow{loreRow(row)})
	if err != nil {
		return nil, err
	}
	return lores[0], nil
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
	lores, err := withMembers(ctx, q, arcID, rows)
	if err != nil {
		return nil, "", err
	}
	return lores, next, nil
}

// withMembers builds the wire lores for rows, reading their member
// perspectives and encounters in one query each.
func withMembers(ctx context.Context, q *dbq.Queries, arcID uuid.UUID, rows []loreRow) ([]*perspectivv1.ArcLore, error) {
	out := make([]*perspectivv1.ArcLore, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	memberRows, err := q.ListLorePerspectives(ctx, ids)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list lore members: %w", err))
	}
	members := map[uuid.UUID][]*perspectivv1.PerspectiveSummary{}
	for _, m := range memberRows {
		sum, err := summary(arcID, memberRow(m))
		if err != nil {
			return nil, apiutil.Internal(err)
		}
		members[m.MemberOf] = append(members[m.MemberOf], sum)
	}
	encounters, err := q.ListLoreEncounters(ctx, ids)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list encounters: %w", err))
	}
	for _, r := range rows {
		al, err := arcLore(arcID, r, members[r.ID], encounters)
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
