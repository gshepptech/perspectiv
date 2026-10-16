package moderation

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// Handler binds ModerationService to the Connect API. It parses requests,
// calls the service and maps errors through apiutil.ToConnect.
type Handler struct {
	svc *Service
}

var _ perspectivv1connect.ModerationServiceHandler = (*Handler)(nil)

// NewHandler returns the ModerationService handler over svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Mount builds the ModerationService HTTP handler; it has the api.Mount shape.
func (h *Handler) Mount(opts ...connect.HandlerOption) (string, http.Handler) {
	return perspectivv1connect.NewModerationServiceHandler(h, opts...)
}

// callerID returns the signed-in caller's id. The interceptor already refuses
// an anonymous call to these routes; this keeps the handler fail-closed if a
// route's policy ever changes.
func callerID(ctx context.Context) (uuid.UUID, error) {
	c, ok := apiutil.CallerFrom(ctx)
	if !ok || c.UserID == uuid.Nil {
		return uuid.Nil, apiutil.Unauthenticated("sign in required")
	}
	return c.UserID, nil
}

func parseID(field, s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, apiutil.InvalidArg(field + " must be an id")
	}
	return id, nil
}

// Report files a report. The route requires a verified email and counts
// against the report rate limit; the interceptor enforces both.
func (h *Handler) Report(ctx context.Context, req *connect.Request[perspectivv1.ReportRequest]) (*connect.Response[perspectivv1.ReportResponse], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	m := req.Msg
	target, err := parseID("target_id", m.GetTargetId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	id, err := h.svc.Report(ctx, caller, ReportInput{
		TargetKind: m.GetTargetKind(),
		TargetID:   target,
		Reason:     m.GetReason(),
		Details:    m.GetDetails(),
	})
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ReportResponse{ReportId: id.String()}), nil
}

// ListReports returns a page of the admin queue. ADMIN only.
func (h *Handler) ListReports(ctx context.Context, req *connect.Request[perspectivv1.ListReportsRequest]) (*connect.Response[perspectivv1.ListReportsResponse], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	page, err := apiutil.ParsePageToken(req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	reports, next, err := h.svc.ListReports(ctx, caller, req.Msg.GetStatus(), page)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ListReportsResponse{Reports: reports, NextPageToken: next}), nil
}

// Resolve applies an action to a report and audits it. ADMIN only.
func (h *Handler) Resolve(ctx context.Context, req *connect.Request[perspectivv1.ResolveRequest]) (*connect.Response[perspectivv1.ResolveResponse], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	id, err := parseID("report_id", req.Msg.GetReportId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	r, err := h.svc.Resolve(ctx, caller, ResolveInput{ReportID: id, Action: req.Msg.GetAction(), Reason: req.Msg.GetReason()})
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ResolveResponse{Report: r}), nil
}
