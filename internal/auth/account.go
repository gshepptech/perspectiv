package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/auth/dbq"
	"perspectiv/internal/jobs"
)

// EmailChangeTokenTTL is how long a link to confirm a new email address works.
const EmailChangeTokenTTL = 48 * time.Hour

const (
	purposeChangeEmail = "change_email"
	confirmEmailPage   = "/confirm-email"
)

var (
	errWrongPassword     = apiutil.PermissionDenied("your current password is not right")
	errBadEmailChange    = apiutil.InvalidArg("this confirmation link is invalid or has expired")
	errSameEmail         = apiutil.InvalidArg("that is already your email")
	errEmailTaken        = apiutil.AlreadyExists("an account with that email already exists")
	errAccountNotFound   = apiutil.NotFound("account not found")
	errNoCurrentPassword = apiutil.InvalidArg("enter your current password")
)

// checkPassword verifies a signed-in user's current password under the same
// lockout as Login: the attempt is recorded in login_attempts, and an account
// locked out by failed logins cannot use this to keep guessing.
func (s *Service) checkPassword(ctx context.Context, q *dbq.Queries, u dbq.GetUserByIDRow, password string) error {
	if password == "" {
		return errNoCurrentPassword
	}
	if err := q.LockLoginAttempts(ctx, u.ID); err != nil {
		return internal("lock login attempts", err)
	}
	failures, err := q.CountRecentFailedLogins(ctx, dbq.CountRecentFailedLoginsParams{
		UserID: u.ID, WindowMinutes: int32(LockoutWindow / time.Minute),
	})
	if err != nil {
		return internal("count failed logins", err)
	}
	if failures >= LockoutFailures {
		return errLockedOut
	}
	ok, err := s.passwords.VerifyPassword(u.PasswordHash, password)
	if err != nil {
		return apiutil.Internal(err)
	}
	if err := q.InsertLoginAttempt(ctx, dbq.InsertLoginAttemptParams{UserID: u.ID, Succeeded: ok}); err != nil {
		return internal("record login attempt", err)
	}
	if !ok {
		return errWrongPassword
	}
	return nil
}

// signedInUser loads the caller's account.
func signedInUser(ctx context.Context, q *dbq.Queries, id uuid.UUID) (dbq.GetUserByIDRow, error) {
	u, err := q.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.GetUserByIDRow{}, errAccountNotFound
	}
	if err != nil {
		return dbq.GetUserByIDRow{}, internal("get user", err)
	}
	return u, nil
}

// ChangePassword sets a new password for the signed-in caller, given their
// current one, and signs out every other device. The caller's own session
// stays signed in. A wrong current password is recorded as a failed login.
func (s *Service) ChangePassword(ctx context.Context, caller apiutil.Caller, current, next string) error {
	var refused error
	err := s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		u, err := signedInUser(ctx, q, caller.UserID)
		if err != nil {
			return err
		}
		if err := s.checkPassword(ctx, q, u, current); err != nil {
			// A refused attempt must commit, so the refusal is returned
			// after the transaction rather than rolling it back.
			return deferRefusal(err, &refused)
		}
		if err := ValidatePassword(next, u.Email, u.Username); err != nil {
			return err
		}
		hash, err := s.passwords.Hash(next)
		if err != nil {
			return apiutil.Internal(err)
		}
		if err := q.UpdatePasswordHash(ctx, dbq.UpdatePasswordHashParams{ID: u.ID, PasswordHash: hash}); err != nil {
			return internal("update password", err)
		}
		// A reset link sent before the change would undo it, and an email
		// change started by whoever had the old password would hand them
		// the account.
		if err := retireTokens(ctx, q, u.ID, purposePasswordReset, purposeChangeEmail); err != nil {
			return err
		}
		return revokeOthers(ctx, q, u.ID, caller.SessionID)
	})
	if err != nil {
		return err
	}
	if refused != nil {
		s.logRefusal("password_change", caller.UserID, refused)
		return refused
	}
	s.log.Info().Str("event", "password_changed").Str("sub", caller.UserID.String()).Msg("password changed; other sessions revoked")
	return nil
}

