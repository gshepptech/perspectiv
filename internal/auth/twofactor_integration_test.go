//go:build integration

package auth_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: RFC 6238 TOTP is HMAC-SHA-1
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/auth"
)

// totpAt is the six-digit RFC 6238 code for secret (base32, as
// BeginTwoFactorSetup returns it) at t: written here from the RFC, apart
// from the server's own code, the way an authenticator app computes it.
func totpAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret %q: %v", secret, err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30)) //nolint:gosec // G115: after 1970
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1_000_000)
}

// steadyClock waits, when needed, until the current 30-second step has at
// least 10 seconds left, so a test that computes codes for now and the steps
// beside it is not caught out by the server's clock crossing a step
// boundary mid-test.
func steadyClock(t *testing.T) {
	t.Helper()
	if left := 30 - time.Now().Unix()%30; left < 10 {
		time.Sleep(time.Duration(left)*time.Second + 500*time.Millisecond)
	}
}

// member is a signed-in account for the two-factor tests.
type member struct {
	id       uuid.UUID
	username string
	access   string
	secret   string
	codes    []string
}

func (m *member) email() string { return emailOf(m.username) }

func (e *env) newMember(username string) *member {
	e.t.Helper()
	id := e.signUp(username)
	access, _ := e.mustLogin(username)
	return &member{id: id, username: username, access: access}
}

// begin starts setup and keeps the secret.
func (e *env) begin(m *member) *perspectivv1.BeginTwoFactorSetupResponse {
	e.t.Helper()
	res, err := e.authAs(m.access).BeginTwoFactorSetup(context.Background(), connect.NewRequest(&perspectivv1.BeginTwoFactorSetupRequest{
		CurrentPassword: goodPassword,
	}))
	if err != nil {
		e.t.Fatalf("BeginTwoFactorSetup: %v", err)
	}
	m.secret = res.Msg.GetSecret()
	return res.Msg
}

func (e *env) confirm(m *member, code string) ([]string, error) {
	e.t.Helper()
	res, err := e.authAs(m.access).ConfirmTwoFactorSetup(context.Background(), connect.NewRequest(&perspectivv1.ConfirmTwoFactorSetupRequest{Code: code}))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetRecoveryCodes(), nil
}

// enrol turns two-factor sign-in on for m with a code for now.
func (e *env) enrol(m *member) {
	e.t.Helper()
	e.begin(m)
	codes, err := e.confirm(m, totpAt(e.t, m.secret, time.Now()))
	if err != nil {
		e.t.Fatalf("ConfirmTwoFactorSetup: %v", err)
	}
	m.codes = codes
}

// challenge signs in with the password and returns the challenge, failing
// unless the response is a challenge and nothing else.
func (e *env) challenge(m *member, password string) string {
	e.t.Helper()
	res, err := e.auth().Login(context.Background(), connect.NewRequest(&perspectivv1.LoginRequest{Email: m.email(), Password: password}))
	if err != nil {
		e.t.Fatalf("Login: %v", err)
	}
	msg := res.Msg
	if msg.GetTwoFactorChallenge() == "" || msg.GetAccessToken() != "" || msg.GetUser() != nil {
		e.t.Fatalf("Login = %v; want a challenge and no session", msg)
	}
	if left := time.Until(msg.GetTwoFactorChallengeExpiresAt().AsTime()); left <= 4*time.Minute || left > 5*time.Minute {
		e.t.Fatalf("challenge lives %v, want about 5 minutes", left)
	}
	if got := res.Header().Values("Set-Cookie"); len(got) != 0 {
		e.t.Fatalf("a challenge set cookies %v; the session must wait for the second factor", got)
	}
	return msg.GetTwoFactorChallenge()
}

func (e *env) complete(challenge, code string) (*connect.Response[perspectivv1.CompleteTwoFactorLoginResponse], error) {
	return e.auth().CompleteTwoFactorLogin(context.Background(), connect.NewRequest(&perspectivv1.CompleteTwoFactorLoginRequest{
		Challenge: challenge, Code: code,
	}))
}

