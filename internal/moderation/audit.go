package moderation

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/moderation/dbq"
)

// AuditEntry is one admin action for the audit log. Actor is nil for an
// action taken from the command line. A target is named by TargetID, or for
// an access code by TargetRef, never both.
type AuditEntry struct {
	Actor      *uuid.UUID
	Action     perspectivv1.AuditAction
	TargetKind perspectivv1.AuditTargetKind
	TargetID   *uuid.UUID
	TargetRef  string
	ReportID   *uuid.UUID
	Reason     string
}

// WriteAudit appends e to the audit log inside the caller's transaction, so
// the action and its record commit or roll back together. Other domains
// call it for their admin actions.
func WriteAudit(ctx context.Context, tx pgx.Tx, e AuditEntry) error {
	return writeAudit(ctx, dbq.New(tx), e)
}

func writeAudit(ctx context.Context, q *dbq.Queries, e AuditEntry) error {
	if !validAuditAction(e.Action) || !validAuditTargetKind(e.TargetKind) {
		return fmt.Errorf("audit entry: action %v on %v is not a recordable value", e.Action, e.TargetKind)
	}
	if (e.TargetID == nil) == (e.TargetRef == "") {
		return errors.New("audit entry: exactly one of target id and target ref is required")
	}
	return q.InsertAudit(ctx, dbq.InsertAuditParams{
		ActorID: e.Actor, Action: e.Action.String(), TargetKind: e.TargetKind.String(),
		TargetID: e.TargetID, TargetRef: e.TargetRef, ReportID: e.ReportID, Reason: e.Reason,
	})
}

// AuditFilter narrows the audit log; a zero field is no filter.
type AuditFilter struct {
	Action  perspectivv1.AuditAction
	Actor   uuid.UUID
	Subject uuid.UUID
}

// ListAuditLog returns one page of the audit log, newest first.
func (s *Service) ListAuditLog(ctx context.Context, admin uuid.UUID, f AuditFilter, page apiutil.PageToken) ([]*perspectivv1.AuditEntry, string, error) {
	if err := s.requireAdmin(ctx, s.q, admin, perspectivv1connect.ModerationServiceListAuditLogProcedure); err != nil {
		return nil, "", err
	}
	params := dbq.ListAuditLogParams{MaxRows: page.Limit + 1}
	if f.Action != perspectivv1.AuditAction_AUDIT_ACTION_UNSPECIFIED {
		if !validAuditAction(f.Action) {
			return nil, "", apiutil.InvalidArg("action is not a known audit action")
		}
		a := f.Action.String()
		params.Action = &a
	}
	if f.Actor != uuid.Nil {
		params.ActorID = &f.Actor
	}
	if f.Subject != uuid.Nil {
		params.SubjectID = &f.Subject
	}
	if page.After != "" {
		before, err := strconv.ParseInt(page.After, 10, 64)
		if err != nil || before <= 0 {
			return nil, "", apiutil.InvalidArg("invalid page token")
		}
		params.BeforeID = &before
	}
	rows, err := s.q.ListAuditLog(ctx, params)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list audit log: %w", err))
	}
	next := ""
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		next = strconv.FormatInt(rows[len(rows)-1].ID, 10)
	}
	out := make([]*perspectivv1.AuditEntry, 0, len(rows))
	for _, r := range rows {
		e, err := auditProto(r)
		if err != nil {
			return nil, "", apiutil.Internal(err)
		}
		out = append(out, e)
	}
	return out, apiutil.NextPageToken(next), nil
}

func auditProto(r dbq.ListAuditLogRow) (*perspectivv1.AuditEntry, error) {
	action, err := enumFromDB[perspectivv1.AuditAction]("moderation_audit.action", r.Action, perspectivv1.AuditAction_value)
	if err != nil {
		return nil, err
	}
	kind, err := enumFromDB[perspectivv1.AuditTargetKind]("moderation_audit.target_kind", r.TargetKind, perspectivv1.AuditTargetKind_value)
	if err != nil {
		return nil, err
	}
	e := &perspectivv1.AuditEntry{
		Id: strconv.FormatInt(r.ID, 10), Action: action, TargetKind: kind, TargetRef: r.TargetRef,
		TargetLabel: r.TargetLabel, Reason: r.Reason, At: timestamppb.New(r.At),
	}
	if r.ActorID != nil {
		e.Actor = &perspectivv1.UserRef{UserId: r.ActorID.String(), Username: r.ActorUsername}
	}
	if r.TargetID != nil {
		e.TargetId = r.TargetID.String()
	}
	if r.ReportID != nil {
		e.ReportId = r.ReportID.String()
	}
	return e, nil
}

