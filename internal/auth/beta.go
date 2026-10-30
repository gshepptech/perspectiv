package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/auth/dbq"
	"perspectiv/internal/jobs"
	"perspectiv/internal/moderation"
)

// SignUpMode decides who may create an account.
type SignUpMode string

const (
	// SignUpOpen lets anyone sign up.
	SignUpOpen SignUpMode = "open"
	// SignUpInvite needs an access code: a beta code or a member's invite
	// link code. Reading stays public.
	SignUpInvite SignUpMode = "invite"
)

// Beta access bounds.
const (
	maxCodeNote      = 200
	maxWaitlistNote  = 300
	maxCodeUses      = 10000
	maxCodeDays      = 365
	waitlistCodeDays = 30
	// maxInviteSignups is how many accounts one member's invite link can
	// admit while sign-up is invite-only: enough for the people who were
	// there, not enough to open the beta to the internet.
	maxInviteSignups = 5
)

var codePattern = regexp.MustCompile(`^[a-z0-9-]{4,32}$`)

// normalizeCode lower-cases a typed code and drops spaces around and inside
// it, so "  BETA-ucf " is "beta-ucf".
func normalizeCode(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), ""))
}

// admit checks the access code an invite-only sign-up carries, inside the
// sign-up transaction: a live beta code is used once, a member's invite code
// is checked for a live inviter. It returns what users.joined_with records.
// While sign-up is open no code is needed and none is used.
func (s *Service) admit(ctx context.Context, q *dbq.Queries, raw string) (*string, error) {
	if s.cfg.SignUpMode == SignUpOpen {
		return nil, nil
	}
	code := normalizeCode(raw)
	refused := apiutil.PermissionDenied("Perspectiv is invite-only for now: that code is not valid. Join the waitlist and we will email you.")
	if code == "" {
		return nil, apiutil.PermissionDenied("Perspectiv is invite-only for now: enter your invite code, or join the waitlist.")
	}
	used, err := q.UseAccessCode(ctx, code)
	switch {
	case err == nil:
		joined := "code:" + used
		return &joined, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, apiutil.Internal(fmt.Errorf("use access code: %w", err))
	}
	used, err = q.UseMemberInvite(ctx, dbq.UseMemberInviteParams{Code: code, MaxSignups: maxInviteSignups})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, refused
	case err != nil:
		return nil, apiutil.Internal(fmt.Errorf("use invite code: %w", err))
	}
	joined := "invite:" + used
	return &joined, nil
}

// InviteOnly reports whether signing up needs an access code.
func (s *Service) InviteOnly() bool { return s.cfg.SignUpMode == SignUpInvite }

// JoinWaitlist records a request to join. A repeat request is a no-op, and
// both answer the same, so the list reveals nothing about who is on it.
func (s *Service) JoinWaitlist(ctx context.Context, email, note, honeypot string) error {
	if honeypot != "" {
		s.log.Info().Str("event", "waitlist_honeypot").Msg("automated waitlist request ignored")
		return nil
	}
	addr, err := validateEmail(email)
	if err != nil {
		return err
	}
	note = strings.Join(strings.Fields(note), " ")
	if utf8.RuneCountInString(note) > maxWaitlistNote {
		return apiutil.InvalidArg(fmt.Sprintf("keep the note to %d characters", maxWaitlistNote))
	}
	if err := s.q.JoinWaitlist(ctx, dbq.JoinWaitlistParams{Email: addr, Note: note}); err != nil {
		return apiutil.Internal(fmt.Errorf("join waitlist: %w", err))
	}
	s.log.Info().Str("event", "waitlist_joined").Msg("waitlist request")
	return nil
}

