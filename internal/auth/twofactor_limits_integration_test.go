//go:build integration

package auth_test

import (
	"context"
	"crypto/sha256"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/auth"
)

// Turning two-factor sign-in on signs out every other session, so a refresh
// cookie someone took before it was on stops working; the session that
// turned it on stays signed in.
func TestTurningOnTwoFactorSignsOutOtherSessions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Other_Dev")
	otherAccess, otherCookie := e.mustLogin("Other_Dev")
	e.enrol(m)
	ctx := context.Background()

	_, _, err := e.refresh(otherCookie, "")
	wantCode(t, err, connect.CodeUnauthenticated)
	_, err = e.authAs(otherAccess).GetTwoFactorStatus(ctx, connect.NewRequest(&perspectivv1.GetTwoFactorStatusRequest{}))
	wantCode(t, err, connect.CodeUnauthenticated)
	if _, err := e.authAs(m.access).GetTwoFactorStatus(ctx, connect.NewRequest(&perspectivv1.GetTwoFactorStatusRequest{})); err != nil {
		t.Fatalf("the session that turned two-factor on was signed out: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, m.id); n != 1 {
		t.Fatalf("live sessions = %d, want 1", n)
	}
}

// Wrong codes at setup count against the pending setup, not toward the
// sign-in lockout: the fifth deletes the pending secret, after which setup
// starts again behind the password, and nothing lands in
// two_factor_attempts. So a stolen session cannot lock the owner out.
func TestPendingSetupCountsItsOwnWrongCodes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Pend_Lock")
	e.begin(m)
	const setupFailures = auth.TwoFactorSetupFailures
	for i := range setupFailures - 1 {
		_, err := e.confirm(m, "000000")
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("wrong code %d: %v, want InvalidArgument", i+1, err)
		}
	}
	// Starting again clears the count.
	e.begin(m)
	for i := range setupFailures - 1 {
		_, err := e.confirm(m, "000000")
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("wrong code %d after starting again: %v, want InvalidArgument", i+1, err)
		}
	}
	_, err := e.confirm(m, "000000")
	wantCode(t, err, connect.CodeFailedPrecondition)
	if n := e.count(`SELECT count(*) FROM totp_secrets WHERE user_id = $1`, m.id); n != 0 {
		t.Fatalf("the pending secret survived %d wrong codes", setupFailures)
	}
	// Whoever holds the session keeps getting nowhere, even with the right
	// code for the deleted secret (a few more calls; the route's rate limit
	// caps the rest).
	for range setupFailures {
		_, err = e.confirm(m, "000000")
		wantCode(t, err, connect.CodeFailedPrecondition)
	}
	_, err = e.confirm(m, totpAt(t, m.secret, time.Now()))
	wantCode(t, err, connect.CodeFailedPrecondition)
	if n := e.count(`SELECT count(*) FROM two_factor_attempts WHERE user_id = $1`, m.id); n != 0 {
		t.Fatalf("setup codes counted toward the sign-in lockout: %d attempts", n)
	}
	e.wantLog("two_factor_setup_failed")
	if !strings.Contains(e.logs.String(), `"reason":"too_many_wrong_codes"`) {
		t.Fatal("abandoning the setup was not audited")
	}

	// The owner starts again and turns it on.
	e.enrol(m)
}

// Every use of a recovery code, and every new set of codes, is emailed.
func TestRecoveryCodeChangesAreEmailed(t *testing.T) {
	t.Parallel()
	newCodes := func(e *env, m *member, code string) error {
		_, err := e.authAs(m.access).RegenerateRecoveryCodes(context.Background(), connect.NewRequest(&perspectivv1.RegenerateRecoveryCodesRequest{
			CurrentPassword: goodPassword, Code: code,
		}))
		return err
	}
	turnOff := func(e *env, m *member, code string) error {
		_, err := e.authAs(m.access).DisableTwoFactor(context.Background(), connect.NewRequest(&perspectivv1.DisableTwoFactorRequest{
			CurrentPassword: goodPassword, Code: code,
		}))
		return err
	}
	tests := []struct {
		name        string
		act         func(e *env, m *member, code string) error
		useRecovery bool
		want        []string // templates queued, in order, with used_for after a colon
	}{
		{name: "new codes with an app code", act: newCodes, want: []string{"recovery_codes_replaced"}},
		{name: "new codes with a recovery code", act: newCodes, useRecovery: true, want: []string{"recovery_code_used:new_codes", "recovery_codes_replaced"}},
		{name: "turn off with an app code", act: turnOff, want: []string{"two_factor_disabled"}},
		{name: "turn off with a recovery code", act: turnOff, useRecovery: true, want: []string{"recovery_code_used:turn_off", "two_factor_disabled"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			steadyClock(t)
			m := e.newMember("Mail_Me")
			e.enrol(m)
			before := len(e.emailJobs(m.email()))
			code := totpAt(t, m.secret, time.Now().Add(30*time.Second))
			if tc.useRecovery {
				code = m.codes[0]
			}
			if err := tc.act(e, m, code); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := sentSince(e, m.email(), before); !slices.Equal(got, tc.want) {
				t.Fatalf("emails = %v, want %v", got, tc.want)
			}
			if tc.useRecovery {
				e.wantLog("recovery_code_used")
			}
		})
	}
}

