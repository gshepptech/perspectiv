package place

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// Handler binds PlaceService to the Service.
type Handler struct {
	svc *Service
}

var _ perspectivv1connect.PlaceServiceHandler = (*Handler)(nil)

// NewHandler returns the PlaceService handler. Mount it with
// perspectivv1connect.NewPlaceServiceHandler and the API interceptor chain.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// parseID parses a required UUID field.
func parseID(s, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, apiutil.InvalidArg(field + " is not a valid id")
	}
	return id, nil
}

// parseOptionalID parses a UUID field where "" means unset (uuid.Nil).
func parseOptionalID(s, field string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, nil
	}
	return parseID(s, field)
}

// SearchPlaces is the place typeahead.
func (h *Handler) SearchPlaces(ctx context.Context, req *connect.Request[perspectivv1.SearchPlacesRequest]) (*connect.Response[perspectivv1.SearchPlacesResponse], error) {
	parentID, err := parseOptionalID(req.Msg.GetParentId(), "parent_id")
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	ps, err := h.svc.Search(ctx, req.Msg.GetQuery(), parentID, req.Msg.GetKind())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.SearchPlacesResponse{Places: placesToProto(ps)}), nil
}

// GetPlace returns one place with its ancestors.
func (h *Handler) GetPlace(ctx context.Context, req *connect.Request[perspectivv1.GetPlaceRequest]) (*connect.Response[perspectivv1.GetPlaceResponse], error) {
	id, err := parseID(req.Msg.GetPlaceId(), "place_id")
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	p, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.GetPlaceResponse{Place: placeToProto(p)}), nil
}

// ListChildren is the drill-down: a parent's children, or the states.
func (h *Handler) ListChildren(ctx context.Context, req *connect.Request[perspectivv1.ListChildrenRequest]) (*connect.Response[perspectivv1.ListChildrenResponse], error) {
	parentID, err := parseOptionalID(req.Msg.GetParentId(), "parent_id")
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	page, err := apiutil.ParsePageToken(req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	if req.Msg.GetWithStories() {
		ps, err := h.svc.StoriedChildren(ctx, parentID, page.Limit)
		if err != nil {
			return nil, apiutil.ToConnect(err)
		}
		return connect.NewResponse(&perspectivv1.ListChildrenResponse{Places: placesToProto(ps)}), nil
	}
	ps, next, err := h.svc.ListChildren(ctx, parentID, req.Msg.GetCategory(), page)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ListChildrenResponse{Places: placesToProto(ps), NextPageToken: next}), nil
}

// CreatePlace adds a user MALL or STORE. The route policy requires a signed-in
// caller with a verified email before this runs.
func (h *Handler) CreatePlace(ctx context.Context, req *connect.Request[perspectivv1.CreatePlaceRequest]) (*connect.Response[perspectivv1.CreatePlaceResponse], error) {
	caller, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return nil, apiutil.ToConnect(apiutil.Unauthenticated("sign in required"))
	}
	parentID, err := parseID(req.Msg.GetParentId(), "parent_id")
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	p, err := h.svc.Create(ctx, caller.UserID, req.Msg.GetName(), req.Msg.GetKind(), parentID)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.CreatePlaceResponse{PlaceId: p.ID.String(), Place: placeToProto(p)}), nil
}

// GetEpic lists one page of a place's epic.
func (h *Handler) GetEpic(ctx context.Context, req *connect.Request[perspectivv1.GetEpicRequest]) (*connect.Response[perspectivv1.GetEpicResponse], error) {
	// No place is the front page: every visible place.
	var id uuid.UUID
	if req.Msg.GetPlaceId() != "" {
		var err error
		if id, err = parseID(req.Msg.GetPlaceId(), "place_id"); err != nil {
			return nil, apiutil.ToConnect(err)
		}
	}
	page, err := apiutil.ParsePageToken(req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	epic, err := h.svc.Epic(ctx, EpicRequest{PlaceID: id, Tab: req.Msg.GetTab(), Genre: req.Msg.GetGenre(), Page: page})
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res := &perspectivv1.GetEpicResponse{NextPageToken: epic.Next}
	if epic.Place.ID != uuid.Nil {
		res.Place = placeToProto(epic.Place)
	}
	for _, a := range epic.Arcs {
		res.Arcs = append(res.Arcs, arcToProto(a))
	}
	for _, t := range epic.Tales {
		res.Tales = append(res.Tales, taleToProto(t))
	}
	return connect.NewResponse(res), nil
}

func idString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func refToProto(r Ref) *perspectivv1.PlaceRef {
	return &perspectivv1.PlaceRef{Id: r.ID.String(), Kind: r.Kind, Name: r.Name}
}

func placeToProto(p Place) *perspectivv1.Place {
	out := &perspectivv1.Place{
		Id: p.ID.String(), Kind: p.Kind, Name: p.Name, ParentId: idString(p.ParentID),
		Timezone: p.Timezone, Source: p.Source, UserAdded: p.UserAdded, Category: p.Category,
		Ancestors: make([]*perspectivv1.PlaceRef, len(p.Ancestors)),
	}
	for i, a := range p.Ancestors {
		out.Ancestors[i] = refToProto(a)
	}
	return out
}

func placesToProto(ps []Place) []*perspectivv1.Place {
	out := make([]*perspectivv1.Place, len(ps))
	for i, p := range ps {
		out[i] = placeToProto(p)
	}
	return out
}

const dateLayout = "2006-01-02"

func arcToProto(a EpicArc) *perspectivv1.EpicArc {
	return &perspectivv1.EpicArc{
		ArcId: a.ID.String(), Title: a.Title, Place: refToProto(a.Place),
		LocalDate: a.LocalDate.Format(dateLayout), Hour: a.Hour,
		CanonLoreId: idString(a.CanonLoreID), CanonScore: a.CanonScore,
		MemberCount: a.MemberCount, PerspectiveCount: a.PerspectiveCount,
		CreatedAt: timestamppb.New(a.CreatedAt), City: a.City, Excerpt: a.Excerpt,
	}
}

func taleToProto(t EpicTale) *perspectivv1.EpicTale {
	return &perspectivv1.EpicTale{
		PerspectiveId: t.PerspectiveID.String(), ArcId: t.ArcID.String(), ArcTitle: t.ArcTitle,
		Place: refToProto(t.Place), LocalDate: t.LocalDate.Format(dateLayout), Hour: t.Hour,
		Author: &perspectivv1.UserRef{UserId: t.AuthorID.String(), Username: t.AuthorUsername},
		Genre:  t.Genre, LoreStatus: t.LoreStatus,
		PublishedAt: timestamppb.New(t.PublishedAt), Excerpt: t.Excerpt,
	}
}
