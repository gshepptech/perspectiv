package notify

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// Handler binds NotifyService to the Connect API.
type Handler struct {
	svc *Service
}

var _ perspectivv1connect.NotifyServiceHandler = (*Handler)(nil)

// NewHandler returns the NotifyService handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// callerID returns the signed-in caller. The interceptor already refuses an
// anonymous call to these User routes; this keeps the handler fail-closed.
func callerID(ctx context.Context) (uuid.UUID, error) {
	c, ok := apiutil.CallerFrom(ctx)
	if !ok || c.UserID == uuid.Nil {
		return uuid.Nil, apiutil.Unauthenticated("sign in required")
	}
	return c.UserID, nil
}

func (h *Handler) ListInbox(ctx context.Context, req *connect.Request[perspectivv1.ListInboxRequest]) (*connect.Response[perspectivv1.ListInboxResponse], error) {
	me, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	m := req.Msg
	page, err := h.svc.ListInbox(ctx, me, m.GetPageToken(), m.GetPageSize(), m.GetUnreadOnly())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	resp := &perspectivv1.ListInboxResponse{NextPageToken: page.NextPageToken, UnreadCount: page.UnreadCount}
	for _, n := range page.Notifications {
		resp.Notifications = append(resp.Notifications, notificationToProto(n))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) MarkRead(ctx context.Context, req *connect.Request[perspectivv1.MarkReadRequest]) (*connect.Response[perspectivv1.MarkReadResponse], error) {
	me, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	m := req.Msg
	var ids []uuid.UUID
	if !m.GetAll() {
		raw := m.GetNotificationIds()
		if len(raw) == 0 {
			return nil, apiutil.ToConnect(apiutil.InvalidArg("name the notifications to mark read, or set all"))
		}
		if len(raw) > maxMarkRead {
			return nil, apiutil.ToConnect(apiutil.InvalidArg("at most 100 notifications at a time"))
		}
		ids = make([]uuid.UUID, 0, len(raw))
		for _, s := range raw {
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, apiutil.ToConnect(apiutil.InvalidArg("notification_ids must be notification ids"))
			}
			ids = append(ids, id)
		}
	}
	unread, err := h.svc.MarkRead(ctx, me, ids, m.GetAll())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.MarkReadResponse{UnreadCount: unread}), nil
}

func (h *Handler) GetPreferences(ctx context.Context, _ *connect.Request[perspectivv1.GetPreferencesRequest]) (*connect.Response[perspectivv1.GetPreferencesResponse], error) {
	me, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	prefs, err := h.svc.Preferences(ctx, me)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.GetPreferencesResponse{Preferences: preferencesToProto(prefs)}), nil
}

func (h *Handler) UpdatePreferences(ctx context.Context, req *connect.Request[perspectivv1.UpdatePreferencesRequest]) (*connect.Response[perspectivv1.UpdatePreferencesResponse], error) {
	me, err := callerID(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	in := make([]Preference, 0, len(req.Msg.GetPreferences()))
	for _, p := range req.Msg.GetPreferences() {
		in = append(in, Preference{Kind: p.GetKind(), InApp: p.GetInApp(), Email: p.GetEmail()})
	}
	prefs, err := h.svc.UpdatePreferences(ctx, me, in)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.UpdatePreferencesResponse{Preferences: preferencesToProto(prefs)}), nil
}

func notificationToProto(n Notification) *perspectivv1.Notification {
	out := &perspectivv1.Notification{
		Id:        n.ID.String(),
		Kind:      n.Kind,
		ArcTitle:  n.ArcTitle,
		CreatedAt: timestamppb.New(n.CreatedAt),
	}
	p := n.Payload
	if p.ActorID != nil {
		out.Actor = &perspectivv1.UserRef{UserId: p.ActorID.String(), Username: n.ActorUsername}
	}
	out.ArcId = idString(p.ArcID)
	out.PerspectiveId = idString(p.PerspectiveID)
	out.ClaimId = idString(p.ClaimID)
	out.LoreId = idString(p.LoreID)
	out.ReportId = idString(p.ReportID)
	if n.ReadAt != nil {
		out.ReadAt = timestamppb.New(*n.ReadAt)
	}
	return out
}

func idString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func preferencesToProto(prefs []Preference) []*perspectivv1.Preference {
	out := make([]*perspectivv1.Preference, 0, len(prefs))
	for _, p := range prefs {
		out = append(out, &perspectivv1.Preference{Kind: p.Kind, InApp: p.InApp, Email: p.Email})
	}
	return out
}
