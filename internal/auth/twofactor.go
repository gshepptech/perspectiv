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
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/auth/dbq"
	"perspectiv/internal/jobs"
	"perspectiv/internal/moderation"
	"perspectiv/pkg/crypto"
)

// Two-factor sign-in.
//
//   - A challenge, the step between a right password and a session, lives
//     TwoFactorChallengeTTL and is spent by the first right code.
//   - A setup that was started but never confirmed with a code is refused
//     after TwoFactorSetupTTL, and setup starts again with a new secret.
//   - Wrong codes are counted in two_factor_attempts, apart from wrong
//     passwords: TwoFactorFailures inside TwoFactorWindow, or
//     TwoFactorDailyFailures inside TwoFactorDailyWindow, refuse every code
//     for the account, the right one included, until the oldest of them
//     leaves the window. Six digits leave a guesser one chance in about
//     330,000 per code with the drift window, so the daily cap is what keeps
//     a year of guessing, by someone who already has the password, below a
//     few percent. A refused code is not itself counted.
const (
	TwoFactorChallengeTTL  = 5 * time.Minute
	TwoFactorSetupTTL      = 30 * time.Minute
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

// gateSecondFactor serialises the account's code checks for the rest of the
// transaction and refuses a locked-out account before any code is compared.
func gateSecondFactor(ctx context.Context, q *dbq.Queries, userID uuid.UUID) error {
	if err := q.LockTwoFactorAttempts(ctx, userID); err != nil {
		return internal("lock two-factor attempts", err)
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
func (s *Service) openSecret(row dbq.TotpSecret) ([]byte, error) {
	secret, err := s.enc.Decrypt(crypto.Sealed{KeyID: row.KeyID, Algorithm: row.Algorithm, Ciphertext: row.Ciphertext})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("open totp secret: %w", err))
	}
	return secret, nil
}

// checkSecondFactor checks a code for an account with two-factor sign-in
// on, after gateSecondFactor: six digits against the app's secret, anything
// else as a recovery code, which it spends. Every check is recorded in
// two_factor_attempts. reason is set when the code is refused.
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
	ok := false
	if digits, isTOTP := isTOTPCode(code); isTOTP {
		secret, err := s.openSecret(row)
		if err != nil {
			return "", "", err
		}
		step, result := matchTOTP(secret, digits, s.now(), row.LastUsedStep)
		switch result {
		case totpAccepted:
			if err := q.SetTotpLastUsedStep(ctx, dbq.SetTotpLastUsedStepParams{UserID: userID, Step: step}); err != nil {
				return "", "", internal("record code step", err)
			}
			ok, factor = true, factorTOTP
		case totpReplayed:
			reason = "replayed_code"
		case totpWrong:
			reason = "wrong_code"
		}
	} else if norm, isCode := normalizeRecoveryCode(code); isCode {
		_, err := q.UseRecoveryCode(ctx, dbq.UseRecoveryCodeParams{UserID: userID, CodeHash: hashRecoveryCode(norm)})
		switch {
		case err == nil:
			ok, factor = true, factorRecovery
		case errors.Is(err, pgx.ErrNoRows):
			reason = "wrong_recovery_code"
		default:
			return "", "", internal("use recovery code", err)
		}
	} else {
		reason = "malformed_code"
	}
	if err := q.InsertTwoFactorAttempt(ctx, dbq.InsertTwoFactorAttemptParams{UserID: userID, Succeeded: ok}); err != nil {
		return "", "", internal("record two-factor attempt", err)
	}
	if !ok {
		return "", reason, wrong
	}
	return factor, "", nil
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

// completeSecondStep is CompleteTwoFactorLogin's transaction.
func (s *Service) completeSecondStep(ctx context.Context, q *dbq.Queries, tx pgx.Tx, challenge, code, userAgent string, st *secondStep) error {
	ch, err := q.GetTwoFactorChallengeForUpdate(ctx, hashToken(challenge))
	if errors.Is(err, pgx.ErrNoRows) {
		return errChallengeEnded
	}
	if err != nil {
		return internal("load two-factor challenge", err)
	}
	st.userID = ch.UserID
	if err := gateSecondFactor(ctx, q, ch.UserID); err != nil {
		return deferAnyRefusal(err, &st.refused)
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
	return s.securityMail(ctx, tx, ch.Email, ch.Username, jobs.TemplateRecoveryCodeUsed,
		map[string]string{"left": strconv.Itoa(int(st.out.CodesLeft))})
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

// GetTwoFactorStatus reads whether the caller has two-factor sign-in on.
func (s *Service) GetTwoFactorStatus(ctx context.Context, caller apiutil.Caller) (TwoFactorStatus, error) {
	row, err := s.q.GetTwoFactorStatus(ctx, caller.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TwoFactorStatus{}, nil
	}
	if err != nil {
		return TwoFactorStatus{}, internal("read two-factor status", err)
	}
	st := TwoFactorStatus{Enabled: true, CodesLeft: row.CodesLeft}
	if row.ConfirmedAt != nil {
		st.EnabledAt = *row.ConfirmedAt
	}
	return st, nil
}

// TwoFactorSetup is a new secret waiting for its first code.
type TwoFactorSetup struct {
	// Secret is the shared secret in base32, for typing in by hand.
	Secret string
	// URI is the otpauth:// URI the QR code carries.
	URI string
}

// BeginTwoFactorSetup makes a new secret for the caller, given their
// current password, and keeps it encrypted until ConfirmTwoFactorSetup.
// The password is asked so that someone holding only a signed-in session
// cannot put their own authenticator on the account and lock its owner out.
// Starting again replaces an unconfirmed secret; a confirmed one is never
// replaced here.
func (s *Service) BeginTwoFactorSetup(ctx context.Context, caller apiutil.Caller, password string) (TwoFactorSetup, error) {
	secret, err := newTOTPSecret()
	if err != nil {
		return TwoFactorSetup{}, apiutil.Internal(err)
	}
	sealed, err := s.enc.Encrypt(secret)
	if err != nil {
		return TwoFactorSetup{}, apiutil.Internal(fmt.Errorf("seal totp secret: %w", err))
	}
	var out TwoFactorSetup
	var refused error
	err = s.inTx(ctx, func(q *dbq.Queries, _ pgx.Tx) error {
		u, err := signedInUser(ctx, q, caller.UserID)
		if err != nil {
			return err
		}
		if err := s.checkPassword(ctx, q, u, password); err != nil {
			return deferRefusal(err, &refused)
		}
		_, err = q.UpsertPendingTotp(ctx, dbq.UpsertPendingTotpParams{
			UserID: u.ID, KeyID: sealed.KeyID, Algorithm: sealed.Algorithm, Ciphertext: sealed.Ciphertext,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return errTwoFactorOn
		}
		if err != nil {
			return internal("store totp secret", err)
		}
		out = TwoFactorSetup{Secret: secretEncoding.EncodeToString(secret), URI: otpauthURI(secret, u.Username)}
		return nil
	})
	if err != nil {
		return TwoFactorSetup{}, err
	}
	if refused != nil {
		s.logRefusal("two_factor_setup", caller.UserID, refused)
		return TwoFactorSetup{}, refused
	}
	s.log.Info().Str("event", "two_factor_setup_started").Str("sub", caller.UserID.String()).Msg("totp secret issued")
	return out, nil
}

// ConfirmTwoFactorSetup turns two-factor sign-in on once code matches the
// pending secret, and returns the account's ten recovery codes. The code's
// step is recorded as used, so it cannot sign in afterwards. A wrong code
// counts toward the two-factor lockout.
func (s *Service) ConfirmTwoFactorSetup(ctx context.Context, caller apiutil.Caller, code string) ([]string, error) {
	digits, isTOTP := isTOTPCode(code)
	if !isTOTP {
		return nil, apiutil.InvalidArg("enter the six digits your app shows")
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	var refused error
	err = s.inTx(ctx, func(q *dbq.Queries, tx pgx.Tx) error {
		u, err := signedInUser(ctx, q, caller.UserID)
		if err != nil {
			return err
		}
		if err := gateSecondFactor(ctx, q, u.ID); err != nil {
			return deferAnyRefusal(err, &refused)
		}
		secret, err := s.pendingSecret(ctx, q, u.ID)
		if err != nil {
			return err
		}
		step, result := matchTOTP(secret, digits, s.now(), nil)
		ok := result == totpAccepted
		if err := q.InsertTwoFactorAttempt(ctx, dbq.InsertTwoFactorAttemptParams{UserID: u.ID, Succeeded: ok}); err != nil {
			return internal("record two-factor attempt", err)
		}
		if !ok {
			refused = errWrongCode
			return nil
		}
		if err := q.ConfirmTotp(ctx, dbq.ConfirmTotpParams{UserID: u.ID, Step: step}); err != nil {
			return internal("confirm totp", err)
		}
		if err := replaceRecoveryCodes(ctx, q, u.ID, codes); err != nil {
			return err
		}
		return s.securityMail(ctx, tx, u.Email, u.Username, jobs.TemplateTwoFactorEnabled, nil)
	})
	if err != nil {
		return nil, err
	}
	if refused != nil {
		s.logCodeRefusal("two_factor_setup", caller.UserID, "wrong_code", refused)
		return nil, refused
	}
	s.log.Info().Str("event", "two_factor_enabled").Str("sub", caller.UserID.String()).Msg("two-factor sign-in turned on")
	return codes, nil
}

// pendingSecret loads and opens the secret of a setup that is waiting for
// its first code: one that exists, is not confirmed and is not older than
// TwoFactorSetupTTL.
func (s *Service) pendingSecret(ctx context.Context, q *dbq.Queries, userID uuid.UUID) ([]byte, error) {
	row, err := q.GetTotpForUpdate(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errSetupNotStarted
	}
	if err != nil {
		return nil, internal("load totp secret", err)
	}
	if row.ConfirmedAt != nil {
		return nil, errTwoFactorOn
	}
	if s.now().Sub(row.CreatedAt) > TwoFactorSetupTTL {
		return nil, errSetupNotStarted
	}
	return s.openSecret(row)
}

// replaceRecoveryCodes deletes the account's recovery codes and stores the
// hashes of codes in their place.
func replaceRecoveryCodes(ctx context.Context, q *dbq.Queries, userID uuid.UUID, codes []string) error {
	if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
		return internal("delete recovery codes", err)
	}
	for _, c := range codes {
		norm, ok := normalizeRecoveryCode(c)
		if !ok {
			return apiutil.Internal(errors.New("generated recovery code does not normalise"))
		}
		if err := q.InsertRecoveryCode(ctx, dbq.InsertRecoveryCodeParams{UserID: userID, CodeHash: hashRecoveryCode(norm)}); err != nil {
			return internal("store recovery code", err)
		}
	}
	return nil
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
// is emailed.
func (s *Service) DisableTwoFactor(ctx context.Context, caller apiutil.Caller, password, code string) error {
	err := s.withPasswordAndCode(ctx, caller, "two_factor_disable", password, code,
		func(q *dbq.Queries, tx pgx.Tx, u dbq.GetUserByIDRow, _ secondFactor) error {
			if err := removeTwoFactor(ctx, q, u.ID); err != nil {
				return err
			}
			return s.securityMail(ctx, tx, u.Email, u.Username, jobs.TemplateTwoFactorDisabled, nil)
		})
	if err != nil {
		return err
	}
	s.log.Info().Str("event", "two_factor_disabled").Str("sub", caller.UserID.String()).Msg("two-factor sign-in turned off")
	return nil
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
// given the current password and a current code. A recovery code given as
// that code is spent before the new set replaces it.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, caller apiutil.Caller, password, code string) ([]string, error) {
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	err = s.withPasswordAndCode(ctx, caller, "recovery_codes_regenerate", password, code,
		func(q *dbq.Queries, _ pgx.Tx, u dbq.GetUserByIDRow, _ secondFactor) error {
			return replaceRecoveryCodes(ctx, q, u.ID, codes)
		})
	if err != nil {
		return nil, err
	}
	s.log.Info().Str("event", "recovery_codes_regenerated").Str("sub", caller.UserID.String()).Msg("recovery codes replaced")
	return codes, nil
}

// ErrUnknownUser is returned by ResetTwoFactor when no live account holds
// the username.
var ErrUnknownUser = errors.New("no account has that username")

// ResetTwoFactor is the way back in for a member who lost both their
// authenticator and every recovery code. An operator runs it from the
// command line (perspectiv admin reset-two-factor) once they have satisfied
// themselves, outside the app, that the request comes from the account's
// owner; no API route reaches it. In one transaction it removes the
// account's two-factor sign-in, signs out every session, writes a
// TWO_FACTOR_RESET audit row with no actor and the operator's reason, and
// emails the member. An account without two-factor sign-in is refused, so
// nothing is audited that did not happen.
func ResetTwoFactor(ctx context.Context, pool *pgxpool.Pool, enq *jobs.Enqueuer, publicBaseURL, username, reason string) error {
	username, reason = strings.TrimSpace(username), strings.TrimSpace(reason)
	if username == "" || reason == "" {
		return errors.New("reset two-factor: a username and a reason are required")
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
		if _, err := q.GetTwoFactorStatus(ctx, u.ID); errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("reset two-factor for %q: %w", username, errTwoFactorOff)
		} else if err != nil {
			return fmt.Errorf("reset two-factor: read status: %w", err)
		}
		if err := removeTwoFactor(ctx, q, u.ID); err != nil {
			return fmt.Errorf("reset two-factor: %w", err)
		}
		if err := q.RevokeUserSessions(ctx, u.ID); err != nil {
			return fmt.Errorf("reset two-factor: revoke sessions: %w", err)
		}
		if err := q.RevokeUserRefreshTokens(ctx, u.ID); err != nil {
			return fmt.Errorf("reset two-factor: revoke refresh tokens: %w", err)
		}
		if err := moderation.WriteAudit(ctx, tx, moderation.AuditEntry{
			Action:     perspectivv1.AuditAction_AUDIT_ACTION_TWO_FACTOR_RESET,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER,
			TargetID:   &u.ID,
			Reason:     reason,
		}); err != nil {
			return fmt.Errorf("reset two-factor: write audit row: %w", err)
		}
		return enq.EmailTx(ctx, tx, jobs.EmailArgs{To: u.Email, Template: jobs.TemplateTwoFactorDisabled, Data: map[string]string{
			"link": strings.TrimSuffix(publicBaseURL, "/") + "/forgot-password", "username": u.Username,
		}})
	})
}