func (e *env) wantLog(event string) {
	e.t.Helper()
	if !strings.Contains(e.logs.String(), `"event":"`+event+`"`) {
		e.t.Fatalf("no %s audit line in:\n%s", event, e.logs.String())
	}
}

// Enrolling: the password is asked first, the secret is stored encrypted
// and is not in force until a code from the app confirms it, and then ten
// recovery codes come back once, stored only as hashes.
func TestTwoFactorEnrolAndConfirm(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Enrol_Er")
	ctx := context.Background()

	_, err := e.authAs(m.access).BeginTwoFactorSetup(ctx, connect.NewRequest(&perspectivv1.BeginTwoFactorSetupRequest{CurrentPassword: "wrong password!"}))
	wantCode(t, err, connect.CodePermissionDenied)
	if n := e.count(`SELECT count(*) FROM login_attempts WHERE user_id = $1 AND NOT succeeded`, m.id); n != 1 {
		t.Fatalf("wrong password at setup recorded %d failed attempts, want 1", n)
	}

	setup := e.begin(m)
	if !strings.HasPrefix(setup.GetOtpauthUri(), "otpauth://totp/Perspectiv:Enrol_Er?") ||
		!strings.Contains(setup.GetOtpauthUri(), "secret="+setup.GetSecret()) {
		t.Fatalf("otpauth uri = %q", setup.GetOtpauthUri())
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.GetSecret())
	if err != nil || len(raw) != 20 {
		t.Fatalf("secret %q is not 20 bytes of base32: %v", setup.GetSecret(), err)
	}
	var keyID, algorithm string
	var ciphertext []byte
	e.scan(`SELECT key_id, algorithm, ciphertext FROM totp_secrets WHERE user_id = $1 AND confirmed_at IS NULL`, []any{m.id},
		&keyID, &algorithm, &ciphertext)
	if keyID == "" || algorithm != "age-x25519-v1" || bytes.Contains(ciphertext, raw) || bytes.Contains(ciphertext, []byte(setup.GetSecret())) {
		t.Fatalf("stored secret is not sealed: key %q algorithm %q", keyID, algorithm)
	}

	// Until it is confirmed, sign-in is unchanged and the status is off.
	if _, _, err := e.login("Enrol_Er", goodPassword); err != nil {
		t.Fatalf("Login before confirming: %v", err)
	}
	st, err := e.authAs(m.access).GetTwoFactorStatus(ctx, connect.NewRequest(&perspectivv1.GetTwoFactorStatusRequest{}))
	if err != nil || st.Msg.GetEnabled() {
		t.Fatalf("status before confirming = %v, %v; want off", st, err)
	}

	_, err = e.confirm(m, "000000")
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.confirm(m, "not digits")
	wantCode(t, err, connect.CodeInvalidArgument)

	codes, err := e.confirm(m, totpAt(t, m.secret, time.Now()))
	if err != nil {
		t.Fatalf("ConfirmTwoFactorSetup: %v", err)
	}
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("%d recovery codes, want %d", len(codes), auth.RecoveryCodeCount)
	}
	if n := e.count(`SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL`, m.id); n != auth.RecoveryCodeCount {
		t.Fatalf("stored recovery codes = %d", n)
	}
	for _, c := range codes {
		if n := e.count(`SELECT count(*) FROM recovery_codes WHERE position($1::bytea IN code_hash) > 0`, []byte(c)); n != 0 {
			t.Fatalf("recovery code %q is stored in the clear", c)
		}
	}
	st, err = e.authAs(m.access).GetTwoFactorStatus(ctx, connect.NewRequest(&perspectivv1.GetTwoFactorStatusRequest{}))
	if err != nil || !st.Msg.GetEnabled() || st.Msg.GetRecoveryCodesLeft() != 10 || st.Msg.GetEnabledAt() == nil {
		t.Fatalf("status after confirming = %v, %v", st, err)
	}
	if jobs := e.emailJobs(m.email()); jobs[len(jobs)-1].Template != "two_factor_enabled" {
		t.Fatalf("last email = %s, want two_factor_enabled", jobs[len(jobs)-1].Template)
	}

	// Setup cannot start again over a confirmed secret.
	_, err = e.authAs(m.access).BeginTwoFactorSetup(ctx, connect.NewRequest(&perspectivv1.BeginTwoFactorSetupRequest{CurrentPassword: goodPassword}))
	wantCode(t, err, connect.CodeFailedPrecondition)
	e.wantLog("two_factor_setup_started")
	e.wantLog("two_factor_setup_failed")
	e.wantLog("two_factor_enabled")
}

