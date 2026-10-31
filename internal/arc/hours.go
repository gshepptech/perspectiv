package arc

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc/dbq"
)

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
	canons, _, err := loadCanons(ctx, q, hourOwners(nil, rows), nil)
	if err != nil {
		return nil, "", err
	}
	return hourArcs(rows, chain, canons), next, nil
}

// dayCanonAndHours fills a day arc page's canon, its first page of hours
// and its sublores (a page from pageSublores). The day's canon and sublores
// are built in the same batch as its hours' canons, so the day costs no
// statements beyond listHours'.
func dayCanonAndHours(ctx context.Context, q *dbq.Queries, resp *perspectivv1.GetArcResponse, canonID uuid.UUID, sublores []shownLore, page apiutil.PageToken) error {
	day := resp.GetArc()
	dayID := parseID(day.GetId())
	rows, next, err := pageHours(ctx, q, dayID, page)
	if err != nil {
		return err
	}
	canons, subs, err := loadCanons(ctx, q, hourOwners([]canonOwner{{arcID: dayID, canonID: canonID}}, rows), sublores)
	if err != nil {
		return err
	}
	resp.Canon = canons[0]
	resp.Sublores = subs
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
