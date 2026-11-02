package moderation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/moderation/dbq"
)

// The admin actions outside the report queue: undoing a suspension or a
// hide, and tidying the place hierarchy. Each needs a reason and writes an
// audit entry in its own transaction.

var errNotSuspended = apiutil.FailedPrecondition("this account is not suspended")

// LiftSuspension restores a suspended account's standing: it can report
// again and, if an admin, moderate again. The reason is required and goes
// on the audit entry with the admin who lifted it.
func (s *Service) LiftSuspension(ctx context.Context, admin, target uuid.UUID, reason string) (*perspectivv1.AdminUserView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, apiutil.InvalidArg("give a reason for lifting the suspension")
	}
	if utf8.RuneCountInString(reason) > MaxReasonRunes {
		return nil, apiutil.InvalidArg(fmt.Sprintf("keep the reason to %d characters", MaxReasonRunes))
	}
	var view *perspectivv1.AdminUserView
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := s.requireAdmin(ctx, q, admin, perspectivv1connect.ModerationServiceLiftSuspensionProcedure); err != nil {
			return err
		}
		n, err := q.LiftSuspension(ctx, target)
		if err != nil {
			return apiutil.Internal(fmt.Errorf("lift suspension: %w", err))
		}
		if n == 0 {
			if _, err := q.FindUserByID(ctx, target); errors.Is(err, pgx.ErrNoRows) {
				return errNoSuchUser
			}
			return errNotSuspended
		}
		if err := writeAudit(ctx, q, AuditEntry{
			Actor: &admin, Action: perspectivv1.AuditAction_AUDIT_ACTION_LIFT_SUSPENSION,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER, TargetID: &target, Reason: reason,
		}); err != nil {
			return apiutil.Internal(fmt.Errorf("write audit row: %w", err))
		}
		view, err = adminUserView(ctx, q, target)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.log.Info().Str("event", "suspension_lifted").Str("sub", admin.String()).Str("target", target.String()).Msg("suspension lifted")
	return view, nil
}

var errNotHidden = apiutil.FailedPrecondition("this is not hidden")

// Restore makes hidden content readable again: a perspective, a place or a
// thread post. A restored perspective marks its arc's lore dirty so the rank
// job counts it again; claims rejected while it was hidden stay rejected.
// The reason is required and goes on the audit entry.
func (s *Service) Restore(ctx context.Context, admin uuid.UUID, kind perspectivv1.AuditTargetKind, target uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return apiutil.InvalidArg("give a reason for restoring it")
	}
	if utf8.RuneCountInString(reason) > MaxReasonRunes {
		return apiutil.InvalidArg(fmt.Sprintf("keep the reason to %d characters", MaxReasonRunes))
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := s.requireAdmin(ctx, q, admin, perspectivv1connect.ModerationServiceRestoreProcedure); err != nil {
			return err
		}
		if err := restore(ctx, q, kind, target); err != nil {
			return err
		}
		if err := writeAudit(ctx, q, AuditEntry{
			Actor: &admin, Action: perspectivv1.AuditAction_AUDIT_ACTION_RESTORE, TargetKind: kind, TargetID: &target, Reason: reason,
		}); err != nil {
			return apiutil.Internal(fmt.Errorf("write audit row: %w", err))
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "content_restored").Str("sub", admin.String()).Str("kind", kind.String()).
		Str("target", target.String()).Msg("hidden content restored")
	return nil
}

func restore(ctx context.Context, q *dbq.Queries, kind perspectivv1.AuditTargetKind, target uuid.UUID) error {
	switch kind {
	case perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PERSPECTIVE:
		arcID, err := q.RestorePerspective(ctx, target)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotHidden
		}
		if err != nil {
			return apiutil.Internal(fmt.Errorf("restore perspective: %w", err))
		}
		if arcID != nil {
			if err := q.MarkArcLoreDirty(ctx, *arcID); err != nil {
				return apiutil.Internal(fmt.Errorf("mark arc lore dirty: %w", err))
			}
		}
		return nil
	case perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PLACE:
		return restoredOne(q.RestorePlace(ctx, target))
	case perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_LORE_POST:
		return restoredOne(q.RestoreLorePost(ctx, target))
	case perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER,
		perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_LORE_NAME,
		perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_ACCESS_CODE,
		perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_WAITLIST_ENTRY,
		perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_UNSPECIFIED:
	}
	return apiutil.InvalidArg("only a story, place or thread post can be restored")
}

