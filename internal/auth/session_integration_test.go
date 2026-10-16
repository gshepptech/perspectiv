//go:build integration

package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
)

// inbox calls a User route with access; a call that passes authorization
// answers Unimplemented from the stand-in handler.
func (e *env) inbox(access string) error {
	e.t.Helper()
	c := perspectivv1connect.NewNotifyServiceClient(e.srv.Client(), e.srv.URL+"/api", clientOpts(access)...)
	_, err := c.ListInbox(context.Background(), connect.NewRequest(&perspectivv1.ListInboxRequest{}))
	return err
}

// CT-002: login returns an access JWT in the body and the refresh token only
// as an HttpOnly Secure cookie; the access token authenticates a User route.
func TestLoginIssuesTokens(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	res, cookie, err := e.login("Writer_One", goodPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	m := res.Msg
	if m.GetAccessToken() == "" || cookie == "" || strings.Contains(m.String(), cookie) {
		t.Fatalf("access=%q cookie=%q; the refresh token must be only in the cookie", m.GetAccessToken(), cookie)
	}
	u := m.GetUser()
	if u.GetUserId() != id.String() || u.GetUsername() != "Writer_One" || u.GetRole() != perspectivv1.Role_ROLE_USER || u.GetEmailVerified() {
		t.Fatalf("user = %v", u)
	}
	ttl := time.Until(m.GetAccessTokenExpiresAt().AsTime())
	if ttl <= 10*time.Minute || ttl > 15*time.Minute {
		t.Fatalf("access token lives %v, want 15 minutes", ttl)
	}
	wantCode(t, e.inbox(m.GetAccessToken()), connect.CodeUnimplemented)
	wantCode(t, e.inbox(""), connect.CodeUnauthenticated)

	// Email is matched without regard to case.
	_, err = e.auth().Login(context.Background(), connect.NewRequest(&perspectivv1.LoginRequest{
		Email: "WRITER_ONE@EXAMPLE.TEST", Password: goodPassword,
	}))
	if err != nil {
		t.Fatalf("Login with upper-case email: %v", err)
	}
	var succeeded int
	e.scan(`SELECT count(*) FROM login_attempts WHERE user_id = $1 AND succeeded`, []any{id}, &succeeded)
	if succeeded != 2 {
		t.Fatalf("successful attempts recorded = %d, want 2", succeeded)
	}
	if !strings.Contains(e.logs.String(), `"event":"login_succeeded"`) {
		t.Fatal("no login_succeeded audit line")
	}
}

// CT-002: bad credentials are UNAUTHENTICATED, the same for an unknown email
// as for a wrong password, and a deleted account cannot sign in.
func TestLoginBadCredentials(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	gone := e.signUp("Gone_User")
	e.signUp("Writer_One")
	e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, gone)
	tests := []struct {
		name, email, password string
	}{
		{name: "wrong password", email: emailOf("Writer_One"), password: "wrong password!"},
		{name: "unknown email", email: "nobody@example.test", password: goodPassword},
		{name: "deleted account", email: emailOf("Gone_User"), password: goodPassword},
		{name: "empty", email: "", password: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.auth().Login(context.Background(), connect.NewRequest(&perspectivv1.LoginRequest{
				Email: tc.email, Password: tc.password,
			}))
			wantCode(t, err, connect.CodeUnauthenticated)
		})
	}
	if !strings.Contains(e.logs.String(), `"event":"login_failed"`) {
		t.Fatal("no login_failed audit line")
	}
}

