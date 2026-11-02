package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/auth/dbq"
	"perspectiv/internal/jobs"
	"perspectiv/pkg/crypto"
)

// Two-factor sign-in.
//
//   - A challenge, the step between a right password and a session, lives
//     TwoFactorChallengeTTL and is spent by the first right code.
//   - A setup that was started but never confirmed with a code is refused
//     after TwoFactorSetupTTL, and setup starts again with a new secret.
//     Its wrong codes are counted on the setup itself
//     (totp_secrets.setup_failures), not toward the sign-in lockout below:
//     the TwoFactorSetupFailures-th deletes the pending secret, and setup
//     starts again behind the password. So a stolen session can neither
//     guess its way through setup nor lock the owner out of signing in.
//   - Wrong codes at sign-in, and when turning two-factor off or making new
//     recovery codes, are counted in two_factor_attempts, apart from wrong
//     passwords: TwoFactorFailures inside TwoFactorWindow, or
//     TwoFactorDailyFailures inside TwoFactorDailyWindow, refuse every code
//     for the account, the right one included, until the oldest of them
//     leaves the window. Six digits leave a guesser one chance in about
//     330,000 per code with the drift window, so the daily cap is what keeps
//     a year of guessing, by someone who already has the password, below a
//     few percent. A refused code is not itself counted, and neither is a
//     replayed one (a right code for a step already used), so a client
//     that retries a request cannot lock its own member out; the replay is
//     still refused and logged.
//
// Lock order. Every transaction here that touches an account's sign-in
// state takes the account's locks in this order, skipping any it does not
// need, so no two of them can wait on each other:
//
//  1. the login lock (LockLoginAttempts), around a password check;
//  2. the users row: FOR SHARE (ShareLockUser, LockUserForLogin), or the
//     UPDATE of a password reset or change, or the operator reset's
//     FOR UPDATE;
//  3. the two-factor lock (LockTwoFactorAttempts), through lockTwoFactor;
//  4. the account's two-factor rows (its totp_secrets row, challenges and
//     recovery codes), then its sessions and refresh tokens.
//
// Step 2 comes before 3 because every insert that references users (a
// session, an attempt, a recovery code) takes a key-share lock on the users
// row: had the two-factor lock come first, a transaction holding the users
// row (the operator reset, account deletion) could wait for it while the
// holder waits for the row. CompleteTwoFactorLogin reads its challenge's
// account without a lock (PeekTwoFactorChallenge), takes 2 and 3, and only
// then locks the challenge row, so it and DisableTwoFactor or the operator
// reset, which delete challenges after taking 3, never hold one lock each.
const (
	TwoFactorChallengeTTL  = 5 * time.Minute
	TwoFactorSetupTTL      = 30 * time.Minute
	TwoFactorSetupFailures = 5
	TwoFactorFailures      = 5
	TwoFactorWindow        = 15 * time.Minute
	TwoFactorDailyFailures = 20
	TwoFactorDailyWindow   = 24 * time.Hour
)

var (
	errTwoFactorLocked      = apiutil.ResourceExhausted("too many wrong codes; try again in 15 minutes")
	errTwoFactorLockedDaily = apiutil.ResourceExhausted("too many wrong codes today; try again tomorrow")
	// errWrongLoginCode is a wrong second factor at sign-in, worded and coded
	// like a wrong password.
	errWrongLoginCode = apiutil.Unauthenticated("that code is not right")
	// errWrongCode is a wrong code from a signed-in member in Settings.
	errWrongCode        = apiutil.InvalidArg("that code is not right")
	errChallengeEnded   = apiutil.FailedPrecondition("this sign-in has timed out; enter your password again")
	errTwoFactorOn      = apiutil.FailedPrecondition("two-factor sign-in is already on")
	errTwoFactorOff     = apiutil.FailedPrecondition("two-factor sign-in is not on")
	errSetupNotStarted  = apiutil.FailedPrecondition("set up two-factor sign-in again; that setup has expired")
	errSetupAbandoned   = apiutil.FailedPrecondition("too many wrong codes; set up two-factor sign-in again")
	errNoCode           = apiutil.InvalidArg("enter a code")
	errTwoFactorRefusal = []error{errTwoFactorLocked, errTwoFactorLockedDaily, errWrongLoginCode, errWrongCode}
)

// secondFactor is how a code was accepted.
type secondFactor string

const (
	factorTOTP     secondFactor = "totp"
	factorRecovery secondFactor = "recovery_code"
)

