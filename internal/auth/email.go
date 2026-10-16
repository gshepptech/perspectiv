package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/auth/dbq"
	"perspectiv/internal/jobs"
)

var (
	errBadVerifyLink = apiutil.InvalidArg("this verification link is invalid or has expired")
	errBadResetLink  = apiutil.InvalidArg("this reset link is invalid or has expired")
)

// VerifyEmail follows a verification link: an unused, unexpired token marks
// the account's email verified and retires the account's other verification
// links. Verifying an already verified account keeps its first verification
// time.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	if token == "" {
		return errBadVerifyLink
	}
	var userID string
	err := s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		id, err := q.ConsumeEmailToken(ctx, dbq.ConsumeEmailTokenParams{TokenHash: hashToken(token), Purpose: purposeVerifyEmail})
		if errors.Is(err, pgx.ErrNoRows) {
			return errBadVerifyLink
		}
		if err != nil {
			return internal("consume verification token", err)
		}
		if err := q.MarkEmailVerified(ctx, id); err != nil {
			return internal("mark email verified", err)
		}
		if err := q.RetireEmailTokens(ctx, dbq.RetireEmailTokensParams{UserID: id, Purpose: purposeVerifyEmail}); err != nil {
			return internal("retire verification tokens", err)
		}
		userID = id.String()
		return nil
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "email_verified").Str("sub", userID).Msg("email verified")
	return nil
}

// RequestPasswordReset emails a reset link to the account with email. It
// answers the same whether or not the account exists, including for a
// malformed address, so the answer never reveals who has an account. Reset
// emails are limited per account by LimitPasswordReset; a request over the
// limit is dropped just as silently.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	addr, err := validateEmail(email)
	if err != nil {
		return nil //nolint:nilerr // a malformed address is answered like an unknown one
	}
	u, err := s.q.GetUserByEmail(ctx, addr)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return internal("get user", err)
	}
	allowed, err := s.lim.Allow(ctx, LimitPasswordReset, "user:"+u.ID.String())
	if err != nil {
		return apiutil.Internal(err)
	}
	if !allowed {
		s.log.Warn().Str("event", "password_reset_limited").Str("sub", u.ID.String()).Msg("reset email not sent")
		return nil
	}
	tok, tokHash, err := newOpaqueToken()
	if err != nil {
		return apiutil.Internal(err)
	}
	err = s.inTx(ctx, func(q *dbq.Queries, tx pgx.Tx) error {
		if err := q.InsertEmailToken(ctx, dbq.InsertEmailTokenParams{
			UserID: u.ID, Purpose: purposePasswordReset, TokenHash: tokHash, ExpiresAt: time.Now().Add(ResetTokenTTL),
		}); err != nil {
			return internal("insert reset token", err)
		}
		if err := s.enq.EmailTx(ctx, tx, jobs.EmailArgs{
			To: u.Email, Template: jobs.TemplatePasswordReset,
			Data: map[string]string{"link": s.link(resetPasswordPage, tok), "username": u.Username},
		}); err != nil {
			return apiutil.Internal(err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "password_reset_requested").Str("sub", u.ID.String()).Msg("reset email queued")
	return nil
}

// ResetPassword follows a reset link: it sets the new password, retires the
// account's other reset links and revokes every session, so whoever held the
// old password is signed out everywhere. Following a link sent to the
// address proves the address, so the email is marked verified too. A weak
// new password is refused and leaves the link usable.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if token == "" {
		return errBadResetLink
	}
	var userID string
	err := s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		id, err := q.ConsumeEmailToken(ctx, dbq.ConsumeEmailTokenParams{TokenHash: hashToken(token), Purpose: purposePasswordReset})
		if errors.Is(err, pgx.ErrNoRows) {
			return errBadResetLink
		}
		if err != nil {
			return internal("consume reset token", err)
		}
		u, err := q.GetUserByID(ctx, id)
		if err != nil {
			return internal("get user", err)
		}
		if err := ValidatePassword(newPassword, u.Email, u.Username); err != nil {
			return err
		}
		hash, err := s.passwords.Hash(newPassword)
		if err != nil {
			return apiutil.Internal(err)
		}
		steps := []struct {
			what string
			run  func() error
		}{
			{"update password", func() error {
				return q.UpdatePasswordHash(ctx, dbq.UpdatePasswordHashParams{ID: id, PasswordHash: hash})
			}},
			{"retire reset tokens", func() error {
				return q.RetireEmailTokens(ctx, dbq.RetireEmailTokensParams{UserID: id, Purpose: purposePasswordReset})
			}},
			{"mark email verified", func() error { return q.MarkEmailVerified(ctx, id) }},
			{"revoke sessions", func() error { return q.RevokeUserSessions(ctx, id) }},
			{"revoke refresh tokens", func() error { return q.RevokeUserRefreshTokens(ctx, id) }},
		}
		for _, st := range steps {
			if err := st.run(); err != nil {
				return internal(st.what, err)
			}
		}
		userID = id.String()
		return nil
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "password_reset").Str("sub", userID).Msg("password reset; sessions revoked")
	return nil
}