// deferRefusal keeps a wrong password or a lockout in refused for the caller
// to return after the transaction commits the recorded attempt; any other
// error rolls the transaction back.
func deferRefusal(err error, refused *error) error {
	if errors.Is(err, errWrongPassword) || errors.Is(err, errLockedOut) || errors.Is(err, errNoCurrentPassword) {
		*refused = err
		return nil
	}
	return err
}

func (s *Service) logRefusal(action string, userID uuid.UUID, err error) {
	reason := "wrong_password"
	switch {
	case errors.Is(err, errLockedOut):
		reason = "locked_out"
	case errors.Is(err, errNoCurrentPassword):
		reason = "no_password"
	}
	s.log.Warn().Str("event", action+"_refused").Str("reason", reason).Str("sub", userID.String()).Msg("current password not accepted")
}

// retireTokens spends every unused emailed link of the given purposes.
func retireTokens(ctx context.Context, q *dbq.Queries, userID uuid.UUID, purposes ...string) error {
	for _, p := range purposes {
		if err := q.RetireEmailTokens(ctx, dbq.RetireEmailTokensParams{UserID: userID, Purpose: p}); err != nil {
			return internal("retire "+p+" tokens", err)
		}
	}
	return nil
}

func revokeOthers(ctx context.Context, q *dbq.Queries, userID, keep uuid.UUID) error {
	if err := q.RevokeOtherSessions(ctx, dbq.RevokeOtherSessionsParams{UserID: userID, Keep: keep}); err != nil {
		return internal("revoke other sessions", err)
	}
	if err := q.RevokeOtherRefreshTokens(ctx, dbq.RevokeOtherRefreshTokensParams{UserID: userID, Keep: keep}); err != nil {
		return internal("revoke other refresh tokens", err)
	}
	return nil
}

// RequestEmailChange starts moving the caller's account to newEmail, given
// their current password. The new address gets a link that makes the change;
// the old address gets a notice, so someone who took over a session cannot
// move the account away quietly. Nothing changes until the link is followed.
func (s *Service) RequestEmailChange(ctx context.Context, caller apiutil.Caller, newEmail, current string) error {
	addr, err := validateEmail(newEmail)
	if err != nil {
		return err
	}
	tok, tokHash, err := newOpaqueToken()
	if err != nil {
		return apiutil.Internal(err)
	}
	var refused error
	err = s.inTx(ctx, func(q *dbq.Queries, tx pgx.Tx) error {
		u, err := signedInUser(ctx, q, caller.UserID)
		if err != nil {
			return err
		}
		if err := s.checkPassword(ctx, q, u, current); err != nil {
			return deferRefusal(err, &refused)
		}
		if strings.EqualFold(addr, u.Email) {
			return errSameEmail
		}
		taken, err := q.EmailTaken(ctx, dbq.EmailTakenParams{Email: addr, ID: u.ID})
		if err != nil {
			return internal("check email", err)
		}
		if taken {
			return errEmailTaken
		}
		// Only the newest request counts.
		if err := q.RetireEmailTokens(ctx, dbq.RetireEmailTokensParams{UserID: u.ID, Purpose: purposeChangeEmail}); err != nil {
			return internal("retire change tokens", err)
		}
		if err := q.InsertEmailChangeToken(ctx, dbq.InsertEmailChangeTokenParams{
			UserID: u.ID, TokenHash: tokHash, ExpiresAt: time.Now().Add(EmailChangeTokenTTL), NewEmail: addr,
		}); err != nil {
			return internal("insert change token", err)
		}
		return s.emailChangeMail(ctx, tx, u, addr, tok)
	})
	if err != nil {
		return err
	}
	if refused != nil {
		s.logRefusal("email_change", caller.UserID, refused)
		return refused
	}
	s.log.Info().Str("event", "email_change_requested").Str("sub", caller.UserID.String()).Msg("confirmation email queued")
	return nil
}

func (s *Service) emailChangeMail(ctx context.Context, tx pgx.Tx, u dbq.GetUserByIDRow, addr, tok string) error {
	mails := []jobs.EmailArgs{
		{To: addr, Template: jobs.TemplateChangeEmail, Data: map[string]string{
			"link": s.link(confirmEmailPage, tok), "username": u.Username,
		}},
		{To: u.Email, Template: jobs.TemplateEmailChangeNotice, Data: map[string]string{
			"link": strings.TrimSuffix(s.cfg.PublicBaseURL, "/") + "/forgot-password", "username": u.Username,
		}},
	}
	for _, m := range mails {
		if err := s.enq.EmailTx(ctx, tx, m); err != nil {
			return apiutil.Internal(err)
		}
	}
	return nil
}

