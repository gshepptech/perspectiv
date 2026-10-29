// Package notify owns the inbox and notification preferences, the fan-out
// worker that turns a domain event into inbox rows and email jobs for its
// recipients, and the email worker that sends through a mailer.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/notify/dbq"
)

// maxMarkRead is how many ids one MarkRead call may name.
const maxMarkRead = 100

// Service reads and updates a user's inbox and preferences.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns the notify service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Notification is one inbox entry with its display fields resolved.
type Notification struct {
	ID            uuid.UUID
	Kind          perspectivv1.NotificationKind
	Payload       Payload
	ActorUsername string
	ArcTitle      string
	CreatedAt     time.Time
	ReadAt        *time.Time
}

// Payload is the notifications.payload JSON: the ids that apply to the
// notification's kind, each absent (nil) otherwise.
type Payload struct {
	ActorID       *uuid.UUID `json:"actor_id,omitempty"`
	ArcID         *uuid.UUID `json:"arc_id,omitempty"`
	PerspectiveID *uuid.UUID `json:"perspective_id,omitempty"`
	ClaimID       *uuid.UUID `json:"claim_id,omitempty"`
	LoreID        *uuid.UUID `json:"lore_id,omitempty"`
	ReportID      *uuid.UUID `json:"report_id,omitempty"`
}

// InboxPage is one page of the inbox.
type InboxPage struct {
	Notifications []Notification
	NextPageToken string
	UnreadCount   int32
}

// ListInbox returns the user's notifications newest first. The page token's
// keyset is "<created_at RFC 3339 nano>|<id>" of the last row of the
// previous page.
func (s *Service) ListInbox(ctx context.Context, userID uuid.UUID, pageToken string, pageSize int32, unreadOnly bool) (InboxPage, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return InboxPage{}, err
	}
	params := dbq.ListInboxParams{UserID: userID, UnreadOnly: unreadOnly, RowLimit: page.Limit + 1}
	if page.After != "" {
		at, id, err := parseCursor(page.After)
		if err != nil {
			return InboxPage{}, err
		}
		params.AfterAt, params.AfterID = &at, &id
	}
	q := dbq.New(s.pool)
	rows, err := q.ListInbox(ctx, params)
	if err != nil {
		return InboxPage{}, apiutil.Internal(fmt.Errorf("list inbox: %w", err))
	}
	unread, err := q.CountUnread(ctx, userID)
	if err != nil {
		return InboxPage{}, apiutil.Internal(fmt.Errorf("count unread: %w", err))
	}
	out := InboxPage{UnreadCount: unread}
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		last := rows[len(rows)-1]
		out.NextPageToken = apiutil.NextPageToken(last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String())
	}
	for _, r := range rows {
		n, err := notificationFromRow(r)
		if err != nil {
			return InboxPage{}, apiutil.Internal(err)
		}
		out.Notifications = append(out.Notifications, n)
	}
	return out, nil
}

func parseCursor(after string) (time.Time, uuid.UUID, error) {
	ts, idText, ok := strings.Cut(after, "|")
	if !ok {
		return time.Time{}, uuid.Nil, apiutil.InvalidArg("invalid page token")
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, apiutil.InvalidArg("invalid page token")
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return time.Time{}, uuid.Nil, apiutil.InvalidArg("invalid page token")
	}
	return at, id, nil
}

func notificationFromRow(r dbq.ListInboxRow) (Notification, error) {
	kind, err := kindFromText(r.Kind)
	if err != nil {
		return Notification{}, err
	}
	var p Payload
	if err := json.Unmarshal(r.Payload, &p); err != nil {
		return Notification{}, fmt.Errorf("notification %s payload: %w", r.ID, err)
	}
	n := Notification{ID: r.ID, Kind: kind, Payload: p, CreatedAt: r.CreatedAt, ReadAt: r.ReadAt}
	if r.ActorUsername != nil {
		n.ActorUsername = *r.ActorUsername
	}
	switch {
	case r.ArcHidden:
		// The arc's place, or a place above it, was hidden by moderation:
		// the row stays, but it neither names nor links the arc.
		n.Payload.ArcID = nil
	case r.ArcTitle != nil:
		n.ArcTitle = *r.ArcTitle
	}
	return n, nil
}

