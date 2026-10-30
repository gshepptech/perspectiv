//go:build integration

package auth_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
)

// AC-001, CT-001, OT-001: sign up returns a user id and queues a
// verification email whose link carries a token.
func TestSignUpQueuesVerificationEmail(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")

	var username, email string
	var ageConfirmed, verified bool
	e.scan(`SELECT username, email, age_confirmed_at IS NOT NULL, email_verified_at IS NOT NULL FROM users WHERE id = $1`,
		[]any{id}, &username, &email, &ageConfirmed, &verified)
	if username != "Writer_One" || email != emailOf("Writer_One") || !ageConfirmed || verified {
		t.Fatalf("row = %q %q age=%v verified=%v", username, email, ageConfirmed, verified)
	}
	var hash string
	e.scan(`SELECT password_hash FROM users WHERE id = $1`, []any{id}, &hash)
	if strings.Contains(hash, goodPassword) || !strings.HasPrefix(hash, "$2") {
		t.Fatalf("password stored as %q, want a bcrypt hash", hash)
	}

	jobs := e.emailJobs(emailOf("Writer_One"))
	if len(jobs) != 1 || jobs[0].Template != "verify_email" || jobs[0].Data["username"] != "Writer_One" {
		t.Fatalf("email jobs = %+v, want one verify_email", jobs)
	}
	tok := e.linkToken(emailOf("Writer_One"), "verify_email", "/verify-email")
	var stored int
	e.scan(`SELECT count(*) FROM email_tokens WHERE user_id = $1 AND purpose = 'verify_email' AND token_hash <> convert_to($2, 'UTF8')`,
		[]any{id, tok}, &stored)
	if stored != 1 {
		t.Fatalf("verify tokens stored (hashed) = %d, want 1", stored)
	}
}

// AC-002, AC-003, CT-001, OT-002, OT-005: invalid input is refused with
// INVALID_ARGUMENT and nothing is written.
func TestSignUpRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	valid := func() *perspectivv1.SignUpRequest {
		return &perspectivv1.SignUpRequest{
			Email: "valid@example.test", Password: goodPassword, Username: "valid_name", AgeConfirmed: true,
		}
	}
	tests := []struct {
		name string
		mut  func(*perspectivv1.SignUpRequest)
	}{
		{name: "username Ab is too short", mut: func(r *perspectivv1.SignUpRequest) { r.Username = "Ab" }},
		{name: "a 21-character username is too long", mut: func(r *perspectivv1.SignUpRequest) { r.Username = strings.Repeat("a", 21) }},
		{name: "a username with a hyphen", mut: func(r *perspectivv1.SignUpRequest) { r.Username = "bad-name" }},
		{name: "a username with a space", mut: func(r *perspectivv1.SignUpRequest) { r.Username = "bad name" }},
		{name: "no 18-or-over confirmation", mut: func(r *perspectivv1.SignUpRequest) { r.AgeConfirmed = false }},
		{name: "a bad email", mut: func(r *perspectivv1.SignUpRequest) { r.Email = "not-an-email" }},
		{name: "a short password", mut: func(r *perspectivv1.SignUpRequest) { r.Password = "short" }},
		{name: "the username as password", mut: func(r *perspectivv1.SignUpRequest) { r.Password = "Valid_Name" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := valid()
			tc.mut(req)
			_, err := e.auth().SignUp(context.Background(), connect.NewRequest(req))
			wantCode(t, err, connect.CodeInvalidArgument)
		})
	}
	var users int
	e.scan(`SELECT count(*) FROM users`, nil, &users)
	if users != 0 {
		t.Fatalf("users = %d after refused sign ups, want 0", users)
	}
	// The boundaries themselves are accepted.
	for _, name := range []string{"Abc", strings.Repeat("z", 20)} {
		req := valid()
		req.Username, req.Email = name, strings.ToLower(name)+"@example.test"
		if _, err := e.auth().SignUp(context.Background(), connect.NewRequest(req)); err != nil {
			t.Fatalf("SignUp(%q): %v", name, err)
		}
	}
}

// AC-002, CT-001: a username or an email differing only by case collides.
func TestSignUpCollisionsIgnoreCase(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	tests := []struct {
		name     string
		email    string
		username string
		wantMsg  string
	}{
		{name: "username in another case", email: "other@example.test", username: "WRITER_one", wantMsg: "username"},
		{name: "email in another case", email: "WRITER_ONE@Example.TEST", username: "someone_else", wantMsg: "email"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.auth().SignUp(context.Background(), connect.NewRequest(&perspectivv1.SignUpRequest{
				Email: tc.email, Password: goodPassword, Username: tc.username, AgeConfirmed: true,
			}))
			wantCode(t, err, connect.CodeAlreadyExists)
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %v, want it to name the %s", err, tc.wantMsg)
			}
		})
	}
	if jobs := e.emailJobs("other@example.test"); len(jobs) != 0 {
		t.Fatalf("a refused sign up queued %d emails", len(jobs))
	}
}

// FR-039, CT-001: sign ups from one address are rate limited in Postgres.
func TestSignUpRateLimitedPerIP(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	client := perspectivv1connect.NewAuthServiceClient(e.srv.Client(), e.srv.URL+"/api",
		connect.WithInterceptors(forwardedFor("198.51.100.7")))
	var err error
	for i := range 20 {
		_, err = client.SignUp(context.Background(), connect.NewRequest(&perspectivv1.SignUpRequest{
			Email: uuid.NewString() + "@example.test", Password: goodPassword,
			Username: "user_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10], AgeConfirmed: true,
		}))
		if err != nil {
			if i == 0 {
				t.Fatalf("first sign up: %v", err)
			}
			break
		}
	}
	wantCode(t, err, connect.CodeResourceExhausted)
	var counted int
	e.scan(`SELECT coalesce(sum(count), 0) FROM rate_limit_counters WHERE action = 'signup' AND subject = 'ip:198.51.100.7'`, nil, &counted)
	if counted != 11 {
		t.Fatalf("signup counter for the address = %d, want 11", counted)
	}
	var users int
	e.scan(`SELECT count(*) FROM users`, nil, &users)
	if users != 10 {
		t.Fatalf("users = %d, want the 10 the limit allows", users)
	}
}

// Signing up signs the new member in, as a sign-in would: an access token
// and the refresh cookie, before the email is verified.
func TestSignUpSignsIn(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	req := connect.NewRequest(&perspectivv1.SignUpRequest{
		Email: emailOf("Writer_One"), Password: goodPassword, Username: "Writer_One", AgeConfirmed: true,
	})
	req.Header().Set("User-Agent", firefoxMac)
	res, err := e.auth().SignUp(context.Background(), req)
	if err != nil {
		t.Fatalf("SignUp: %v", err)
	}
	if res.Msg.GetAccessToken() == "" || res.Msg.GetUser().GetUsername() != "Writer_One" || res.Msg.GetUser().GetEmailVerified() {
		t.Fatalf("SignUp response = %v, want a session for an unverified member", res.Msg)
	}
	cookie := setCookieValue(t, res.Header())
	if _, _, err := e.refresh(cookie, ""); err != nil {
		t.Fatalf("refresh with the sign-up cookie: %v", err)
	}
	sessions, err := e.authAs(res.Msg.GetAccessToken()).ListSessions(context.Background(), connect.NewRequest(&perspectivv1.ListSessionsRequest{}))
	if err != nil || len(sessions.Msg.GetSessions()) != 1 || sessions.Msg.GetSessions()[0].GetDevice() != "Firefox on macOS" {
		t.Fatalf("sessions after sign-up = %v, %v", sessions, err)
	}
}
