package vote

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// Handler binds VoteService to the vote Service. It validates every request
// before calling the service.
type Handler struct {
	svc *Service
}

var _ perspectivv1connect.VoteServiceHandler = (*Handler)(nil)

// NewHandler returns the VoteService handler over svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Mount builds the VoteService HTTP handler; it has the api.Mount shape.
func (h *Handler) Mount(opts ...connect.HandlerOption) (string, http.Handler) {
	return perspectivv1connect.NewVoteServiceHandler(h, opts...)
}

// Vote sets or clears the caller's up-vote on a perspective.
func (h *Handler) Vote(ctx context.Context, req *connect.Request[perspectivv1.VoteRequest]) (*connect.Response[perspectivv1.VoteResponse], error) {
	caller, id, err := callerAndTarget(ctx, req.Msg.GetPerspectiveId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res, err := h.svc.Vote(ctx, caller, id, req.Msg.GetOn())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.VoteResponse{Upvotes: res.Upvotes, On: res.On}), nil
}

// VoteRemove casts the caller's remove vote on a perspective in their lore.
func (h *Handler) VoteRemove(ctx context.Context, req *connect.Request[perspectivv1.VoteRemoveRequest]) (*connect.Response[perspectivv1.VoteRemoveResponse], error) {
	caller, id, err := callerAndTarget(ctx, req.Msg.GetPerspectiveId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res, err := h.svc.VoteRemove(ctx, caller, id)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.VoteRemoveResponse{
		RemoveVotes:    res.RemoveVotes,
		EligibleVoters: res.EligibleVoters,
		Removed:        res.Removed,
	}), nil
}

// callerAndTarget returns the signed-in caller and the parsed perspective id.
func callerAndTarget(ctx context.Context, perspectiveID string) (uuid.UUID, uuid.UUID, error) {
	c, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return uuid.Nil, uuid.Nil, apiutil.Unauthenticated("sign in first")
	}
	if perspectiveID == "" {
		return uuid.Nil, uuid.Nil, apiutil.InvalidArg("perspective_id is required")
	}
	id, err := uuid.Parse(perspectiveID)
	if err != nil {
		return uuid.Nil, uuid.Nil, apiutil.InvalidArg("perspective_id is not a valid id")
	}
	return c.UserID, id, nil
}
