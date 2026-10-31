package place

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place/dbq"
)

// ArcOrder selects an epic arc listing.
type ArcOrder int

const (
	// OrderRanked lists arcs with a canon first, then by stored canon score,
	// member count and newest.
	OrderRanked ArcOrder = iota
	// OrderNewest lists arcs newest first.
	OrderNewest
)

// ArcCursor is the keyset of the last arc on an epic page. OrderNewest reads
// only CreatedAt and ID.
type ArcCursor struct {
	HasCanon  bool
	Score     float64
	Members   int32
	CreatedAt time.Time
	ID        uuid.UUID
}

// EpicArcsQuery lists the arcs at PlaceID and its visible descendants, or
// at every visible place when PlaceID is uuid.Nil. Genre UNSPECIFIED means
// every genre; Category UNSPECIFIED means every place, and any other value
// only the arcs whose own place has it.
type EpicArcsQuery struct {
	PlaceID  uuid.UUID
	Order    ArcOrder
	Genre    perspectivv1.Genre
	Category perspectivv1.PlaceCategory
	After    *ArcCursor
	Limit    int32
}

// EpicArc is one arc in an epic listing.
type EpicArc struct {
	ID               uuid.UUID
	Title            string
	Place            Ref
	LocalDate        time.Time
	Hour             *int32
	CanonLoreID      uuid.UUID
	CanonScore       float64
	MemberCount      int32
	PerspectiveCount int32
	CreatedAt        time.Time
	// City is the name of the city the arc is in, "" when there is none.
	City string
	// Excerpt opens the canon's first story, or the first story without a canon.
	Excerpt string
	// CanonLoreName names the canon lore; "" without a canon or a name.
	CanonLoreName string
	// LatestPublishedAt is when the newest readable story was published.
	LatestPublishedAt time.Time
	// NewThisWeek says a readable story was published in the last
	// newStoryWindow; set by the service.
	NewThisWeek bool
}

// Cursor returns the keyset that resumes a listing after a.
func (a EpicArc) Cursor() ArcCursor {
	return ArcCursor{
		HasCanon: a.CanonLoreID != uuid.Nil, Score: a.CanonScore, Members: a.MemberCount,
		CreatedAt: a.CreatedAt, ID: a.ID,
	}
}

func genreParam(g perspectivv1.Genre) pgtype.Text {
	if g == perspectivv1.Genre_GENRE_UNSPECIFIED {
		return pgtype.Text{}
	}
	return pgtype.Text{String: g.String(), Valid: true}
}

// categoryParam is the query argument for a category filter: NULL for
// UNSPECIFIED, which lists every place.
func categoryParam(c perspectivv1.PlaceCategory) pgtype.Text {
	if c == perspectivv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED {
		return pgtype.Text{}
	}
	return pgtype.Text{String: c.String(), Valid: true}
}

func hourOf(h pgtype.Int2) *int32 {
	if !h.Valid {
		return nil
	}
	v := int32(h.Int16)
	return &v
}

// EpicArcs lists arcs at the place and its visible descendants, or at every
// visible place when PlaceID is uuid.Nil. Each arc's canon is its own;
// ancestors only aggregate the listing. A missing or hidden place lists
// nothing.
func (s *Store) EpicArcs(ctx context.Context, q EpicArcsQuery) ([]EpicArc, error) {
	_, arcs, err := s.EpicArcsAt(ctx, q)
	if errors.Is(err, ErrNotFound) {
		return []EpicArc{}, nil
	}
	return arcs, err
}

// EpicArcsAt is EpicArcs that also returns the place, read in the same
// statement as its arcs, so a place tab needs no place read of its own:
// ErrNotFound when the place is missing or hidden. With no place it lists
// every visible place and returns the zero Place.
func (s *Store) EpicArcsAt(ctx context.Context, q EpicArcsQuery) (Place, []EpicArc, error) {
	if q.PlaceID == uuid.Nil {
		arcs, err := s.everywhereArcs(ctx, q)
		return Place{}, arcs, err
	}
	var rows []dbq.EpicArcsRankedRow
	if q.Order == OrderNewest {
		newest, err := s.q.EpicArcsNewest(ctx, newestParams(q))
		if err != nil {
			return Place{}, nil, fmt.Errorf("epic newest: %w", err)
		}
		for _, r := range newest {
			rows = append(rows, dbq.EpicArcsRankedRow(r))
		}
	} else {
		var err error
		if rows, err = s.q.EpicArcsRanked(ctx, rankedParams(q)); err != nil {
			return Place{}, nil, fmt.Errorf("epic ranked: %w", err)
		}
	}
	// No row: the place is missing or hidden. One row with no arc: the
	// place lists nothing.
	if len(rows) == 0 {
		return Place{}, nil, ErrNotFound
	}
	r0 := rows[0]
	p, err := visiblePlace(dbq.GetVisiblePlaceRow{
		ID: r0.HereID, Kind: r0.HereKind, Name: r0.HereName, ParentID: r0.HereParentID,
		Timezone: r0.HereTimezone, Source: r0.HereSource, UserAdded: r0.HereUserAdded, Category: r0.HereCategory,
		AncestorIds: r0.HereAncestorIds, AncestorKinds: r0.HereAncestorKinds, AncestorNames: r0.HereAncestorNames,
	})
	if err != nil {
		return Place{}, nil, err
	}
	listed := make([]dbq.EverywhereArcsRankedRow, 0, len(rows))
	for _, r := range rows {
		if r.ID.Valid {
			listed = append(listed, listedArc(r))
		}
	}
	return p, epicArcsFrom(listed), nil
}