// isTwoFactorRefusal reports whether err is a refused code or a lockout:
// refusals whose attempt row must commit before the error is returned.
func isTwoFactorRefusal(err error) bool {
	for _, r := range errTwoFactorRefusal {
		if errors.Is(err, r) {
			return true
		}
	}
	return false
}

// deferAnyRefusal is deferRefusal for both a password refusal and a
// second-factor one.
func deferAnyRefusal(err error, refused *error) error {
	if isTwoFactorRefusal(err) {
		*refused = err
		return nil
	}
	return deferRefusal(err, refused)
}

// lockTwoFactor takes steps 2 and 3 of the lock order: the account's users
// row, share-locked, then its two-factor lock, which serialises everything
// that reads or changes the account's two-factor state for the rest of the
// transaction. A deleted account is errAccountNotFound.
func lockTwoFactor(ctx context.Context, q *dbq.Queries, userID uuid.UUID) error {
	if _, err := q.ShareLockUser(ctx, userID); errors.Is(err, pgx.ErrNoRows) {
		return errAccountNotFound
	} else if err != nil {
		return internal("lock user", err)
	}
	if err := q.LockTwoFactorAttempts(ctx, userID); err != nil {
		return internal("lock two-factor attempts", err)
	}
	return nil
}

// gateSecondFactor takes lockTwoFactor's locks and refuses a locked-out
// account before any code is compared.
func gateSecondFactor(ctx context.Context, q *dbq.Queries, userID uuid.UUID) error {
	if err := lockTwoFactor(ctx, q, userID); err != nil {
		return err
	}
	for _, w := range []struct {
		minutes int32
		limit   int32
		err     error
	}{
		{int32(TwoFactorWindow / time.Minute), TwoFactorFailures, errTwoFactorLocked},
		{int32(TwoFactorDailyWindow / time.Minute), TwoFactorDailyFailures, errTwoFactorLockedDaily},
	} {
		n, err := q.CountRecentFailedTwoFactor(ctx, dbq.CountRecentFailedTwoFactorParams{
			UserID: userID, WindowMinutes: w.minutes,
		})
		if err != nil {
			return internal("count failed codes", err)
		}
		if n >= w.limit {
			return w.err
		}
	}
	return nil
}

// openSecret decrypts a stored TOTP secret. A row this server cannot
// decrypt (an unknown key id or algorithm, a damaged ciphertext) is an
// internal error: the code is refused, never waved through.
func (s *Service) openSecret(row dbq.GetTotpForUpdateRow) ([]byte, error) {
	secret, err := s.enc.Decrypt(crypto.Sealed{KeyID: row.KeyID, Algorithm: row.Algorithm, Ciphertext: row.Ciphertext})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("open totp secret: %w", err))
	}
	return secret, nil
}

// checkSecondFactor checks a code for an account with two-factor sign-in
// on, after gateSecondFactor: six digits against the app's secret, anything
// else as a recovery code, which it spends. Every check is recorded in
// two_factor_attempts except a replayed code, which is refused without
// counting toward the lockout. reason is set when the code is refused.
func (s *Service) checkSecondFactor(ctx context.Context, q *dbq.Queries, userID uuid.UUID, code string, wrong error,
) (factor secondFactor, reason string, err error) {
	if strings.TrimSpace(code) == "" {
		return "", "", errNoCode
	}
	row, err := q.GetTotpForUpdate(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.ConfirmedAt == nil) {
		return "", "", errTwoFactorOff
	}
	if err != nil {
		return "", "", internal("load totp secret", err)
	}
	if digits, isTOTP := isTOTPCode(code); isTOTP {
		factor, reason, err = s.checkAppCode(ctx, q, row, digits)
	} else if norm, isCode := normalizeRecoveryCode(code); isCode {
		factor, reason, err = spendRecoveryCode(ctx, q, userID, norm)
	} else {
		reason = "malformed_code"
	}
	if err != nil {
		return "", "", err
	}
	ok := factor != ""
	// A replay was right once: a client retrying a request that already
	// went through sends it, and counting it would lock the member out.
	if reason != "replayed_code" {
		if err := q.InsertTwoFactorAttempt(ctx, dbq.InsertTwoFactorAttemptParams{UserID: userID, Succeeded: ok}); err != nil {
			return "", "", internal("record two-factor attempt", err)
		}
	}
	if !ok {
		return "", reason, wrong
	}
	return factor, "", nil
}

