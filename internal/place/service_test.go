//go:build integration

package place_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/place"
)

// bearerAuthn treats "Bearer <uuid>" as that user; the real authenticator
// lives in the auth domain. The interceptor chain itself is production code.
type bearerAuthn struct{}

func (bearerAuthn) Authenticate(_ context.Context, h http.Header) (apiutil.Caller, bool, error) {
	tok, ok := strings.CutPrefix(h.Get("Authorization"), "Bearer ")
	if !ok {
		return apiutil.Caller{}, false, nil
	}
	id, err := uuid.Parse(tok)
	if err != nil {
		return apiutil.Caller{}, false, apiutil.Unauthenticated("bad token")
	}
	return apiutil.Caller{UserID: id, Role: perspectivv1.Role_ROLE_USER, SessionID: uuid.New()}, true, nil
}

// dbGate reads email_verified_at from the real users table.
type dbGate struct{ pool *pgxpool.Pool }

func (g dbGate) WriteStanding(ctx context.Context, id uuid.UUID) (api.Standing, error) {
	var verified, suspended bool
	err := g.pool.QueryRow(ctx, `SELECT email_verified_at IS NOT NULL, suspended_at IS NOT NULL FROM users WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&verified, &suspended)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return api.StandingUnverified, nil
	case err != nil:
		return api.StandingUnverified, err
	case suspended:
		return api.StandingSuspended, nil
	case !verified:
		return api.StandingUnverified, nil
	}
	return api.StandingWriter, nil
}

type allowAll struct{}

func (allowAll) Allow(context.Context, string, string) (bool, error) { return true, nil }

// client serves the place handler behind the production interceptor chain
// and returns a Connect client for it.
func client(t *testing.T, pool *pgxpool.Pool) perspectivv1connect.PlaceServiceClient {
	t.Helper()
	h := place.NewHandler(place.NewService(place.NewStore(pool)))
	path, handler := perspectivv1connect.NewPlaceServiceHandler(h,
		connect.WithInterceptors(api.Interceptors(zerolog.New(io.Discard), bearerAuthn{}, dbGate{pool}, allowAll{})...))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return perspectivv1connect.NewPlaceServiceClient(srv.Client(), srv.URL)
}

func as[T any](user uuid.UUID, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	if user != uuid.Nil {
		req.Header().Set("Authorization", "Bearer "+user.String())
	}
	return req
}

func wantCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	if got := connect.CodeOf(err); err == nil || got != code {
		t.Fatalf("err = %v (code %v), want %v", err, got, code)
	}
}

func names(places []*perspectivv1.Place) []string {
	out := make([]string, len(places))
	for i, p := range places {
		out[i] = p.GetName()
	}
	return out
}

// search runs SearchPlaces and returns the matched names.
func search(t *testing.T, c perspectivv1connect.PlaceServiceClient, req *perspectivv1.SearchPlacesRequest) []string {
	t.Helper()
	res, err := c.SearchPlaces(context.Background(), as(uuid.Nil, req))
	if err != nil {
		t.Fatalf("SearchPlaces(%q): %v", req.GetQuery(), err)
	}
	return names(res.Msg.GetPlaces())
}

// children lists one page of a parent's children.
func children(t *testing.T, c perspectivv1connect.PlaceServiceClient, req *perspectivv1.ListChildrenRequest) *perspectivv1.ListChildrenResponse {
	t.Helper()
	res, err := c.ListChildren(context.Background(), as(uuid.Nil, req))
	if err != nil {
		t.Fatalf("ListChildren(%q): %v", req.GetParentId(), err)
	}
	return res.Msg
}

// epic fetches one epic page.
func epic(t *testing.T, c perspectivv1connect.PlaceServiceClient, req *perspectivv1.GetEpicRequest) *perspectivv1.GetEpicResponse {
	t.Helper()
	res, err := c.GetEpic(context.Background(), as(uuid.Nil, req))
	if err != nil {
		t.Fatalf("GetEpic: %v", err)
	}
	return res.Msg
}

// epicPages follows next_page_token from the first page to the last and
// returns every arc title in order.
func epicPages(t *testing.T, c perspectivv1connect.PlaceServiceClient, req *perspectivv1.GetEpicRequest) []string {
	t.Helper()
	var titles []string
	for range 20 {
		res := epic(t, c, req)
		titles = append(titles, arcTitles(res.GetArcs())...)
		if res.GetNextPageToken() == "" {
			return titles
		}
		req.PageToken = res.GetNextPageToken()
	}
	t.Fatal("more than 20 pages")
	return nil
}

// TestSearchPlaces checks that typeahead over names
// with the parent chain, the user_added flag, kind and parent filters,
// moderation hiding, and the documented ranking.
func TestSearchPlaces(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	ctx := context.Background()
	h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Springfield Coffee", h.mall)
	h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Old Springfield Diner", h.mall)
	h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "100% Juice", h.mall)
	chiSpring := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Springfield Outlet", h.otherCity)

	tests := []struct {
		name string
		req  *perspectivv1.SearchPlacesRequest
		want []string
	}{
		{
			name: "exact, then prefix, then substring",
			req:  &perspectivv1.SearchPlacesRequest{Query: "springfield"},
			want: []string{"Springfield", "Springfield Coffee", "Springfield Outlet", "Old Springfield Diner"},
		},
		{name: "case and surrounding space ignored", req: &perspectivv1.SearchPlacesRequest{Query: "  CORNER  "}, want: []string{"Corner Books"}},
		{name: "kind filter", req: &perspectivv1.SearchPlacesRequest{Query: "spring", Kind: perspectivv1.PlaceKind_PLACE_KIND_CITY}, want: []string{"Springfield"}},
		{
			name: "parent filter keeps descendants only",
			req:  &perspectivv1.SearchPlacesRequest{Query: "springfield", ParentId: h.city.String()},
			want: []string{"Springfield Coffee", "Old Springfield Diner"},
		},
		{name: "like wildcards are literal", req: &perspectivv1.SearchPlacesRequest{Query: "0% j"}, want: []string{"100% Juice"}},
		{name: "a lone wildcard matches only a name starting with it", req: &perspectivv1.SearchPlacesRequest{Query: "%"}, want: []string{}},
		{
			name: "under three letters matches name starts only",
			req:  &perspectivv1.SearchPlacesRequest{Query: "sp"},
			want: []string{"Springfield", "Springfield Coffee", "Springfield Outlet"},
		},
		{name: "no match", req: &perspectivv1.SearchPlacesRequest{Query: "zzzz"}, want: []string{}},
		{name: "two hundred characters are searched", req: &perspectivv1.SearchPlacesRequest{Query: strings.Repeat("spring ", 28) + "fiel"}, want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := search(t, c, tc.req); !slices.Equal(got, tc.want) {
				t.Fatalf("names = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("result carries kind, parent chain and user_added", func(t *testing.T) {
		res, err := c.SearchPlaces(ctx, as(uuid.Nil, &perspectivv1.SearchPlacesRequest{Query: "Corner Books"}))
		if err != nil {
			t.Fatalf("SearchPlaces: %v", err)
		}
		if len(res.Msg.GetPlaces()) != 1 {
			t.Fatalf("got %d places", len(res.Msg.GetPlaces()))
		}
		p := res.Msg.GetPlaces()[0]
		var chain []string
		for _, a := range p.GetAncestors() {
			chain = append(chain, a.GetKind().String()+":"+a.GetName())
		}
		want := []string{"PLACE_KIND_STATE:Illinois", "PLACE_KIND_CITY:Springfield", "PLACE_KIND_TOWN:Southtown", "PLACE_KIND_MALL:White Oaks Mall"}
		if !slices.Equal(chain, want) || p.GetKind() != perspectivv1.PlaceKind_PLACE_KIND_STORE ||
			p.GetUserAdded() || p.GetParentId() != h.mall.String() || p.GetTimezone() != "America/Chicago" {
			t.Fatalf("place = %v, chain %q", p, chain)
		}
	})

	t.Run("hidden places and their subtrees are not found", func(t *testing.T) {
		h.hide(chiSpring)
		h.hide(h.town)
		if got := search(t, c, &perspectivv1.SearchPlacesRequest{Query: "spring"}); !slices.Equal(got, []string{"Springfield"}) {
			t.Fatalf("names = %q, want only the visible city", got)
		}
	})

	errCases := []struct {
		name string
		req  *perspectivv1.SearchPlacesRequest
		code connect.Code
	}{
		{name: "empty query", req: &perspectivv1.SearchPlacesRequest{}, code: connect.CodeInvalidArgument},
		{name: "blank query", req: &perspectivv1.SearchPlacesRequest{Query: " \t "}, code: connect.CodeInvalidArgument},
		{name: "a query over two hundred characters", req: &perspectivv1.SearchPlacesRequest{Query: strings.Repeat("spring ", 28) + "field"}, code: connect.CodeInvalidArgument},
		{name: "two hundred and one multibyte characters", req: &perspectivv1.SearchPlacesRequest{Query: strings.Repeat("é", 201)}, code: connect.CodeInvalidArgument},
		{name: "malformed parent", req: &perspectivv1.SearchPlacesRequest{Query: "a", ParentId: "nope"}, code: connect.CodeInvalidArgument},
		{name: "unknown parent", req: &perspectivv1.SearchPlacesRequest{Query: "a", ParentId: uuid.NewString()}, code: connect.CodeNotFound},
		{name: "hidden parent", req: &perspectivv1.SearchPlacesRequest{Query: "a", ParentId: h.mall.String()}, code: connect.CodeNotFound},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.SearchPlaces(ctx, as(uuid.Nil, tc.req))
			wantCode(t, err, tc.code)
		})
	}
}

// TestSearchPlacesLimit covers the decided result cap: at most 20 places.
func TestSearchPlacesLimit(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	for i := range 25 {
		h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, fmt.Sprintf("Kiosk %02d", i), h.mall)
	}
	res, err := c.SearchPlaces(context.Background(), as(uuid.Nil, &perspectivv1.SearchPlacesRequest{Query: "kiosk"}))
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	got := names(res.Msg.GetPlaces())
	if len(got) != 20 || got[0] != "Kiosk 00" || got[19] != "Kiosk 19" {
		t.Fatalf("names = %q, want Kiosk 00..19", got)
	}
}

// TestDrillDown checks that from the states down, each level's
// children are the next level, paged by name.
func TestDrillDown(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	ctx := context.Background()
	h.place(perspectivv1.PlaceKind_PLACE_KIND_STATE, "Alaska", uuid.Nil)

	if got := names(children(t, c, &perspectivv1.ListChildrenRequest{}).GetPlaces()); !slices.Equal(got, []string{"Alaska", "Illinois"}) {
		t.Fatalf("states = %q", got)
	}

	chain := []struct {
		parent uuid.UUID
		kind   perspectivv1.PlaceKind
		first  string
	}{
		{h.state, perspectivv1.PlaceKind_PLACE_KIND_CITY, "Chicago"},
		{h.city, perspectivv1.PlaceKind_PLACE_KIND_TOWN, "Southtown"},
		{h.town, perspectivv1.PlaceKind_PLACE_KIND_MALL, "White Oaks Mall"},
		{h.mall, perspectivv1.PlaceKind_PLACE_KIND_STORE, "Corner Books"},
	}
	for _, step := range chain {
		ps := children(t, c, &perspectivv1.ListChildrenRequest{ParentId: step.parent.String()}).GetPlaces()
		if len(ps) == 0 || ps[0].GetName() != step.first || ps[0].GetKind() != step.kind {
			t.Fatalf("children of %v = %v, want %s first", step.parent, ps, step.first)
		}
	}

	t.Run("pages by name without gaps or repeats", func(t *testing.T) {
		for _, n := range []string{"b", "A", "c", "D", "e"} {
			h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, n, h.mall)
		}
		var got []string
		req := &perspectivv1.ListChildrenRequest{ParentId: h.mall.String(), PageSize: 2}
		for range 10 {
			res := children(t, c, req)
			got = append(got, names(res.GetPlaces())...)
			if req.PageToken = res.GetNextPageToken(); req.PageToken == "" {
				break
			}
		}
		want := []string{"A", "b", "c", "Corner Books", "D", "e"}
		if !slices.Equal(got, want) {
			t.Fatalf("pages = %q, want %q", got, want)
		}
	})

	t.Run("hidden child left out, hidden parent not found", func(t *testing.T) {
		h.hide(h.otherCity)
		if got := names(children(t, c, &perspectivv1.ListChildrenRequest{ParentId: h.state.String()}).GetPlaces()); !slices.Equal(got, []string{"Springfield"}) {
			t.Fatalf("cities = %q", got)
		}
		_, err := c.ListChildren(ctx, as(uuid.Nil, &perspectivv1.ListChildrenRequest{ParentId: h.otherCity.String()}))
		wantCode(t, err, connect.CodeNotFound)
		_, err = c.ListChildren(ctx, as(uuid.Nil, &perspectivv1.ListChildrenRequest{ParentId: h.state.String(), PageToken: "garbage"}))
		wantCode(t, err, connect.CodeInvalidArgument)
	})
}

// TestGetPlace covers reading one place with its ancestors.
func TestGetPlace(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	ctx := context.Background()

	res, err := c.GetPlace(ctx, as(uuid.Nil, &perspectivv1.GetPlaceRequest{PlaceId: h.mall.String()}))
	if err != nil {
		t.Fatalf("GetPlace: %v", err)
	}
	p := res.Msg.GetPlace()
	if p.GetName() != "White Oaks Mall" || len(p.GetAncestors()) != 3 || p.GetSource() != perspectivv1.PlaceSource_PLACE_SOURCE_OVERTURE {
		t.Fatalf("place = %v", p)
	}
	// A categorised venue says what it is on its own page, as in search.
	h.exec(`UPDATE places SET category = 'PLACE_CATEGORY_SPORTS_AND_RECREATION' WHERE id = $1`, h.mall)
	res, err = c.GetPlace(ctx, as(uuid.Nil, &perspectivv1.GetPlaceRequest{PlaceId: h.mall.String()}))
	if err != nil || res.Msg.GetPlace().GetCategory() != perspectivv1.PlaceCategory_PLACE_CATEGORY_SPORTS_AND_RECREATION {
		t.Fatalf("category = %v, %v", res.Msg.GetPlace().GetCategory(), err)
	}

	h.hide(h.mall)
	tests := []struct {
		name string
		id   string
		code connect.Code
	}{
		{name: "hidden", id: h.mall.String(), code: connect.CodeNotFound},
		{name: "under hidden", id: h.store.String(), code: connect.CodeNotFound},
		{name: "missing", id: uuid.NewString(), code: connect.CodeNotFound},
		{name: "malformed", id: "x", code: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.GetPlace(ctx, as(uuid.Nil, &perspectivv1.GetPlaceRequest{PlaceId: tc.id}))
			wantCode(t, err, tc.code)
		})
	}
}

// TestCreatePlace covers adding a place under an existing parent.
func TestCreatePlace(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	ctx := context.Background()
	alice := h.user(true)
	bob := h.user(true)
	unverified := h.user(false)

	// A STORE under a MALL another user added is live and searchable.
	mallRes, err := c.CreatePlace(ctx, as(alice, &perspectivv1.CreatePlaceRequest{
		Name: "  Riverside   Plaza ", Kind: perspectivv1.PlaceKind_PLACE_KIND_MALL, ParentId: h.city.String(),
	}))
	if err != nil {
		t.Fatalf("CreatePlace(mall): %v", err)
	}
	mall := mallRes.Msg.GetPlace()
	if mallRes.Msg.GetPlaceId() == "" || mall.GetName() != "Riverside Plaza" || !mall.GetUserAdded() ||
		mall.GetSource() != perspectivv1.PlaceSource_PLACE_SOURCE_USER || mall.GetTimezone() != "America/Chicago" ||
		len(mall.GetAncestors()) != 2 {
		t.Fatalf("created mall = %v", mall)
	}

	storeRes, err := c.CreatePlace(ctx, as(bob, &perspectivv1.CreatePlaceRequest{
		Name: "Pretzel Stand", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: mallRes.Msg.GetPlaceId(),
	}))
	if err != nil {
		t.Fatalf("CreatePlace(store under user mall): %v", err)
	}
	found, err := c.SearchPlaces(ctx, as(uuid.Nil, &perspectivv1.SearchPlacesRequest{Query: "pretzel"}))
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	if ps := found.Msg.GetPlaces(); len(ps) != 1 || ps[0].GetId() != storeRes.Msg.GetPlaceId() || !ps[0].GetUserAdded() {
		t.Fatalf("search after create = %v", ps)
	}

	hiddenMall := h.place(perspectivv1.PlaceKind_PLACE_KIND_MALL, "Closed Mall", h.city)
	h.hide(hiddenMall)

	tests := []struct {
		name string
		user uuid.UUID
		req  *perspectivv1.CreatePlaceRequest
		code connect.Code
	}{
		{name: "same name again under that mall", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "pretzel  STAND", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: mallRes.Msg.GetPlaceId()}, code: connect.CodeAlreadyExists},
		{name: "duplicate of an imported place", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "corner books", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: h.mall.String()}, code: connect.CodeAlreadyExists},
		{name: "store under a hidden mall", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "Ghost", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: hiddenMall.String()}, code: connect.CodeNotFound},
		{name: "missing parent", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "Ghost", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: uuid.NewString()}, code: connect.CodeNotFound},
		{name: "town is refused", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "Newtown", Kind: perspectivv1.PlaceKind_PLACE_KIND_TOWN, ParentId: h.city.String()}, code: connect.CodeInvalidArgument},
		{name: "state is refused", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "Ohio", Kind: perspectivv1.PlaceKind_PLACE_KIND_STATE, ParentId: h.city.String()}, code: connect.CodeInvalidArgument},
		{name: "kind unset", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "X Store", ParentId: h.city.String()}, code: connect.CodeInvalidArgument},
		{name: "store under a store", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "Inner", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: h.store.String()}, code: connect.CodeInvalidArgument},
		{name: "mall under a mall", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "Annex", Kind: perspectivv1.PlaceKind_PLACE_KIND_MALL, ParentId: h.mall.String()}, code: connect.CodeInvalidArgument},
		{name: "blank name", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "   ", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: h.mall.String()}, code: connect.CodeInvalidArgument},
		{name: "name too long", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: strings.Repeat("x", 121), Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: h.mall.String()}, code: connect.CodeInvalidArgument},
		{name: "malformed parent", user: alice, req: &perspectivv1.CreatePlaceRequest{Name: "Ok", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: "nope"}, code: connect.CodeInvalidArgument},
		{name: "signed out", user: uuid.Nil, req: &perspectivv1.CreatePlaceRequest{Name: "Ok", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: h.mall.String()}, code: connect.CodeUnauthenticated},
		{name: "unverified email", user: unverified, req: &perspectivv1.CreatePlaceRequest{Name: "Ok", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: h.mall.String()}, code: connect.CodeFailedPrecondition},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.CreatePlace(ctx, as(tc.user, tc.req))
			wantCode(t, err, tc.code)
		})
	}

	t.Run("a mall directly under a state and a store under a town are valid parents", func(t *testing.T) {
		for _, req := range []*perspectivv1.CreatePlaceRequest{
			{Name: "Statewide Mall", Kind: perspectivv1.PlaceKind_PLACE_KIND_MALL, ParentId: h.state.String()},
			{Name: "Town Store", Kind: perspectivv1.PlaceKind_PLACE_KIND_STORE, ParentId: h.town.String()},
		} {
			if _, err := c.CreatePlace(ctx, as(alice, req)); err != nil {
				t.Fatalf("CreatePlace(%s): %v", req.GetName(), err)
			}
		}
	})
}

func arcTitles(arcs []*perspectivv1.EpicArc) []string {
	out := make([]string, len(arcs))
	for i, a := range arcs {
		out[i] = a.GetTitle()
	}
	return out
}

// TestGetEpicRanked checks the epic order: arcs at the city and its
// descendants, canon first by the stored canon score, then member count, then newest.
func TestGetEpicRanked(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	by := h.user(true)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	mk := func(placeID uuid.UUID, date, title string, created time.Time) uuid.UUID {
		a := h.arc(placeID, date, title, by, created)
		h.perspective(a, by, perspectivv1.Genre_GENRE_COMEDY, perspectivv1.LoreStatus_LORE_STATUS_IN_LORE, created, "text")
		return a
	}
	// The store arc has a canon; the newer city arc does not.
	storeArc := mk(h.store, "2026-10-01", "Store with canon", t0)
	h.canon(storeArc, 2.5, 4)
	cityArc := mk(h.city, "2026-10-02", "City no canon big", t0.Add(time.Hour))
	h.members(cityArc, 9)
	// A stored score left on an arc whose canon was cleared does not lift it
	// above arcs that have a canon.
	h.exec(`UPDATE arcs SET canon_score = 9.5 WHERE id = $1`, cityArc)
	townArc := mk(h.town, "2026-10-03", "Town higher canon", t0.Add(2*time.Hour))
	h.canon(townArc, 7.0, 4)
	mallArc := mk(h.mall, "2026-10-04", "Mall same score more members", t0.Add(3*time.Hour))
	h.canon(mallArc, 2.5, 6)
	mk(h.city, "2026-10-05", "City no canon newest", t0.Add(4*time.Hour))
	hour := h.hourArc(cityArc, 14, "City hour arc", by, t0.Add(5*time.Hour))
	h.perspective(hour, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "x")
	mk(h.otherCity, "2026-10-01", "Other city arc", t0.Add(6*time.Hour))
	// An arc whose only perspective was hidden by moderation is not listed.
	hiddenOnly := h.arc(h.city, "2026-10-06", "Hidden content arc", by, t0.Add(7*time.Hour))
	pid := h.perspective(hiddenOnly, by, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "x")
	h.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, pid)

	res := epic(t, c, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_RANKED})
	// Canon arcs first by stored canon score then member count; then the arcs
	// without canon by member count, ties (the hour arc and the newest city
	// arc, both 0 members) broken by newest.
	got := arcTitles(res.GetArcs())
	wantOrder := []string{"Town higher canon", "Mall same score more members", "Store with canon", "City no canon big", "City hour arc", "City no canon newest"}
	if !slices.Equal(got, wantOrder) {
		t.Fatalf("ranked = %q, want %q", got, wantOrder)
	}
	if res.GetPlace().GetId() != h.city.String() || len(res.GetTales()) != 0 {
		t.Fatalf("epic place = %v, tales %d", res.GetPlace(), len(res.GetTales()))
	}
	first := res.GetArcs()[0]
	if first.GetPlace().GetId() != h.town.String() || first.GetCanonLoreId() == "" || first.GetCanonScore() != 7.0 ||
		first.GetMemberCount() != 4 || first.GetPerspectiveCount() != 1 || first.GetLocalDate() != "2026-10-03" || first.Hour != nil {
		t.Fatalf("first arc = %v", first)
	}
	if a := res.GetArcs()[4]; a.Hour == nil || a.GetHour() != 14 {
		t.Fatalf("hour arc = %v", a)
	}
	// The canon lore's name rides along once a member names it; unnamed is "".
	if first.GetCanonLoreName() != "" {
		t.Fatalf("unnamed canon name = %q", first.GetCanonLoreName())
	}
	h.exec(`UPDATE lores SET name = 'The long queue' WHERE id = $1`, first.GetCanonLoreId())
	named := epic(t, c, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_RANKED})
	if got := named.GetArcs()[0].GetCanonLoreName(); got != "The long queue" {
		t.Fatalf("canon lore name = %q", got)
	}

	t.Run("paged result equals the single page", func(t *testing.T) {
		paged := epicPages(t, c, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_RANKED, PageSize: 2})
		if !slices.Equal(paged, wantOrder) {
			t.Fatalf("paged = %q, want %q", paged, wantOrder)
		}
	})

	t.Run("newest", func(t *testing.T) {
		want := []string{"City hour arc", "City no canon newest", "Mall same score more members", "Town higher canon", "City no canon big", "Store with canon"}
		if got := epicPages(t, c, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_NEWEST, PageSize: 4}); !slices.Equal(got, want) {
			t.Fatalf("newest = %q, want %q", got, want)
		}
	})

	t.Run("genre filter", func(t *testing.T) {
		if got := epicPages(t, c, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_NEWEST, Genre: perspectivv1.Genre_GENRE_DRAMA}); !slices.Equal(got, []string{"City hour arc"}) {
			t.Fatalf("drama arcs = %q", got)
		}
	})
}

// TestGetEpicEverywhere covers the front page: GetEpic without a place lists
// the arcs of every visible place, quotes the opening of the canon's first
// story (or the first story without a canon), and names the city.
func TestGetEpicEverywhere(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	by := h.user(true)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	storeArc := h.arc(h.store, "2026-10-01", "Bookshop canon", by, t0)
	h.perspective(storeArc, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, t0, "A tale told first")
	inCanon := h.perspective(storeArc, by, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_IN_LORE, t0.Add(time.Hour), "The canon opens here")
	lore := h.canon(storeArc, 3.0, 4)
	h.exec(`UPDATE perspectives SET lore_id = $2 WHERE id = $1`, inCanon, lore)
	otherArc := h.arc(h.otherCity, "2026-10-02", "Chicago no canon", by, t0.Add(time.Hour))
	h.perspective(otherArc, by, perspectivv1.Genre_GENRE_COMEDY, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "First in Chicago")
	// A place in another state, then hidden: its arcs leave the front page.
	other := h.place(perspectivv1.PlaceKind_PLACE_KIND_STATE, "Ohio", uuid.Nil)
	hiddenArc := h.arc(other, "2026-10-03", "Under a hidden state", by, t0.Add(2*time.Hour))
	h.perspective(hiddenArc, by, perspectivv1.Genre_GENRE_COMEDY, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "x")
	h.hide(other)

	tests := []struct {
		name string
		tab  perspectivv1.EpicTab
		want []string
	}{
		{name: "ranked puts canon first", tab: perspectivv1.EpicTab_EPIC_TAB_RANKED, want: []string{"Bookshop canon", "Chicago no canon"}},
		{name: "newest", tab: perspectivv1.EpicTab_EPIC_TAB_NEWEST, want: []string{"Chicago no canon", "Bookshop canon"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := epicPages(t, c, &perspectivv1.GetEpicRequest{Tab: tc.tab, PageSize: 1}); !slices.Equal(got, tc.want) {
				t.Fatalf("front page = %q, want %q", got, tc.want)
			}
		})
	}

	res := epic(t, c, &perspectivv1.GetEpicRequest{Tab: perspectivv1.EpicTab_EPIC_TAB_RANKED})
	if res.GetPlace() != nil {
		t.Fatalf("front page place = %v, want none", res.GetPlace())
	}
	bookshop, chicago := res.GetArcs()[0], res.GetArcs()[1]
	if bookshop.GetExcerpt() != "The canon opens here" || bookshop.GetCity() != "Springfield" || bookshop.GetPlace().GetName() != "Corner Books" {
		t.Fatalf("bookshop = excerpt %q, city %q, place %q", bookshop.GetExcerpt(), bookshop.GetCity(), bookshop.GetPlace().GetName())
	}
	if chicago.GetExcerpt() != "First in Chicago" || chicago.GetCity() != "Chicago" {
		t.Fatalf("chicago = excerpt %q, city %q", chicago.GetExcerpt(), chicago.GetCity())
	}

	t.Run("tales need a place", func(t *testing.T) {
		t.Parallel()
		_, err := c.GetEpic(context.Background(), as(uuid.Nil, &perspectivv1.GetEpicRequest{Tab: perspectivv1.EpicTab_EPIC_TAB_TALES}))
		wantCode(t, err, connect.CodeInvalidArgument)
	})
}

// TestGetEpicTales covers the TALES tab: SOLO and REMOVED perspectives at
// the place and its descendants, newest first, without deleted or hidden
// ones.
func TestGetEpicTales(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	by := h.user(true)
	cityArc := h.arc(h.city, "2026-10-01", "City arc", by, t0)
	storeArc := h.arc(h.store, "2026-10-01", "Store arc", by, t0)
	otherArc := h.arc(h.otherCity, "2026-10-01", "Other arc", by, t0)

	author := h.user(true)
	solo := h.perspective(cityArc, author, perspectivv1.Genre_GENRE_MYSTERY, perspectivv1.LoreStatus_LORE_STATUS_SOLO, t0.Add(1*time.Hour), strings.Repeat("a", 300))
	removed := h.perspective(storeArc, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_REMOVED, t0.Add(2*time.Hour), "voted out")
	h.perspective(cityArc, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_IN_LORE, t0.Add(3*time.Hour), "in lore")
	h.perspective(storeArc, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0.Add(4*time.Hour), "pending")
	h.perspective(otherArc, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, t0.Add(5*time.Hour), "elsewhere")
	deleted := h.perspective(cityArc, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, t0.Add(6*time.Hour), "deleted")
	h.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, deleted)
	hidden := h.perspective(storeArc, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, t0.Add(7*time.Hour), "hidden")
	h.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)

	res, err := c.GetEpic(ctx, as(uuid.Nil, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_TALES}))
	if err != nil {
		t.Fatalf("GetEpic: %v", err)
	}
	tales := res.Msg.GetTales()
	var ids []string
	for _, tl := range tales {
		ids = append(ids, tl.GetPerspectiveId())
	}
	if want := []string{removed.String(), solo.String()}; !slices.Equal(ids, want) {
		t.Fatalf("tales = %v, want %v", ids, want)
	}
	if len(res.Msg.GetArcs()) != 0 || res.Msg.GetNextPageToken() != "" {
		t.Fatalf("tales response carries arcs or a next page")
	}
	s := tales[1]
	if s.GetArcTitle() != "City arc" || s.GetAuthor().GetUserId() != author.String() || s.GetAuthor().GetUsername() == "" ||
		s.GetGenre() != perspectivv1.Genre_GENRE_MYSTERY || s.GetLoreStatus() != perspectivv1.LoreStatus_LORE_STATUS_SOLO ||
		s.GetPlace().GetId() != h.city.String() || s.GetLocalDate() != "2026-10-01" || len([]rune(s.GetExcerpt())) > 201 || !strings.HasSuffix(s.GetExcerpt(), "…") {
		t.Fatalf("solo tale = %v", s)
	}
	if tales[0].GetLoreStatus() != perspectivv1.LoreStatus_LORE_STATUS_REMOVED || tales[0].GetPlace().GetId() != h.store.String() {
		t.Fatalf("removed tale = %v", tales[0])
	}

	t.Run("paged", func(t *testing.T) {
		r1, err := c.GetEpic(ctx, as(uuid.Nil, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_TALES, PageSize: 1}))
		if err != nil || len(r1.Msg.GetTales()) != 1 || r1.Msg.GetNextPageToken() == "" {
			t.Fatalf("page 1 = %v, %v", r1, err)
		}
		r2, err := c.GetEpic(ctx, as(uuid.Nil, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_TALES, PageSize: 1, PageToken: r1.Msg.GetNextPageToken()}))
		if err != nil || len(r2.Msg.GetTales()) != 1 || r2.Msg.GetTales()[0].GetPerspectiveId() != solo.String() {
			t.Fatalf("page 2 = %v, %v", r2, err)
		}
		// A tales cursor is not a ranked cursor.
		_, err = c.GetEpic(ctx, as(uuid.Nil, &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_RANKED, PageToken: r1.Msg.GetNextPageToken()}))
		wantCode(t, err, connect.CodeInvalidArgument)
	})
}

// TestGetEpicErrors covers the NOT_FOUND and INVALID_ARGUMENT paths.
func TestGetEpicErrors(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	h.hide(h.mall)
	tests := []struct {
		name string
		req  *perspectivv1.GetEpicRequest
		code connect.Code
	}{
		{name: "missing place", req: &perspectivv1.GetEpicRequest{PlaceId: uuid.NewString(), Tab: perspectivv1.EpicTab_EPIC_TAB_RANKED}, code: connect.CodeNotFound},
		{name: "hidden place", req: &perspectivv1.GetEpicRequest{PlaceId: h.mall.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_RANKED}, code: connect.CodeNotFound},
		{name: "under a hidden place", req: &perspectivv1.GetEpicRequest{PlaceId: h.store.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_TALES}, code: connect.CodeNotFound},
		{name: "tab unset", req: &perspectivv1.GetEpicRequest{PlaceId: h.city.String()}, code: connect.CodeInvalidArgument},
		{name: "malformed id", req: &perspectivv1.GetEpicRequest{PlaceId: "x", Tab: perspectivv1.EpicTab_EPIC_TAB_RANKED}, code: connect.CodeInvalidArgument},
		{name: "bad token", req: &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_NEWEST, PageToken: "zzz"}, code: connect.CodeInvalidArgument},
		{name: "token minted elsewhere", req: &perspectivv1.GetEpicRequest{PlaceId: h.city.String(), Tab: perspectivv1.EpicTab_EPIC_TAB_NEWEST, PageToken: apiutil.NextPageToken("not json")}, code: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := c.GetEpic(context.Background(), as(uuid.Nil, tc.req))
			wantCode(t, err, tc.code)
		})
	}
}

// TestFollowPlace covers following: GetPlace tells a signed-in reader whether
// they follow it, ListFollowing lists the visible places followed (newest
// first), unfollowing removes one, and a hidden place cannot be followed and
// drops out of the list.
func TestFollowPlace(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	ctx := context.Background()
	me := h.user(true)
	follow := func(place uuid.UUID, on bool) error {
		res, err := c.FollowPlace(ctx, as(me, &perspectivv1.FollowPlaceRequest{PlaceId: place.String(), On: on}))
		if err == nil && res.Msg.GetFollowing() != on {
			t.Fatalf("following = %v, want %v", res.Msg.GetFollowing(), on)
		}
		return err
	}
	following := func(user, place uuid.UUID) bool {
		t.Helper()
		res, err := c.GetPlace(ctx, as(user, &perspectivv1.GetPlaceRequest{PlaceId: place.String()}))
		if err != nil {
			t.Fatalf("GetPlace: %v", err)
		}
		return res.Msg.GetPlace().GetFollowing()
	}
	list := func() []string {
		t.Helper()
		res, err := c.ListFollowing(ctx, as(me, &perspectivv1.ListFollowingRequest{}))
		if err != nil {
			t.Fatalf("ListFollowing: %v", err)
		}
		return names(res.Msg.GetPlaces())
	}

	for _, p := range []uuid.UUID{h.city, h.store, h.store} { // following twice is a no-op
		if err := follow(p, true); err != nil {
			t.Fatalf("follow: %v", err)
		}
	}
	if !following(me, h.store) || following(uuid.Nil, h.store) || following(h.user(true), h.store) {
		t.Fatal("GetPlace following: want true for the follower only")
	}
	if got := list(); !slices.Equal(got, []string{"Corner Books", "Springfield"}) {
		t.Fatalf("following = %q", got)
	}
	if err := follow(h.city, false); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	if got := list(); !slices.Equal(got, []string{"Corner Books"}) {
		t.Fatalf("after unfollow = %q", got)
	}

	// Hiding the mall above the store hides it from the list, and it cannot
	// be followed again.
	h.hide(h.mall)
	if got := list(); len(got) != 0 {
		t.Fatalf("after hiding = %q", got)
	}
	wantCode(t, follow(h.store, true), connect.CodeNotFound)
	wantCode(t, follow(uuid.New(), true), connect.CodeNotFound)
	_, err := c.FollowPlace(ctx, as(uuid.Nil, &perspectivv1.FollowPlaceRequest{PlaceId: h.city.String(), On: true}))
	wantCode(t, err, connect.CodeUnauthenticated)
}

// searchCharacterIDs runs a character search signed out and returns the
// found character ids in order; never nil.
func searchCharacterIDs(ctx context.Context, c perspectivv1connect.PlaceServiceClient, place uuid.UUID, q string) ([]string, error) {
	res, err := c.SearchCharacters(ctx, as(uuid.Nil, &perspectivv1.SearchCharactersRequest{PlaceId: place.String(), Query: q}))
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, f := range res.Msg.GetCharacters() {
		ids = append(ids, f.GetCharacterId())
	}
	return ids, nil
}

// uuidStrings formats ids; never nil.
func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// TestSearchCharacters covers "were you in someone's story?": unclaimed
// characters at the place and below whose description contains the query,
// ignoring case and matching wildcards literally, most recent day first.
func TestSearchCharacters(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	ctx := context.Background()
	by := h.user(true)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	story := func(placeID uuid.UUID, date string, status perspectivv1.LoreStatus, labels ...string) []uuid.UUID {
		a := h.arc(placeID, date, "Day "+date+" "+uuid.NewString()[:4], by, t0)
		p := h.perspective(a, h.user(true), perspectivv1.Genre_GENRE_DRAMA, status, t0, "text")
		ids := make([]uuid.UUID, len(labels))
		for i, l := range labels {
			ids[i] = h.id(`INSERT INTO characters (perspective_id, label, position) VALUES ($1, $2, $3) RETURNING id`, p, l, i+1)
		}
		return ids
	}
	claimants := h.arc(h.otherCity, "2026-09-01", "Where the claimants wrote", by, t0)
	claim := func(ch uuid.UUID, status string) {
		claimant := h.user(true)
		cp := h.perspective(claimants, claimant, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "x")
		h.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status) VALUES ($1, $2, $3, $4)`, ch, claimant, cp, status)
	}
	older := story(h.store, "2026-10-01", perspectivv1.LoreStatus_LORE_STATUS_PENDING, "the kid in GOLD body paint", "a man with 100% confidence")
	newer := story(h.town, "2026-10-05", perspectivv1.LoreStatus_LORE_STATUS_PENDING, "a woman with gold earrings")
	taken := story(h.city, "2026-10-03", perspectivv1.LoreStatus_LORE_STATUS_PENDING, "the gold-chain bouncer", "the gold scarf", "the gold hat")
	claim(taken[0], "CLAIM_STATUS_ACCEPTED")
	claim(taken[1], "CLAIM_STATUS_PENDING")
	claim(taken[2], "CLAIM_STATUS_REJECTED")
	story(h.city, "2026-10-04", perspectivv1.LoreStatus_LORE_STATUS_REMOVED, "a voted-out gold coat")
	story(h.otherCity, "2026-10-02", perspectivv1.LoreStatus_LORE_STATUS_PENDING, "a gold watch in Chicago")

	search := func(place uuid.UUID, q string) ([]string, error) { return searchCharacterIDs(ctx, c, place, q) }
	tests := []struct {
		name  string
		place uuid.UUID
		query string
		want  []uuid.UUID
		code  connect.Code
	}{
		{name: "case-insensitive, newest day first, rejected claims still open", place: h.city, query: "  Gold  ", want: []uuid.UUID{newer[0], taken[2], older[0]}},
		{name: "only under the place", place: h.mall, query: "gold", want: []uuid.UUID{older[0]}},
		{name: "a percent sign is literal", place: h.city, query: "100%", want: []uuid.UUID{older[1]}},
		{name: "an underscore is literal", place: h.city, query: "g_ld", want: nil},
		{name: "too short", place: h.city, query: "g", code: connect.CodeInvalidArgument},
		{name: "too long", place: h.city, query: strings.Repeat("g", 61), code: connect.CodeInvalidArgument},
		{name: "missing place", place: uuid.New(), query: "gold", code: connect.CodeNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := search(tc.place, tc.query)
			if tc.code != 0 {
				wantCode(t, err, tc.code)
				return
			}
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if want := uuidStrings(tc.want); !slices.Equal(got, want) {
				t.Fatalf("found %v, want %v", got, want)
			}
		})
	}

	t.Run("a word common elsewhere finds the same characters by walking the place", func(t *testing.T) {
		t.Parallel()
		// Enough "gold" labels in another city that the label-first probe
		// gives up; the cases above give the same answer on either path.
		// Ten characters a story, so two hundred writers in one arc.
		crowd := h.arc(h.otherCity, "2026-09-02", "The crowd", by, t0)
		h.exec(`WITH u AS (
				INSERT INTO users (email, email_verified_at, password_hash, username, age_confirmed_at)
				SELECT 'crowd' || n || '@example.test', now(), 'x', 'crowd_' || n, now() FROM generate_series(1, 200) AS n
				RETURNING id),
			p AS (
				INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
				SELECT $1, id, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now() FROM u
				RETURNING id)
			INSERT INTO characters (perspective_id, label, position)
			SELECT p.id, 'a gold extra', n FROM p, generate_series(1, 10) AS n`, crowd)
		got, err := search(h.city, "gold")
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if want := uuidStrings([]uuid.UUID{newer[0], taken[2], older[0]}); !slices.Equal(got, want) {
			t.Fatalf("found %v, want %v", got, want)
		}
	})

	t.Run("a hidden place below drops its stories", func(t *testing.T) {
		t.Parallel()
		// A word no other case searches for: this story is visible under the
		// city until the hide lands, and the cases above run alongside.
		hidden := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Hidden Tin Shop", h.town)
		story(hidden, "2026-10-06", perspectivv1.LoreStatus_LORE_STATUS_PENDING, "the hidden tinsmith")
		h.hide(hidden)
		got, err := search(h.city, "tinsmith")
		if err != nil || len(got) != 0 {
			t.Fatalf("found %v, %v; want nothing", got, err)
		}
	})
}

