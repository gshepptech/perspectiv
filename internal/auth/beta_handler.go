package auth

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/auth/dbq"
)

// GetSignUpInfo says whether signing up needs an access code.
func (h *Handler) GetSignUpInfo(_ context.Context, _ *connect.Request[perspectivv1.GetSignUpInfoRequest]) (*connect.Response[perspectivv1.GetSignUpInfoResponse], error) {
	return connect.NewResponse(&perspectivv1.GetSignUpInfoResponse{InviteOnly: h.svc.InviteOnly()}), nil
}

// JoinWaitlist asks for a place in the beta.
func (h *Handler) JoinWaitlist(ctx context.Context, req *connect.Request[perspectivv1.JoinWaitlistRequest]) (*connect.Response[perspectivv1.JoinWaitlistResponse], error) {
	if err := h.svc.JoinWaitlist(ctx, req.Msg.GetEmail(), req.Msg.GetNote()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.JoinWaitlistResponse{}), nil
}

// admin returns the signed-in caller of an admin call.
func admin(ctx context.Context) (uuid.UUID, error) {
	c, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return uuid.Nil, apiutil.Unauthenticated("sign in")
	}
	return c.UserID, nil
}

func tsOrNil(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func accessCodeProto(code, note string, maxUses, uses int32, created time.Time, expires, revoked *time.Time) *perspectivv1.AccessCode {
	return &perspectivv1.AccessCode{
		Code: code, Note: note, MaxUses: maxUses, Uses: uses,
		CreatedAt: timestamppb.New(created), ExpiresAt: tsOrNil(expires), Revoked: revoked != nil,
	}
}

// maskEmail keeps the first two characters of the local part and the
// domain: "pat@example.test" is "pa•••@example.test".
func maskEmail(addr string) string {
	local, domain, ok := strings.Cut(addr, "@")
	if !ok {
		return "•••"
	}
	keep := []rune(local)
	if len(keep) > 2 {
		keep = keep[:2]
	}
	return string(keep) + "•••@" + domain
}

func waitlistProto(w dbq.Waitlist) *perspectivv1.WaitlistEntry {
	out := &perspectivv1.WaitlistEntry{
		Id: w.ID.String(), EmailHint: maskEmail(w.Email), Note: w.Note, CreatedAt: timestamppb.New(w.CreatedAt), InvitedAt: tsOrNil(w.InvitedAt),
	}
	if w.Code != nil {
		out.Code = *w.Code
	}
	return out
}

// CreateAccessCode issues a beta code.
func (h *Handler) CreateAccessCode(ctx context.Context, req *connect.Request[perspectivv1.CreateAccessCodeRequest]) (*connect.Response[perspectivv1.CreateAccessCodeResponse], error) {
	caller, err := admin(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	m := req.Msg
	r, err := h.svc.CreateAccessCode(ctx, caller, AccessCodeInput{
		Code: m.GetCode(), Note: m.GetNote(), MaxUses: m.GetMaxUses(), ExpiresInDays: m.GetExpiresInDays(),
	})
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.CreateAccessCodeResponse{
		Code: accessCodeProto(r.Code, r.Note, r.MaxUses, r.Uses, r.CreatedAt, r.ExpiresAt, r.RevokedAt),
	}), nil
}

// ListAccessCodes pages the beta codes.
func (h *Handler) ListAccessCodes(ctx context.Context, req *connect.Request[perspectivv1.ListAccessCodesRequest]) (*connect.Response[perspectivv1.ListAccessCodesResponse], error) {
	caller, err := admin(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	page, err := apiutil.ParsePageToken(req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	rows, next, err := h.svc.ListAccessCodes(ctx, caller, page)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res := &perspectivv1.ListAccessCodesResponse{NextPageToken: next}
	for _, r := range rows {
		res.Codes = append(res.Codes, accessCodeProto(r.Code, r.Note, r.MaxUses, r.Uses, r.CreatedAt, r.ExpiresAt, r.RevokedAt))
	}
	return connect.NewResponse(res), nil
}

// RevokeAccessCode stops a code admitting anyone more.
func (h *Handler) RevokeAccessCode(ctx context.Context, req *connect.Request[perspectivv1.RevokeAccessCodeRequest]) (*connect.Response[perspectivv1.RevokeAccessCodeResponse], error) {
	caller, err := admin(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	r, err := h.svc.RevokeAccessCode(ctx, caller, req.Msg.GetCode())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.RevokeAccessCodeResponse{
		Code: accessCodeProto(r.Code, r.Note, r.MaxUses, r.Uses, r.CreatedAt, r.ExpiresAt, r.RevokedAt),
	}), nil
}

// ListWaitlist pages the waitlist.
func (h *Handler) ListWaitlist(ctx context.Context, req *connect.Request[perspectivv1.ListWaitlistRequest]) (*connect.Response[perspectivv1.ListWaitlistResponse], error) {
	caller, err := admin(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	page, err := apiutil.ParsePageToken(req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	rows, next, err := h.svc.ListWaitlist(ctx, caller, req.Msg.GetWaitingOnly(), page)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res := &perspectivv1.ListWaitlistResponse{NextPageToken: next}
	for _, r := range rows {
		res.Entries = append(res.Entries, waitlistProto(r))
	}
	return connect.NewResponse(res), nil
}

// InviteFromWaitlist emails a waiting person a single-use code.
func (h *Handler) InviteFromWaitlist(ctx context.Context, req *connect.Request[perspectivv1.InviteFromWaitlistRequest]) (*connect.Response[perspectivv1.InviteFromWaitlistResponse], error) {
	caller, err := admin(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, apiutil.ToConnect(apiutil.InvalidArg("id is not a valid id"))
	}
	w, err := h.svc.InviteFromWaitlist(ctx, caller, id)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.InviteFromWaitlistResponse{Entry: waitlistProto(w)}), nil
}
