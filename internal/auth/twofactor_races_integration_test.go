//go:build integration

package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// The tests here park one request on a lock the test holds, run a second
// request against the same account, then let the first go, and check what
// is left. They pin the lock order in twofactor.go.

// holdLock runs query in a transaction the test keeps open, so the locks it
// takes are held until the returned release commits it.
func (e *env) holdLock(query string, args ...any) (release func()) {
	e.t.Helper()
	ctx := context.Background()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		e.t.Fatalf("begin: %v", err)
	}
	e.t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if err := tx.Commit(ctx); err != nil {
				e.t.Fatalf("release lock: %v", err)
			}
		})
	}
}

// lockWaiters is the number of this test database's sessions waiting on a
// lock.
func (e *env) lockWaiters() int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`)
}

// waitUntil polls until cond holds, failing after ten seconds.
func (e *env) waitUntil(what string, cond func() bool) {
	e.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	e.t.Fatalf("timed out waiting until %s", what)
}

// parkedOrDone waits until n requests are parked on a lock or done has
// fired, whichever the code under test does.
func (e *env) parkedOrDone(n int, done <-chan struct{}) {
	e.t.Helper()
	e.waitUntil("the second request is parked or done", func() bool {
		select {
		case <-done:
			return true
		default:
			return e.lockWaiters() >= n
		}
	})
}

// goRun starts fn in a goroutine; the channel closes when it returns.
func goRun(fn func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	return done
}

// wantSignedOut fails unless the access token and refresh cookie a second
// step returned are both refused.
func (e *env) wantSignedOut(res *connect.Response[perspectivv1.CompleteTwoFactorLoginResponse]) {
	e.t.Helper()
	access := res.Msg.GetAccessToken()
	_, err := e.authAs(access).GetTwoFactorStatus(context.Background(), connect.NewRequest(&perspectivv1.GetTwoFactorStatusRequest{}))
	wantCode(e.t, err, connect.CodeUnauthenticated)
	_, _, err = e.refresh(setCookieValue(e.t, res.Header()), "")
	wantCode(e.t, err, connect.CodeUnauthenticated)
}

// A second step that already holds its challenge when the member resets
// their password, or signs out everywhere, must not leave a session behind:
// the ending request deletes the challenges before it revokes sessions, so
// it waits for the second step to commit and then revokes its session.
// The second step is parked on the secret's row, after it locked its
// challenge.
func TestSecondStepRacingSignOutLeavesNoSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// prepare runs before the race; end is the request that ends every
		// sign-in.
		prepare func(e *env, m *member) (end func() error)
	}{
		{
			name: "password reset",
			prepare: func(e *env, m *member) func() error {
				ctx := context.Background()
				if _, err := e.auth().RequestPasswordReset(ctx, connect.NewRequest(&perspectivv1.RequestPasswordResetRequest{Email: m.email()})); err != nil {
					e.t.Fatalf("RequestPasswordReset: %v", err)
				}
				tok := e.linkToken(m.email(), "password_reset", "/reset-password")
				return func() error {
					_, err := e.auth().ResetPassword(ctx, connect.NewRequest(&perspectivv1.ResetPasswordRequest{
						Token: tok, NewPassword: "a brand new passphrase here",
					}))
					return err
				}
			},
		},
		{
			name: "sign out everywhere",
			prepare: func(e *env, m *member) func() error {
				return func() error {
					_, err := e.authAs(m.access).SignOutEverywhere(context.Background(), connect.NewRequest(&perspectivv1.SignOutEverywhereRequest{}))
					return err
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			steadyClock(t)
			m := e.newMember("Race_Out")
			e.enrol(m)
			ch := e.challenge(m, goodPassword)
			end := tc.prepare(e, m)

			release := e.holdLock(`SELECT 1 FROM totp_secrets WHERE user_id = $1 FOR UPDATE`, m.id)
			code := totpAt(t, m.secret, time.Now().Add(30*time.Second))
			var res *connect.Response[perspectivv1.CompleteTwoFactorLoginResponse]
			var completeErr error
			completed := goRun(func() { res, completeErr = e.complete(ch, code) })
			e.waitUntil("the second step is parked", func() bool { return e.lockWaiters() >= 1 })

			var endErr error
			ended := goRun(func() { endErr = end() })
			e.parkedOrDone(2, ended)
			release()
			<-completed
			<-ended

			if endErr != nil {
				t.Fatalf("%s: %v", tc.name, endErr)
			}
			if completeErr == nil {
				e.wantSignedOut(res)
			} else {
				wantCode(t, completeErr, connect.CodeFailedPrecondition)
			}
			if n := e.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, m.id); n != 0 {
				t.Fatalf("%d sessions still live after %s", n, tc.name)
			}
		})
	}
}

// A sign-in that started with the old password and was still waiting when
// the password was reset checks the new one: the hash is read inside the
// sign-in's transaction, not before it.
func TestLoginRacingPasswordResetChecksTheNewPassword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		twoFactor bool
	}{
		{name: "password only"},
		{name: "two-factor", twoFactor: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			m := e.newMember("Old_Pass")
			if tc.twoFactor {
				e.enrol(m)
			}
			ctx := context.Background()
			if _, err := e.auth().RequestPasswordReset(ctx, connect.NewRequest(&perspectivv1.RequestPasswordResetRequest{Email: m.email()})); err != nil {
				t.Fatalf("RequestPasswordReset: %v", err)
			}
			tok := e.linkToken(m.email(), "password_reset", "/reset-password")

			release := e.holdLock(`SELECT pg_advisory_xact_lock(hashtextextended('perspectiv.login:' || $1::uuid::text, 0))`, m.id)
			var loginErr error
			loggedIn := goRun(func() {
				_, loginErr = e.auth().Login(ctx, connect.NewRequest(&perspectivv1.LoginRequest{Email: m.email(), Password: goodPassword}))
			})
			e.waitUntil("the sign-in is parked", func() bool { return e.lockWaiters() >= 1 })
			if _, err := e.auth().ResetPassword(ctx, connect.NewRequest(&perspectivv1.ResetPasswordRequest{
				Token: tok, NewPassword: "a brand new passphrase here",
			})); err != nil {
				t.Fatalf("ResetPassword: %v", err)
			}
			release()
			<-loggedIn
			wantCode(t, loginErr, connect.CodeUnauthenticated)
		})
	}
}

// A sign-in on another device that read "two-factor is off" while the
// member was turning it on must not keep its session: Login reads the state
// under the two-factor lock that confirming takes, so the session it starts
// is committed before confirming revokes every other session.
func TestLoginRacingTwoFactorSetupKeepsNoSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Race_On")
	e.begin(m)
	ctx := context.Background()

	// The sign-in parks after reading the state, at its first refresh
	// token, which a share lock on the table holds back.
	release := e.holdLock(`LOCK TABLE refresh_tokens IN SHARE MODE`)
	var login *connect.Response[perspectivv1.LoginResponse]
	var loginErr error
	loggedIn := goRun(func() {
		login, loginErr = e.auth().Login(ctx, connect.NewRequest(&perspectivv1.LoginRequest{Email: m.email(), Password: goodPassword}))
	})
	e.waitUntil("the sign-in is parked", func() bool { return e.lockWaiters() >= 1 })
	code := totpAt(t, m.secret, time.Now())
	var confirmErr error
	confirmed := goRun(func() { _, confirmErr = e.confirm(m, code) })
	e.parkedOrDone(2, confirmed)
	release()
	<-loggedIn
	<-confirmed

	if confirmErr != nil {
		t.Fatalf("ConfirmTwoFactorSetup: %v", confirmErr)
	}
	if loginErr != nil {
		t.Fatalf("Login: %v", loginErr)
	}
	if login.Msg.GetTwoFactorChallenge() == "" {
		_, _, err := e.refresh(setCookieValue(t, login.Header()), "")
		wantCode(t, err, connect.CodeUnauthenticated)
	}
	if n := e.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, m.id); n != 1 {
		t.Fatalf("%d sessions live after turning two-factor on, want 1 (the one that turned it on)", n)
	}
}

// Turning two-factor off while a second step is waiting on the same
// account ends that second step cleanly instead of deadlocking: both take
// the two-factor lock before the challenge row.
func TestDisableRacingSecondStepDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	steadyClock(t)
	m := e.newMember("Race_Off")
	e.enrol(m)
	ch := e.challenge(m, goodPassword)
	ctx := context.Background()

	// Turning off parks on the secret's row, holding the two-factor lock.
	release := e.holdLock(`SELECT 1 FROM totp_secrets WHERE user_id = $1 FOR UPDATE`, m.id)
	code := totpAt(t, m.secret, time.Now().Add(30*time.Second))
	var disableErr error
	disabled := goRun(func() {
		_, disableErr = e.authAs(m.access).DisableTwoFactor(ctx, connect.NewRequest(&perspectivv1.DisableTwoFactorRequest{
			CurrentPassword: goodPassword, Code: code,
		}))
	})
	e.waitUntil("turning off is parked", func() bool { return e.lockWaiters() >= 1 })
	var completeErr error
	completed := goRun(func() { _, completeErr = e.complete(ch, m.codes[0]) })
	e.parkedOrDone(2, completed)
	release()
	<-disabled
	<-completed

	if disableErr != nil {
		t.Fatalf("DisableTwoFactor: %v", disableErr)
	}
	wantCode(t, completeErr, connect.CodeFailedPrecondition)
}

// One code, from the app or a recovery code, sent with several challenges
// at once signs in exactly once.
func TestParallelSecondStepsSpendACodeOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code func(t *testing.T, m *member) string
	}{
		{name: "app code", code: func(t *testing.T, m *member) string { return totpAt(t, m.secret, time.Now().Add(30*time.Second)) }},
		{name: "recovery code", code: func(_ *testing.T, m *member) string { return m.codes[0] }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			steadyClock(t)
			m := e.newMember("Par_Code")
			e.enrol(m)
			const n = 8
			chs := make([]string, n)
			for i := range chs {
				chs[i] = e.challenge(m, goodPassword)
			}
			code := tc.code(t, m)
			start := make(chan struct{})
			var wg sync.WaitGroup
			var mu sync.Mutex
			ok := 0
			for _, ch := range chs {
				wg.Go(func() {
					<-start
					if _, err := e.complete(ch, code); err == nil {
						mu.Lock()
						ok++
						mu.Unlock()
					}
				})
			}
			close(start)
			wg.Wait()
			if ok != 1 {
				t.Fatalf("%d of %d second steps signed in with one code, want 1", ok, n)
			}
		})
	}
}