// Same-named places: the one with stories comes first, with its count, so
// friends searching for one venue pick the same entry.
func TestSearchRanksStoriedPlacesFirst(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	ctx := context.Background()
	quiet := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Twin Stadium", h.mall)
	busy := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "Twin Stadium", h.otherCity)
	by := h.user(true)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	a := h.arc(busy, "2026-10-01", "Game day", by, t0)
	h.perspective(a, by, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "text")
	h.perspective(a, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "text")
	res, err := c.SearchPlaces(ctx, as(uuid.Nil, &perspectivv1.SearchPlacesRequest{Query: "twin stadium"}))
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	got := res.Msg.GetPlaces()
	if len(got) != 2 || got[0].GetId() != busy.String() || got[0].GetStories() != 2 ||
		got[1].GetId() != quiet.String() || got[1].GetStories() != 0 {
		t.Fatalf("results = %v, want the storied place first with 2 stories", got)
	}
}

// Stories under a hidden place are not readable, so a search does not count
// them for the places above it.
func TestSearchCountsSkipHiddenPlaces(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	venue := h.place(perspectivv1.PlaceKind_PLACE_KIND_MALL, "Quiet Arena", h.city)
	stand := h.place(perspectivv1.PlaceKind_PLACE_KIND_STORE, "North Stand", venue)
	by := h.user(true)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	h.perspective(h.arc(stand, "2026-10-01", "Game day", by, t0), by, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_PENDING, t0, "text")
	count := func() int32 {
		t.Helper()
		res, err := c.SearchPlaces(context.Background(), as(uuid.Nil, &perspectivv1.SearchPlacesRequest{Query: "quiet arena"}))
		if err != nil || len(res.Msg.GetPlaces()) != 1 {
			t.Fatalf("SearchPlaces = %v, %v", res, err)
		}
		return res.Msg.GetPlaces()[0].GetStories()
	}
	if n := count(); n != 1 {
		t.Fatalf("stories before hiding = %d, want 1", n)
	}
	h.hide(stand)
	if n := count(); n != 0 {
		t.Fatalf("stories after hiding the stand = %d, want 0", n)
	}
}

