// Package moderation owns user reports, the admin report queue, the actions
// an admin resolves a report with, the append-only moderation audit trail and
// the command-line admin grant.
//
// Suspension. SUSPEND_USER sets users.suspended_at; nothing clears it in v1.
// A suspended account keeps reading, signing in and managing its own account
// (profile, settings, blocks, deletion), and what it already published stays
// readable: removing content is the separate hide actions. What it loses is
// standing in moderation: a suspended account cannot file a report, and a
// suspended admin cannot open the queue or resolve a report. Both are checked
// against the users row on every call, never against the access token.
package moderation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/logging"
	"perspectiv/internal/jobs"
	"perspectiv/internal/moderation/dbq"
)

// MaxDetailsRunes bounds a reporter's free text, and MaxReasonRunes an
// admin's resolution reason, both counted after trimming whitespace.
const (
	MaxDetailsRunes = 1000
	MaxReasonRunes  = 1000
)

// Notifier enqueues a notification event inside the caller's transaction.
// *jobs.Enqueuer satisfies it.
type Notifier interface {
	NotifyTx(ctx context.Context, tx pgx.Tx, a jobs.NotifyArgs) error
}

// Service is the moderation domain's logic over its sqlc queries.
type Service struct {
	pool   *pgxpool.Pool
	q      *dbq.Queries
	notify Notifier
	log    zerolog.Logger
}

// NewService builds the service. All arguments are required.
func NewService(pool *pgxpool.Pool, notify Notifier, log zerolog.Logger) *Service {
	return &Service{pool: pool, q: dbq.New(pool), notify: notify, log: log}
}

// ReportInput is one user report.
type ReportInput struct {
	TargetKind perspectivv1.ReportTargetKind
	TargetID   uuid.UUID
	Reason     perspectivv1.ReportReason
	Details    string
}

func (in *ReportInput) validate() error {
	if !validTargetKind(in.TargetKind) {
		return apiutil.InvalidArg("target_kind must be a perspective, place, user, post or lore name")
	}
	if in.TargetID == uuid.Nil {
		return apiutil.InvalidArg("target_id is required")
	}
	if !validReason(in.Reason) {
		return apiutil.InvalidArg("reason is required")
	}
	in.Details = strings.TrimSpace(in.Details)
	if utf8.RuneCountInString(in.Details) > MaxDetailsRunes {
		return apiutil.InvalidArg(fmt.Sprintf("details must be at most %d characters", MaxDetailsRunes))
	}
	return nil
}