// requireAdmin re-reads the caller's stored role: the interceptor checked the
// token's, and a token minted before a demotion, suspension or deletion must
// not outlive it. Every refusal writes the authz_denied line the interceptor
// writes, with the method and the reason.
func (s *Service) requireAdmin(ctx context.Context, caller uuid.UUID, method string) error {
	a, err := s.q.ActorStanding(ctx, caller)
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
		if role, err = roleFromDB(a.Role); err != nil {
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
	s.log.Warn().Str("event", "authz_denied").Str("sub", caller.String()).Str("method", method).
		Str("required", "admin").Str("role", role.String()).Str("reason", reason).Msg("request denied")
	return apiutil.PermissionDenied("admin only")
}

// AccessCodeInput asks for one beta code.
type AccessCodeInput struct {
	Code          string
	Note          string
	MaxUses       int32
	ExpiresInDays int32
}

// generatedCode is "beta-" and eight random letters and digits.
func generatedCode() (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "beta-" + string(b), nil
}

// CreateAccessCode issues a beta code for an admin.
func (s *Service) CreateAccessCode(ctx context.Context, admin uuid.UUID, in AccessCodeInput) (dbq.InsertAccessCodeRow, error) {
	if err := s.requireAdmin(ctx, admin, perspectivv1connect.AuthServiceCreateAccessCodeProcedure); err != nil {
		return dbq.InsertAccessCodeRow{}, err
	}
	code := normalizeCode(in.Code)
	if code == "" {
		var err error
		if code, err = generatedCode(); err != nil {
			return dbq.InsertAccessCodeRow{}, apiutil.Internal(err)
		}
	}
	note := strings.Join(strings.Fields(in.Note), " ")
	switch {
	case !codePattern.MatchString(code):
		return dbq.InsertAccessCodeRow{}, apiutil.InvalidArg("a code is 4 to 32 lower-case letters, digits and dashes")
	case utf8.RuneCountInString(note) > maxCodeNote:
		return dbq.InsertAccessCodeRow{}, apiutil.InvalidArg(fmt.Sprintf("keep the note to %d characters", maxCodeNote))
	case in.MaxUses < 1 || in.MaxUses > maxCodeUses:
		return dbq.InsertAccessCodeRow{}, apiutil.InvalidArg(fmt.Sprintf("uses must be 1 to %d", maxCodeUses))
	case in.ExpiresInDays < 0 || in.ExpiresInDays > maxCodeDays:
		return dbq.InsertAccessCodeRow{}, apiutil.InvalidArg(fmt.Sprintf("expiry must be 0 to %d days", maxCodeDays))
	}
	var expires *time.Time
	if in.ExpiresInDays > 0 {
		t := time.Now().Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour)
		expires = &t
	}
	var row dbq.InsertAccessCodeRow
	err := s.inTx(ctx, func(q *dbq.Queries, tx pgx.Tx) error {
		var err error
		row, err = q.InsertAccessCode(ctx, dbq.InsertAccessCodeParams{
			Code: code, Note: note, MaxUses: in.MaxUses, CreatedBy: admin, ExpiresAt: expires,
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return apiutil.AlreadyExists("that code already exists")
		}
		if err != nil {
			return apiutil.Internal(fmt.Errorf("insert access code: %w", err))
		}
		return auditCode(ctx, tx, admin, perspectivv1.AuditAction_AUDIT_ACTION_ACCESS_CODE_CREATE, code,
			fmt.Sprintf("%d uses. %s", in.MaxUses, note))
	})
	if err != nil {
		return dbq.InsertAccessCodeRow{}, err
	}
	s.log.Info().Str("event", "access_code_created").Str("sub", admin.String()).Str("code", code).
		Int32("max_uses", in.MaxUses).Msg("beta code issued")
	return row, nil
}

// codeCursor is the access code list page token payload.
type codeCursor struct {
	At   time.Time `json:"at"`
	Code string    `json:"c"`
}

// ListAccessCodes pages the beta codes, newest first.
func (s *Service) ListAccessCodes(ctx context.Context, admin uuid.UUID, page apiutil.PageToken) ([]dbq.ListAccessCodesRow, string, error) {
	if err := s.requireAdmin(ctx, admin, perspectivv1connect.AuthServiceListAccessCodesProcedure); err != nil {
		return nil, "", err
	}
	p := dbq.ListAccessCodesParams{Lim: page.Limit + 1}
	if page.After != "" {
		var c codeCursor
		if err := json.Unmarshal([]byte(page.After), &c); err != nil || c.Code == "" {
			return nil, "", apiutil.InvalidArg("invalid page token")
		}
		p.AfterCreated, p.AfterCode = &c.At, &c.Code
	}
	rows, err := s.q.ListAccessCodes(ctx, p)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list access codes: %w", err))
	}
	if len(rows) <= int(page.Limit) {
		return rows, "", nil
	}
	rows = rows[:page.Limit]
	last := rows[len(rows)-1]
	return rows, mustToken(codeCursor{At: last.CreatedAt, Code: last.Code}), nil
}

// RevokeAccessCode stops a code admitting anyone more; accounts it already
// admitted keep their access.
func (s *Service) RevokeAccessCode(ctx context.Context, admin uuid.UUID, code string) (dbq.RevokeAccessCodeRow, error) {
	if err := s.requireAdmin(ctx, admin, perspectivv1connect.AuthServiceRevokeAccessCodeProcedure); err != nil {
		return dbq.RevokeAccessCodeRow{}, err
	}
	var row dbq.RevokeAccessCodeRow
	err := s.inTx(ctx, func(q *dbq.Queries, tx pgx.Tx) error {
		var err error
		row, err = q.RevokeAccessCode(ctx, normalizeCode(code))
		if errors.Is(err, pgx.ErrNoRows) {
			return apiutil.NotFound("no such code")
		}
		if err != nil {
			return apiutil.Internal(fmt.Errorf("revoke access code: %w", err))
		}
		return auditCode(ctx, tx, admin, perspectivv1.AuditAction_AUDIT_ACTION_ACCESS_CODE_REVOKE, row.Code, "")
	})
	if err != nil {
		return dbq.RevokeAccessCodeRow{}, err
	}
	s.log.Info().Str("event", "access_code_revoked").Str("sub", admin.String()).Str("code", row.Code).Msg("beta code revoked")
	return row, nil
}