// A setup left unconfirmed for longer than TwoFactorSetupTTL must be
// started again.
func TestTwoFactorSetupExpires(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.newMember("Slow_Setup")
	e.begin(m)
	e.exec(`UPDATE totp_secrets SET created_at = now() - interval '31 minutes' WHERE user_id = $1`, m.id)
	_, err := e.confirm(m, totpAt(t, m.secret, time.Now()))
	wantCode(t, err, connect.CodeFailedPrecondition)
}

// Signing in with two-factor on: the password alone yields a challenge, the
// app's code completes it into the same session Login would give, a code
// for the next step (a phone clock ahead) is accepted, a used code is
// refused, and the challenge is single use.
func TestTwoFactorSignInWithTOTP(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Two_Step")
	e.enrol(m)

	ch := e.challenge(m, goodPassword)
	e.wantLog("login_two_factor_required")
	// The confirming code used this step, so a phone a step ahead signs in.
	ahead := totpAt(t, m.secret, time.Now().Add(30*time.Second))
	_, err := e.complete(ch, "123")
	wantCode(t, err, connect.CodeUnauthenticated)
	res, err := e.complete(ch, ahead)
	if err != nil {
		t.Fatalf("CompleteTwoFactorLogin: %v", err)
	}
	cookie := setCookieValue(t, res.Header())
	u := res.Msg.GetUser()
	if res.Msg.GetAccessToken() == "" || u.GetUserId() != m.id.String() || !u.GetTwoFactorEnabled() || res.Msg.GetUsedRecoveryCode() {
		t.Fatalf("response = %v", res.Msg)
	}
	wantCode(t, e.inbox(res.Msg.GetAccessToken()), connect.CodeUnimplemented)
	// The session refreshes like any other and still says two-factor is on.
	ref, _, err := e.refresh(cookie, "")
	if err != nil || !ref.Msg.GetUser().GetTwoFactorEnabled() {
		t.Fatalf("Refresh = %v, %v", ref, err)
	}

	// The challenge is spent.
	_, err = e.complete(ch, ahead)
	wantCode(t, err, connect.CodeFailedPrecondition)
	// The same code on a new challenge is a replay.
	_, err = e.complete(e.challenge(m, goodPassword), ahead)
	wantCode(t, err, connect.CodeUnauthenticated)
	if !strings.Contains(e.logs.String(), `"reason":"replayed_code"`) {
		t.Fatal("the replay was not audited as one")
	}
	// A replay is refused but not counted (TestReplayedCodeIsNotCounted).
	if n := e.count(`SELECT count(*) FROM two_factor_attempts WHERE user_id = $1 AND NOT succeeded`, m.id); n != 1 {
		t.Fatalf("failed codes recorded = %d, want 1 (the short one)", n)
	}
	if !strings.Contains(e.logs.String(), `"second_factor":"totp"`) {
		t.Fatal("no login_succeeded line naming the second factor")
	}
	e.wantLog("login_two_factor_failed")
}

