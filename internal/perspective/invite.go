package perspective

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/jobs"
	"perspectiv/internal/perspective/dbq"
)

// inviteAlphabet spells invite codes: lowercase letters and digits without
// the ones people misread (0, 1, l, o), so a code read aloud survives.
const inviteAlphabet = "23456789abcdefghijkmnpqrstuvwxyz"

// inviteLen is the code length; 32^10 codes make guessing one impractical.
const inviteLen = 10

// teaserLen is how many characters of the opening an invite shows.
const teaserLen = 240

var errInviteNotFound = apiutil.NotFound("invite not found")

func newInviteCode() (string, error) {
	b := make([]byte, inviteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("invite code: %w", err)
	}
	for i := range b {
		b[i] = inviteAlphabet[int(b[i])%len(inviteAlphabet)]
	}
	return string(b), nil
}

// CreateInvite returns the author's share link for one of their published
// perspectives, optionally naming one of its characters. Only the author may
// share it; a deleted, hidden or unpublished perspective has nothing to
// share. Asking twice for the same perspective and character returns the
// same code.
func (s *Service) CreateInvite(ctx context.Context, caller, perspectiveID uuid.UUID, characterID *uuid.UUID) (string, error) {
	var code string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		t, err := q.InviteTarget(ctx, dbq.InviteTargetParams{PerspectiveID: perspectiveID, CharacterID: characterID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return apiutil.NotFound("story not found")
		case err != nil:
			return apiutil.Internal(fmt.Errorf("read invite target: %w", err))
		case t.AuthorID != caller:
			return apiutil.PermissionDenied("only the author can invite people to this story")
		case !t.Readable:
			return apiutil.FailedPrecondition("only a published story can be shared")
		case !t.CharacterOk:
			return apiutil.InvalidArg("that character is not in this story")
		}
		fresh, err := newInviteCode()
		if err != nil {
			return apiutil.Internal(err)
		}
		code, err = q.UpsertInvite(ctx, dbq.UpsertInviteParams{
			Code: fresh, PerspectiveID: perspectiveID, InviterID: caller, CharacterID: characterID,
		})
		if err != nil {
			return apiutil.Internal(fmt.Errorf("save invite: %w", err))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// GetInvite returns what an invite link shows before sign-in. An unknown
// code, or one whose perspective is no longer readable (deleted, hidden, at
// or below a hidden place, or its author deleted), is NotFound.
func (s *Service) GetInvite(ctx context.Context, code string) (*perspectivv1.Invite, error) {
	var out *perspectivv1.Invite
	err := s.readTx(ctx, func(q *dbq.Queries) error {
		r, err := q.GetInviteTeaser(ctx, code)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !r.Readable) {
			return errInviteNotFound
		}
		if err != nil {
			return apiutil.Internal(fmt.Errorf("read invite: %w", err))
		}
		kind, err := placeKindFromDB(r.PlaceKind)
		if err != nil {
			return apiutil.Internal(err)
		}
		out = &perspectivv1.Invite{
			Code:           r.Code,
			Inviter:        &perspectivv1.UserRef{UserId: r.InviterID.String(), Username: r.InviterUsername},
			ArcId:          r.ArcID.String(),
			ArcTitle:       r.ArcTitle,
			Place:          &perspectivv1.PlaceRef{Id: r.PlaceID.String(), Kind: kind, Name: r.PlaceName},
			LocalDate:      dateFromDB(r.LocalDate),
			Hour:           hourFromDB(r.Hour),
			Excerpt:        teaser(r.Opening),
			CharacterLabel: r.CharacterLabel,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func teaser(opening string) string {
	if utf8.RuneCountInString(opening) <= teaserLen {
		return opening
	}
	return string([]rune(opening)[:teaserLen]) + "…"
}

// recordInvite notes, inside the publish transaction, that the author came
// through an invite and tells the inviter they wrote their side. It does
// nothing when the code is empty, unknown, the caller's own, or names another
// arc: an invite is to one arc, and a publish elsewhere did not answer it.
func (s *Service) recordInvite(ctx context.Context, tx pgx.Tx, caller, perspectiveID, arcID uuid.UUID, code string) error {
	if code == "" {
		return nil
	}
	q := dbq.New(tx)
	inv, err := q.InviteArc(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read invite arc: %w", err))
	}
	if inv.ArcID == nil || *inv.ArcID != arcID || inv.InviterID == caller {
		return nil
	}
	if err := q.SetInviteCode(ctx, dbq.SetInviteCodeParams{Code: code, ID: perspectiveID}); err != nil {
		return apiutil.Internal(fmt.Errorf("record invite: %w", err))
	}
	if err := s.enq.NotifyTx(ctx, tx, jobs.NotifyArgs{
		Kind:    perspectivv1.NotificationKind_NOTIFICATION_KIND_INVITE_ANSWERED,
		ActorID: caller, ArcID: arcID, PerspectiveID: perspectiveID,
	}); err != nil {
		return apiutil.Internal(err)
	}
	return nil
}