// ConfirmEmailChange follows the link sent to a new address and makes it
// the account's email. Following the link proves the address, so it is
// verified. If another account took the address in the meantime, the change
// is refused and the link is spent.
func (s *Service) ConfirmEmailChange(ctx context.Context, token string) error {
	if token == "" {
		return errBadEmailChange
	}
	var userID uuid.UUID
	var taken bool
	err := s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		row, err := q.ConsumeEmailChangeToken(ctx, hashToken(token))
		if errors.Is(err, pgx.ErrNoRows) {
			return errBadEmailChange
		}
		if err != nil {
			return internal("consume change token", err)
		}
		userID = row.UserID
		inUse, err := q.EmailTaken(ctx, dbq.EmailTakenParams{Email: row.NewEmail, ID: row.UserID})
		if err != nil {
			return internal("check email", err)
		}
		if inUse {
			taken = true
			return nil
		}
		if err := q.UpdateEmail(ctx, dbq.UpdateEmailParams{ID: row.UserID, Email: row.NewEmail}); err != nil {
			return signUpConflict(err)
		}
		// Links mailed to the old address must not work once it is no
		// longer the account's: whoever holds that inbox could reset the
		// password.
		return retireTokens(ctx, q, row.UserID, purposePasswordReset, purposeVerifyEmail, purposeChangeEmail)
	})
	if err != nil {
		return err
	}
	if taken {
		return errEmailTaken
	}
	s.log.Info().Str("event", "email_changed").Str("sub", userID.String()).Msg("email changed")
	return nil
}

// SignOutEverywhere revokes every session the caller has, this one included.
func (s *Service) SignOutEverywhere(ctx context.Context, caller apiutil.Caller) error {
	err := s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		if err := q.RevokeUserSessions(ctx, caller.UserID); err != nil {
			return internal("revoke sessions", err)
		}
		if err := q.RevokeUserRefreshTokens(ctx, caller.UserID); err != nil {
			return internal("revoke refresh tokens", err)
		}
		// Signing everyone out also cancels an email change someone else
		// may have started from a session.
		return retireTokens(ctx, q, caller.UserID, purposeChangeEmail)
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "signed_out_everywhere").Str("sub", caller.UserID.String()).Msg("every session revoked")
	return nil
}

// Session is one of an account's signed-in devices.
type Session struct {
	ID         uuid.UUID
	Device     string
	SignedInAt time.Time
	LastUsedAt time.Time
	Current    bool
}

// ListSessions returns the caller's live sessions, most recently used first,
// marking the one the caller is using.
func (s *Service) ListSessions(ctx context.Context, caller apiutil.Caller) ([]Session, error) {
	rows, err := s.q.ListLiveSessions(ctx, caller.UserID)
	if err != nil {
		return nil, internal("list sessions", err)
	}
	out := make([]Session, len(rows))
	for i, r := range rows {
		out[i] = Session{ID: r.ID, Device: r.Device, SignedInAt: r.CreatedAt, LastUsedAt: r.LastUsedAt, Current: r.ID == caller.SessionID}
	}
	return out, nil
}

var errNoSuchSession = apiutil.NotFound("that device is not signed in")

// RevokeSession signs out one of the caller's other sessions. Another
// account's session reads as not found, so ids cannot be probed. The
// caller's own session is refused: signing out here is Logout.
func (s *Service) RevokeSession(ctx context.Context, caller apiutil.Caller, sid uuid.UUID) error {
	if sid == caller.SessionID {
		return apiutil.FailedPrecondition("this is the device you are using; sign out instead")
	}
	err := s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		live, err := q.OwnLiveSession(ctx, dbq.OwnLiveSessionParams{ID: sid, UserID: caller.UserID})
		if err != nil {
			return internal("check session", err)
		}
		if !live {
			return errNoSuchSession
		}
		return revokeSession(ctx, q, sid)
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "session_revoked").Str("sub", caller.UserID.String()).Str("session", sid.String()).Msg("device signed out")
	return nil
}