// A phone clock a step behind is accepted too.
func TestTwoFactorAcceptsDriftBehind(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Slow_Clock")
	e.begin(m)
	codes, err := e.confirm(m, totpAt(t, m.secret, time.Now().Add(-30*time.Second)))
	if err != nil || len(codes) != 10 {
		t.Fatalf("confirming with the previous step's code: %v", err)
	}
	// Two steps behind is outside the window.
	_, err = e.complete(e.challenge(m, goodPassword), totpAt(t, m.secret, time.Now().Add(-60*time.Second)))
	wantCode(t, err, connect.CodeUnauthenticated)
	if _, err := e.complete(e.challenge(m, goodPassword), totpAt(t, m.secret, time.Now())); err != nil {
		t.Fatalf("the current step after the previous one was used: %v", err)
	}
}

// A recovery code signs in once, in any case and with or without dashes,
// says how many are left and emails the member; it never works again.
func TestTwoFactorSignInWithRecoveryCode(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.newMember("Lost_Phone")
	e.enrol(m)

	typed := strings.ToUpper(strings.ReplaceAll(m.codes[3], "-", " "))
	res, err := e.complete(e.challenge(m, goodPassword), typed)
	if err != nil {
		t.Fatalf("CompleteTwoFactorLogin with a recovery code: %v", err)
	}
	if !res.Msg.GetUsedRecoveryCode() || res.Msg.GetRecoveryCodesLeft() != 9 || res.Msg.GetAccessToken() == "" {
		t.Fatalf("response = %v", res.Msg)
	}
	jobs := e.emailJobs(m.email())
	last := jobs[len(jobs)-1]
	if last.Template != "recovery_code_used" || last.Data["left"] != "9" {
		t.Fatalf("last email = %+v, want recovery_code_used with 9 left", last)
	}
	_, err = e.complete(e.challenge(m, goodPassword), m.codes[3])
	wantCode(t, err, connect.CodeUnauthenticated)
	if n := e.count(`SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NOT NULL`, m.id); n != 1 {
		t.Fatalf("spent codes = %d, want 1", n)
	}
	e.wantLog("recovery_code_used")
}

// A challenge that expired, or that never existed, sends the member back to
// the password; so does one from before a password reset.
func TestTwoFactorChallengeEnds(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.newMember("Late_Code")
	e.enrol(m)
	ch := e.challenge(m, goodPassword)
	e.exec(`UPDATE two_factor_challenges SET expires_at = now() - interval '1 second' WHERE user_id = $1`, m.id)
	tests := []struct{ name, challenge string }{
		{name: "expired", challenge: ch},
		{name: "unknown", challenge: "not-a-challenge"},
		{name: "empty", challenge: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.complete(tc.challenge, totpAt(t, m.secret, time.Now().Add(30*time.Second)))
			wantCode(t, err, connect.CodeFailedPrecondition)
		})
	}
}