// AC-004, NFR-001, OT-003: the eleventh login after ten wrong passwords in 15
// minutes is RESOURCE_EXHAUSTED even with the right password, the counter
// is in Postgres so a restarted server still refuses, and the lockout lifts
// once the failures age out of the window.
func TestLoginLockout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	for i := range 10 {
		_, _, err := e.login("Writer_One", "wrong password!")
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatalf("wrong password %d: %v, want Unauthenticated", i+1, err)
		}
	}
	_, _, err := e.login("Writer_One", goodPassword)
	wantCode(t, err, connect.CodeResourceExhausted)

	var failures int
	e.scan(`SELECT count(*) FROM login_attempts WHERE user_id = $1 AND NOT succeeded`, []any{id}, &failures)
	if failures != 10 {
		t.Fatalf("failed attempts in Postgres = %d, want 10 (a refused attempt while locked is not counted)", failures)
	}

	restarted := newEnvOn(t, e.pool)
	_, _, err = restarted.login("Writer_One", goodPassword)
	wantCode(t, err, connect.CodeResourceExhausted)
	if !strings.Contains(restarted.logs.String(), `"event":"login_locked"`) {
		t.Fatal("no login_locked audit line")
	}

	// Nine failures in the window are below the threshold.
	e.exec(`UPDATE login_attempts SET attempted_at = now() - interval '16 minutes'
		WHERE id = (SELECT min(id) FROM login_attempts WHERE user_id = $1)`, id)
	if _, _, err := restarted.login("Writer_One", goodPassword); err != nil {
		t.Fatalf("login with 9 failures in the window: %v", err)
	}
}

// The lockout is per account: another account signs in while one is locked.
func TestLoginLockoutIsPerAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	locked := e.signUp("Locked_One")
	e.signUp("Writer_Two")
	for range 10 {
		e.exec(`INSERT INTO login_attempts (user_id, succeeded) VALUES ($1, false)`, locked)
	}
	_, _, err := e.login("Locked_One", goodPassword)
	wantCode(t, err, connect.CodeResourceExhausted)
	if _, _, err := e.login("Writer_Two", goodPassword); err != nil {
		t.Fatalf("other account: %v", err)
	}
}

// Refresh rotates the cookie on every use; presenting a rotated token again
// is reuse, which revokes the whole session.
func TestRefreshRotationAndReuse(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	access, first := e.mustLogin("Writer_One")

	res, second, err := e.refresh(first, "")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if second == first || res.Msg.GetAccessToken() == "" || res.Msg.GetUser().GetUsername() != "Writer_One" {
		t.Fatalf("refresh did not rotate: first=%q second=%q msg=%v", first, second, res.Msg)
	}
	res2, third, err := e.refresh(second, "")
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	newAccess := res2.Msg.GetAccessToken()
	wantCode(t, e.inbox(newAccess), connect.CodeUnimplemented)

	var chain int
	e.scan(`SELECT count(*) FROM refresh_tokens WHERE parent_id IS NOT NULL`, nil, &chain)
	if chain != 2 {
		t.Fatalf("rotated tokens linked to a parent = %d, want 2", chain)
	}

	// Reuse of the first token: refused, and the session dies with it.
	_, _, err = e.refresh(first, "")
	wantCode(t, err, connect.CodeUnauthenticated)
	_, _, err = e.refresh(third, "")
	wantCode(t, err, connect.CodeUnauthenticated)
	wantCode(t, e.inbox(newAccess), connect.CodeUnauthenticated)
	wantCode(t, e.inbox(access), connect.CodeUnauthenticated)
	if !strings.Contains(e.logs.String(), `"event":"refresh_token_reuse"`) {
		t.Fatal("no refresh_token_reuse audit line")
	}
}

