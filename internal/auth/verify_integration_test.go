//go:build integration

package auth_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
)

// verifiedRoutes calls every route that requires a verified email, as the
// holder of access, and returns each call's error by procedure.
func (e *env) verifiedRoutes(access string) map[string]error {
	e.t.Helper()
	ctx := context.Background()
	h, base, o := e.srv.Client(), e.srv.URL+"/api", clientOpts(access)
	out := map[string]error{}
	_, out["CreatePlace"] = perspectivv1connect.NewPlaceServiceClient(h, base, o...).
		CreatePlace(ctx, connect.NewRequest(&perspectivv1.CreatePlaceRequest{}))
	_, out["Publish"] = perspectivv1connect.NewPerspectiveServiceClient(h, base, o...).
		Publish(ctx, connect.NewRequest(&perspectivv1.PublishRequest{}))
	_, out["ClaimCharacter"] = perspectivv1connect.NewLoreServiceClient(h, base, o...).
		ClaimCharacter(ctx, connect.NewRequest(&perspectivv1.ClaimCharacterRequest{}))
	vote := perspectivv1connect.NewVoteServiceClient(h, base, o...)
	_, out["Vote"] = vote.Vote(ctx, connect.NewRequest(&perspectivv1.VoteRequest{}))
	_, out["VoteRemove"] = vote.VoteRemove(ctx, connect.NewRequest(&perspectivv1.VoteRemoveRequest{}))
	_, out["React"] = perspectivv1connect.NewReactServiceClient(h, base, o...).
		React(ctx, connect.NewRequest(&perspectivv1.ReactRequest{}))
	_, out["Report"] = perspectivv1connect.NewModerationServiceClient(h, base, o...).
		Report(ctx, connect.NewRequest(&perspectivv1.ReportRequest{}))
	return out
}

// AC-001, FR-064, ST-011: login before verification succeeds, and publish,
// claim, vote, react, create place and report are refused with
// FAILED_PRECONDITION until the verification link is followed.
func TestVerifiedEmailGate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	access, cookie := e.mustLogin("Writer_One")

	for rpc, err := range e.verifiedRoutes(access) {
		if connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("%s before verification: %v, want FailedPrecondition", rpc, err)
		}
	}

	tok := e.linkToken(emailOf("Writer_One"), "verify_email", "/verify-email")
	if _, err := e.auth().VerifyEmail(context.Background(), connect.NewRequest(&perspectivv1.VerifyEmailRequest{Token: tok})); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	// The same access token now passes the gate: the gate reads Postgres.
	for rpc, err := range e.verifiedRoutes(access) {
		if connect.CodeOf(err) != connect.CodeUnimplemented {
			t.Errorf("%s after verification: %v, want it to reach the handler", rpc, err)
		}
	}
	res, _, err := e.refresh(cookie, "")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !res.Msg.GetUser().GetEmailVerified() {
		t.Fatal("session user is not marked verified after verification")
	}
}