// Report files a report and returns its id.
//
// A report is on something a reader can see: a published perspective that is
// neither deleted nor hidden, a place that is not hidden, or an account that
// is not deleted. Anything else is NotFound. Reporting your own account is
// InvalidArg. A reporter who already has an open report on the same target
// gets that report's id back and no second row is written, so a double submit
// does not grow the queue.
func (s *Service) Report(ctx context.Context, reporter uuid.UUID, in ReportInput) (uuid.UUID, error) {
	if err := in.validate(); err != nil {
		return uuid.Nil, err
	}
	if in.TargetKind == perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER && in.TargetID == reporter {
		return uuid.Nil, apiutil.InvalidArg("you cannot report yourself")
	}
	kind := in.TargetKind.String()
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := requireReporter(ctx, q, reporter); err != nil {
			return err
		}
		if err := requireReportable(ctx, q, in.TargetKind, in.TargetID); err != nil {
			return err
		}
		if err := q.LockReportKey(ctx, reporter.String()+"/"+kind+"/"+in.TargetID.String()); err != nil {
			return apiutil.Internal(fmt.Errorf("lock report key: %w", err))
		}
		existing, err := q.FindOpenReport(ctx, dbq.FindOpenReportParams{
			ReporterID: reporter, TargetKind: kind, TargetID: in.TargetID,
		})
		switch {
		case err == nil:
			id = existing
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return apiutil.Internal(fmt.Errorf("find open report: %w", err))
		}
		id, err = q.InsertReport(ctx, dbq.InsertReportParams{
			ReporterID: reporter, TargetKind: kind, TargetID: in.TargetID,
			Reason: in.Reason.String(), Details: in.Details,
		})
		if err != nil {
			return apiutil.Internal(fmt.Errorf("insert report: %w", err))
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// requireReporter refuses a reporter whose account is gone or suspended.
func requireReporter(ctx context.Context, q *dbq.Queries, reporter uuid.UUID) error {
	a, err := q.GetActor(ctx, reporter)
	if errors.Is(err, pgx.ErrNoRows) {
		return apiutil.Unauthenticated("account no longer exists")
	}
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read reporter: %w", err))
	}
	if a.DeletedAt != nil {
		return apiutil.Unauthenticated("account no longer exists")
	}
	if a.SuspendedAt != nil {
		return apiutil.PermissionDenied("a suspended account cannot file reports")
	}
	return nil
}

func requireReportable(ctx context.Context, q *dbq.Queries, kind perspectivv1.ReportTargetKind, id uuid.UUID) error {
	var (
		ok   bool
		err  error
		what string
	)
	switch kind {
	case perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE:
		ok, err = q.PerspectiveReportable(ctx, id)
		what = "perspective"
	case perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE:
		ok, err = q.PlaceReportable(ctx, id)
		what = "place"
	case perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER:
		ok, err = q.UserReportable(ctx, id)
		what = "user"
	case perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_POST:
		ok, err = q.LorePostReportable(ctx, id)
		what = "post"
	case perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_NAME:
		ok, err = q.LoreNameReportable(ctx, id)
		what = "lore name"
	case perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_UNSPECIFIED:
		return apiutil.InvalidArg("target_kind must be a perspective, place, user, post or lore name")
	default:
		return apiutil.InvalidArg("target_kind must be a perspective, place, user, post or lore name")
	}
	if err != nil {
		return apiutil.Internal(fmt.Errorf("check %s target: %w", what, err))
	}
	if !ok {
		return apiutil.NotFound(what + " not found")
	}
	return nil
}

// requireAdmin checks the stored account behind an admin call: it must exist,
// not be deleted or suspended, and hold ROLE_ADMIN. The interceptor has
// already checked the role the access token carries; this re-reads the row so
// a token minted before a change cannot outlive it. A refusal writes the same
// authz_denied line the interceptor writes.
func (s *Service) requireAdmin(ctx context.Context, q *dbq.Queries, caller uuid.UUID, method string) error {
	a, err := q.GetActor(ctx, caller)
	reason := ""
	role := perspectivv1.Role_ROLE_UNSPECIFIED
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		reason = "account_missing"
	case err != nil:
		return apiutil.Internal(fmt.Errorf("read caller: %w", err))
	case a.DeletedAt != nil:
		reason = "account_deleted"
	default:
		role, err = roleFromDB(a.Role)
		if err != nil {
			return apiutil.Internal(err)
		}
		switch {
		case role != perspectivv1.Role_ROLE_ADMIN:
			reason = "role"
		case a.SuspendedAt != nil:
			reason = "suspended"
		}
	}
	if reason == "" {
		return nil
	}
	logging.Ctx(ctx, s.log).Warn().
		Str("event", "authz_denied").
		Str("sub", caller.String()).
		Str("method", method).
		Str("required", "admin").
		Str("role", role.String()).
		Str("reason", reason).
		Msg("request denied")
	return apiutil.PermissionDenied("admin only")
}

// ReportFilter narrows the admin queue; a zero field is no filter. Subject
// keeps reports against one user: on the account, or on a perspective or
// thread post they wrote. Reporter keeps reports one user filed.
type ReportFilter struct {
	Status   perspectivv1.ReportStatus
	Subject  uuid.UUID
	Reporter uuid.UUID
}

// ListReports returns one page of the admin queue, oldest first.
func (s *Service) ListReports(ctx context.Context, admin uuid.UUID, f ReportFilter,
	page apiutil.PageToken,
) ([]*perspectivv1.Report, string, error) {
	if err := s.requireAdmin(ctx, s.q, admin, perspectivv1connect.ModerationServiceListReportsProcedure); err != nil {
		return nil, "", err
	}
	if !validStatusFilter(f.Status) {
		return nil, "", apiutil.InvalidArg("status must be OPEN, RESOLVED or unspecified")
	}
	params := dbq.ListReportsParams{MaxRows: page.Limit + 1}
	if f.Status != perspectivv1.ReportStatus_REPORT_STATUS_UNSPECIFIED {
		st := f.Status.String()
		params.Status = &st
	}
	if f.Subject != uuid.Nil {
		params.SubjectID = &f.Subject
	}
	if f.Reporter != uuid.Nil {
		params.ReporterID = &f.Reporter
	}
	if page.After != "" {
		at, id, err := decodeCursor(page.After)
		if err != nil {
			return nil, "", err
		}
		params.AfterCreatedAt, params.AfterID = &at, &id
	}
	rows, err := s.q.ListReports(ctx, params)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list reports: %w", err))
	}
	next := ""
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		last := rows[len(rows)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	out := make([]*perspectivv1.Report, 0, len(rows))
	for _, r := range rows {
		p, err := reportProto(dbq.GetReportRow(r))
		if err != nil {
			return nil, "", err
		}
		out = append(out, p)
	}
	return out, apiutil.NextPageToken(next), nil
}