// everywhereArcs lists the arcs of every visible place: the front page.
func (s *Store) everywhereArcs(ctx context.Context, q EpicArcsQuery) ([]EpicArc, error) {
	if q.Order == OrderNewest {
		p := newestParams(q)
		all, err := s.q.EverywhereArcsNewest(ctx, dbq.EverywhereArcsNewestParams{
			Genre: p.Genre, Category: p.Category, AfterID: p.AfterID, AfterCreated: p.AfterCreated, Lim: p.Lim,
		})
		if err != nil {
			return nil, fmt.Errorf("everywhere newest: %w", err)
		}
		rows := make([]dbq.EverywhereArcsRankedRow, len(all))
		for i, r := range all {
			rows[i] = dbq.EverywhereArcsRankedRow(r)
		}
		return epicArcsFrom(rows), nil
	}
	p := rankedParams(q)
	rows, err := s.q.EverywhereArcsRanked(ctx, dbq.EverywhereArcsRankedParams{
		Genre: p.Genre, Category: p.Category, AfterID: p.AfterID, AfterHasCanon: p.AfterHasCanon, AfterScore: p.AfterScore,
		AfterMembers: p.AfterMembers, AfterCreated: p.AfterCreated, Lim: p.Lim,
	})
	if err != nil {
		return nil, fmt.Errorf("epic ranked: %w", err)
	}
	return epicArcsFrom(rows), nil
}

func rankedParams(q EpicArcsQuery) dbq.EpicArcsRankedParams {
	p := dbq.EpicArcsRankedParams{PlaceID: q.PlaceID, Genre: genreParam(q.Genre), Category: categoryParam(q.Category), Lim: q.Limit}
	if c := q.After; c != nil {
		p.AfterID = uuid.NullUUID{UUID: c.ID, Valid: true}
		p.AfterHasCanon = pgtype.Bool{Bool: c.HasCanon, Valid: true}
		p.AfterScore = pgtype.Float8{Float64: c.Score, Valid: true}
		p.AfterMembers = pgtype.Int4{Int32: c.Members, Valid: true}
		p.AfterCreated = pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}
	}
	return p
}

func newestParams(q EpicArcsQuery) dbq.EpicArcsNewestParams {
	p := dbq.EpicArcsNewestParams{PlaceID: q.PlaceID, Genre: genreParam(q.Genre), Category: categoryParam(q.Category), Lim: q.Limit}
	if c := q.After; c != nil {
		p.AfterID = uuid.NullUUID{UUID: c.ID, Valid: true}
		p.AfterCreated = pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}
	}
	return p
}

// listedArc is a place listing's arc columns, which the LEFT JOIN on the
// place makes nullable, as the front page's listing reads them.
func listedArc(r dbq.EpicArcsRankedRow) dbq.EverywhereArcsRankedRow {
	return dbq.EverywhereArcsRankedRow{
		ID: r.ID.UUID, Title: r.Title.String, PlaceID: r.PlaceID.UUID, PlaceKind: r.PlaceKind.String,
		PlaceName: r.PlaceName.String, LocalDate: r.LocalDate, Hour: r.Hour, CanonLoreID: r.CanonLoreID,
		CanonScore: r.CanonScore.Float64, MemberCount: r.MemberCount.Int32, CreatedAt: r.CreatedAt,
		PerspectiveCount: r.PerspectiveCount.Int32, CityName: r.CityName.String, Excerpt: r.Excerpt.String,
		CanonLoreName: r.CanonLoreName.String, LatestPublishedAt: r.LatestPublishedAt,
	}
}

