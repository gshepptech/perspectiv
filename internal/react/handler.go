package react

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// Handler binds ReactService to the react Service. It validates every
// request before calling the service.
type Handler struct {
	svc *Service
}

var _ perspectivv1connect.ReactServiceHandler = (*Handler)(nil)

// NewHandler returns the ReactService handler over svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Mount builds the ReactService HTTP handler; it has the api.Mount shape.
func (h *Handler) Mount(opts ...connect.HandlerOption) (string, http.Handler) {
	return perspectivv1connect.NewReactServiceHandler(h, opts...)
}

// React adds or clears the caller's emoji on a perspective.
func (h *Handler) React(ctx context.Context, req *connect.Request[perspectivv1.ReactRequest]) (*connect.Response[perspectivv1.ReactResponse], error) {
	c, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return nil, apiutil.ToConnect(apiutil.Unauthenticated("sign in first"))
	}
	id, err := parseID(req.Msg.GetPerspectiveId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	if _, err := emojiToDB(req.Msg.GetEmoji()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	counts, err := h.svc.React(ctx, c.UserID, id, req.Msg.GetEmoji(), req.Msg.GetOn())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ReactResponse{Counts: counts}), nil
}

func parseID(s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, apiutil.InvalidArg("perspective_id is required")
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, apiutil.InvalidArg("perspective_id is not a valid id")
	}
	return id, nil
}