// TestFollowingNewThisWeek covers "what's new" for followed places: each
// counts the readable stories others published at it and below in the last
// week, and the places with new stories come first. The follower's own
// stories are not news to them.
func TestFollowingNewThisWeek(t *testing.T) {
	t.Parallel()
	h := newHierarchy(t)
	c := client(t, h.pool)
	ctx := context.Background()
	me := h.user(true)
	for _, p := range []uuid.UUID{h.city, h.otherCity} {
		if _, err := c.FollowPlace(ctx, as(me, &perspectivv1.FollowPlaceRequest{PlaceId: p.String(), On: true})); err != nil {
			t.Fatalf("follow: %v", err)
		}
	}
	now := time.Now()
	by := h.user(true)
	story := func(placeID uuid.UUID, at time.Time) uuid.UUID {
		a := h.arc(placeID, at.Format(time.DateOnly), "Day "+uuid.NewString()[:4], by, at)
		return h.perspective(a, h.user(true), perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, at, "text")
	}
	story(h.store, now.Add(-24*time.Hour))   // below the city: counts
	story(h.city, now.Add(-2*time.Hour))     // at the city: counts
	story(h.city, now.Add(-10*24*time.Hour)) // too old
	hidden := story(h.city, now.Add(-3*24*time.Hour))
	h.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)
	// The follower's own stories, at the city and below it: never counted.
	for i, at := range []uuid.UUID{h.city, h.store} {
		day := now.AddDate(0, 0, -30-i).Format(time.DateOnly) // a day of its own; published now
		mine := h.arc(at, day, "Mine "+uuid.NewString()[:4], me, now)
		h.perspective(mine, me, perspectivv1.Genre_GENRE_DRAMA, perspectivv1.LoreStatus_LORE_STATUS_SOLO, now.Add(-time.Hour), "mine")
	}

	res, err := c.ListFollowing(ctx, as(me, &perspectivv1.ListFollowingRequest{}))
	if err != nil {
		t.Fatalf("ListFollowing: %v", err)
	}
	got := map[string]int32{}
	for _, p := range res.Msg.GetPlaces() {
		got[p.GetName()] = p.GetNewThisWeek()
	}
	// The other city was followed last, so only its zero count keeps it second.
	if n := names(res.Msg.GetPlaces()); !slices.Equal(n, []string{"Springfield", "Chicago"}) || got["Springfield"] != 2 || got["Chicago"] != 0 {
		t.Fatalf("following = %q, new this week = %v; want Springfield 2 first", n, got)
	}

	// A hidden venue's stories stop counting for the city above it.
	h.hide(h.mall)
	res, err = c.ListFollowing(ctx, as(me, &perspectivv1.ListFollowingRequest{}))
	if err != nil {
		t.Fatalf("ListFollowing: %v", err)
	}
	if n := res.Msg.GetPlaces()[0].GetNewThisWeek(); n != 1 {
		t.Fatalf("after hiding the mall, Springfield new = %d, want 1", n)
	}
}