// checkAppCode checks six digits against the account's app secret and, when
// they are right, records their step as used.
func (s *Service) checkAppCode(ctx context.Context, q *dbq.Queries, row dbq.GetTotpForUpdateRow, digits string) (secondFactor, string, error) {
	secret, err := s.openSecret(row)
	if err != nil {
		return "", "", err
	}
	step, result := matchTOTP(secret, digits, s.now(), row.LastUsedStep)
	switch result {
	case totpAccepted:
		if err := q.SetTotpLastUsedStep(ctx, dbq.SetTotpLastUsedStepParams{UserID: row.UserID, Step: step}); err != nil {
			return "", "", internal("record code step", err)
		}
		return factorTOTP, "", nil
	case totpReplayed:
		return "", "replayed_code", nil
	default:
		return "", "wrong_code", nil
	}
}

// spendRecoveryCode spends the account's unused recovery code norm, matched
// by its salted hash or, for a code stored before hashes were salted, its
// unsalted one.
func spendRecoveryCode(ctx context.Context, q *dbq.Queries, userID uuid.UUID, norm string) (secondFactor, string, error) {
	_, err := q.UseRecoveryCode(ctx, dbq.UseRecoveryCodeParams{
		UserID: userID, CodeHash: hashRecoveryCode(userID, norm), LegacyCodeHash: legacyRecoveryCodeHash(norm),
	})
	switch {
	case err == nil:
		return factorRecovery, "", nil
	case errors.Is(err, pgx.ErrNoRows):
		return "", "wrong_recovery_code", nil
	default:
		return "", "", internal("use recovery code", err)
	}
}

// startChallenge records a single-use challenge for an account whose
// password was right and returns its token.
func startChallenge(ctx context.Context, q *dbq.Queries, userID uuid.UUID, now time.Time) (string, time.Time, error) {
	tok, tokHash, err := newOpaqueToken()
	if err != nil {
		return "", time.Time{}, apiutil.Internal(err)
	}
	exp := now.Add(TwoFactorChallengeTTL)
	if err := q.InsertTwoFactorChallenge(ctx, dbq.InsertTwoFactorChallengeParams{
		UserID: userID, TokenHash: tokHash, ExpiresAt: exp,
	}); err != nil {
		return "", time.Time{}, internal("insert two-factor challenge", err)
	}
	return tok, exp, nil
}

// TwoFactorLogin is a completed second step.
type TwoFactorLogin struct {
	Tokens Tokens
	// UsedRecoveryCode is true when a recovery code signed in; CodesLeft is
	// then the number still unused.
	UsedRecoveryCode bool
	CodesLeft        int32
}

// CompleteTwoFactorLogin spends a challenge from Login with a code from
// the authenticator app or a recovery code, and starts a session labelled
// with userAgent. A wrong code leaves the challenge open until it expires,
// so the member can try again, and counts toward the two-factor lockout.
func (s *Service) CompleteTwoFactorLogin(ctx context.Context, challenge, code, userAgent string) (TwoFactorLogin, error) {
	if challenge == "" {
		return TwoFactorLogin{}, errChallengeEnded
	}
	var st secondStep
	err := s.inTx(ctx, func(q *dbq.Queries, tx pgx.Tx) error {
		return s.completeSecondStep(ctx, q, tx, challenge, code, userAgent, &st)
	})
	if errors.Is(err, errChallengeEnded) {
		s.log.Warn().Str("event", "login_two_factor_failed").Str("reason", "challenge_ended").Str("sub", st.userID.String()).Msg("second step refused")
	}
	if err != nil {
		return TwoFactorLogin{}, err
	}
	if st.refused != nil {
		s.logCodeRefusal("login_two_factor", st.userID, st.reason, st.refused)
		return TwoFactorLogin{}, st.refused
	}
	factor := factorTOTP
	if st.out.UsedRecoveryCode {
		factor = factorRecovery
		s.log.Warn().Str("event", "recovery_code_used").Str("sub", st.userID.String()).Int32("left", st.out.CodesLeft).Msg("recovery code spent")
	}
	s.log.Info().Str("event", "login_succeeded").Str("sub", st.userID.String()).Str("second_factor", string(factor)).Msg("signed in")
	return st.out, nil
}

// secondStep is what CompleteTwoFactorLogin's transaction found: the
// session on success, or the refusal to return once the attempt commits.
type secondStep struct {
	out     TwoFactorLogin
	userID  uuid.UUID
	reason  string
	refused error
}