// epicArcsFrom maps listing rows; the four arc listings select the same
// columns, so their rows convert to one type.
func epicArcsFrom(rows []dbq.EverywhereArcsRankedRow) []EpicArc {
	out := make([]EpicArc, len(rows))
	for i, r := range rows {
		out[i] = EpicArc{
			ID: r.ID, Title: r.Title, Place: Ref{ID: r.PlaceID, Kind: kindOf(r.PlaceKind), Name: r.PlaceName},
			LocalDate: r.LocalDate.Time, Hour: hourOf(r.Hour), CanonLoreID: r.CanonLoreID.UUID,
			CanonScore: r.CanonScore, MemberCount: r.MemberCount, PerspectiveCount: r.PerspectiveCount,
			CreatedAt: r.CreatedAt.Time, City: r.CityName, Excerpt: clipExcerpt(r.Excerpt),
			CanonLoreName: r.CanonLoreName, LatestPublishedAt: r.LatestPublishedAt.Time,
		}
	}
	return out
}

// TaleCursor is the keyset of the last tale on a TALES page.
type TaleCursor struct {
	PublishedAt time.Time
	ID          uuid.UUID
}

// TalesQuery lists the tales at PlaceID and its visible descendants;
// Category UNSPECIFIED means every place, any other value only the tales in
// arcs whose own place has it.
type TalesQuery struct {
	PlaceID  uuid.UUID
	Genre    perspectivv1.Genre
	Category perspectivv1.PlaceCategory
	After    *TaleCursor
	Limit    int32
}

// EpicTale is one SOLO or REMOVED perspective in a TALES listing.
type EpicTale struct {
	PerspectiveID  uuid.UUID
	ArcID          uuid.UUID
	ArcTitle       string
	Place          Ref
	LocalDate      time.Time
	Hour           *int32
	AuthorID       uuid.UUID
	AuthorUsername string
	Genre          perspectivv1.Genre
	LoreStatus     perspectivv1.LoreStatus
	PublishedAt    time.Time
	Excerpt        string
}

// EpicTales lists SOLO and REMOVED perspectives, newest published first,
// and returns the place, read in the same statement: ErrNotFound when it is
// missing or hidden.
func (s *Store) EpicTales(ctx context.Context, q TalesQuery) (Place, []EpicTale, error) {
	p := dbq.EpicTalesParams{PlaceID: q.PlaceID, Genre: genreParam(q.Genre), Category: categoryParam(q.Category), Lim: q.Limit}
	if c := q.After; c != nil {
		p.AfterID = uuid.NullUUID{UUID: c.ID, Valid: true}
		p.AfterPublished = pgtype.Timestamptz{Time: c.PublishedAt, Valid: true}
	}
	rows, err := s.q.EpicTales(ctx, p)
	if err != nil {
		return Place{}, nil, fmt.Errorf("epic tales: %w", err)
	}
	// No row: the place is missing or hidden. One row with no tale: the
	// place lists nothing.
	if len(rows) == 0 {
		return Place{}, nil, ErrNotFound
	}
	r0 := rows[0]
	here, err := visiblePlace(dbq.GetVisiblePlaceRow{
		ID: r0.HereID, Kind: r0.HereKind, Name: r0.HereName, ParentID: r0.HereParentID,
		Timezone: r0.HereTimezone, Source: r0.HereSource, UserAdded: r0.HereUserAdded, Category: r0.HereCategory,
		AncestorIds: r0.HereAncestorIds, AncestorKinds: r0.HereAncestorKinds, AncestorNames: r0.HereAncestorNames,
	})
	if err != nil {
		return Place{}, nil, err
	}
	out := make([]EpicTale, 0, len(rows))
	for _, r := range rows {
		if !r.ID.Valid {
			continue
		}
		out = append(out, EpicTale{
			PerspectiveID: r.ID.UUID, ArcID: r.ArcID.UUID, ArcTitle: r.ArcTitle.String,
			Place:     Ref{ID: r.PlaceID.UUID, Kind: kindOf(r.PlaceKind.String), Name: r.PlaceName.String},
			LocalDate: r.LocalDate.Time, Hour: hourOf(r.Hour),
			AuthorID: r.AuthorID.UUID, AuthorUsername: r.Username.String,
			Genre:       perspectivv1.Genre(perspectivv1.Genre_value[r.Genre.String]),
			LoreStatus:  perspectivv1.LoreStatus(perspectivv1.LoreStatus_value[r.LoreStatus.String]),
			PublishedAt: r.PublishedAt.Time, Excerpt: clipExcerpt(r.Excerpt.String),
		})
	}
	return here, out, nil
}
