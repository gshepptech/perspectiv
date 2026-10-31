package arc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc/dbq"
)

// excerptLen is how many characters of a perspective's first part an arc page
// summary carries; a longer opening is cut there and ends in "…".
const excerptLen = 240

// perspectiveRow is one perspective on an arc page; every arc page
// perspective query returns these columns.
type perspectiveRow = dbq.ListArcTalesRow

// sectionPerspectives serves a ListArcSection page of the tales or the
// unlinked perspectives. The list query reads the arc's visibility itself,
// so an arc at or under a hidden place lists nothing; the arc row is read
// only when the page comes back empty, or the token is bad, to answer
// NotFound for a missing or hidden arc as before the list.
func sectionPerspectives(ctx context.Context, q *dbq.Queries, arcID uuid.UUID, section perspectivv1.ArcSection, page apiutil.PageToken) ([]*perspectivv1.PerspectiveSummary, string, error) {
	sums, next, err := listPerspectives(ctx, q, arcID, section, page)
	if (err == nil && len(sums) == 0) || errors.Is(err, errBadToken) {
		if _, aerr := loadArc(ctx, q, arcID); aerr != nil {
			return nil, "", aerr
		}
	}
	return sums, next, err
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
	return perspectivePage(arcID, rows, page.Limit)
}

// firstPerspectives fills an arc page's first page of tales and of
// unlinked perspectives, both read in one statement.
func firstPerspectives(ctx context.Context, q *dbq.Queries, resp *perspectivv1.GetArcResponse, page apiutil.PageToken) error {
	arcID := parseID(resp.GetArc().GetId())
	rows, err := q.ListArcFirstPerspectives(ctx, dbq.ListArcFirstPerspectivesParams{ArcID: arcID, RowLimit: page.Limit + 1})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("list tales and unlinked perspectives: %w", err))
	}
	var tales, unlinked []perspectiveRow
	for _, r := range rows {
		row := perspectiveRow{
			ID: r.ID, AuthorID: r.AuthorID, AuthorUsername: r.AuthorUsername, Genre: r.Genre, LoreStatus: r.LoreStatus,
			MemberOf: r.MemberOf, PublishedAt: r.PublishedAt, DeletedAt: r.DeletedAt, HiddenAt: r.HiddenAt,
			Opening: r.Opening, PartCount: r.PartCount, Upvotes: r.Upvotes,
		}
		if r.Tale {
			tales = append(tales, row)
		} else {
			unlinked = append(unlinked, row)
		}
	}
	if resp.Tales, resp.TalesNextPageToken, err = perspectivePage(arcID, tales, page.Limit); err != nil {
		return err
	}
	resp.Unlinked, resp.UnlinkedNextPageToken, err = perspectivePage(arcID, unlinked, page.Limit)
	return err
}

// perspectivePage builds a page of summaries from up to limit+1 rows, with
// the next page token when there was one more row.
func perspectivePage(arcID uuid.UUID, rows []perspectiveRow, limit int32) ([]*perspectivv1.PerspectiveSummary, string, error) {
	next := ""
	if len(rows) > int(limit) {
		rows = rows[:limit]
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
