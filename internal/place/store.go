package place

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
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
	// Stories counts readable stories at the place and below, up to 100;
	// set by Search only.
	Stories int32
	// NewThisWeek counts readable stories published at the place and below
	// in the last week, up to 100; set by ListFollowing only.
	NewThisWeek int32
	Ancestors   []Ref
	// sortKey is the name the children list orders and pages by; set by
	// Children only.
	sortKey string
	// namePrefix says the search text starts the name (or is it); set by
	// Search only.
	namePrefix bool
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

// Get returns a visible place with its ancestors, in one query.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Place, error) {
	r, err := s.q.GetVisiblePlace(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Place{}, ErrNotFound
	}
	if err != nil {
		return Place{}, fmt.Errorf("get place: %w", err)
	}
	return visiblePlace(r)
}

// visiblePlace builds a place from GetVisiblePlace's columns, which the
// place epic listings also carry (see EpicArcsAt).
func visiblePlace(r dbq.GetVisiblePlaceRow) (Place, error) {
	if len(r.AncestorKinds) != len(r.AncestorIds) || len(r.AncestorNames) != len(r.AncestorIds) {
		return Place{}, fmt.Errorf("get place: ancestor arrays differ in length (%d ids, %d kinds, %d names)",
			len(r.AncestorIds), len(r.AncestorKinds), len(r.AncestorNames))
	}
	p := placeOf(r.ID, r.Kind, r.Name, r.ParentID, r.Timezone, r.Source, r.UserAdded)
	p.Category = categoryOf(r.Category)
	p.Ancestors = make([]Ref, len(r.AncestorIds))
	for i, id := range r.AncestorIds {
		p.Ancestors[i] = Ref{ID: id, Kind: kindOf(r.AncestorKinds[i]), Name: r.AncestorNames[i]}
	}
	return p, nil
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
// lowercased, whitespace runs collapsed). ParentIDs restricts matches to
// the descendants of those places, ranked by the first of them each match
// is under; nil and Kind UNSPECIFIED mean no filter. Candidates bounds how
// many ranked matches are checked for a hidden ancestor before Limit
// survivors are returned.
type SearchQuery struct {
	Text       string
	ParentIDs  []uuid.UUID
	Kind       perspectivv1.PlaceKind
	Limit      int32
	Candidates int32
}

// minSubstringRunes is the shortest search matched anywhere in a name.
const minSubstringRunes = 3

// Search returns visible places whose name contains q.Text (or, under
// minSubstringRunes characters, starts with it), ranked as the
// SearchPlaces query documents. They carry no ancestors: the caller merges
// several searches and fills them once with withAncestors.
func (s *Store) Search(ctx context.Context, q SearchQuery) ([]Place, error) {
	lit := escapeLike(q.Text)
	contains := "%" + lit + "%"
	if utf8.RuneCountInString(q.Text) < minSubstringRunes {
		// The trigram index needs three characters; shorter input is matched
		// as a name prefix, which places_name_rank_idx and
		// places_name_locality_idx serve, instead of scanning every place for
		// the substring.
		contains = lit + "%"
	}
	prefix := lit + "%"
	params := dbq.SearchPlacesParams{
		Exact:    q.Text,
		Prefix:   prefix,
		Contains: contains,
		// Read a kind at a time only when that keeps the ranking: there is
		// no substring step, and every match ranks under its parent alike.
		BroadFirst:     contains == prefix && len(q.ParentIDs) <= 1,
		ParentIds:      q.ParentIDs,
		ResultLimit:    q.Limit,
		CandidateLimit: q.Candidates,
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
		ps[i].Category = categoryOf(r.Category)
		ps[i].Stories = r.Stories
		ps[i].namePrefix = r.IsPrefix
	}
	return ps, nil
}

// maxLocalityRows bounds the cities and towns FindLocalities reads for one
// search, across every name it asks about.
const maxLocalityRows = 100

// Locality is a city or town a search names: its id and lowercased name.
type Locality struct {
	ID   uuid.UUID
	Name string
}

// Localities returns the visible cities and towns named exactly one of
// names (lowercased), best first as the FindLocalities query documents.
// Under a parent other than uuid.Nil only its descendants count.
func (s *Store) Localities(ctx context.Context, names []string, parent uuid.UUID) ([]Locality, error) {
	params := dbq.FindLocalitiesParams{Names: names, Lim: maxLocalityRows}
	if parent != uuid.Nil {
		params.ParentID = uuid.NullUUID{UUID: parent, Valid: true}
	}
	rows, err := s.q.FindLocalities(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("find localities: %w", err)
	}
	out := make([]Locality, len(rows))
	for i, r := range rows {
		out[i] = Locality{ID: r.ID, Name: r.Name}
	}
	return out, nil
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
	rows, err := s.q.ListChildren(ctx, dbq.ListChildrenParams{ParentID: parent, Category: categoryParam(category), AfterID: afterID, AfterName: afterName, Lim: limit})
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

// SetFollowing follows or unfollows a place for a user; either is a no-op
// when already so.
func (s *Store) SetFollowing(ctx context.Context, userID, placeID uuid.UUID, on bool) error {
	if on {
		if err := s.q.FollowPlace(ctx, dbq.FollowPlaceParams{UserID: userID, PlaceID: placeID}); err != nil {
			return fmt.Errorf("follow place: %w", err)
		}
		return nil
	}
	if err := s.q.UnfollowPlace(ctx, dbq.UnfollowPlaceParams{UserID: userID, PlaceID: placeID}); err != nil {
		return fmt.Errorf("unfollow place: %w", err)
	}
	return nil
}

// IsFollowing reports whether the user follows the place.
func (s *Store) IsFollowing(ctx context.Context, userID, placeID uuid.UUID) (bool, error) {
	ok, err := s.q.IsFollowing(ctx, dbq.IsFollowingParams{UserID: userID, PlaceID: placeID})
	if err != nil {
		return false, fmt.Errorf("is following: %w", err)
	}
	return ok, nil
}

// FollowedPlaceIDs lists up to limit places the user follows, newest first.
func (s *Store) FollowedPlaceIDs(ctx context.Context, userID uuid.UUID, limit int32) ([]uuid.UUID, error) {
	ids, err := s.q.FollowedPlaceIDs(ctx, dbq.FollowedPlaceIDsParams{UserID: userID, Lim: limit})
	if err != nil {
		return nil, fmt.Errorf("followed places: %w", err)
	}
	return ids, nil
}

// RecentStories counts, for each place, readable stories at it or below
// published since the cutoff by anyone but reader.
func (s *Store) RecentStories(ctx context.Context, reader uuid.UUID, ids []uuid.UUID, since time.Time) (map[uuid.UUID]int32, error) {
	rows, err := s.q.RecentStoriesUnder(ctx, dbq.RecentStoriesUnderParams{
		Ids: ids, Since: pgtype.Timestamptz{Time: since, Valid: true}, ReaderID: reader,
	})
	if err != nil {
		return nil, fmt.Errorf("recent stories: %w", err)
	}
	out := make(map[uuid.UUID]int32, len(rows))
	for _, r := range rows {
		out[r.PlaceID] = r.Recent
	}
	return out, nil
}

// FoundCharacter is one unclaimed character a character search found.
type FoundCharacter struct {
	ID             uuid.UUID
	Label          string
	PerspectiveID  uuid.UUID
	AuthorID       uuid.UUID
	AuthorUsername string
	ArcID          uuid.UUID
	ArcTitle       string
	LocalDate      time.Time
	Hour           *int32
	Place          Ref
}

// labelFirstCap is how many label matches anywhere still count as a rare
// word. At beta scale a rare word in a big city took 570 ms walking the
// city's stories and 23 ms by label; a common one went the other way.
const labelFirstCap = 2000

// SearchCharacters finds unclaimed characters at the place and below whose
// label contains pattern, a LIKE pattern with its wildcards already escaped.
func (s *Store) SearchCharacters(ctx context.Context, placeID uuid.UUID, pattern string) ([]FoundCharacter, error) {
	params := dbq.SearchCharactersParams{PlaceID: placeID, Pattern: pattern}
	// A rare word is fastest found by label and then filtered to the place;
	// a common one by walking the place's stories. The trigram index needs
	// three characters, so shorter words always walk the place.
	if utf8.RuneCountInString(pattern) >= minSubstringRunes {
		ids, err := s.q.CharactersLabelled(ctx, dbq.CharactersLabelledParams{Pattern: pattern, Cap: labelFirstCap})
		if err != nil {
			return nil, fmt.Errorf("search character labels: %w", err)
		}
		if len(ids) == 0 {
			return []FoundCharacter{}, nil
		}
		if len(ids) < labelFirstCap {
			params.Ids = ids
		}
	}
	rows, err := s.q.SearchCharacters(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("search characters: %w", err)
	}
	out := make([]FoundCharacter, len(rows))
	for i, r := range rows {
		out[i] = FoundCharacter{
			ID: r.ID, Label: r.Label, PerspectiveID: r.PerspectiveID, AuthorID: r.AuthorID, AuthorUsername: r.Username,
			ArcID: r.ArcID, ArcTitle: r.ArcTitle, LocalDate: r.LocalDate.Time, Hour: hourOf(r.Hour),
			Place: Ref{ID: r.PlaceID, Kind: kindOf(r.PlaceKind), Name: r.PlaceName},
		}
	}
	return out, nil
}

// excerptRunes is how much of a story's opening a listing quotes. The
// queries read a little more, so a clip can tell an opening that ends there
// from one that goes on.
const excerptRunes = 200

// clipExcerpt cuts an opening at the last word that fits and marks the cut
// with an ellipsis, so a listing never quotes half a word.
func clipExcerpt(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= excerptRunes {
		return string(r)
	}
	cut := excerptRunes
	for i := excerptRunes; i > excerptRunes/2; i-- {
		if unicode.IsSpace(r[i]) {
			cut = i
			break
		}
	}
	return strings.TrimRightFunc(string(r[:cut]), func(c rune) bool { return unicode.IsSpace(c) || c == ',' || c == ';' || c == ':' }) + "…"
}

// Sitemap lists up to limit places with a readable story at or below them,
// most recently written first.
func (s *Store) Sitemap(ctx context.Context, limit int32) ([]apiutil.SitemapEntry, error) {
	rows, err := s.q.SitemapPlaces(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("sitemap places: %w", err)
	}
	out := make([]apiutil.SitemapEntry, len(rows))
	for i, r := range rows {
		out[i] = apiutil.SitemapEntry{ID: r.ID, LastMod: r.LastMod.Time}
	}
	return out, nil
}

// StoriedCategories returns, of cats and in their order, the categories
// with a readable day at a visible place of that category.
func (s *Store) StoriedCategories(ctx context.Context, cats []perspectivv1.PlaceCategory) ([]perspectivv1.PlaceCategory, error) {
	names := make([]string, len(cats))
	for i, c := range cats {
		names[i] = c.String()
	}
	rows, err := s.q.StoriedCategories(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("storied categories: %w", err)
	}
	out := make([]perspectivv1.PlaceCategory, len(rows))
	for i, r := range rows {
		out[i] = categoryOf(r)
	}
	return out, nil
}

// CategoryPage is one category's "best days" page in the sitemap, with when
// its newest readable story was published.
type CategoryPage struct {
	Category perspectivv1.PlaceCategory
	LastMod  time.Time
}

// CategorySitemap lists the categories with a readable story at a visible
// place of that category, most recently written first.
func (s *Store) CategorySitemap(ctx context.Context) ([]CategoryPage, error) {
	rows, err := s.q.SitemapCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("sitemap categories: %w", err)
	}
	out := make([]CategoryPage, len(rows))
	for i, r := range rows {
		out[i] = CategoryPage{Category: categoryOf(r.Category), LastMod: r.LastMod.Time}
	}
	return out, nil
}
