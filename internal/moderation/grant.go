package moderation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/moderation/dbq"
)

// grantReason is the audit reason of a grant made from the command line.
const grantReason = "granted with perspectiv admin grant"

// ErrUnknownUser is returned by GrantAdmin when no live account holds the
// username. The command exits non-zero on it.
var ErrUnknownUser = errors.New("no account has that username")

// GrantAdmin gives the account with this username the ADMIN role and writes a
// moderation_audit row with no actor, since the command line has no signed-in
// admin. It is the only path that grants ADMIN; no API route does.
//
// The username matches without regard to case; a deleted account matches
// nothing. Granting to an account that is already ADMIN changes nothing and
// writes no audit row, so running the command twice is harmless. The role is
// read from the users row on every admin call, so the grant takes effect at
// once for the moderation service; the role carried in the account's access
// token updates on its next sign-in or refresh.
func GrantAdmin(ctx context.Context, pool *pgxpool.Pool, username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("grant admin: a username is required")
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		u, err := q.LockActiveUserByUsername(ctx, username)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("grant admin to %q: %w", username, ErrUnknownUser)
		}
		if err != nil {
			return fmt.Errorf("grant admin: find user: %w", err)
		}
		if u.Role == perspectivv1.Role_ROLE_ADMIN.String() {
			return nil
		}
		if err := q.SetRoleAdmin(ctx, u.ID); err != nil {
			return fmt.Errorf("grant admin: set role: %w", err)
		}
		if err := writeAudit(ctx, q, AuditEntry{
			Action:     perspectivv1.AuditAction_AUDIT_ACTION_ADMIN_GRANT,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER,
			TargetID:   &u.ID,
			Reason:     grantReason,
		}); err != nil {
			return fmt.Errorf("grant admin: write audit row: %w", err)
		}
		return nil
	})
}