// sentSince names the emails queued to address after the first before, as
// the template with any used_for after a colon.
func sentSince(e *env, address string, before int) []string {
	var got []string
	for _, j := range e.emailJobs(address)[before:] {
		name := j.Template
		if used := j.Data["used_for"]; used != "" {
			name += ":" + used
		}
		got = append(got, name)
	}
	return got
}

// A right code sent again for a step already used (a client retrying a
// request that went through) is refused but not counted, so retries cannot
// lock the member out; a wrong code still counts.
func TestReplayedCodeIsNotCounted(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Re_Try")
	e.enrol(m)
	used := totpAt(t, m.secret, time.Now().Add(30*time.Second))
	if _, err := e.complete(e.challenge(m, goodPassword), used); err != nil {
		t.Fatalf("CompleteTwoFactorLogin: %v", err)
	}
	for i := range auth.TwoFactorFailures + 1 {
		_, err := e.complete(e.challenge(m, goodPassword), used)
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatalf("replay %d: %v, want Unauthenticated (refused, not locked)", i+1, err)
		}
	}
	_, err := e.authAs(m.access).RegenerateRecoveryCodes(context.Background(), connect.NewRequest(&perspectivv1.RegenerateRecoveryCodesRequest{
		CurrentPassword: goodPassword, Code: used,
	}))
	wantCode(t, err, connect.CodeInvalidArgument)
	if n := e.count(`SELECT count(*) FROM two_factor_attempts WHERE user_id = $1 AND NOT succeeded`, m.id); n != 0 {
		t.Fatalf("replays counted as %d failed codes, want 0", n)
	}
	if !strings.Contains(e.logs.String(), `"reason":"replayed_code"`) {
		t.Fatal("the replays were not audited")
	}
	_, err = e.complete(e.challenge(m, goodPassword), "000000")
	wantCode(t, err, connect.CodeUnauthenticated)
	if n := e.count(`SELECT count(*) FROM two_factor_attempts WHERE user_id = $1 AND NOT succeeded`, m.id); n != 1 {
		t.Fatalf("a wrong code counted as %d failed codes, want 1", n)
	}
}

// The lockout belongs to the account, not to one challenge: a new password
// sign-in does not reset it.
func TestTwoFactorLockoutIsPerAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Lock_Acct")
	e.enrol(m)
	for range auth.TwoFactorFailures {
		_, err := e.complete(e.challenge(m, goodPassword), "000000")
		wantCode(t, err, connect.CodeUnauthenticated)
	}
	_, err := e.complete(e.challenge(m, goodPassword), totpAt(t, m.secret, time.Now().Add(30*time.Second)))
	wantCode(t, err, connect.CodeResourceExhausted)
}

// legacyHash is how a recovery code was stored before hashes were salted
// per account (hash version 1).
func legacyHash(code string) []byte {
	sum := sha256.Sum256([]byte("perspectiv.recovery:" + strings.ReplaceAll(code, "-", "")))
	return sum[:]
}

// Recovery codes are stored salted with the account (hash version 2), and a
// code stored the old, unsalted way still signs in until it is used.
func TestRecoveryCodeHashes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.newMember("Salt_Ed")
	e.enrol(m)
	for _, c := range m.codes {
		if n := e.count(`SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND code_hash = $2`, m.id, legacyHash(c)); n != 0 {
			t.Fatalf("recovery code %q is stored with the unsalted hash", c)
		}
	}
	if n := e.count(`SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND hash_version = 2`, m.id); n != auth.RecoveryCodeCount {
		t.Fatalf("version 2 hashes = %d, want %d", n, auth.RecoveryCodeCount)
	}

	const old = "abcd-efgh-jkmn-pqrs"
	e.exec(`INSERT INTO recovery_codes (user_id, code_hash, hash_version) VALUES ($1, $2, 1)`, m.id, legacyHash(old))
	res, err := e.complete(e.challenge(m, goodPassword), strings.ToUpper(old))
	if err != nil {
		t.Fatalf("a version 1 code: %v", err)
	}
	if !res.Msg.GetUsedRecoveryCode() {
		t.Fatalf("response = %v, want a recovery code sign-in", res.Msg)
	}
	_, err = e.complete(e.challenge(m, goodPassword), old)
	wantCode(t, err, connect.CodeUnauthenticated)
}