// completeSecondStep is CompleteTwoFactorLogin's transaction. It learns the
// challenge's account without a lock, takes the account's locks, and only
// then locks the challenge (the lock order at the top of this file); a
// challenge ended in between, by a reset or by turning two-factor off, is
// gone by the time it is locked.
func (s *Service) completeSecondStep(ctx context.Context, q *dbq.Queries, tx pgx.Tx, challenge, code, userAgent string, st *secondStep) error {
	tokHash := hashToken(challenge)
	userID, err := q.PeekTwoFactorChallenge(ctx, tokHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return errChallengeEnded
	}
	if err != nil {
		return internal("find two-factor challenge", err)
	}
	st.userID = userID
	if err := gateSecondFactor(ctx, q, userID); errors.Is(err, errAccountNotFound) {
		return errChallengeEnded
	} else if err != nil {
		return deferAnyRefusal(err, &st.refused)
	}
	ch, err := q.GetTwoFactorChallengeForUpdate(ctx, tokHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return errChallengeEnded
	}
	if err != nil {
		return internal("load two-factor challenge", err)
	}
	factor, why, err := s.checkSecondFactor(ctx, q, ch.UserID, code, errWrongLoginCode)
	st.reason = why
	if errors.Is(err, errTwoFactorOff) {
		// Turned off since the password step: start again from it.
		return errChallengeEnded
	}
	if err != nil {
		return deferAnyRefusal(err, &st.refused)
	}
	if err := q.UseTwoFactorChallenge(ctx, ch.ID); err != nil {
		return internal("spend two-factor challenge", err)
	}
	role, err := roleFromDB(ch.Role)
	if err != nil {
		return apiutil.Internal(err)
	}
	st.out.Tokens, err = s.startSession(ctx, q, deviceLabel(userAgent), SessionUser{
		ID: ch.UserID, Username: ch.Username, Role: role, EmailVerified: ch.EmailVerifiedAt != nil, TwoFactorEnabled: true,
	})
	if err != nil || factor != factorRecovery {
		return err
	}
	st.out.UsedRecoveryCode = true
	if st.out.CodesLeft, err = q.CountRecoveryCodesLeft(ctx, ch.UserID); err != nil {
		return internal("count recovery codes", err)
	}
	return s.securityMail(ctx, tx, ch.Email, ch.Username, jobs.TemplateRecoveryCodeUsed, map[string]string{
		"used_for": jobs.RecoveryCodeUsedForSignIn, "left": strconv.Itoa(int(st.out.CodesLeft)),
	})
}

// logCodeRefusal writes the audit line for a refused second factor:
// action_locked for a lockout, action_failed otherwise.
func (s *Service) logCodeRefusal(action string, userID uuid.UUID, reason string, err error) {
	if errors.Is(err, errTwoFactorLocked) || errors.Is(err, errTwoFactorLockedDaily) {
		s.log.Warn().Str("event", action+"_locked").Str("sub", userID.String()).Msg("code refused by two-factor lockout")
		return
	}
	if errors.Is(err, errWrongPassword) || errors.Is(err, errLockedOut) || errors.Is(err, errNoCurrentPassword) {
		s.logRefusal(action, userID, err)
		return
	}
	if reason == "" {
		reason = "wrong_code"
	}
	s.log.Warn().Str("event", action+"_failed").Str("reason", reason).Str("sub", userID.String()).Msg("code refused")
}

// securityMail queues one of the account-security notices to the member.
// Its link is the password reset page, for when it was not them.
func (s *Service) securityMail(ctx context.Context, tx pgx.Tx, to, username, template string, extra map[string]string) error {
	data := map[string]string{
		"link": strings.TrimSuffix(s.cfg.PublicBaseURL, "/") + "/forgot-password", "username": username,
	}
	for k, v := range extra {
		data[k] = v
	}
	if err := s.enq.EmailTx(ctx, tx, jobs.EmailArgs{To: to, Template: template, Data: data}); err != nil {
		return apiutil.Internal(err)
	}
	return nil
}

// TwoFactorStatus is the caller's two-factor standing for Settings.
type TwoFactorStatus struct {
	Enabled   bool
	EnabledAt time.Time
	CodesLeft int32
}

// withPasswordAndCode runs act for a signed-in caller once both their
// current password and a current second factor are right. Wrong ones are
// recorded (the password in login_attempts, the code in
// two_factor_attempts) and committed before the refusal is returned.
func (s *Service) withPasswordAndCode(ctx context.Context, caller apiutil.Caller, action, password, code string,
	act func(q *dbq.Queries, tx pgx.Tx, u dbq.GetUserByIDRow, factor secondFactor) error,
) error {
	var reason string
	var refused error
	err := s.inTx(ctx, func(q *dbq.Queries, tx pgx.Tx) error {
		u, err := signedInUser(ctx, q, caller.UserID)
		if err != nil {
			return err
		}
		if !u.TwoFactorEnabled {
			return errTwoFactorOff
		}
		if err := s.checkPassword(ctx, q, u, password); err != nil {
			return deferRefusal(err, &refused)
		}
		if err := gateSecondFactor(ctx, q, u.ID); err != nil {
			return deferAnyRefusal(err, &refused)
		}
		factor, why, err := s.checkSecondFactor(ctx, q, u.ID, code, errWrongCode)
		reason = why
		if err != nil {
			return deferAnyRefusal(err, &refused)
		}
		return act(q, tx, u, factor)
	})
	if err != nil {
		return err
	}
	if refused != nil {
		s.logCodeRefusal(action, caller.UserID, reason, refused)
		return refused
	}
	return nil
}