// encodeCursor is the queue's keyset: the row's created_at in Unix
// microseconds (PostgreSQL's precision, so it round-trips exactly) and its id.
func encodeCursor(at time.Time, id uuid.UUID) string {
	return strconv.FormatInt(at.UnixMicro(), 10) + "_" + id.String()
}

func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	micros, idText, ok := strings.Cut(s, "_")
	if !ok {
		return time.Time{}, uuid.Nil, apiutil.InvalidArg("invalid page token")
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, apiutil.InvalidArg("invalid page token")
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return time.Time{}, uuid.Nil, apiutil.InvalidArg("invalid page token")
	}
	return time.UnixMicro(us).UTC(), id, nil
}

// ResolveInput is an admin's resolution of one report.
type ResolveInput struct {
	ReportID uuid.UUID
	Action   perspectivv1.ModerationAction
	Reason   string
}

func (in *ResolveInput) validate() error {
	if in.ReportID == uuid.Nil {
		return apiutil.InvalidArg("report_id is required")
	}
	if _, _, ok := actionTargets(in.Action); !ok {
		return apiutil.InvalidArg("action must be HIDE_PERSPECTIVE, HIDE_PLACE, SUSPEND_USER or DISMISS")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		return apiutil.InvalidArg("reason is required; it is written to the audit log")
	}
	if utf8.RuneCountInString(in.Reason) > MaxReasonRunes {
		return apiutil.InvalidArg(fmt.Sprintf("reason must be at most %d characters", MaxReasonRunes))
	}
	return nil
}

// Resolve applies an admin's action to an open report, records it on the
// report and writes one moderation_audit row with the actor, the target and
// the reason, all in one transaction with the action itself.
//
// An action other than DISMISS must act on the report's own target kind:
// HIDE_PERSPECTIVE on a perspective report, HIDE_PLACE on a place report,
// SUSPEND_USER on a user report; a mismatch is InvalidArg. Resolving only
// closes the report named: other open reports on the same target stay in the
// queue for their own resolution and audit row. A report already resolved is
// FailedPrecondition. An admin cannot suspend their own account.
//
// The reporter hears through a REPORT_RESOLVED notification enqueued in the
// same transaction. It names no actor: the resolution comes from the site,
// not from one admin, so the admin is not disclosed and a reporter's block
// of an admin does not suppress it.
func (s *Service) Resolve(ctx context.Context, admin uuid.UUID, in ResolveInput) (*perspectivv1.Report, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	var out *perspectivv1.Report
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		out, err = s.resolveTx(ctx, tx, admin, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) resolveTx(ctx context.Context, tx pgx.Tx, admin uuid.UUID, in ResolveInput) (*perspectivv1.Report, error) {
	q := s.q.WithTx(tx)
	if err := s.requireAdmin(ctx, q, admin, perspectivv1connect.ModerationServiceResolveProcedure); err != nil {
		return nil, err
	}
	r, err := q.LockReport(ctx, in.ReportID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apiutil.NotFound("report not found")
	}
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("lock report: %w", err))
	}
	if r.Status != perspectivv1.ReportStatus_REPORT_STATUS_OPEN.String() {
		return nil, apiutil.FailedPrecondition("report already resolved")
	}
	kind, err := targetKindFromDB(r.TargetKind)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	if err := applyAction(ctx, q, admin, in.Action, kind, r.TargetID); err != nil {
		return nil, err
	}
	if err := q.ResolveReport(ctx, dbq.ResolveReportParams{
		ID: r.ID, ResolvedBy: admin, Resolution: in.Action.String(), ResolutionReason: in.Reason,
	}); err != nil {
		return nil, apiutil.Internal(fmt.Errorf("resolve report: %w", err))
	}
	action, err := auditAction(in.Action)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	auditKind, err := auditTargetKind(kind)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	if err := writeAudit(ctx, q, AuditEntry{
		Actor: &admin, Action: action, TargetKind: auditKind,
		TargetID: &r.TargetID, ReportID: &r.ID, Reason: in.Reason,
	}); err != nil {
		return nil, apiutil.Internal(fmt.Errorf("write audit row: %w", err))
	}
	if err := s.notify.NotifyTx(ctx, tx, jobs.NotifyArgs{
		Kind:     perspectivv1.NotificationKind_NOTIFICATION_KIND_REPORT_RESOLVED,
		ReportID: r.ID,
	}); err != nil {
		return nil, apiutil.Internal(err)
	}
	row, err := q.GetReport(ctx, r.ID)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read resolved report: %w", err))
	}
	return reportProto(row)
}

