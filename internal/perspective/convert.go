package perspective

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// The database stores proto enum value names. These are the perspective
// domain's conversions; an unknown stored name is an error, never a zero
// value, so schema and proto drift surfaces as Internal.

var (
	statusDraft     = perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_DRAFT.String()
	statusPublished = perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_PUBLISHED.String()
	lorePending     = perspectivv1.LoreStatus_LORE_STATUS_PENDING.String()
)

func statusFromDB(s string) (perspectivv1.PerspectiveStatus, error) {
	v, ok := perspectivv1.PerspectiveStatus_value[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown perspective status %q", s)
	}
	return perspectivv1.PerspectiveStatus(v), nil
}

// statusToDB returns the stored name for a ListMine status filter; nil means
// no filter.
func statusToDB(s perspectivv1.PerspectiveStatus) (*string, error) {
	switch s {
	case perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_UNSPECIFIED:
		return nil, nil
	case perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_DRAFT:
		return &statusDraft, nil
	case perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_PUBLISHED:
		return &statusPublished, nil
	default:
		return nil, fmt.Errorf("unknown perspective status %d", s)
	}
}

// loreStatusFromDB maps a nullable stored lore status; NULL (a draft) is
// UNSPECIFIED.
func loreStatusFromDB(s *string) (perspectivv1.LoreStatus, error) {
	if s == nil {
		return perspectivv1.LoreStatus_LORE_STATUS_UNSPECIFIED, nil
	}
	v, ok := perspectivv1.LoreStatus_value[*s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown lore status %q", *s)
	}
	return perspectivv1.LoreStatus(v), nil
}

// genreFromDB maps a nullable stored genre; NULL (a draft without one) is
// UNSPECIFIED.
func genreFromDB(s *string) (perspectivv1.Genre, error) {
	if s == nil {
		return perspectivv1.Genre_GENRE_UNSPECIFIED, nil
	}
	v, ok := perspectivv1.Genre_value[*s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown genre %q", *s)
	}
	return perspectivv1.Genre(v), nil
}

func placeKindFromDB(s string) (perspectivv1.PlaceKind, error) {
	v, ok := perspectivv1.PlaceKind_value[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown place kind %q", s)
	}
	return perspectivv1.PlaceKind(v), nil
}

func emojiFromDB(s string) (perspectivv1.Emoji, error) {
	v, ok := perspectivv1.Emoji_value[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown emoji %q", s)
	}
	return perspectivv1.Emoji(v), nil
}

func dateToDB(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

func dateFromDB(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format(dateLayout)
}

// hourToDB narrows a validated hour (0 to 23, see checkHour) to the
// smallint column.
func hourToDB(h *int32) *int16 {
	if h == nil {
		return nil
	}
	v := int16(*h) //nolint:gosec // checkHour bounds it to 0..23
	return &v
}

func hourFromDB(h *int16) *int32 {
	if h == nil {
		return nil
	}
	v := int32(*h)
	return &v
}

func timestampFromDB(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

// claimStatusFromDB reads a claims.status name; "" (no claim) is UNSPECIFIED.
func claimStatusFromDB(s string) (perspectivv1.ClaimStatus, error) {
	if s == "" {
		return perspectivv1.ClaimStatus_CLAIM_STATUS_UNSPECIFIED, nil
	}
	v, ok := perspectivv1.ClaimStatus_value[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown claim status %q", s)
	}
	return perspectivv1.ClaimStatus(v), nil
}
