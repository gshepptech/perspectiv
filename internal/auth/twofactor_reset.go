package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/auth/dbq"
	"perspectiv/internal/jobs"
	"perspectiv/internal/moderation"
)

// ErrUnknownUser is returned by ResetTwoFactor when no live account holds
// the username.
var ErrUnknownUser = errors.New("no account has that username")

// maxOperatorName bounds the operator name ResetTwoFactor records.
const maxOperatorName = 64

// ResetTwoFactor is the way back in for a member who lost both their
// authenticator and every recovery code. An operator runs it from the
// command line (perspectiv admin reset-two-factor) once they have satisfied
// themselves, outside the app, that the request comes from the account's
// owner; no API route reaches it. In one transaction it removes the
// account's two-factor sign-in, signs out every session, writes a
// TWO_FACTOR_RESET audit row with no actor and the reason "operator
// <operator>: <reason>", so the row says who ran it, and emails the member.
// An account without two-factor sign-in is refused, so nothing is audited
// that did not happen.
func ResetTwoFactor(ctx context.Context, pool *pgxpool.Pool, enq *jobs.Enqueuer, publicBaseURL, username, operator, reason string) error {
	username = strings.TrimSpace(username)
	audit, err := operatorReason(operator, reason)
	if err != nil {
		return err
	}
	if username == "" {
		return errors.New("reset two-factor: a username is required")
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		u, err := q.LockActiveUserByUsername(ctx, username)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("reset two-factor for %q: %w", username, ErrUnknownUser)
		}
		if err != nil {
			return fmt.Errorf("reset two-factor: find user: %w", err)
		}
		// Lock order step 3, after the users row above, before the rows.
		if err := q.LockTwoFactorAttempts(ctx, u.ID); err != nil {
			return fmt.Errorf("reset two-factor: lock two-factor: %w", err)
		}
		if _, err := q.GetTwoFactorStatus(ctx, u.ID); errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("reset two-factor for %q: %w", username, errTwoFactorOff)
		} else if err != nil {
			return fmt.Errorf("reset two-factor: read status: %w", err)
		}
		if err := endTwoFactor(ctx, q, u.ID); err != nil {
			return fmt.Errorf("reset two-factor: %w", err)
		}
		if err := moderation.WriteAudit(ctx, tx, moderation.AuditEntry{
			Action:     perspectivv1.AuditAction_AUDIT_ACTION_TWO_FACTOR_RESET,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER,
			TargetID:   &u.ID,
			Reason:     audit,
		}); err != nil {
			return fmt.Errorf("reset two-factor: write audit row: %w", err)
		}
		return enq.EmailTx(ctx, tx, jobs.EmailArgs{To: u.Email, Template: jobs.TemplateTwoFactorDisabled, Data: map[string]string{
			"link": strings.TrimSuffix(publicBaseURL, "/") + "/forgot-password", "username": u.Username,
		}})
	})
}

// operatorReason is the audit reason for an operator's reset: "operator
// <operator>: <reason>". Both are required, and the name is one line of at
// most maxOperatorName bytes.
func operatorReason(operator, reason string) (string, error) {
	operator, reason = strings.TrimSpace(operator), strings.TrimSpace(reason)
	if operator == "" || reason == "" {
		return "", errors.New("reset two-factor: the operator's name and a reason are required")
	}
	if len(operator) > maxOperatorName || strings.ContainsFunc(operator, unicode.IsControl) {
		return "", fmt.Errorf("reset two-factor: the operator's name must be one line of at most %d bytes", maxOperatorName)
	}
	return "operator " + operator + ": " + reason, nil
}

// endTwoFactor removes the account's two-factor sign-in and then signs out
// every session: after the challenges are gone, so a second step that was
// waiting on them cannot start a session this revocation misses.
func endTwoFactor(ctx context.Context, q *dbq.Queries, userID uuid.UUID) error {
	if err := removeTwoFactor(ctx, q, userID); err != nil {
		return err
	}
	if err := q.RevokeUserSessions(ctx, userID); err != nil {
		return internal("revoke sessions", err)
	}
	if err := q.RevokeUserRefreshTokens(ctx, userID); err != nil {
		return internal("revoke refresh tokens", err)
	}
	return nil
}
