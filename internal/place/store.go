package place

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place/dbq"
)

// ErrNotFound reports a place that does not exist or is hidden by
// moderation, itself or through an ancestor.
var ErrNotFound = errors.New("place not found")

// ErrDuplicateName reports a name already used under the same parent,
// compared without regard to case or whitespace runs.
var ErrDuplicateName = errors.New("a place with this name already exists there")

// uniqueViolation is the Postgres SQLSTATE for a unique index collision, and
// parentNameKey the index that enforces one name per parent.
const (
	uniqueViolation = "23505"
	parentNameKey   = "places_parent_name_key"
)

// Ref is a place as a link.
type Ref struct {
	ID   uuid.UUID
	Kind perspectivv1.PlaceKind
	Name string
}

// Place is one node of the hierarchy. ParentID is uuid.Nil for a STATE.
// Ancestors run from the STATE down to the parent.
type Place struct {
	ID        uuid.UUID
	Kind      perspectivv1.PlaceKind
	Name      string
	ParentID  uuid.UUID
	Timezone  string
	Source    perspectivv1.PlaceSource
	UserAdded bool
	Category  perspectivv1.PlaceCategory
	Ancestors []Ref
	// sortKey is the name the children list orders and pages by; set by
	// Children only.
	sortKey string
}

// Store reads the place hierarchy, writes user-added places and lists the
// arcs and tales of a place's epic.
type Store struct {
	pool *pgxpool.Pool
	q    *dbq.Queries
}

// NewStore returns a Store on pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: dbq.New(pool)}
}

func kindOf(s string) perspectivv1.PlaceKind {
	return perspectivv1.PlaceKind(perspectivv1.PlaceKind_value[s])
}

func sourceOf(s string) perspectivv1.PlaceSource {
	return perspectivv1.PlaceSource(perspectivv1.PlaceSource_value[s])
}

func placeOf(id uuid.UUID, kind, name string, parent uuid.NullUUID, tz, source string, userAdded bool) Place {
	return Place{
		ID: id, Kind: kindOf(kind), Name: name, ParentID: parent.UUID,
		Timezone: tz, Source: sourceOf(source), UserAdded: userAdded,
	}
}

// Get returns a visible place with its ancestors.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Place, error) {
	r, err := s.q.GetVisiblePlace(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Place{}, ErrNotFound
	}
	if err != nil {
		return Place{}, fmt.Errorf("get place: %w", err)
	}
	ps := []Place{placeOf(r.ID, r.Kind, r.Name, r.ParentID, r.Timezone, r.Source, r.UserAdded)}
	if err := s.withAncestors(ctx, ps); err != nil {
		return Place{}, err
	}
	return ps[0], nil
}

// withAncestors fills Ancestors on every place in ps with one query.
func (s *Store) withAncestors(ctx context.Context, ps []Place) error {
	ids := make([]uuid.UUID, 0, len(ps))
	for _, p := range ps {
		if p.ParentID != uuid.Nil {
			ids = append(ids, p.ID)
		}
	}
	chains := make(map[uuid.UUID][]Ref, len(ids))
	if len(ids) > 0 {
		rows, err := s.q.ListAncestors(ctx, ids)
		if err != nil {
			return fmt.Errorf("list ancestors: %w", err)
		}
		for _, r := range rows {
			chains[r.PlaceID] = append(chains[r.PlaceID], Ref{ID: r.AncestorID, Kind: kindOf(r.Kind), Name: r.Name})
		}
	}
	for i := range ps {
		ps[i].Ancestors = chains[ps[i].ID]
		if ps[i].Ancestors == nil {
			ps[i].Ancestors = []Ref{}
		}
	}
	return nil
}

// SearchQuery is a typeahead request. Text is already normalized (trimmed,
// lowercased, whitespace runs collapsed). ParentID uuid.Nil and Kind
// UNSPECIFIED mean no filter. Candidates bounds how many ranked matches are
// checked for a hidden ancestor before Limit survivors are returned.
type SearchQuery struct {
	Text       string
	ParentID   uuid.UUID
	Kind       perspectivv1.PlaceKind
	Limit      int32
	Candidates int32
}

// Search returns visible places whose name contains q.Text, ranked as the
// SearchPlaces query documents, each with its ancestors.
func (s *Store) Search(ctx context.Context, q SearchQuery) ([]Place, error) {
	lit := escapeLike(q.Text)
	params := dbq.SearchPlacesParams{
		Exact:          q.Text,
		Prefix:         lit + "%",
		Contains:       "%" + lit + "%",
		ResultLimit:    q.Limit,
		CandidateLimit: q.Candidates,
	}
	if q.ParentID != uuid.Nil {
		params.ParentID = uuid.NullUUID{UUID: q.ParentID, Valid: true}
	}
	if q.Kind != perspectivv1.PlaceKind_PLACE_KIND_UNSPECIFIED {
		params.Kind = pgtype.Text{String: q.Kind.String(), Valid: true}
	}
	rows, err := s.q.SearchPlaces(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("search places: %w", err)
	}
	ps := make([]Place, len(rows))
	for i, r := range rows {
		ps[i] = placeOf(r.ID, r.Kind, r.Name, r.ParentID, r.Timezone, r.Source, r.UserAdded)
	}
	if err := s.withAncestors(ctx, ps); err != nil {
		return nil, err
	}
	return ps, nil
}

