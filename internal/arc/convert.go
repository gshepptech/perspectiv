package arc

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// The database stores proto enum value names; these are the arc domain's
// conversions from those strings. An unknown name is an error, never a
// zero value, so a schema and proto drift surfaces as Internal.

func placeKindFromDB(s string) (perspectivv1.PlaceKind, error) {
	v, ok := perspectivv1.PlaceKind_value[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown place kind %q", s)
	}
	return perspectivv1.PlaceKind(v), nil
}

func loreStandingFromDB(s string) (perspectivv1.LoreStanding, error) {
	v, ok := perspectivv1.LoreStanding_value[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown lore standing %q", s)
	}
	return perspectivv1.LoreStanding(v), nil
}

func loreStatusFromDB(t pgtype.Text) (perspectivv1.LoreStatus, error) {
	v, ok := perspectivv1.LoreStatus_value[t.String]
	if !t.Valid || !ok || v == 0 {
		return 0, fmt.Errorf("unknown lore status %q", t.String)
	}
	return perspectivv1.LoreStatus(v), nil
}

func genreFromDB(t pgtype.Text) (perspectivv1.Genre, error) {
	v, ok := perspectivv1.Genre_value[t.String]
	if !t.Valid || !ok || v == 0 {
		return 0, fmt.Errorf("unknown genre %q", t.String)
	}
	return perspectivv1.Genre(v), nil
}

// dateLayout is the wire form of a place-local calendar day.
const dateLayout = "2006-01-02"

// dateToDB keeps only the calendar day of t, as written in t's own location.
func dateToDB(t time.Time) pgtype.Date {
	y, m, d := t.Date()
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

func dateFromDB(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format(dateLayout)
}

func timestampFromDB(t pgtype.Timestamptz) *timestamppb.Timestamp {
	if !t.Valid {
		return nil
	}
	return timestamppb.New(t.Time)
}

func hourFromDB(h pgtype.Int2) *int32 {
	if !h.Valid {
		return nil
	}
	v := int32(h.Int16)
	return &v
}