// DisableTwoFactor turns two-factor sign-in off, given the current password
// and a current code (from the app, or a recovery code). The secret, the
// recovery codes and any open sign-in challenge are deleted, and the member
// is emailed; a recovery code used for it is emailed about too.
func (s *Service) DisableTwoFactor(ctx context.Context, caller apiutil.Caller, password, code string) error {
	var used secondFactor
	err := s.withPasswordAndCode(ctx, caller, "two_factor_disable", password, code,
		func(q *dbq.Queries, tx pgx.Tx, u dbq.GetUserByIDRow, factor secondFactor) error {
			used = factor
			if err := removeTwoFactor(ctx, q, u.ID); err != nil {
				return err
			}
			if err := s.recoveryCodeMail(ctx, tx, u, factor, jobs.RecoveryCodeUsedForTurnOff); err != nil {
				return err
			}
			return s.securityMail(ctx, tx, u.Email, u.Username, jobs.TemplateTwoFactorDisabled, nil)
		})
	if err != nil {
		return err
	}
	s.logRecoveryCodeUsed(caller.UserID, used, jobs.RecoveryCodeUsedForTurnOff)
	s.log.Info().Str("event", "two_factor_disabled").Str("sub", caller.UserID.String()).Msg("two-factor sign-in turned off")
	return nil
}

// recoveryCodeMail queues the recovery-code-used email when factor is a
// recovery code; usedFor says what it was used for.
func (s *Service) recoveryCodeMail(ctx context.Context, tx pgx.Tx, u dbq.GetUserByIDRow, factor secondFactor, usedFor string) error {
	if factor != factorRecovery {
		return nil
	}
	return s.securityMail(ctx, tx, u.Email, u.Username, jobs.TemplateRecoveryCodeUsed, map[string]string{"used_for": usedFor})
}

// logRecoveryCodeUsed writes the audit line for a recovery code spent on
// something other than signing in.
func (s *Service) logRecoveryCodeUsed(userID uuid.UUID, factor secondFactor, usedFor string) {
	if factor == factorRecovery {
		s.log.Warn().Str("event", "recovery_code_used").Str("sub", userID.String()).Str("used_for", usedFor).Msg("recovery code spent")
	}
}

// removeTwoFactor deletes the account's secret, recovery codes and open
// challenges.
func removeTwoFactor(ctx context.Context, q *dbq.Queries, userID uuid.UUID) error {
	steps := []struct {
		what string
		run  func(context.Context, uuid.UUID) error
	}{
		{"delete totp secret", q.DeleteTotp},
		{"delete recovery codes", q.DeleteRecoveryCodes},
		{"delete two-factor challenges", q.DeleteTwoFactorChallenges},
	}
	for _, st := range steps {
		if err := st.run(ctx, userID); err != nil {
			return internal(st.what, err)
		}
	}
	return nil
}

// RegenerateRecoveryCodes replaces every recovery code with ten new ones,
// given the current password and a current code, and emails the member. A
// recovery code given as that code is spent before the new set replaces it,
// and is emailed about too.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, caller apiutil.Caller, password, code string) ([]string, error) {
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	var used secondFactor
	err = s.withPasswordAndCode(ctx, caller, "recovery_codes_regenerate", password, code,
		func(q *dbq.Queries, tx pgx.Tx, u dbq.GetUserByIDRow, factor secondFactor) error {
			used = factor
			if err := replaceRecoveryCodes(ctx, q, u.ID, codes); err != nil {
				return err
			}
			if err := s.recoveryCodeMail(ctx, tx, u, factor, jobs.RecoveryCodeUsedForNewCodes); err != nil {
				return err
			}
			return s.securityMail(ctx, tx, u.Email, u.Username, jobs.TemplateRecoveryCodesReplaced, nil)
		})
	if err != nil {
		return nil, err
	}
	s.logRecoveryCodeUsed(caller.UserID, used, jobs.RecoveryCodeUsedForNewCodes)
	s.log.Info().Str("event", "recovery_codes_regenerated").Str("sub", caller.UserID.String()).Msg("recovery codes replaced")
	return codes, nil
}
