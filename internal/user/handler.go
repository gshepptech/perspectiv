package user

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// Handler binds UserService to the Connect API. It validates requests,
// calls the service and maps errors through apiutil.ToConnect.
type Handler struct {
	svc *Service
}

var _ perspectivv1connect.UserServiceHandler = (*Handler)(nil)

// NewHandler returns the UserService handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// callerID returns the signed-in caller's id. The interceptor already refuses
// an anonymous call to a User route; this keeps the handler fail-closed if a
// route's policy ever changes.
func callerID(ctx context.Context) (uuid.UUID, error) {
	c, ok := apiutil.CallerFrom(ctx)
	if !ok || c.UserID == uuid.Nil {
		return uuid.Nil, apiutil.Unauthenticated("sign in required")
	}
	return c.UserID, nil
}

func parseUserID(field, s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, apiutil.InvalidArg(field + " must be a user id")
	}
	return id, nil
}

// GetProfile is public. An anonymous caller may read any profile but not ask
// for their own.
func (h *Handler) GetProfile(ctx context.Context, req *connect.Request[perspectivv1.GetProfileRequest]) (*connect.Response[perspectivv1.GetProfileResponse], error) {
	m := req.Msg
	if m.GetUserId() != "" && m.GetUsername() != "" {
		return nil, apiutil.ToConnect(apiutil.InvalidArg("give user_id or username, not both"))
	}
	var q ProfileQuery
	if m.GetUserId() != "" {
		id, err := parseUserID("user_id", m.GetUserId())
		if err != nil {
			return nil, apiutil.ToConnect(err)
		}
		q.UserID = id
	}
	q.Username = m.GetUsername()
	var caller *uuid.UUID
	if c, ok := apiutil.CallerFrom(ctx); ok && c.UserID != uuid.Nil {
		caller = &c.UserID
	}
	p, acct, err := h.svc.GetProfile(ctx, caller, q)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.GetProfileResponse{Profile: profileToProto(p), Account: accountToProto(acct)}), nil
}

func (h *Handler) UpdateProfile(ctx context.Context, req *connect.Request[perspectivv1.UpdateProfileRequest]) (*connect.Response[perspectivv1.UpdateProfileResponse], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	m := req.Msg
	p, err := h.svc.UpdateProfile(ctx, caller, ProfileUpdate{
		Bio:               m.GetBio(),
		Avatar:            m.GetAvatar(),
		AvatarContentType: m.GetAvatarContentType(),
		ClearAvatar:       m.GetClearAvatar(),
	})
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.UpdateProfileResponse{Profile: profileToProto(p)}), nil
}

func (h *Handler) ChangeUsername(ctx context.Context, req *connect.Request[perspectivv1.ChangeUsernameRequest]) (*connect.Response[perspectivv1.ChangeUsernameResponse], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	if err := ValidateUsername(req.Msg.GetUsername()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	p, next, err := h.svc.ChangeUsername(ctx, caller, req.Msg.GetUsername())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ChangeUsernameResponse{
		Profile:              profileToProto(p),
		NextUsernameChangeAt: timestamppb.New(next),
	}), nil
}

func (h *Handler) BlockUser(ctx context.Context, req *connect.Request[perspectivv1.BlockUserRequest]) (*connect.Response[perspectivv1.BlockUserResponse], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	target, err := parseUserID("user_id", req.Msg.GetUserId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	if err := h.svc.BlockUser(ctx, caller, target); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.BlockUserResponse{}), nil
}

func (h *Handler) UnblockUser(ctx context.Context, req *connect.Request[perspectivv1.UnblockUserRequest]) (*connect.Response[perspectivv1.UnblockUserResponse], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	target, err := parseUserID("user_id", req.Msg.GetUserId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	if err := h.svc.UnblockUser(ctx, caller, target); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.UnblockUserResponse{}), nil
}

func (h *Handler) DeleteAccount(ctx context.Context, req *connect.Request[perspectivv1.DeleteAccountRequest]) (*connect.Response[perspectivv1.DeleteAccountResponse], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	if req.Msg.GetPassword() == "" {
		return nil, apiutil.ToConnect(apiutil.InvalidArg("password is required to delete the account"))
	}
	if err := h.svc.DeleteAccount(ctx, caller, req.Msg.GetPassword()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.DeleteAccountResponse{}), nil
}

func profileToProto(p Profile) *perspectivv1.Profile {
	out := &perspectivv1.Profile{
		UserId:     p.UserID.String(),
		Username:   p.Username,
		Bio:        p.Bio,
		AvatarUrl:  p.AvatarURL,
		Suspended:  p.Suspended,
		CanonCount: p.CanonCount,
		LoreCount:  p.LoreCount,
		Deleted:    p.Deleted,
	}
	if !p.JoinedAt.IsZero() {
		out.JoinedAt = timestamppb.New(p.JoinedAt)
	}
	return out
}

func accountToProto(a *Account) *perspectivv1.Account {
	if a == nil {
		return nil
	}
	out := &perspectivv1.Account{
		Email:         a.Email,
		EmailVerified: a.EmailVerified,
		Role:          a.Role,
		Blocked:       make([]*perspectivv1.UserRef, 0, len(a.Blocked)),
	}
	if a.UsernameChangedAt != nil {
		out.UsernameChangedAt = timestamppb.New(*a.UsernameChangedAt)
	}
	if a.NextUsernameChangeAt != nil {
		out.NextUsernameChangeAt = timestamppb.New(*a.NextUsernameChangeAt)
	}
	for _, r := range a.Blocked {
		out.Blocked = append(out.Blocked, &perspectivv1.UserRef{UserId: r.UserID.String(), Username: r.Username})
	}
	return out
}

// ExportMyData returns the caller's own data as JSON.
func (h *Handler) ExportMyData(ctx context.Context, _ *connect.Request[perspectivv1.ExportMyDataRequest]) (*connect.Response[perspectivv1.ExportMyDataResponse], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	doc, err := h.svc.ExportMyData(ctx, caller)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ExportMyDataResponse{Json: doc}), nil
}
