package perspective

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
)

// Handler binds PerspectiveService to the perspective Service. It validates
// every request before calling the service.
type Handler struct {
	svc *Service
}

var _ perspectivv1connect.PerspectiveServiceHandler = (*Handler)(nil)

// NewHandler returns the PerspectiveService handler over svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Mount builds the PerspectiveService HTTP handler; it has the api.Mount shape.
func (h *Handler) Mount(opts ...connect.HandlerOption) (string, http.Handler) {
	return perspectivv1connect.NewPerspectiveServiceHandler(h, opts...)
}

// SaveDraft creates or replaces the caller's draft.
func (h *Handler) SaveDraft(ctx context.Context, req *connect.Request[perspectivv1.SaveDraftRequest]) (*connect.Response[perspectivv1.SaveDraftResponse], error) {
	caller, err := signedIn(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	in, err := draftInput(req.Msg)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	p, err := h.svc.SaveDraft(ctx, caller, in)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.SaveDraftResponse{Perspective: p}), nil
}

// Publish files the perspective into its arc.
func (h *Handler) Publish(ctx context.Context, req *connect.Request[perspectivv1.PublishRequest]) (*connect.Response[perspectivv1.PublishResponse], error) {
	caller, err := signedIn(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	in, err := publishInput(req.Msg)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	pid, aid, err := h.svc.Publish(ctx, caller, in)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.PublishResponse{PerspectiveId: pid.String(), ArcId: aid.String()}), nil
}

// Update edits a published perspective.
func (h *Handler) Update(ctx context.Context, req *connect.Request[perspectivv1.PerspectiveServiceUpdateRequest]) (*connect.Response[perspectivv1.PerspectiveServiceUpdateResponse], error) {
	caller, err := signedIn(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	in, err := updateInput(req.Msg)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	p, err := h.svc.Update(ctx, caller, in)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.PerspectiveServiceUpdateResponse{Perspective: p}), nil
}