// Wrong codes lock the second factor like wrong passwords lock sign-in:
// TwoFactorFailures in TwoFactorWindow refuse even the right code, the
// count is in Postgres so a restarted server still refuses, the lock lifts
// once the failures leave the window, and TwoFactorDailyFailures in a day
// lock it again.
func TestTwoFactorLockout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Guess_Er")
	e.enrol(m)
	ch := e.challenge(m, goodPassword)
	for i := range auth.TwoFactorFailures {
		_, err := e.complete(ch, "000000")
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatalf("wrong code %d: %v, want Unauthenticated", i+1, err)
		}
	}
	right := totpAt(t, m.secret, time.Now().Add(30*time.Second))
	_, err := e.complete(ch, right)
	wantCode(t, err, connect.CodeResourceExhausted)
	if n := e.count(`SELECT count(*) FROM two_factor_attempts WHERE user_id = $1 AND NOT succeeded`, m.id); n != auth.TwoFactorFailures {
		t.Fatalf("failed codes in Postgres = %d, want %d (a refused code is not counted)", n, auth.TwoFactorFailures)
	}
	// Recovery codes are refused under the lock too.
	_, err = e.complete(ch, m.codes[0])
	wantCode(t, err, connect.CodeResourceExhausted)

	restarted := newEnvOn(t, e.pool)
	_, err = restarted.complete(ch, right)
	wantCode(t, err, connect.CodeResourceExhausted)
	restarted.wantLog("login_two_factor_locked")

	// Out of the 15-minute window the lock lifts.
	e.exec(`UPDATE two_factor_attempts SET attempted_at = now() - interval '16 minutes' WHERE user_id = $1`, m.id)
	if _, err := e.complete(ch, right); err != nil {
		t.Fatalf("after the window: %v", err)
	}

	// A day's cap holds even when the failures are spread out.
	e.exec(`INSERT INTO two_factor_attempts (user_id, succeeded, attempted_at)
		SELECT $1, false, now() - interval '2 hours' FROM generate_series(1, $2::int)`, m.id, auth.TwoFactorDailyFailures)
	_, err = e.complete(e.challenge(m, goodPassword), totpAt(t, m.secret, time.Now()))
	wantCode(t, err, connect.CodeResourceExhausted)
}