// applyAction performs the moderation action on the report's target inside
// the resolving transaction. Hiding a perspective marks its arc's lore dirty
// in the same transaction, so the rank job recomputes lore without it.
func applyAction(ctx context.Context, q *dbq.Queries, admin uuid.UUID, action perspectivv1.ModerationAction,
	kind perspectivv1.ReportTargetKind, target uuid.UUID,
) error {
	wantKind, dismiss, _ := actionTargets(action)
	if dismiss {
		return nil
	}
	if wantKind != kind {
		return apiutil.InvalidArg(fmt.Sprintf("%s does not apply to a %s report", action, kind))
	}
	switch action {
	case perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PERSPECTIVE:
		arcID, err := q.HidePerspective(ctx, target)
		if errors.Is(err, pgx.ErrNoRows) {
			return apiutil.NotFound("the reported perspective no longer exists")
		}
		if err != nil {
			return apiutil.Internal(fmt.Errorf("hide perspective: %w", err))
		}
		if arcID != nil {
			if err := q.MarkArcLoreDirty(ctx, *arcID); err != nil {
				return apiutil.Internal(fmt.Errorf("mark arc lore dirty: %w", err))
			}
		}
		return nil
	case perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PLACE:
		n, err := q.HidePlace(ctx, target)
		return oneRow(n, err, "hide place", "the reported place no longer exists")
	case perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_LORE_POST:
		n, err := q.HideLorePost(ctx, target)
		return oneRow(n, err, "hide lore post", "the reported post no longer exists")
	case perspectivv1.ModerationAction_MODERATION_ACTION_CLEAR_LORE_NAME:
		n, err := q.ClearLoreName(ctx, target)
		return oneRow(n, err, "clear lore name", "the reported lore no longer exists")
	case perspectivv1.ModerationAction_MODERATION_ACTION_SUSPEND_USER:
		if target == admin {
			return apiutil.FailedPrecondition("you cannot suspend your own account")
		}
		n, err := q.SuspendUser(ctx, target)
		return oneRow(n, err, "suspend user", "the reported account no longer exists")
	case perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS,
		perspectivv1.ModerationAction_MODERATION_ACTION_UNSPECIFIED:
		return nil
	}
	return apiutil.InvalidArg("unknown action")
}

// oneRow turns an :execrows result into an error: a database error is
// Internal and zero rows is NotFound.
func oneRow(n int64, err error, op, missing string) error {
	if err != nil {
		return apiutil.Internal(fmt.Errorf("%s: %w", op, err))
	}
	if n == 0 {
		return apiutil.NotFound(missing)
	}
	return nil
}

// reportProto converts a queue row. Every enum column is parsed; a value
// outside the proto enum is Internal rather than a silently wrong answer.
func reportProto(r dbq.GetReportRow) (*perspectivv1.Report, error) {
	kind, err := targetKindFromDB(r.TargetKind)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	reason, err := reasonFromDB(r.Reason)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	status, err := statusFromDB(r.Status)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	out := &perspectivv1.Report{
		Id:               r.ID.String(),
		Reporter:         &perspectivv1.UserRef{UserId: r.ReporterID.String(), Username: r.ReporterUsername},
		TargetKind:       kind,
		TargetId:         r.TargetID.String(),
		TargetLabel:      r.TargetLabel,
		Reason:           reason,
		Details:          r.Details,
		Status:           status,
		ResolutionReason: r.ResolutionReason,
		CreatedAt:        timestamppb.New(r.CreatedAt),
	}
	if r.ResolvedBy != nil {
		out.ResolvedBy = &perspectivv1.UserRef{UserId: r.ResolvedBy.String(), Username: r.ResolverUsername}
	}
	if r.Resolution != nil {
		out.Resolution, err = actionFromDB(*r.Resolution)
		if err != nil {
			return nil, apiutil.Internal(err)
		}
	}
	if r.ResolvedAt != nil {
		out.ResolvedAt = timestamppb.New(*r.ResolvedAt)
	}
	return out, nil
}
