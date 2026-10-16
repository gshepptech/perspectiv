package importer

import (
	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// kindDB and sourceDB give the text the places columns hold: the full proto
// value name, such as PLACE_KIND_STORE.
func kindDB(k perspectivv1.PlaceKind) string { return k.String() }

func sourceDB(s perspectivv1.PlaceSource) string { return s.String() }

func float8(v *float64) pgtype.Float8 {
	if v == nil {
		return pgtype.Float8{}
	}
	return pgtype.Float8{Float64: *v, Valid: true}
}