// Turning two-factor off and making new recovery codes take the password
// and a current code; each refusal is the right one, and off means the
// password alone signs in again.
func TestTwoFactorDisableAndRegenerate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Turn_Off")
	e.enrol(m)
	ctx := context.Background()
	client := e.authAs(m.access)
	next := totpAt(t, m.secret, time.Now().Add(30*time.Second))

	regen := func(password, code string) ([]string, error) {
		res, err := client.RegenerateRecoveryCodes(ctx, connect.NewRequest(&perspectivv1.RegenerateRecoveryCodesRequest{
			CurrentPassword: password, Code: code,
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetRecoveryCodes(), nil
	}
	refusals := []struct {
		name, password, code string
		want                 connect.Code
	}{
		{name: "wrong password", password: "wrong password!", code: next, want: connect.CodePermissionDenied},
		{name: "no code", password: goodPassword, code: "", want: connect.CodeInvalidArgument},
		{name: "wrong code", password: goodPassword, code: "000000", want: connect.CodeInvalidArgument},
		{name: "unknown recovery code", password: goodPassword, code: "aaaa-bbbb-cccc-dddd", want: connect.CodeInvalidArgument},
	}
	for _, tc := range refusals {
		t.Run("regenerate "+tc.name, func(t *testing.T) {
			_, err := regen(tc.password, tc.code)
			wantCode(t, err, tc.want)
		})
	}
	fresh, err := regen(goodPassword, m.codes[0])
	if err != nil || len(fresh) != 10 {
		t.Fatalf("RegenerateRecoveryCodes with a recovery code: %v", err)
	}
	// The old set is gone, the new one works.
	_, err = e.complete(e.challenge(m, goodPassword), m.codes[1])
	wantCode(t, err, connect.CodeUnauthenticated)
	if _, err := e.complete(e.challenge(m, goodPassword), fresh[0]); err != nil {
		t.Fatalf("a new recovery code: %v", err)
	}
	e.wantLog("recovery_codes_regenerated")

	// The wrong codes above count toward the two-factor lockout; clear them
	// so the refusals below are each judged on their own.
	e.exec(`DELETE FROM two_factor_attempts WHERE user_id = $1`, m.id)
	disable := func(password, code string) error {
		_, err := client.DisableTwoFactor(ctx, connect.NewRequest(&perspectivv1.DisableTwoFactorRequest{CurrentPassword: password, Code: code}))
		return err
	}
	for _, tc := range refusals {
		t.Run("disable "+tc.name, func(t *testing.T) {
			wantCode(t, disable(tc.password, tc.code), tc.want)
		})
	}
	if err := disable(goodPassword, next); err != nil {
		t.Fatalf("DisableTwoFactor: %v", err)
	}
	for _, table := range []string{"totp_secrets", "recovery_codes", "two_factor_challenges"} {
		if n := e.count(`SELECT count(*) FROM `+table+` WHERE user_id = $1`, m.id); n != 0 {
			t.Errorf("%s keeps %d rows after turning off", table, n)
		}
	}
	if jobs := e.emailJobs(m.email()); jobs[len(jobs)-1].Template != "two_factor_disabled" {
		t.Fatalf("last email = %s, want two_factor_disabled", jobs[len(jobs)-1].Template)
	}
	res, _, err := e.login("Turn_Off", goodPassword)
	if err != nil || res.Msg.GetAccessToken() == "" || res.Msg.GetTwoFactorChallenge() != "" {
		t.Fatalf("Login after turning off = %v, %v; want a session", res, err)
	}
	wantCode(t, disable(goodPassword, next), connect.CodeFailedPrecondition)
	e.wantLog("two_factor_disabled")
	e.wantLog("two_factor_disable_failed")
}

// A password reset by email does not get round the second factor: the
// reset ends any open challenge, and the next sign-in with the new password
// still asks for a code.
func TestPasswordResetStillRequiresTwoFactor(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.newMember("Forgot_It")
	e.enrol(m)
	open := e.challenge(m, goodPassword)
	ctx := context.Background()
	if _, err := e.auth().RequestPasswordReset(ctx, connect.NewRequest(&perspectivv1.RequestPasswordResetRequest{Email: m.email()})); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	const newPassword = "a brand new passphrase here"
	tok := e.linkToken(m.email(), "password_reset", "/reset-password")
	if _, err := e.auth().ResetPassword(ctx, connect.NewRequest(&perspectivv1.ResetPasswordRequest{Token: tok, NewPassword: newPassword})); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	_, err := e.complete(open, totpAt(t, m.secret, time.Now().Add(30*time.Second)))
	wantCode(t, err, connect.CodeFailedPrecondition)

	ch := e.challenge(m, newPassword)
	if n := e.count(`SELECT count(*) FROM totp_secrets WHERE user_id = $1 AND confirmed_at IS NOT NULL`, m.id); n != 1 {
		t.Fatal("the reset removed two-factor sign-in")
	}
	if _, err := e.complete(ch, m.codes[0]); err != nil {
		t.Fatalf("completing after the reset: %v", err)
	}
}

// The new RPCs need a signed-in caller, except the second step of sign-in,
// which carries its own challenge.
func TestTwoFactorRoutesNeedACaller(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	c := e.auth()
	calls := map[string]func() error{
		"GetTwoFactorStatus": func() error {
			_, err := c.GetTwoFactorStatus(ctx, connect.NewRequest(&perspectivv1.GetTwoFactorStatusRequest{}))
			return err
		},
		"BeginTwoFactorSetup": func() error {
			_, err := c.BeginTwoFactorSetup(ctx, connect.NewRequest(&perspectivv1.BeginTwoFactorSetupRequest{CurrentPassword: goodPassword}))
			return err
		},
		"ConfirmTwoFactorSetup": func() error {
			_, err := c.ConfirmTwoFactorSetup(ctx, connect.NewRequest(&perspectivv1.ConfirmTwoFactorSetupRequest{Code: "123456"}))
			return err
		},
		"DisableTwoFactor": func() error {
			_, err := c.DisableTwoFactor(ctx, connect.NewRequest(&perspectivv1.DisableTwoFactorRequest{CurrentPassword: goodPassword, Code: "123456"}))
			return err
		},
		"RegenerateRecoveryCodes": func() error {
			_, err := c.RegenerateRecoveryCodes(ctx, connect.NewRequest(&perspectivv1.RegenerateRecoveryCodesRequest{CurrentPassword: goodPassword, Code: "123456"}))
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			wantCode(t, call(), connect.CodeUnauthenticated)
		})
	}
}