var errNoSuchUser = apiutil.NotFound("no account matches")

// LookupUser finds one account by id, email or username (in that order of
// recognition: an id parses as one, an email has an @) and returns what a
// moderator needs to judge a report about it. The email is masked. A match
// is itself an admin action and writes a USER_LOOKUP audit entry naming how
// the account was found, never the address searched for.
func (s *Service) LookupUser(ctx context.Context, admin uuid.UUID, query string) (*perspectivv1.AdminUserView, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, apiutil.InvalidArg("enter a username, email or account id")
	}
	var view *perspectivv1.AdminUserView
	missedBy := ""
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := s.requireAdmin(ctx, q, admin, perspectivv1connect.ModerationServiceLookupUserProcedure); err != nil {
			return err
		}
		id, how, err := findUser(ctx, q, query)
		if errors.Is(err, errNoSuchUser) {
			missedBy = how
		}
		if err != nil {
			return err
		}
		if view, err = adminUserView(ctx, q, id); err != nil {
			return err
		}
		return writeAudit(ctx, q, AuditEntry{
			Actor: &admin, Action: perspectivv1.AuditAction_AUDIT_ACTION_USER_LOOKUP,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER, TargetID: &id, Reason: "looked up by " + how,
		})
	})
	if missedBy != "" {
		// A miss is audited too, so probing for which addresses have
		// accounts shows in the log; the searched text is never kept.
		if aerr := writeAudit(ctx, s.q, AuditEntry{
			Actor: &admin, Action: perspectivv1.AuditAction_AUDIT_ACTION_USER_LOOKUP,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER, TargetRef: noMatchRef,
			Reason: "looked up by " + missedBy + ", no match",
		}); aerr != nil {
			return nil, apiutil.Internal(fmt.Errorf("write audit row: %w", aerr))
		}
	}
	if err != nil {
		return nil, err
	}
	return view, nil
}

// noMatchRef stands in for the account of a lookup that found none; the
// audit row needs a target and the searched text is not kept.
const noMatchRef = "no match"

func findUser(ctx context.Context, q *dbq.Queries, query string) (uuid.UUID, string, error) {
	var (
		id  uuid.UUID
		how string
		err error
	)
	if parsed, perr := uuid.Parse(query); perr == nil {
		id, err = q.FindUserByID(ctx, parsed)
		how = "id"
	} else if strings.Contains(query, "@") {
		if _, perr := mail.ParseAddress(query); perr != nil {
			return uuid.Nil, "", apiutil.InvalidArg("that is not an email address")
		}
		id, err = q.FindUserByEmail(ctx, query)
		how = "email"
	} else {
		id, err = q.FindUserByUsername(ctx, query)
		how = "username"
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, how, errNoSuchUser
	}
	if err != nil {
		return uuid.Nil, "", apiutil.Internal(fmt.Errorf("find user: %w", err))
	}
	return id, how, nil
}

func adminUserView(ctx context.Context, q *dbq.Queries, id uuid.UUID) (*perspectivv1.AdminUserView, error) {
	u, err := q.AdminUserFacts(ctx, id)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read account: %w", err))
	}
	against, err := q.CountReportsAgainst(ctx, id)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("count reports: %w", err))
	}
	role, err := roleFromDB(u.Role)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	v := &perspectivv1.AdminUserView{
		UserId: u.ID.String(), Username: u.Username, EmailVerified: u.EmailVerified, Role: role,
		JoinedAt: timestamppb.New(u.CreatedAt), Deleted: u.Deleted,
		FailedSignIns_24H: u.FailedSignIns24h, Stories: u.Stories, HiddenStories: u.HiddenStories,
		OpenReportsAgainst: against.Open, ReportsAgainst: against.Total, ReportsFiled: u.ReportsFiled,
	}
	// A deleted account's address is a placeholder; there is nothing to hint.
	if !u.Deleted {
		v.EmailHint = apiutil.MaskEmail(u.Email)
	}
	if u.SuspendedAt != nil {
		v.SuspendedAt = timestamppb.New(*u.SuspendedAt)
	}
	last, err := q.LastSignIn(ctx, id)
	switch {
	case err == nil:
		v.LastSignInAt = timestamppb.New(last)
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, apiutil.Internal(fmt.Errorf("read last sign-in: %w", err))
	}
	return v, nil
}
