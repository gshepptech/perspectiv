package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/auth/dbq"
)

var errSessionEnded = apiutil.Unauthenticated("your session has ended; sign in again")

// Authenticate turns an "Authorization: Bearer <access token>" header into
// the caller. The token must verify and its session must still be live in
// Postgres, so a logout, a reuse revocation or a deleted account takes
// effect on the next call; the role is read from the users row, not the
// token.
//
// A token that does not verify, or whose session has ended, is treated as
// no credentials (ok false) rather than an error. A route that needs a
// caller then answers Unauthenticated, which tells the browser to refresh,
// while the auth service's own cookie-authenticated calls (Refresh, Logout)
// still work for a browser that sends its expired access token along.
// Only a failure to check, such as Postgres being unreachable, is an error,
// which the interceptor turns into a refusal.
func (s *Service) Authenticate(ctx context.Context, h http.Header) (apiutil.Caller, bool, error) {
	tok, ok := bearerToken(h)
	if !ok {
		return apiutil.Caller{}, false, nil
	}
	userID, sessionID, err := s.tokens.parse(tok)
	if err != nil {
		return apiutil.Caller{}, false, nil
	}
	roleName, err := s.q.GetLiveSessionRole(ctx, dbq.GetLiveSessionRoleParams{SessionID: sessionID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return apiutil.Caller{}, false, nil
	}
	if err != nil {
		return apiutil.Caller{}, false, fmt.Errorf("authenticate: load session: %w", err)
	}
	role, err := roleFromDB(roleName)
	if err != nil {
		return apiutil.Caller{}, false, fmt.Errorf("authenticate: %w", err)
	}
	return apiutil.Caller{UserID: userID, Role: role, SessionID: sessionID}, true, nil
}

// EmailVerified is the gate every content write passes through: it reports
// whether the user may write, which needs a verified email and an account
// that is not suspended. An unknown or deleted account may not. A suspended
// account keeps signing in and reading; it is refused here, on every call,
// from the users row rather than the access token, so a suspension takes
// effect on the account's next write.
func (s *Service) EmailVerified(ctx context.Context, userID uuid.UUID) (bool, error) {
	ok, err := s.q.EmailVerified(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("email verified: %w", err)
	}
	return ok, nil
}

// Refresh exchanges a refresh token for a new one and a new access token.
// The presented token is marked rotated; presenting a rotated token again
// is reuse, a sign the token was copied, and revokes the whole session so
// neither copy works any more.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if refreshToken == "" {
		return Tokens{}, errSessionEnded
	}
	var out Tokens
	var reused *dbq.GetRefreshTokenForUpdateRow
	err := s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		row, err := q.GetRefreshTokenForUpdate(ctx, hashToken(refreshToken))
		if errors.Is(err, pgx.ErrNoRows) {
			return errSessionEnded
		}
		if err != nil {
			return internal("load refresh token", err)
		}
		if row.RotatedAt != nil {
			reused = &row
			// Committed, so the revocation holds although the call fails.
			return revokeSession(ctx, q, row.SessionID)
		}
		out, err = s.rotateRefresh(ctx, q, row)
		return err
	})
	if err != nil {
		return Tokens{}, err
	}
	if reused != nil {
		s.log.Warn().Str("event", "refresh_token_reuse").Str("sub", reused.UserID.String()).
			Str("session", reused.SessionID.String()).Msg("rotated refresh token presented again; session revoked")
		return Tokens{}, errSessionEnded
	}
	return out, nil
}

// rotateRefresh checks a not-yet-rotated refresh token is still live,
// marks it rotated and issues its successor.
func (s *Service) rotateRefresh(ctx context.Context, q *dbq.Queries, row dbq.GetRefreshTokenForUpdateRow) (Tokens, error) {
	now := time.Now()
	if row.RevokedAt != nil || row.SessionRevokedAt != nil ||
		!row.ExpiresAt.After(now) || !row.SessionExpiresAt.After(now) {
		return Tokens{}, errSessionEnded
	}
	u, err := q.GetUserByID(ctx, row.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, errSessionEnded
	}
	if err != nil {
		return Tokens{}, internal("load user", err)
	}
	role, err := roleFromDB(u.Role)
	if err != nil {
		return Tokens{}, apiutil.Internal(err)
	}
	if err := q.MarkRefreshTokenRotated(ctx, row.ID); err != nil {
		return Tokens{}, internal("rotate refresh token", err)
	}
	if err := q.TouchSession(ctx, row.SessionID); err != nil {
		return Tokens{}, internal("touch session", err)
	}
	return s.rotate(ctx, q, SessionUser{
		ID: u.ID, Username: u.Username, Role: role, EmailVerified: u.EmailVerifiedAt != nil,
	}, row.SessionID, &row.ID, row.SessionExpiresAt)
}

// Logout revokes the session behind the refresh token, and the caller's own
// session when the call carried a valid access token. Either may be absent;
// logging out with neither succeeds and does nothing.
func (s *Service) Logout(ctx context.Context, refreshToken string, callerSession uuid.UUID) error {
	return s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		var sessions []uuid.UUID
		if callerSession != uuid.Nil {
			sessions = append(sessions, callerSession)
		}
		if refreshToken != "" {
			row, err := q.GetRefreshTokenForUpdate(ctx, hashToken(refreshToken))
			switch {
			case err == nil:
				sessions = append(sessions, row.SessionID)
			case !errors.Is(err, pgx.ErrNoRows):
				return internal("load refresh token", err)
			}
		}
		for _, sid := range sessions {
			if err := revokeSession(ctx, q, sid); err != nil {
				return err
			}
			s.log.Info().Str("event", "logout").Str("session", sid.String()).Msg("session revoked")
		}
		return nil
	})
}

// revokeSession revokes a session and every refresh token in its chain.
func revokeSession(ctx context.Context, q *dbq.Queries, sid uuid.UUID) error {
	if err := q.RevokeSession(ctx, sid); err != nil {
		return internal("revoke session", err)
	}
	if err := q.RevokeSessionRefreshTokens(ctx, sid); err != nil {
		return internal("revoke refresh tokens", err)
	}
	return nil
}
