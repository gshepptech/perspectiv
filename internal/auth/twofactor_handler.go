package auth

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
)

// CompleteTwoFactorLogin finishes a sign-in that Login answered with a
// challenge: the access token in the body and the refresh cookie set, as
// Login does for an account without two-factor sign-in.
func (h *Handler) CompleteTwoFactorLogin(ctx context.Context, req *connect.Request[perspectivv1.CompleteTwoFactorLoginRequest],
) (*connect.Response[perspectivv1.CompleteTwoFactorLoginResponse], error) {
	if req.Msg.GetChallenge() == "" {
		return nil, apiutil.ToConnect(errChallengeEnded)
	}
	r, err := h.svc.CompleteTwoFactorLogin(ctx, req.Msg.GetChallenge(), req.Msg.GetCode(), req.Header().Get("User-Agent"))
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	t := r.Tokens
	res := connect.NewResponse(&perspectivv1.CompleteTwoFactorLoginResponse{
		AccessToken:          t.AccessToken,
		AccessTokenExpiresAt: timestamppb.New(t.AccessExpiresAt),
		User:                 sessionUser(t.User),
		UsedRecoveryCode:     r.UsedRecoveryCode,
		RecoveryCodesLeft:    r.CodesLeft,
	})
	res.Header().Add("Set-Cookie", h.refreshCookie(t.RefreshToken, t.RefreshExpiresAt).String())
	return res, nil
}

// GetTwoFactorStatus says whether the caller has two-factor sign-in on.
func (h *Handler) GetTwoFactorStatus(ctx context.Context, _ *connect.Request[perspectivv1.GetTwoFactorStatusRequest],
) (*connect.Response[perspectivv1.GetTwoFactorStatusResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.svc.GetTwoFactorStatus(ctx, c)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res := &perspectivv1.GetTwoFactorStatusResponse{Enabled: st.Enabled, RecoveryCodesLeft: st.CodesLeft}
	if st.Enabled {
		res.EnabledAt = timestamppb.New(st.EnabledAt)
	}
	return connect.NewResponse(res), nil
}

// BeginTwoFactorSetup issues a new secret for the caller's authenticator app.
func (h *Handler) BeginTwoFactorSetup(ctx context.Context, req *connect.Request[perspectivv1.BeginTwoFactorSetupRequest],
) (*connect.Response[perspectivv1.BeginTwoFactorSetupResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	setup, err := h.svc.BeginTwoFactorSetup(ctx, c, req.Msg.GetCurrentPassword())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.BeginTwoFactorSetupResponse{Secret: setup.Secret, OtpauthUri: setup.URI}), nil
}

// ConfirmTwoFactorSetup turns two-factor sign-in on and returns the
// recovery codes.
func (h *Handler) ConfirmTwoFactorSetup(ctx context.Context, req *connect.Request[perspectivv1.ConfirmTwoFactorSetupRequest],
) (*connect.Response[perspectivv1.ConfirmTwoFactorSetupResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := h.svc.ConfirmTwoFactorSetup(ctx, c, req.Msg.GetCode())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ConfirmTwoFactorSetupResponse{RecoveryCodes: codes}), nil
}

// DisableTwoFactor turns two-factor sign-in off.
func (h *Handler) DisableTwoFactor(ctx context.Context, req *connect.Request[perspectivv1.DisableTwoFactorRequest],
) (*connect.Response[perspectivv1.DisableTwoFactorResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DisableTwoFactor(ctx, c, req.Msg.GetCurrentPassword(), req.Msg.GetCode()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.DisableTwoFactorResponse{}), nil
}

// RegenerateRecoveryCodes replaces the caller's recovery codes.
func (h *Handler) RegenerateRecoveryCodes(ctx context.Context, req *connect.Request[perspectivv1.RegenerateRecoveryCodesRequest],
) (*connect.Response[perspectivv1.RegenerateRecoveryCodesResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := h.svc.RegenerateRecoveryCodes(ctx, c, req.Msg.GetCurrentPassword(), req.Msg.GetCode())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.RegenerateRecoveryCodesResponse{RecoveryCodes: codes}), nil
}