// Refresh works while the browser still sends an expired access token: an
// invalid bearer token reads as no credentials, not as a refusal.
func TestRefreshWithStaleAccessToken(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	_, cookie := e.mustLogin("Writer_One")
	var sid uuid.UUID
	e.scan(`SELECT id FROM sessions WHERE user_id = $1`, []any{id}, &sid)
	stale, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": id.String(), "sid": sid.String(), "iss": "perspectiv", "aud": "perspectiv-api",
		"iat": time.Now().Add(-time.Hour).Unix(), "exp": time.Now().Add(-30 * time.Minute).Unix(),
	}).SignedString([]byte(jwtSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	wantCode(t, e.inbox(stale), connect.CodeUnauthenticated)
	if _, _, err := e.refresh(cookie, stale); err != nil {
		t.Fatalf("Refresh with a stale bearer token: %v", err)
	}
}

func TestRefreshRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	tests := []struct {
		name  string
		setup func() string
	}{
		{name: "no cookie", setup: func() string { return "" }},
		{name: "an unknown token", setup: func() string { return "bm90LWEtcmVhbC10b2tlbg" }},
		{name: "an expired token", setup: func() string {
			_, c := e.mustLogin("Writer_One")
			e.exec(`UPDATE refresh_tokens SET expires_at = now() - interval '1 second'
				WHERE id = (SELECT id FROM refresh_tokens ORDER BY id DESC LIMIT 1)`)
			return c
		}},
		{name: "a session past its absolute expiry", setup: func() string {
			_, c := e.mustLogin("Writer_One")
			e.exec(`UPDATE sessions SET expires_at = now() - interval '1 second'
				WHERE id = (SELECT session_id FROM refresh_tokens ORDER BY id DESC LIMIT 1)`)
			return c
		}},
		{name: "a deleted account", setup: func() string {
			other := e.signUp("Gone_User")
			_, c := e.mustLogin("Gone_User")
			e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, other)
			return c
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := e.refresh(tc.setup(), "")
			wantCode(t, err, connect.CodeUnauthenticated)
		})
	}
}

// Logout revokes the session behind the cookie and clears the cookie; the
// access token from that session stops working at once.
func TestLogout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	access, cookie := e.mustLogin("Writer_One")

	req := connect.NewRequest(&perspectivv1.LogoutRequest{})
	req.Header().Set("Cookie", "perspectiv_refresh="+cookie)
	res, err := e.auth().Logout(context.Background(), req)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	cleared := false
	for _, line := range res.Header().Values("Set-Cookie") {
		if strings.HasPrefix(line, "perspectiv_refresh=;") && strings.Contains(line, "Max-Age=0") {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("Set-Cookie = %v, want the refresh cookie cleared", res.Header().Values("Set-Cookie"))
	}
	_, _, err = e.refresh(cookie, "")
	wantCode(t, err, connect.CodeUnauthenticated)
	wantCode(t, e.inbox(access), connect.CodeUnauthenticated)

	// Logout without a cookie, or with only an access token, still succeeds.
	if _, err := e.auth().Logout(context.Background(), connect.NewRequest(&perspectivv1.LogoutRequest{})); err != nil {
		t.Fatalf("Logout without a cookie: %v", err)
	}
	access2, cookie2 := e.mustLogin("Writer_One")
	c := perspectivv1connect.NewAuthServiceClient(e.srv.Client(), e.srv.URL+"/api", clientOpts(access2)...)
	if _, err := c.Logout(context.Background(), connect.NewRequest(&perspectivv1.LogoutRequest{})); err != nil {
		t.Fatalf("Logout with a bearer token: %v", err)
	}
	_, _, err = e.refresh(cookie2, "")
	wantCode(t, err, connect.CodeUnauthenticated)
}

// An admin's role comes from the database on every call.
func TestAuthenticateReadsRoleFromDatabase(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Admin_One")
	access, _ := e.mustLogin("Admin_One")
	mod := perspectivv1connect.NewModerationServiceClient(e.srv.Client(), e.srv.URL+"/api", clientOpts(access)...)
	_, err := mod.ListReports(context.Background(), connect.NewRequest(&perspectivv1.ListReportsRequest{}))
	wantCode(t, err, connect.CodePermissionDenied)
	e.exec(`UPDATE users SET role = 'ROLE_ADMIN' WHERE id = $1`, id)
	_, err = mod.ListReports(context.Background(), connect.NewRequest(&perspectivv1.ListReportsRequest{}))
	wantCode(t, err, connect.CodeUnimplemented)
}

// The authenticator fails closed: a database it cannot reach denies the call
// as Unavailable rather than treating the caller as anonymous.
func TestAuthenticateFailsClosed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	access, _ := e.mustLogin("Writer_One")
	e.pool.Close()
	wantCode(t, e.inbox(access), connect.CodeUnavailable)
}