func restoredOne(n int64, err error) error {
	if err != nil {
		return apiutil.Internal(fmt.Errorf("restore: %w", err))
	}
	if n == 0 {
		return errNotHidden
	}
	return nil
}

var errNotAStore = apiutil.FailedPrecondition("only a store that is not inside a mall or venue can become a venue")

// MakeVenue turns an imported store, such as a stadium or a theme park, into
// a mall-level venue so its stands, rides and gates can be added inside it.
// Its arcs and stories stay where they are. The reason is required and goes
// on the audit entry.
func (s *Service) MakeVenue(ctx context.Context, admin, place uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return apiutil.InvalidArg("give a reason for making it a venue")
	}
	if utf8.RuneCountInString(reason) > MaxReasonRunes {
		return apiutil.InvalidArg(fmt.Sprintf("keep the reason to %d characters", MaxReasonRunes))
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := s.requireAdmin(ctx, q, admin, perspectivv1connect.ModerationServiceMakeVenueProcedure); err != nil {
			return err
		}
		n, err := q.MakeVenue(ctx, place)
		if err != nil {
			return apiutil.Internal(fmt.Errorf("make venue: %w", err))
		}
		if n == 0 {
			return errNotAStore
		}
		if err := writeAudit(ctx, q, AuditEntry{
			Actor: &admin, Action: perspectivv1.AuditAction_AUDIT_ACTION_MAKE_VENUE,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PLACE, TargetID: &place, Reason: reason,
		}); err != nil {
			return apiutil.Internal(fmt.Errorf("write audit row: %w", err))
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "venue_made").Str("sub", admin.String()).Str("place", place.String()).Msg("store made a venue")
	return nil
}

// MergePlace folds a duplicate place into its twin. The duplicate must have
// no arcs of its own, so no story moves; places filed under it move to the
// twin, which must sit higher than each of them, and the duplicate is
// hidden. A child with the same name as one of the twin's is refused rather
// than merged. The reason is required and goes on the audit entry.
func (s *Service) MergePlace(ctx context.Context, admin, from, into uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return apiutil.InvalidArg("give a reason for the merge")
	case utf8.RuneCountInString(reason) > MaxReasonRunes:
		return apiutil.InvalidArg(fmt.Sprintf("keep the reason to %d characters", MaxReasonRunes))
	case from == into:
		return apiutil.InvalidArg("a place cannot be merged into itself")
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := s.requireAdmin(ctx, q, admin, perspectivv1connect.ModerationServiceMergePlaceProcedure); err != nil {
			return err
		}
		if err := checkMerge(ctx, q, from, into); err != nil {
			return err
		}
		if err := q.MovePlaceChildren(ctx, dbq.MovePlaceChildrenParams{IntoID: into, FromID: from}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return apiutil.FailedPrecondition("both places hold a place with the same name; rename or hide one first")
			}
			return apiutil.Internal(fmt.Errorf("move places: %w", err))
		}
		if _, err := q.HidePlace(ctx, from); err != nil {
			return apiutil.Internal(fmt.Errorf("hide duplicate: %w", err))
		}
		return writeAudit(ctx, q, AuditEntry{
			Actor: &admin, Action: perspectivv1.AuditAction_AUDIT_ACTION_MERGE_PLACE,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PLACE, TargetID: &from,
			Reason: reason + " (merged into " + into.String() + ")",
		})
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "place_merged").Str("sub", admin.String()).Str("place", from.String()).
		Str("into", into.String()).Msg("duplicate place merged")
	return nil
}

func checkMerge(ctx context.Context, q *dbq.Queries, from, into uuid.UUID) error {
	rows, err := q.LockPlacesForMerge(ctx, []uuid.UUID{from, into})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("lock places: %w", err))
	}
	var dup, twin *dbq.LockPlacesForMergeRow
	for i := range rows {
		switch rows[i].ID {
		case from:
			dup = &rows[i]
		case into:
			twin = &rows[i]
		}
	}
	switch {
	case dup == nil || twin == nil || twin.AncestorHidden:
		return apiutil.NotFound("both places must exist and be visible")
	case dup.Arcs > 0:
		return apiutil.FailedPrecondition("the duplicate has stories or drafts of its own; merge the other way, or hide it")
	case slices.Contains(twin.Path, from):
		// Its children would move under themselves.
		return apiutil.FailedPrecondition("a place cannot be merged into a place inside it")
	case dup.ChildLevel > 0 && dup.ChildLevel <= twin.Level:
		// Every child, the broadest included, must sit lower than the twin.
		return apiutil.FailedPrecondition("the places under the duplicate cannot be filed under the other place")
	}
	return nil
}