// MarkRead marks the named notifications, or all of them, read and returns
// the unread count. Ids that are not the user's are ignored.
func (s *Service) MarkRead(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, all bool) (int32, error) {
	q := dbq.New(s.pool)
	var err error
	if all {
		err = q.MarkAllRead(ctx, userID)
	} else {
		err = q.MarkRead(ctx, dbq.MarkReadParams{UserID: userID, Ids: ids})
	}
	if err != nil {
		return 0, apiutil.Internal(fmt.Errorf("mark read: %w", err))
	}
	unread, err := q.CountUnread(ctx, userID)
	if err != nil {
		return 0, apiutil.Internal(fmt.Errorf("count unread: %w", err))
	}
	return unread, nil
}

// Preference is the two toggles of one kind.
type Preference struct {
	Kind  perspectivv1.NotificationKind
	InApp bool
	Email bool
}

// Preferences returns every kind in enum order. A kind the user never set
// has both toggles on.
func (s *Service) Preferences(ctx context.Context, userID uuid.UUID) ([]Preference, error) {
	return preferences(ctx, dbq.New(s.pool), userID)
}

func preferences(ctx context.Context, q *dbq.Queries, userID uuid.UUID) ([]Preference, error) {
	rows, err := q.ListPreferences(ctx, userID)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list preferences: %w", err))
	}
	stored := make(map[perspectivv1.NotificationKind]dbq.ListPreferencesRow, len(rows))
	for _, r := range rows {
		k, err := kindFromText(r.Kind)
		if err != nil {
			return nil, apiutil.Internal(err)
		}
		stored[k] = r
	}
	out := make([]Preference, 0, len(allKinds))
	for _, k := range allKinds {
		p := Preference{Kind: k, InApp: true, Email: true}
		if r, ok := stored[k]; ok {
			p.InApp, p.Email = r.InApp, r.Email
		}
		out = append(out, p)
	}
	return out, nil
}

// UpdatePreferences stores the toggles of each listed kind and returns all
// kinds. Kinds not listed keep their toggles. A kind that is undefined or
// listed twice is refused before anything is written.
func (s *Service) UpdatePreferences(ctx context.Context, userID uuid.UUID, prefs []Preference) ([]Preference, error) {
	seen := make(map[perspectivv1.NotificationKind]bool, len(prefs))
	for _, p := range prefs {
		if !validKind(p.Kind) {
			return nil, apiutil.InvalidArg("preferences: unknown notification kind")
		}
		if seen[p.Kind] {
			return nil, apiutil.InvalidArg("preferences: a kind is listed more than once")
		}
		seen[p.Kind] = true
	}
	var out []Preference
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		for _, p := range prefs {
			if err := q.UpsertPreference(ctx, dbq.UpsertPreferenceParams{
				UserID: userID, Kind: kindText(p.Kind), InApp: p.InApp, Email: p.Email,
			}); err != nil {
				return apiutil.Internal(fmt.Errorf("store preference: %w", err))
			}
		}
		var err error
		out, err = preferences(ctx, q, userID)
		return err
	})
	if err != nil {
		// Errors inside the transaction are already typed; a failed commit
		// is not, and ToConnect reports it as internal.
		return nil, err
	}
	return out, nil
}

// EmailDigest reports whether the user takes email as one daily summary.
func (s *Service) EmailDigest(ctx context.Context, userID uuid.UUID) (bool, error) {
	on, err := dbq.New(s.pool).GetEmailDigest(ctx, userID)
	if err != nil {
		return false, apiutil.Internal(fmt.Errorf("read digest setting: %w", err))
	}
	return on, nil
}

// SetEmailDigest turns the daily summary on or off. Items already waiting
// still go out in the next summary.
func (s *Service) SetEmailDigest(ctx context.Context, userID uuid.UUID, on bool) error {
	if err := dbq.New(s.pool).SetEmailDigest(ctx, dbq.SetEmailDigestParams{UserID: userID, Digest: on}); err != nil {
		return apiutil.Internal(fmt.Errorf("store digest setting: %w", err))
	}
	return nil
}
