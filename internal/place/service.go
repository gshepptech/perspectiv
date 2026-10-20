// Package place owns the STATE > CITY > TOWN > MALL > STORE hierarchy:
// drill-down and typeahead reads, user-added malls and stores, and a place's
// epic, the listing of arcs and tales at the place and its descendants.
//
// A place hidden by moderation hides its whole subtree from every read here.
// Each arc keeps its own canon; a place's epic only lists the arcs below it.
package place

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
)

// Search returns at most SearchLimit places. The typeahead shows one short
// list and the user narrows by typing, so it is not paginated. Ranking is
// exact name, then name prefix, then substring; within each, broader kinds
// first (a city before a store of the same name), then shorter names, then
// alphabetical. The top searchCandidates matches are checked for a hidden
// ancestor before the cut, so a page can come back short only when more than
// searchCandidates-SearchLimit of the best matches sit under hidden places.
const (
	SearchLimit      = 20
	searchCandidates = 100
)

// MaxNameLength bounds a user-added place name, in characters, after
// whitespace normalization.
const MaxNameLength = 120

// Service holds the place domain's rules over a Store.
type Service struct {
	store *Store
}

// NewService returns a Service on store.
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// normalizeName trims a name and collapses each whitespace run to one
// space: the same normalization the per-parent uniqueness index applies.
func normalizeName(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func notFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return apiutil.NotFound("place not found")
	}
	return apiutil.Internal(err)
}

// Search runs the typeahead. parentID uuid.Nil searches every place; a
// parent that is missing or hidden is NotFound. kind UNSPECIFIED matches
// every kind.
func (s *Service) Search(ctx context.Context, query string, parentID uuid.UUID, kind perspectivv1.PlaceKind) ([]Place, error) {
	text := strings.ToLower(normalizeName(query))
	if text == "" {
		return nil, apiutil.InvalidArg("query is required")
	}
	if parentID != uuid.Nil {
		if _, err := s.store.Get(ctx, parentID); err != nil {
			return nil, notFound(err)
		}
	}
	ps, err := s.store.Search(ctx, SearchQuery{
		Text: text, ParentID: parentID, Kind: kind, Limit: SearchLimit, Candidates: searchCandidates,
	})
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	return ps, nil
}

// Get returns a visible place with its ancestors.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Place, error) {
	p, err := s.store.Get(ctx, id)
	if err != nil {
		return Place{}, notFound(err)
	}
	return p, nil
}

// childCursor is the drill-down page token payload.
type childCursor struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

// ListChildren returns one page of a parent's visible children by name, or
// of the states when parentID is uuid.Nil, and the page token for the next
// page ("" on the last page).
func (s *Service) ListChildren(ctx context.Context, parentID uuid.UUID, page apiutil.PageToken) ([]Place, string, error) {
	var after *ChildCursor
	if page.After != "" {
		var c childCursor
		if err := json.Unmarshal([]byte(page.After), &c); err != nil || c.ID == uuid.Nil {
			return nil, "", apiutil.InvalidArg("invalid page token")
		}
		after = &ChildCursor{Name: c.Name, ID: c.ID}
	}
	if parentID != uuid.Nil {
		if _, err := s.store.Get(ctx, parentID); err != nil {
			return nil, "", notFound(err)
		}
	}
	ps, err := s.store.Children(ctx, parentID, after, page.Limit+1)
	if err != nil {
		return nil, "", apiutil.Internal(err)
	}
	if len(ps) <= int(page.Limit) {
		return ps, "", nil
	}
	ps = ps[:page.Limit]
	last := ps[len(ps)-1]
	key := last.sortKey
	if parentID == uuid.Nil {
		// The states page by plain lowercased name (ListStates).
		key = strings.ToLower(last.Name)
	}
	return ps, mustCursor(childCursor{Name: key, ID: last.ID}), nil
}

