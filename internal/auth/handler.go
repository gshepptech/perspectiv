package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// RefreshCookieName is the cookie that carries the refresh token. It is
// HttpOnly, SameSite=Strict and scoped to the auth service's path, so the
// browser sends it only to Refresh and Logout and script never reads it.
const RefreshCookieName = "perspectiv_refresh"

const refreshCookiePath = "/api/" + perspectivv1connect.AuthServiceName + "/"

// Handler binds AuthService to the Connect API. It validates requests,
// calls the service, moves the refresh token between the service and the
// cookie, and maps errors through apiutil.ToConnect.
type Handler struct {
	svc          *Service
	cookieSecure bool
}

var _ perspectivv1connect.AuthServiceHandler = (*Handler)(nil)

// NewHandler returns the AuthService handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc, cookieSecure: svc.cfg.CookieSecure}
}

// refreshCookie is the refresh cookie holding value until expires. Secure
// is on unless the deployment turned it off for plain-HTTP local use.
func (h *Handler) refreshCookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure is config-gated for plain-HTTP development and on by default.
		Name:     RefreshCookieName,
		Value:    value,
		Path:     refreshCookiePath,
		MaxAge:   int(time.Until(expires) / time.Second),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
}

func (h *Handler) clearCookie() *http.Cookie {
	c := h.refreshCookie("", time.Time{}) //nolint:gosec // see refreshCookie
	c.MaxAge = -1
	return c
}

// refreshCookieValue returns the refresh token from the request's cookies,
// or "" when there is none.
func refreshCookieValue(header http.Header) string {
	for _, line := range header.Values("Cookie") {
		cookies, err := http.ParseCookie(line)
		if err != nil {
			continue
		}
		for _, c := range cookies {
			if c.Name == RefreshCookieName {
				return c.Value
			}
		}
	}
	return ""
}

func sessionUser(u SessionUser) *perspectivv1.SessionUser {
	return &perspectivv1.SessionUser{
		UserId: u.ID.String(), Username: u.Username, Role: u.Role, EmailVerified: u.EmailVerified,
	}
}

// SignUp creates an account and queues its verification email.
func (h *Handler) SignUp(ctx context.Context, req *connect.Request[perspectivv1.SignUpRequest]) (*connect.Response[perspectivv1.SignUpResponse], error) {
	m := req.Msg
	id, err := h.svc.SignUp(ctx, SignUpInput{
		Email: m.GetEmail(), Password: m.GetPassword(), Username: m.GetUsername(), AgeConfirmed: m.GetAgeConfirmed(),
		AccessCode: m.GetAccessCode(), Honeypot: m.GetWebsite(),
	})
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.SignUpResponse{UserId: id.String()}), nil
}

// Login returns the access token in the body and sets the refresh cookie.
func (h *Handler) Login(ctx context.Context, req *connect.Request[perspectivv1.LoginRequest]) (*connect.Response[perspectivv1.LoginResponse], error) {
	if req.Msg.GetEmail() == "" || req.Msg.GetPassword() == "" {
		return nil, apiutil.ToConnect(errBadCredentials)
	}
	t, err := h.svc.Login(ctx, req.Msg.GetEmail(), req.Msg.GetPassword(), req.Header().Get("User-Agent"))
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res := connect.NewResponse(&perspectivv1.LoginResponse{
		AccessToken:          t.AccessToken,
		AccessTokenExpiresAt: timestamppb.New(t.AccessExpiresAt),
		User:                 sessionUser(t.User),
	})
	res.Header().Add("Set-Cookie", h.refreshCookie(t.RefreshToken, t.RefreshExpiresAt).String())
	return res, nil
}

// Logout revokes the session and clears the cookie, whatever was presented.
func (h *Handler) Logout(ctx context.Context, req *connect.Request[perspectivv1.LogoutRequest]) (*connect.Response[perspectivv1.LogoutResponse], error) {
	sid := uuid.Nil
	if c, ok := apiutil.CallerFrom(ctx); ok {
		sid = c.SessionID
	}
	if err := h.svc.Logout(ctx, refreshCookieValue(req.Header()), sid); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res := connect.NewResponse(&perspectivv1.LogoutResponse{})
	res.Header().Add("Set-Cookie", h.clearCookie().String())
	return res, nil
}

// Refresh rotates the refresh cookie and returns a new access token. A
// refused refresh clears the cookie, since it can never work again.
func (h *Handler) Refresh(ctx context.Context, req *connect.Request[perspectivv1.RefreshRequest]) (*connect.Response[perspectivv1.RefreshResponse], error) {
	t, err := h.svc.Refresh(ctx, refreshCookieValue(req.Header()))
	if err != nil {
		cerr := apiutil.ToConnect(err)
		var ce *connect.Error
		if errors.As(cerr, &ce) && ce.Code() == connect.CodeUnauthenticated {
			ce.Meta().Add("Set-Cookie", h.clearCookie().String())
		}
		return nil, cerr
	}
	res := connect.NewResponse(&perspectivv1.RefreshResponse{
		AccessToken:          t.AccessToken,
		AccessTokenExpiresAt: timestamppb.New(t.AccessExpiresAt),
		User:                 sessionUser(t.User),
	})
	res.Header().Add("Set-Cookie", h.refreshCookie(t.RefreshToken, t.RefreshExpiresAt).String())
	return res, nil
}

