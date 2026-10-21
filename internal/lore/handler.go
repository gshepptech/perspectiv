package lore

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// Handler binds LoreService to the lore Service.
type Handler struct {
	svc *Service
}

var _ perspectivv1connect.LoreServiceHandler = (*Handler)(nil)

// NewHandler returns the LoreService handler over svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Mount builds the LoreService HTTP handler; it has the api.Mount shape.
func (h *Handler) Mount(opts ...connect.HandlerOption) (string, http.Handler) {
	return perspectivv1connect.NewLoreServiceHandler(h, opts...)
}

// ClaimCharacter opens a claim by the caller on another author's character.
func (h *Handler) ClaimCharacter(
	ctx context.Context, req *connect.Request[perspectivv1.ClaimCharacterRequest],
) (*connect.Response[perspectivv1.ClaimCharacterResponse], error) {
	caller, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return nil, apiutil.ToConnect(apiutil.Unauthenticated("sign in to claim a character"))
	}
	id, err := parseID("character_id", req.Msg.GetCharacterId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	claimID, err := h.svc.ClaimCharacter(ctx, caller.UserID, id)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ClaimCharacterResponse{
		ClaimId: claimID.String(),
		Status:  perspectivv1.ClaimStatus_CLAIM_STATUS_PENDING,
	}), nil
}

// ResolveClaim accepts or rejects a claim on one of the caller's characters.
func (h *Handler) ResolveClaim(
	ctx context.Context, req *connect.Request[perspectivv1.ResolveClaimRequest],
) (*connect.Response[perspectivv1.ResolveClaimResponse], error) {
	caller, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return nil, apiutil.ToConnect(apiutil.Unauthenticated("sign in to resolve a claim"))
	}
	id, err := parseID("claim_id", req.Msg.GetClaimId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	status, err := h.svc.ResolveClaim(ctx, caller.UserID, id, req.Msg.GetAccept())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ResolveClaimResponse{Status: status}), nil
}

// ListClaims lists the caller's incoming or outgoing claims.
func (h *Handler) ListClaims(
	ctx context.Context, req *connect.Request[perspectivv1.ListClaimsRequest],
) (*connect.Response[perspectivv1.ListClaimsResponse], error) {
	caller, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return nil, apiutil.ToConnect(apiutil.Unauthenticated("sign in to see your claims"))
	}
	page, err := apiutil.ParsePageToken(req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	claims, next, err := h.svc.ListClaims(ctx, caller.UserID, req.Msg.GetDirection(), req.Msg.GetStatus(), page)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ListClaimsResponse{Claims: claims, NextPageToken: next}), nil
}

// GetLore returns one lore with its members and encounters. It is public.
func (h *Handler) GetLore(
	ctx context.Context, req *connect.Request[perspectivv1.GetLoreRequest],
) (*connect.Response[perspectivv1.GetLoreResponse], error) {
	id, err := parseID("lore_id", req.Msg.GetLoreId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	l, err := h.svc.GetLore(ctx, id)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.GetLoreResponse{Lore: l}), nil
}

// ListLorePosts returns a lore's thread, oldest first. It is public.
func (h *Handler) ListLorePosts(
	ctx context.Context, req *connect.Request[perspectivv1.ListLorePostsRequest],
) (*connect.Response[perspectivv1.ListLorePostsResponse], error) {
	id, err := parseID("lore_id", req.Msg.GetLoreId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	posts, next, err := h.svc.LorePosts(ctx, id, req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ListLorePostsResponse{Posts: posts, NextPageToken: next}), nil
}

// AddLorePost posts to a lore's thread.
func (h *Handler) AddLorePost(
	ctx context.Context, req *connect.Request[perspectivv1.AddLorePostRequest],
) (*connect.Response[perspectivv1.AddLorePostResponse], error) {
	caller, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return nil, apiutil.ToConnect(apiutil.Unauthenticated("sign in to post"))
	}
	id, err := parseID("lore_id", req.Msg.GetLoreId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	post, err := h.svc.AddLorePost(ctx, caller.UserID, id, req.Msg.GetBody())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.AddLorePostResponse{Post: post}), nil
}

// NameLore names a lore; the caller must be one of its members.
func (h *Handler) NameLore(
	ctx context.Context, req *connect.Request[perspectivv1.NameLoreRequest],
) (*connect.Response[perspectivv1.NameLoreResponse], error) {
	caller, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return nil, apiutil.ToConnect(apiutil.Unauthenticated("sign in to name a lore"))
	}
	id, err := parseID("lore_id", req.Msg.GetLoreId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	name, err := h.svc.NameLore(ctx, caller.UserID, id, req.Msg.GetName())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.NameLoreResponse{Name: name}), nil
}

func parseID(field, s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, apiutil.InvalidArg(field + " is required")
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, apiutil.InvalidArg(field + " is not a valid id")
	}
	return id, nil
}