// StoriedChildren returns up to limit children of parentID (the states when
// it is uuid.Nil) that have a readable story at or below them, most recently
// started arc first.
func (s *Service) StoriedChildren(ctx context.Context, parentID uuid.UUID, limit int32) ([]Place, error) {
	if parentID != uuid.Nil {
		if _, err := s.store.Get(ctx, parentID); err != nil {
			return nil, notFound(err)
		}
	}
	ps, err := s.store.StoriedChildren(ctx, parentID, limit)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	return ps, nil
}

// level is a kind's depth in the hierarchy, STATE = 1 through STORE = 5.
func level(k perspectivv1.PlaceKind) int {
	switch k {
	case perspectivv1.PlaceKind_PLACE_KIND_STATE:
		return 1
	case perspectivv1.PlaceKind_PLACE_KIND_CITY:
		return 2
	case perspectivv1.PlaceKind_PLACE_KIND_TOWN:
		return 3
	case perspectivv1.PlaceKind_PLACE_KIND_MALL:
		return 4
	case perspectivv1.PlaceKind_PLACE_KIND_STORE:
		return 5
	case perspectivv1.PlaceKind_PLACE_KIND_UNSPECIFIED:
		return 0
	}
	return 0
}

// Create adds a user MALL or STORE, live at once and flagged user-added.
//
// Any visible place is a valid parent, user-added ones included, provided it
// sits higher in the hierarchy than the new place: the drill-down order is
// fixed, so a store cannot hold a store and a mall cannot hold a mall. A mall
// or store may skip levels (a store directly under a town), because imported
// data has stores outside any mall. The name is stored trimmed with
// whitespace runs collapsed; a same-name place under that parent, compared
// case-insensitively, is AlreadyExists.
func (s *Service) Create(ctx context.Context, caller uuid.UUID, name string, kind perspectivv1.PlaceKind, parentID uuid.UUID) (Place, error) {
	if kind != perspectivv1.PlaceKind_PLACE_KIND_MALL && kind != perspectivv1.PlaceKind_PLACE_KIND_STORE {
		return Place{}, apiutil.InvalidArg("only malls and stores can be added")
	}
	name = normalizeName(name)
	if name == "" {
		return Place{}, apiutil.InvalidArg("name is required")
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return Place{}, apiutil.InvalidArg("name is too long")
	}
	parent, err := s.store.Get(ctx, parentID)
	if err != nil {
		return Place{}, notFound(err)
	}
	if level(parent.Kind) >= level(kind) {
		return Place{}, apiutil.InvalidArg("a " + kindWord(kind) + " cannot be added under a " + kindWord(parent.Kind))
	}
	p, err := s.store.Insert(ctx, NewPlace{Name: name, Kind: kind, ParentID: parentID, CreatedBy: caller})
	switch {
	case errors.Is(err, ErrDuplicateName):
		return Place{}, apiutil.AlreadyExists("a place with this name already exists there")
	case err != nil:
		return Place{}, notFound(err)
	}
	return p, nil
}

func kindWord(k perspectivv1.PlaceKind) string {
	return strings.ToLower(strings.TrimPrefix(k.String(), "PLACE_KIND_"))
}

// EpicRequest asks for one page of a place's epic.
type EpicRequest struct {
	PlaceID uuid.UUID
	Tab     perspectivv1.EpicTab
	Genre   perspectivv1.Genre
	Page    apiutil.PageToken
}

// Epic is one page of a place's epic: Arcs for RANKED and NEWEST, Tales for
// TALES, and the token of the next page ("" on the last page).
type Epic struct {
	Place Place
	Arcs  []EpicArc
	Tales []EpicTale
	Next  string
}

// epicCursor is the epic page token payload. Tab names the listing it was
// minted for, so a token from one tab is refused by another.
type epicCursor struct {
	Tab      string    `json:"t"`
	HasCanon bool      `json:"c,omitempty"`
	Score    float64   `json:"s,omitempty"`
	Members  int32     `json:"m,omitempty"`
	At       time.Time `json:"at"`
	ID       uuid.UUID `json:"id"`
}