// Delete soft-deletes the caller's perspective.
func (h *Handler) Delete(ctx context.Context, req *connect.Request[perspectivv1.PerspectiveServiceDeleteRequest]) (*connect.Response[perspectivv1.PerspectiveServiceDeleteResponse], error) {
	caller, err := signedIn(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	id, err := parseID("perspective_id", req.Msg.GetPerspectiveId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	if err := h.svc.Delete(ctx, caller, id); err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.PerspectiveServiceDeleteResponse{}), nil
}

// Get returns one perspective; it is public for published perspectives.
func (h *Handler) Get(ctx context.Context, req *connect.Request[perspectivv1.PerspectiveServiceGetRequest]) (*connect.Response[perspectivv1.PerspectiveServiceGetResponse], error) {
	id, err := parseID("perspective_id", req.Msg.GetPerspectiveId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	viewer := uuid.Nil
	if c, ok := apiutil.CallerFrom(ctx); ok {
		viewer = c.UserID
	}
	p, err := h.svc.Get(ctx, viewer, id)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.PerspectiveServiceGetResponse{Perspective: p}), nil
}

// ListMine pages the caller's own perspectives.
func (h *Handler) ListMine(ctx context.Context, req *connect.Request[perspectivv1.ListMineRequest]) (*connect.Response[perspectivv1.ListMineResponse], error) {
	caller, err := signedIn(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	f, err := listFilter(req.Msg)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	ps, next, err := h.svc.ListMine(ctx, caller, f, req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ListMineResponse{Perspectives: ps, NextPageToken: next}), nil
}

// ListByAuthor pages one author's published perspectives for their profile.
// It is public; a signed-in viewer also gets their own vote state.
func (h *Handler) ListByAuthor(ctx context.Context, req *connect.Request[perspectivv1.ListByAuthorRequest]) (*connect.Response[perspectivv1.ListByAuthorResponse], error) {
	author, err := parseID("user_id", req.Msg.GetUserId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	viewer := uuid.Nil
	if c, ok := apiutil.CallerFrom(ctx); ok {
		viewer = c.UserID
	}
	ps, next, err := h.svc.ListByAuthor(ctx, viewer, author, req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ListByAuthorResponse{Perspectives: ps, NextPageToken: next}), nil
}

// CreateInvite returns the author's share link for one of their perspectives.
func (h *Handler) CreateInvite(ctx context.Context, req *connect.Request[perspectivv1.CreateInviteRequest]) (*connect.Response[perspectivv1.CreateInviteResponse], error) {
	caller, err := signedIn(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	pid, err := parseID("perspective_id", req.Msg.GetPerspectiveId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	var character *uuid.UUID
	if req.Msg.GetCharacterId() != "" {
		id, err := parseID("character_id", req.Msg.GetCharacterId())
		if err != nil {
			return nil, apiutil.ToConnect(err)
		}
		character = &id
	}
	code, err := h.svc.CreateInvite(ctx, caller, pid, character)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.CreateInviteResponse{Code: code}), nil
}

// GetInvite returns what an invite link shows. It is public.
func (h *Handler) GetInvite(ctx context.Context, req *connect.Request[perspectivv1.GetInviteRequest]) (*connect.Response[perspectivv1.GetInviteResponse], error) {
	code, err := parseInviteCode(req.Msg.GetCode(), true)
	if err != nil {
		return nil, apiutil.ToConnect(errInviteNotFound)
	}
	inv, err := h.svc.GetInvite(ctx, code)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.GetInviteResponse{Invite: inv}), nil
}

// SavePerspective saves a readable story for the caller to read later, or
// unsaves it.
func (h *Handler) SavePerspective(ctx context.Context, req *connect.Request[perspectivv1.SavePerspectiveRequest]) (*connect.Response[perspectivv1.SavePerspectiveResponse], error) {
	caller, err := signedIn(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	id, err := parseID("perspective_id", req.Msg.GetPerspectiveId())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	saved, err := h.svc.Save(ctx, caller, id, req.Msg.GetOn())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.SavePerspectiveResponse{Saved: saved}), nil
}

// ListSaved pages the caller's saved stories, most recently saved first.
func (h *Handler) ListSaved(ctx context.Context, req *connect.Request[perspectivv1.ListSavedRequest]) (*connect.Response[perspectivv1.ListSavedResponse], error) {
	caller, err := signedIn(ctx)
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	ps, next, err := h.svc.ListSaved(ctx, caller, req.Msg.GetPageToken(), req.Msg.GetPageSize())
	if err != nil {
		return nil, apiutil.ToConnect(err)
	}
	return connect.NewResponse(&perspectivv1.ListSavedResponse{Perspectives: ps, NextPageToken: next}), nil
}

func signedIn(ctx context.Context) (uuid.UUID, error) {
	c, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return uuid.Nil, apiutil.Unauthenticated("sign in first")
	}
	return c.UserID, nil
}

func draftInput(m *perspectivv1.SaveDraftRequest) (DraftInput, error) {
	in := DraftInput{Hour: m.Hour, Parts: m.GetParts()}
	var err error
	if m.GetPerspectiveId() != "" {
		if in.ID, err = parseID("perspective_id", m.GetPerspectiveId()); err != nil {
			return in, err
		}
	}
	if m.GetPlaceId() != "" {
		id, err := parseID("place_id", m.GetPlaceId())
		if err != nil {
			return in, err
		}
		in.PlaceID = &id
	}
	if m.GetLocalDate() != "" {
		d, err := parseDate(m.GetLocalDate())
		if err != nil {
			return in, err
		}
		in.LocalDate = &d
	}
	if err := checkHour(m.Hour); err != nil {
		return in, err
	}
	if title := apiutil.OneLine(m.GetTitleIfNewArc()); title != "" {
		if utf8.RuneCountInString(title) > maxDraftTitleRunes {
			return in, apiutil.InvalidArg("title must be at most 80 characters")
		}
		if apiutil.HasHiddenRune(title) {
			return in, apiutil.InvalidArg("title has an invisible or control character; remove it")
		}
		in.Title = &title
	}
	if in.Genre, err = parseGenre(m.GetGenre(), false); err != nil {
		return in, err
	}
	if err := checkParts(in.Parts, false); err != nil {
		return in, err
	}
	in.Characters, err = checkCharacters(m.GetCharacters())
	return in, err
}

func publishInput(m *perspectivv1.PublishRequest) (PublishInput, error) {
	in := PublishInput{Hour: m.Hour, Title: m.GetTitleIfNewArc(), Parts: m.GetParts()}
	var err error
	if m.GetPerspectiveId() != "" {
		if in.ID, err = parseID("perspective_id", m.GetPerspectiveId()); err != nil {
			return in, err
		}
	}
	if in.PlaceID, err = parseID("place_id", m.GetPlaceId()); err != nil {
		return in, err
	}
	if in.LocalDate, err = parseDate(m.GetLocalDate()); err != nil {
		return in, err
	}
	if err := checkHour(m.Hour); err != nil {
		return in, err
	}
	genre, err := parseGenre(m.GetGenre(), true)
	if err != nil {
		return in, err
	}
	in.Genre = *genre
	if err := checkParts(in.Parts, true); err != nil {
		return in, err
	}
	if in.InviteCode, err = parseInviteCode(m.GetInviteCode(), false); err != nil {
		return in, err
	}
	in.Characters, err = checkCharacters(m.GetCharacters())
	return in, err
}

// parseInviteCode checks an invite code's shape; an empty code is allowed
// only when required is false.
func parseInviteCode(code string, required bool) (string, error) {
	if code == "" && !required {
		return "", nil
	}
	if len(code) != inviteLen {
		return "", apiutil.InvalidArg("invite code is not valid")
	}
	for _, r := range code {
		if !strings.ContainsRune(inviteAlphabet, r) {
			return "", apiutil.InvalidArg("invite code is not valid")
		}
	}
	return code, nil
}

func updateInput(m *perspectivv1.PerspectiveServiceUpdateRequest) (UpdateInput, error) {
	in := UpdateInput{Parts: m.GetParts()}
	var err error
	if in.ID, err = parseID("perspective_id", m.GetPerspectiveId()); err != nil {
		return in, err
	}
	genre, err := parseGenre(m.GetGenre(), true)
	if err != nil {
		return in, err
	}
	in.Genre = *genre
	if err := checkParts(in.Parts, true); err != nil {
		return in, err
	}
	in.Characters, err = checkCharacters(m.GetCharacters())
	return in, err
}

func listFilter(m *perspectivv1.ListMineRequest) (ListFilter, error) {
	f := ListFilter{Hour: m.Hour}
	status, err := statusToDB(m.GetStatus())
	if err != nil {
		return f, apiutil.InvalidArg("unknown status")
	}
	f.Status = status
	if m.GetPlaceId() != "" {
		id, err := parseID("place_id", m.GetPlaceId())
		if err != nil {
			return f, err
		}
		f.PlaceID = &id
	}
	if m.GetLocalDate() != "" {
		d, err := parseDate(m.GetLocalDate())
		if err != nil {
			return f, err
		}
		f.LocalDate = &d
	}
	return f, checkHour(m.Hour)
}