// escapeLike makes s match literally inside a LIKE pattern, whose default
// escape character is the backslash.
func escapeLike(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\\' || r == '%' || r == '_' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}

// ChildCursor is the keyset of the last place on a drill-down page: its
// lowercased name and id.
type ChildCursor struct {
	Name string
	ID   uuid.UUID
}

// Children returns up to limit visible children of parent ordered by name,
// or the states when parent is uuid.Nil, after the cursor when one is given.
// The parent's own visibility is the caller's check. Children carry no
// ancestors.
func (s *Store) Children(ctx context.Context, parent uuid.UUID, category perspectivv1.PlaceCategory, after *ChildCursor, limit int32) ([]Place, error) {
	var afterID uuid.NullUUID
	var afterName pgtype.Text
	if after != nil {
		afterID = uuid.NullUUID{UUID: after.ID, Valid: true}
		afterName = pgtype.Text{String: after.Name, Valid: true}
	}
	var ps []Place
	if parent == uuid.Nil {
		rows, err := s.q.ListStates(ctx, dbq.ListStatesParams{AfterID: afterID, AfterName: afterName, Lim: limit})
		if err != nil {
			return nil, fmt.Errorf("list states: %w", err)
		}
		for _, r := range rows {
			ps = append(ps, placeOf(r.ID, r.Kind, r.Name, r.ParentID, r.Timezone, r.Source, r.UserAdded))
		}
		return ps, nil
	}
	var cat pgtype.Text
	if category != perspectivv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED {
		cat = pgtype.Text{String: category.String(), Valid: true}
	}
	rows, err := s.q.ListChildren(ctx, dbq.ListChildrenParams{ParentID: parent, Category: cat, AfterID: afterID, AfterName: afterName, Lim: limit})
	if err != nil {
		return nil, fmt.Errorf("list children: %w", err)
	}
	for _, r := range rows {
		p := placeOf(r.ID, r.Kind, r.Name, r.ParentID, r.Timezone, r.Source, r.UserAdded)
		p.sortKey = r.SortKey
		p.Category = categoryOf(r.Category)
		ps = append(ps, p)
	}
	return ps, nil
}

// StoriedChildren lists up to limit children of parent (the states when
// parent is uuid.Nil) with a readable story at or below them, most recently
// started arc first.
func (s *Store) StoriedChildren(ctx context.Context, parent uuid.UUID, limit int32) ([]Place, error) {
	var p uuid.NullUUID
	if parent != uuid.Nil {
		p = uuid.NullUUID{UUID: parent, Valid: true}
	}
	rows, err := s.q.ListStoriedChildren(ctx, dbq.ListStoriedChildrenParams{ParentID: p, Lim: limit})
	if err != nil {
		return nil, fmt.Errorf("list storied children: %w", err)
	}
	ps := make([]Place, 0, len(rows))
	for _, r := range rows {
		p := placeOf(r.ID, r.Kind, r.Name, r.ParentID, r.Timezone, r.Source, r.UserAdded)
		p.Category = categoryOf(r.Category)
		ps = append(ps, p)
	}
	return ps, nil
}

// categoryOf reads a stored category; "" or an unknown name is UNSPECIFIED,
// which the places_category_check constraint keeps from happening.
func categoryOf(s string) perspectivv1.PlaceCategory {
	return perspectivv1.PlaceCategory(perspectivv1.PlaceCategory_value[s])
}

// NewPlace is a user-added place. The parent's kind and the place's kind
// are the service's rules; the store enforces that the parent exists and is
// not hidden, and that the name is unique under it.
type NewPlace struct {
	Name      string
	Kind      perspectivv1.PlaceKind
	ParentID  uuid.UUID
	CreatedBy uuid.UUID
}

// Insert adds a user place under a visible parent, inheriting the parent's
// time zone. The parent row is share-locked for the transaction so a
// concurrent hide cannot slip between the check and the insert.
func (s *Store) Insert(ctx context.Context, in NewPlace) (Place, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Place{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	parent, err := q.LockVisibleParent(ctx, in.ParentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Place{}, ErrNotFound
	}
	if err != nil {
		return Place{}, fmt.Errorf("lock parent: %w", err)
	}
	r, err := q.InsertUserPlace(ctx, dbq.InsertUserPlaceParams{
		Kind: in.Kind.String(), Name: in.Name, ParentID: in.ParentID,
		Timezone: parent.Timezone, CreatedBy: in.CreatedBy,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == parentNameKey {
			return Place{}, ErrDuplicateName
		}
		return Place{}, fmt.Errorf("insert place: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Place{}, fmt.Errorf("commit: %w", err)
	}
	ps := []Place{placeOf(r.ID, r.Kind, r.Name, r.ParentID, r.Timezone, r.Source, r.UserAdded)}
	if err := s.withAncestors(ctx, ps); err != nil {
		return Place{}, err
	}
	return ps[0], nil
}

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

// EpicArcsQuery lists the arcs at PlaceID and its visible descendants.
// Genre UNSPECIFIED means every genre.
type EpicArcsQuery struct {
	PlaceID uuid.UUID
	Order   ArcOrder
	Genre   perspectivv1.Genre
	After   *ArcCursor
	Limit   int32
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

func hourOf(h pgtype.Int2) *int32 {
	if !h.Valid {
		return nil
	}
	v := int32(h.Int16)
	return &v
}

// EpicArcs lists arcs at the place and its visible descendants. Each arc's
// canon is its own; ancestors only aggregate the listing.
func (s *Store) EpicArcs(ctx context.Context, q EpicArcsQuery) ([]EpicArc, error) {
	if q.Order == OrderNewest {
		return s.epicArcsNewest(ctx, q)
	}
	p := dbq.EpicArcsRankedParams{PlaceID: q.PlaceID, Genre: genreParam(q.Genre), Lim: q.Limit}
	if c := q.After; c != nil {
		p.AfterID = uuid.NullUUID{UUID: c.ID, Valid: true}
		p.AfterHasCanon = pgtype.Bool{Bool: c.HasCanon, Valid: true}
		p.AfterScore = pgtype.Float8{Float64: c.Score, Valid: true}
		p.AfterMembers = pgtype.Int4{Int32: c.Members, Valid: true}
		p.AfterCreated = pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}
	}
	rows, err := s.q.EpicArcsRanked(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("epic ranked: %w", err)
	}
	out := make([]EpicArc, len(rows))
	for i, r := range rows {
		out[i] = EpicArc{
			ID: r.ID, Title: r.Title, Place: Ref{ID: r.PlaceID, Kind: kindOf(r.PlaceKind), Name: r.PlaceName},
			LocalDate: r.LocalDate.Time, Hour: hourOf(r.Hour), CanonLoreID: r.CanonLoreID.UUID,
			CanonScore: r.CanonScore, MemberCount: r.MemberCount, PerspectiveCount: r.PerspectiveCount,
			CreatedAt: r.CreatedAt.Time,
		}
	}
	return out, nil
}

func (s *Store) epicArcsNewest(ctx context.Context, q EpicArcsQuery) ([]EpicArc, error) {
	p := dbq.EpicArcsNewestParams{PlaceID: q.PlaceID, Genre: genreParam(q.Genre), Lim: q.Limit}
	if c := q.After; c != nil {
		p.AfterID = uuid.NullUUID{UUID: c.ID, Valid: true}
		p.AfterCreated = pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}
	}
	rows, err := s.q.EpicArcsNewest(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("epic newest: %w", err)
	}
	out := make([]EpicArc, len(rows))
	for i, r := range rows {
		out[i] = EpicArc{
			ID: r.ID, Title: r.Title, Place: Ref{ID: r.PlaceID, Kind: kindOf(r.PlaceKind), Name: r.PlaceName},
			LocalDate: r.LocalDate.Time, Hour: hourOf(r.Hour), CanonLoreID: r.CanonLoreID.UUID,
			CanonScore: r.CanonScore, MemberCount: r.MemberCount, PerspectiveCount: r.PerspectiveCount,
			CreatedAt: r.CreatedAt.Time,
		}
	}
	return out, nil
}

// TaleCursor is the keyset of the last tale on a TALES page.
type TaleCursor struct {
	PublishedAt time.Time
	ID          uuid.UUID
}

// TalesQuery lists the tales at PlaceID and its visible descendants.
type TalesQuery struct {
	PlaceID uuid.UUID
	Genre   perspectivv1.Genre
	After   *TaleCursor
	Limit   int32
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

// EpicTales lists SOLO and REMOVED perspectives, newest published first.
func (s *Store) EpicTales(ctx context.Context, q TalesQuery) ([]EpicTale, error) {
	p := dbq.EpicTalesParams{PlaceID: q.PlaceID, Genre: genreParam(q.Genre), Lim: q.Limit}
	if c := q.After; c != nil {
		p.AfterID = uuid.NullUUID{UUID: c.ID, Valid: true}
		p.AfterPublished = pgtype.Timestamptz{Time: c.PublishedAt, Valid: true}
	}
	rows, err := s.q.EpicTales(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("epic tales: %w", err)
	}
	out := make([]EpicTale, len(rows))
	for i, r := range rows {
		out[i] = EpicTale{
			PerspectiveID: r.ID, ArcID: r.ArcID.UUID, ArcTitle: r.ArcTitle,
			Place:     Ref{ID: r.PlaceID, Kind: kindOf(r.PlaceKind), Name: r.PlaceName},
			LocalDate: r.LocalDate.Time, Hour: hourOf(r.Hour),
			AuthorID: r.AuthorID, AuthorUsername: r.Username,
			Genre:       perspectivv1.Genre(perspectivv1.Genre_value[r.Genre.String]),
			LoreStatus:  perspectivv1.LoreStatus(perspectivv1.LoreStatus_value[r.LoreStatus.String]),
			PublishedAt: r.PublishedAt.Time, Excerpt: r.Excerpt,
		}
	}
	return out, nil
}