func mustCursor(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// The cursor types hold only strings, numbers, times and UUIDs,
		// which always marshal.
		panic(err)
	}
	return apiutil.NextPageToken(string(b))
}

func parseEpicCursor(after string, tab perspectivv1.EpicTab) (*epicCursor, error) {
	if after == "" {
		return nil, nil
	}
	var c epicCursor
	if err := json.Unmarshal([]byte(after), &c); err != nil || c.Tab != tab.String() || c.ID == uuid.Nil {
		return nil, apiutil.InvalidArg("invalid page token")
	}
	return &c, nil
}

// Epic lists one page of a visible place's epic. RANKED and NEWEST list the
// arcs at the place and every visible descendant that have a readable
// perspective; TALES lists their SOLO and REMOVED perspectives. A missing or
// hidden place is NotFound.
func (s *Service) Epic(ctx context.Context, req EpicRequest) (Epic, error) {
	var order ArcOrder
	switch req.Tab {
	case perspectivv1.EpicTab_EPIC_TAB_RANKED:
		order = OrderRanked
	case perspectivv1.EpicTab_EPIC_TAB_NEWEST:
		order = OrderNewest
	case perspectivv1.EpicTab_EPIC_TAB_TALES:
	case perspectivv1.EpicTab_EPIC_TAB_UNSPECIFIED:
		return Epic{}, apiutil.InvalidArg("tab must be set")
	default:
		return Epic{}, apiutil.InvalidArg("unknown tab")
	}
	cur, err := parseEpicCursor(req.Page.After, req.Tab)
	if err != nil {
		return Epic{}, err
	}
	p, err := s.store.Get(ctx, req.PlaceID)
	if err != nil {
		return Epic{}, notFound(err)
	}
	if req.Tab == perspectivv1.EpicTab_EPIC_TAB_TALES {
		return s.tales(ctx, p, req, cur)
	}
	q := EpicArcsQuery{PlaceID: p.ID, Order: order, Genre: req.Genre, Limit: req.Page.Limit + 1}
	if cur != nil {
		q.After = &ArcCursor{HasCanon: cur.HasCanon, Score: cur.Score, Members: cur.Members, CreatedAt: cur.At, ID: cur.ID}
	}
	arcs, err := s.store.EpicArcs(ctx, q)
	if err != nil {
		return Epic{}, apiutil.Internal(err)
	}
	out := Epic{Place: p, Arcs: arcs}
	if len(arcs) > int(req.Page.Limit) {
		out.Arcs = arcs[:req.Page.Limit]
		c := out.Arcs[len(out.Arcs)-1].Cursor()
		out.Next = mustCursor(epicCursor{
			Tab: req.Tab.String(), HasCanon: c.HasCanon, Score: c.Score, Members: c.Members, At: c.CreatedAt, ID: c.ID,
		})
	}
	return out, nil
}

func (s *Service) tales(ctx context.Context, p Place, req EpicRequest, cur *epicCursor) (Epic, error) {
	q := TalesQuery{PlaceID: p.ID, Genre: req.Genre, Limit: req.Page.Limit + 1}
	if cur != nil {
		q.After = &TaleCursor{PublishedAt: cur.At, ID: cur.ID}
	}
	tales, err := s.store.EpicTales(ctx, q)
	if err != nil {
		return Epic{}, apiutil.Internal(err)
	}
	out := Epic{Place: p, Tales: tales}
	if len(tales) > int(req.Page.Limit) {
		out.Tales = tales[:req.Page.Limit]
		last := out.Tales[len(out.Tales)-1]
		out.Next = mustCursor(epicCursor{Tab: req.Tab.String(), At: last.PublishedAt, ID: last.PerspectiveID})
	}
	return out, nil
}
