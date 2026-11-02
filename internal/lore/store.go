package lore

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/lore/dbq"
)

// The database stores proto enum value names. These are the lore domain's
// only conversions between the two; an unknown stored name is an error, never
// a zero value, so schema and proto drift surfaces as Internal.

var (
	loreStatusInLore  = perspectivv1.LoreStatus_LORE_STATUS_IN_LORE.String()
	loreStatusSolo    = perspectivv1.LoreStatus_LORE_STATUS_SOLO.String()
	loreStatusRemoved = perspectivv1.LoreStatus_LORE_STATUS_REMOVED.String()

	claimPending  = perspectivv1.ClaimStatus_CLAIM_STATUS_PENDING.String()
	claimAccepted = perspectivv1.ClaimStatus_CLAIM_STATUS_ACCEPTED.String()
	claimRejected = perspectivv1.ClaimStatus_CLAIM_STATUS_REJECTED.String()

	// activeClaims are the statuses that hold a character.
	activeClaims = []string{claimPending, claimAccepted}

	perspectivePublished = perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_PUBLISHED.String()
)

func claimStatusFromDB(s string) (perspectivv1.ClaimStatus, error) {
	v, ok := perspectivv1.ClaimStatus_value[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown claim status %q", s)
	}
	return perspectivv1.ClaimStatus(v), nil
}

// claimStatusToDB maps a status filter; UNSPECIFIED means no filter.
func claimStatusToDB(s perspectivv1.ClaimStatus) (pgtype.Text, error) {
	switch s {
	case perspectivv1.ClaimStatus_CLAIM_STATUS_UNSPECIFIED:
		return pgtype.Text{}, nil
	case perspectivv1.ClaimStatus_CLAIM_STATUS_PENDING,
		perspectivv1.ClaimStatus_CLAIM_STATUS_ACCEPTED,
		perspectivv1.ClaimStatus_CLAIM_STATUS_REJECTED:
		return pgtype.Text{String: s.String(), Valid: true}, nil
	}
	return pgtype.Text{}, apiutil.InvalidArg("unknown claim status")
}

func loreStandingFromDB(s string) (perspectivv1.LoreStanding, error) {
	v, ok := perspectivv1.LoreStanding_value[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown lore standing %q", s)
	}
	return perspectivv1.LoreStanding(v), nil
}

func timestampFromDB(t pgtype.Timestamptz) *timestamppb.Timestamp {
	if !t.Valid {
		return nil
	}
	return timestamppb.New(t.Time)
}

// claimFromRow builds the wire claim. Users appear only as id and username.
func claimFromRow(r dbq.ListIncomingClaimsRow) (*perspectivv1.Claim, error) {
	status, err := claimStatusFromDB(r.Status)
	if err != nil {
		return nil, err
	}
	return &perspectivv1.Claim{
		Id:                    r.ID.String(),
		ArcId:                 r.ArcID.UUID.String(),
		CharacterId:           r.CharacterID.String(),
		CharacterLabel:        r.Label,
		PerspectiveId:         r.PerspectiveID.String(),
		Author:                &perspectivv1.UserRef{UserId: r.AuthorID.String(), Username: r.AuthorUsername},
		Claimant:              &perspectivv1.UserRef{UserId: r.ClaimantUserID.String(), Username: r.ClaimantUsername},
		ClaimantPerspectiveId: r.ClaimantPerspectiveID.String(),
		ClaimantExcerpt:       claimExcerpt(r.ClaimantOpening),
		Status:                status,
		CreatedAt:             timestampFromDB(r.CreatedAt),
		ResolvedAt:            timestampFromDB(r.ResolvedAt),
		PlaceName:             r.PlaceName,
		LocalDate:             dateFromDB(r.LocalDate),
		Hour:                  hourFromDB(r.Hour),
	}, nil
}

// dateFromDB is a place-local day as YYYY-MM-DD, or "" when unset.
func dateFromDB(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format(time.DateOnly)
}

// hourFromDB is an hour arc's hour, or nil for a day arc.
func hourFromDB(h pgtype.Int2) *int32 {
	if !h.Valid {
		return nil
	}
	v := int32(h.Int16)
	return &v
}

// claimExcerptLen is how many characters of the claimant's story a claim
// quotes; the queries read one more, so a cut can tell an opening that ends
// there from one that goes on.
const claimExcerptLen = 240

// claimExcerpt cuts an opening, whose whitespace the query folded, at the
// last word that fits and marks the cut with an ellipsis, so a request never
// quotes half a word.
func claimExcerpt(opening string) string {
	r := []rune(opening)
	if len(r) <= claimExcerptLen {
		return string(r)
	}
	cut := claimExcerptLen
	for i := claimExcerptLen; i > claimExcerptLen/2; i-- {
		if unicode.IsSpace(r[i]) {
			cut = i
			break
		}
	}
	return strings.TrimRightFunc(string(r[:cut]), func(c rune) bool { return unicode.IsSpace(c) || c == ',' || c == ';' || c == ':' }) + "…"
}

// claimKeyset is ListClaims' position: the (created_at, id) of the last row
// of the previous page, encoded as "<unix nanoseconds>_<uuid>".
type claimKeyset struct {
	createdAt time.Time
	id        uuid.UUID
}

func (k claimKeyset) encode() string {
	return fmt.Sprintf("%d_%s", k.createdAt.UnixNano(), k.id)
}

func decodeClaimKeyset(s string) (claimKeyset, error) {
	nanos, id, ok := strings.Cut(s, "_")
	if !ok {
		return claimKeyset{}, apiutil.InvalidArg("invalid page token")
	}
	var n int64
	if _, err := fmt.Sscan(nanos, &n); err != nil {
		return claimKeyset{}, apiutil.InvalidArg("invalid page token")
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return claimKeyset{}, apiutil.InvalidArg("invalid page token")
	}
	return claimKeyset{createdAt: time.Unix(0, n), id: parsed}, nil
}

// isUniqueViolation reports a unique or exclusion constraint failure.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
