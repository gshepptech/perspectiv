package arc

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// Handler binds ArcService to the arc Service.
type Handler struct {
	svc *Service
}

var _ perspectivv1connect.ArcServiceHandler = (*Handler)(nil)

// NewHandler returns the ArcService handler over svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Mount builds the ArcService HTTP handler; it has the api.Mount shape.
func (h *Handler) Mount(opts ...connect.HandlerOption) (string, http.Handler) {
	return perspectivv1connect.NewArcServiceHandler(h, opts...)
}

// GetArc returns an arc page. It is public.
func (h *Handler) GetArc(ctx context.Context, req *connect.Request[perspectivv1.GetArcRequest]) (*connect.Response[perspectivv1.GetArcResponse], error) {
	id, err := parseArcID(req.Msg.GetArcId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	viewer := uuid.Nil
	if c, ok := apiutil.CallerFrom(ctx); ok {
		viewer = c.UserID
	}
	resp, err := h.svc.GetArc(ctx, viewer, id, req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(resp), nil
}

// ListArcSection returns the next page of one list on an arc page. It is
// public.
func (h *Handler) ListArcSection(ctx context.Context, req *connect.Request[perspectivv1.ListArcSectionRequest]) (*connect.Response[perspectivv1.ListArcSectionResponse], error) {
	id, err := parseArcID(req.Msg.GetArcId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	viewer := uuid.Nil
	if c, ok := apiutil.CallerFrom(ctx); ok {
		viewer = c.UserID
	}
	resp, err := h.svc.ListArcSection(ctx, viewer, id, req.Msg.GetSection(), req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(resp), nil
}

// ListOpenCharacters lists the arc's unclaimed characters. It is public.
func (h *Handler) ListOpenCharacters(ctx context.Context, req *connect.Request[perspectivv1.ListOpenCharactersRequest]) (*connect.Response[perspectivv1.ListOpenCharactersResponse], error) {
	id, err := parseArcID(req.Msg.GetArcId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	chars, err := h.svc.ListOpenCharacters(ctx, id)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ListOpenCharactersResponse{Characters: chars}), nil
}

// RetitleHourArc retitles an hour arc for its creator.
func (h *Handler) RetitleHourArc(ctx context.Context, req *connect.Request[perspectivv1.RetitleHourArcRequest]) (*connect.Response[perspectivv1.RetitleHourArcResponse], error) {
	caller, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return nil, apiutil.ToConnect(apiutil.Unauthenticated("sign in to retitle an arc"))
	}
	id, err := parseArcID(req.Msg.GetArcId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	arc, err := h.svc.RetitleHourArc(ctx, caller.UserID, id, req.Msg.GetTitle())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.RetitleHourArcResponse{Arc: arc}), nil
}

func parseArcID(s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, apiutil.InvalidArg("arc_id is required")
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, apiutil.InvalidArg("arc_id is not a valid id")
	}
	return id, nil
}

// FindDayArc says what already exists for a place on a day. It is public.
func (h *Handler) FindDayArc(ctx context.Context, req *connect.Request[perspectivv1.FindDayArcRequest]) (*connect.Response[perspectivv1.FindDayArcResponse], error) {
	placeID, err := uuid.Parse(req.Msg.GetPlaceId())
	if err != nil {
		return nil, apiutil.ToConnect(apiutil.InvalidArg("place_id is not a valid id"))
	}
	day, err := time.Parse(dateLayout, req.Msg.GetLocalDate())
	if err != nil {
		return nil, apiutil.ToConnect(apiutil.InvalidArg("local_date must be YYYY-MM-DD"))
	}
	resp, err := h.svc.FindDayArc(ctx, placeID, day)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(resp), nil
}