func TestVerifyEmailRefusesDeadTokens(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	tok := e.linkToken(emailOf("Writer_One"), "verify_email", "/verify-email")
	verify := func(token string) error {
		_, err := e.auth().VerifyEmail(context.Background(), connect.NewRequest(&perspectivv1.VerifyEmailRequest{Token: token}))
		return err
	}

	// Expired: refused, and the account stays unverified.
	e.exec(`UPDATE email_tokens SET expires_at = now() - interval '1 second' WHERE user_id = $1`, id)
	wantCode(t, verify(tok), connect.CodeInvalidArgument)
	var verified bool
	e.scan(`SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, []any{id}, &verified)
	if verified {
		t.Fatal("an expired token verified the account")
	}

	e.exec(`UPDATE email_tokens SET expires_at = now() + interval '1 hour' WHERE user_id = $1`, id)
	if err := verify(tok); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	tests := []struct {
		name, link string
	}{
		{name: "already used", link: tok},
		{name: "unknown", link: "bm90LWEtcmVhbC10b2tlbg"},
		{name: "empty", link: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, verify(tc.link), connect.CodeInvalidArgument)
		})
	}
	// A reset token is not a verification token.
	if _, err := e.auth().RequestPasswordReset(context.Background(), connect.NewRequest(&perspectivv1.RequestPasswordResetRequest{
		Email: emailOf("Writer_One"),
	})); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	wantCode(t, verify(e.linkToken(emailOf("Writer_One"), "password_reset", "/reset-password")), connect.CodeInvalidArgument)
}

func TestPasswordReset(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	_, oldCookie := e.mustLogin("Writer_One")
	request := func(email string) {
		t.Helper()
		if _, err := e.auth().RequestPasswordReset(context.Background(), connect.NewRequest(&perspectivv1.RequestPasswordResetRequest{
			Email: email,
		})); err != nil {
			t.Fatalf("RequestPasswordReset(%q): %v", email, err)
		}
	}
	reset := func(token, password string) error {
		_, err := e.auth().ResetPassword(context.Background(), connect.NewRequest(&perspectivv1.ResetPasswordRequest{
			Token: token, NewPassword: password,
		}))
		return err
	}

	// An unknown email is answered the same and sends nothing.
	request("nobody@example.test")
	request("not even an email")
	if jobs := e.emailJobs("nobody@example.test"); len(jobs) != 0 {
		t.Fatalf("reset for an unknown email queued %d jobs", len(jobs))
	}

	request("WRITER_ONE@example.test")
	tok := e.linkToken(emailOf("Writer_One"), "password_reset", "/reset-password")

	// A weak new password is refused and leaves the token usable.
	wantCode(t, reset(tok, "short"), connect.CodeInvalidArgument)
	const newPassword = "a different long passphrase"
	if err := reset(tok, newPassword); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	wantCode(t, reset(tok, "yet another passphrase"), connect.CodeInvalidArgument)
	wantCode(t, reset("bm90LWEtcmVhbC10b2tlbg", newPassword), connect.CodeInvalidArgument)

	_, _, err := e.login("Writer_One", goodPassword)
	wantCode(t, err, connect.CodeUnauthenticated)
	if _, _, err := e.login("Writer_One", newPassword); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	// Every session that existed before the reset is revoked.
	_, _, err = e.refresh(oldCookie, "")
	wantCode(t, err, connect.CodeUnauthenticated)
	// Following the emailed link proved the address.
	var verified bool
	e.scan(`SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, []any{id}, &verified)
	if !verified {
		t.Fatal("a completed reset did not verify the email")
	}
}

// Reset emails are rate limited per account, silently: the caller always
// sees success so the answer never reveals whether the email has an account.
func TestPasswordResetEmailsLimited(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	for range 6 {
		if _, err := e.auth().RequestPasswordReset(context.Background(), connect.NewRequest(&perspectivv1.RequestPasswordResetRequest{
			Email: emailOf("Writer_One"),
		})); err != nil {
			t.Fatalf("RequestPasswordReset: %v", err)
		}
	}
	n := 0
	for _, j := range e.emailJobs(emailOf("Writer_One")) {
		if j.Template == "password_reset" {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("reset emails queued = %d, want 3", n)
	}
}

// An expired reset token is refused.
func TestPasswordResetTokenExpires(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	if _, err := e.auth().RequestPasswordReset(context.Background(), connect.NewRequest(&perspectivv1.RequestPasswordResetRequest{
		Email: emailOf("Writer_One"),
	})); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	tok := e.linkToken(emailOf("Writer_One"), "password_reset", "/reset-password")
	var ttl float64
	e.scan(`SELECT extract(epoch FROM expires_at - created_at) FROM email_tokens WHERE user_id = $1 AND purpose = 'password_reset'`,
		[]any{id}, &ttl)
	if ttl < 3590 || ttl > 3610 {
		t.Fatalf("reset token lives %vs, want one hour", ttl)
	}
	e.exec(`UPDATE email_tokens SET expires_at = now() - interval '1 second' WHERE user_id = $1`, id)
	_, err := e.auth().ResetPassword(context.Background(), connect.NewRequest(&perspectivv1.ResetPasswordRequest{
		Token: tok, NewPassword: "a different long passphrase",
	}))
	wantCode(t, err, connect.CodeInvalidArgument)
}