// VerifyEmail follows a verification link.
func (h *Handler) VerifyEmail(ctx context.Context, req *connect.Request[perspectivv1.VerifyEmailRequest]) (*connect.Response[perspectivv1.VerifyEmailResponse], error) {
	if err := h.svc.VerifyEmail(ctx, req.Msg.GetToken()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.VerifyEmailResponse{}), nil
}

// RequestPasswordReset always answers OK; see Service.RequestPasswordReset.
func (h *Handler) RequestPasswordReset(ctx context.Context, req *connect.Request[perspectivv1.RequestPasswordResetRequest]) (*connect.Response[perspectivv1.RequestPasswordResetResponse], error) {
	if err := h.svc.RequestPasswordReset(ctx, req.Msg.GetEmail()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.RequestPasswordResetResponse{}), nil
}

// ResetPassword follows a reset link with the new password.
func (h *Handler) ResetPassword(ctx context.Context, req *connect.Request[perspectivv1.ResetPasswordRequest]) (*connect.Response[perspectivv1.ResetPasswordResponse], error) {
	if err := h.svc.ResetPassword(ctx, req.Msg.GetToken(), req.Msg.GetNewPassword()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ResetPasswordResponse{}), nil
}

// callerOrDeny is the signed-in caller. The policy table already refuses an
// anonymous call to these methods; this keeps a misrouted one closed too.
func callerOrDeny(ctx context.Context) (apiutil.Caller, error) {
	c, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return apiutil.Caller{}, apiutil.ToConnect(apiutil.Unauthenticated("sign in first"))
	}
	return c, nil
}

// ChangePassword sets a new password and signs out every other device.
func (h *Handler) ChangePassword(ctx context.Context, req *connect.Request[perspectivv1.ChangePasswordRequest]) (*connect.Response[perspectivv1.ChangePasswordResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.ChangePassword(ctx, c, req.Msg.GetCurrentPassword(), req.Msg.GetNewPassword()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ChangePasswordResponse{}), nil
}

// RequestEmailChange emails a confirmation link to the new address.
func (h *Handler) RequestEmailChange(ctx context.Context, req *connect.Request[perspectivv1.RequestEmailChangeRequest]) (*connect.Response[perspectivv1.RequestEmailChangeResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.RequestEmailChange(ctx, c, req.Msg.GetNewEmail(), req.Msg.GetCurrentPassword()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.RequestEmailChangeResponse{}), nil
}

// ConfirmEmailChange follows the link sent to the new address.
func (h *Handler) ConfirmEmailChange(ctx context.Context, req *connect.Request[perspectivv1.ConfirmEmailChangeRequest]) (*connect.Response[perspectivv1.ConfirmEmailChangeResponse], error) {
	if err := h.svc.ConfirmEmailChange(ctx, req.Msg.GetToken()); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ConfirmEmailChangeResponse{}), nil
}

// SignOutEverywhere revokes every session and clears this browser's cookie.
func (h *Handler) SignOutEverywhere(ctx context.Context, _ *connect.Request[perspectivv1.SignOutEverywhereRequest]) (*connect.Response[perspectivv1.SignOutEverywhereResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.SignOutEverywhere(ctx, c); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res := connect.NewResponse(&perspectivv1.SignOutEverywhereResponse{})
	res.Header().Add("Set-Cookie", h.clearCookie().String())
	return res, nil
}

// ListSessions lists the caller's signed-in devices.
func (h *Handler) ListSessions(ctx context.Context, _ *connect.Request[perspectivv1.ListSessionsRequest]) (*connect.Response[perspectivv1.ListSessionsResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := h.svc.ListSessions(ctx, c)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	res := &perspectivv1.ListSessionsResponse{Sessions: make([]*perspectivv1.SessionInfo, len(sessions))}
	for i, s := range sessions {
		res.Sessions[i] = &perspectivv1.SessionInfo{
			SessionId: s.ID.String(), Device: s.Device, SignedInAt: timestamppb.New(s.SignedInAt),
			LastUsedAt: timestamppb.New(s.LastUsedAt), Current: s.Current,
		}
	}
	return connect.NewResponse(res), nil
}

// RevokeSession signs out one of the caller's other devices.
func (h *Handler) RevokeSession(ctx context.Context, req *connect.Request[perspectivv1.RevokeSessionRequest]) (*connect.Response[perspectivv1.RevokeSessionResponse], error) {
	c, err := callerOrDeny(ctx)
	if err != nil {
		return nil, err
	}
	sid, err := uuid.Parse(req.Msg.GetSessionId())
	if err != nil {
		return nil, apiutil.ToConnect(apiutil.InvalidArg("session_id must be an id"))
	}
	if err := h.svc.RevokeSession(ctx, c, sid); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.RevokeSessionResponse{}), nil
}