// waitCursor is the waitlist page token payload.
type waitCursor struct {
	At time.Time `json:"at"`
	ID uuid.UUID `json:"id"`
}

// ListWaitlist pages the waitlist, oldest first.
func (s *Service) ListWaitlist(ctx context.Context, admin uuid.UUID, waitingOnly bool, page apiutil.PageToken) ([]dbq.Waitlist, string, error) {
	if err := s.requireAdmin(ctx, admin, perspectivv1connect.AuthServiceListWaitlistProcedure); err != nil {
		return nil, "", err
	}
	p := dbq.ListWaitlistParams{WaitingOnly: waitingOnly, Lim: page.Limit + 1}
	if page.After != "" {
		var c waitCursor
		if err := json.Unmarshal([]byte(page.After), &c); err != nil || c.ID == uuid.Nil {
			return nil, "", apiutil.InvalidArg("invalid page token")
		}
		p.AfterCreated, p.AfterID = &c.At, &c.ID
	}
	out, err := s.q.ListWaitlist(ctx, p)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list waitlist: %w", err))
	}
	if len(out) <= int(page.Limit) {
		return out, "", nil
	}
	out = out[:page.Limit]
	last := out[len(out)-1]
	return out, mustToken(waitCursor{At: last.CreatedAt, ID: last.ID}), nil
}

// InviteFromWaitlist issues a waiting person a single-use code that expires
// in 30 days and emails it, in one transaction. Inviting someone already
// invited sends the same code again.
func (s *Service) InviteFromWaitlist(ctx context.Context, admin, id uuid.UUID) (dbq.Waitlist, error) {
	if err := s.requireAdmin(ctx, admin, perspectivv1connect.AuthServiceInviteFromWaitlistProcedure); err != nil {
		return dbq.Waitlist{}, err
	}
	var out dbq.Waitlist
	err := s.inTx(ctx, func(q *dbq.Queries, tx pgx.Tx) error {
		w, err := q.LockWaitlistEntry(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apiutil.NotFound("no such waitlist entry")
		}
		if err != nil {
			return apiutil.Internal(fmt.Errorf("lock waitlist entry: %w", err))
		}
		out = w
		code := ""
		if w.Code != nil {
			code = *w.Code
		} else {
			if code, err = generatedCode(); err != nil {
				return apiutil.Internal(err)
			}
			code = "wl-" + strings.TrimPrefix(code, "beta-")
			expires := time.Now().Add(waitlistCodeDays * 24 * time.Hour)
			if _, err := q.InsertAccessCode(ctx, dbq.InsertAccessCodeParams{
				Code: code, Note: "waitlist: " + apiutil.MaskEmail(w.Email), MaxUses: 1, CreatedBy: admin, ExpiresAt: &expires,
			}); err != nil {
				return apiutil.Internal(fmt.Errorf("insert waitlist code: %w", err))
			}
			m, err := q.MarkWaitlistInvited(ctx, dbq.MarkWaitlistInvitedParams{ID: id, Code: code})
			if err != nil {
				return apiutil.Internal(fmt.Errorf("mark invited: %w", err))
			}
			out = m
		}
		if err := moderation.WriteAudit(ctx, tx, moderation.AuditEntry{
			Actor: &admin, Action: perspectivv1.AuditAction_AUDIT_ACTION_WAITLIST_INVITE,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_WAITLIST_ENTRY, TargetID: &w.ID,
			Reason: "sent code " + code,
		}); err != nil {
			return apiutil.Internal(fmt.Errorf("write audit row: %w", err))
		}
		return s.enq.EmailTx(ctx, tx, jobs.EmailArgs{
			To: w.Email, Template: jobs.TemplateBetaInvite,
			Data: map[string]string{"link": s.signUpLink(code)},
		})
	})
	if err != nil {
		return dbq.Waitlist{}, err
	}
	s.log.Info().Str("event", "waitlist_invited").Str("sub", admin.String()).Str("waitlist_id", id.String()).Msg("waitlist invite sent")
	return out, nil
}

// auditCode records an admin action on an access code in the audit log,
// inside the action's transaction.
func auditCode(ctx context.Context, tx pgx.Tx, admin uuid.UUID, action perspectivv1.AuditAction, code, reason string) error {
	if err := moderation.WriteAudit(ctx, tx, moderation.AuditEntry{
		Actor: &admin, Action: action, TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_ACCESS_CODE,
		TargetRef: code, Reason: strings.TrimSpace(reason),
	}); err != nil {
		return apiutil.Internal(fmt.Errorf("write audit row: %w", err))
	}
	return nil
}

// signUpLink is the sign-up page with the code filled in.
func (s *Service) signUpLink(code string) string {
	return strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/signup?code=" + url.QueryEscape(code)
}

// mustToken mints a page token from a cursor that always marshals.
func mustToken(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return apiutil.NextPageToken(string(b))
}
