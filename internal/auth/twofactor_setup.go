package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/auth/dbq"
	"perspectiv/internal/jobs"
)

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
// step is recorded as used, so it cannot sign in afterwards. Turning it on
// signs out every other session, as a password change does, so a session
// someone else held before it was on does not outlast it, and ends any open
// challenge; the member is emailed. A wrong code counts against the pending
// setup only: the TwoFactorSetupFailures-th deletes it.
//
// Login takes the same two-factor lock before it reads whether two-factor
// sign-in is on, so a sign-in running alongside either commits its session
// before the revocation here runs, which then ends it, or waits and is asked
// for a code.
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
		if err := lockTwoFactor(ctx, q, u.ID); err != nil {
			return err
		}
		secret, err := s.pendingSecret(ctx, q, u.ID)
		if err != nil {
			return err
		}
		step, result := matchTOTP(secret, digits, s.now(), nil)
		if result != totpAccepted {
			return failSetup(ctx, q, u.ID, &refused)
		}
		return s.turnOnTwoFactor(ctx, q, tx, u, caller.SessionID, step, codes)
	})
	if err != nil {
		return nil, err
	}
	if refused != nil {
		reason := "wrong_code"
		if errors.Is(refused, errSetupAbandoned) {
			reason = "too_many_wrong_codes"
		}
		s.logCodeRefusal("two_factor_setup", caller.UserID, reason, refused)
		return nil, refused
	}
	s.log.Info().Str("event", "two_factor_enabled").Str("sub", caller.UserID.String()).Msg("two-factor sign-in turned on")
	return codes, nil
}

// turnOnTwoFactor confirms the pending secret with the step its first code
// used, stores the recovery codes, ends any open challenge, signs out every
// session but keep, and emails the member.
func (s *Service) turnOnTwoFactor(ctx context.Context, q *dbq.Queries, tx pgx.Tx, u dbq.GetUserByIDRow, keep uuid.UUID,
	step int64, codes []string,
) error {
	if err := q.ConfirmTotp(ctx, dbq.ConfirmTotpParams{UserID: u.ID, Step: step}); err != nil {
		return internal("confirm totp", err)
	}
	if err := replaceRecoveryCodes(ctx, q, u.ID, codes); err != nil {
		return err
	}
	if err := q.DeleteTwoFactorChallenges(ctx, u.ID); err != nil {
		return internal("end two-factor challenges", err)
	}
	if err := revokeOthers(ctx, q, u.ID, keep); err != nil {
		return err
	}
	return s.securityMail(ctx, tx, u.Email, u.Username, jobs.TemplateTwoFactorEnabled, nil)
}

// failSetup counts a wrong code against the pending setup and keeps the
// refusal in refused, to return once the count commits: errWrongCode, or
// errSetupAbandoned once the TwoFactorSetupFailures-th wrong code has
// deleted the setup.
func failSetup(ctx context.Context, q *dbq.Queries, userID uuid.UUID, refused *error) error {
	n, err := q.RecordSetupFailure(ctx, userID)
	if err != nil {
		return internal("count wrong setup code", err)
	}
	if n < TwoFactorSetupFailures {
		*refused = errWrongCode
		return nil
	}
	if err := q.DeletePendingTotp(ctx, userID); err != nil {
		return internal("end two-factor setup", err)
	}
	*refused = errSetupAbandoned
	return nil
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
		if err := q.InsertRecoveryCode(ctx, dbq.InsertRecoveryCodeParams{UserID: userID, CodeHash: hashRecoveryCode(userID, norm)}); err != nil {
			return internal("store recovery code", err)
		}
	}
	return nil
}
