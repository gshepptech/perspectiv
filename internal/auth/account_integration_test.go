//go:build integration

package auth_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

func TestChangePassword(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	access, cookie := e.mustLogin("Writer_One")
	_, otherCookie := e.mustLogin("Writer_One")
	change := func(access, current, next string) error {
		_, err := e.authAs(access).ChangePassword(context.Background(), connect.NewRequest(&perspectivv1.ChangePasswordRequest{
			CurrentPassword: current, NewPassword: next,
		}))
		return err
	}
	const newPassword = "a different long passphrase"

	tests := []struct {
		name, access, current, next string
		want                        connect.Code
	}{
		{name: "signed out", current: goodPassword, next: newPassword, want: connect.CodeUnauthenticated},
		{name: "no current password", access: access, next: newPassword, want: connect.CodeInvalidArgument},
		{name: "wrong current password", access: access, current: "not the password at all", next: newPassword, want: connect.CodePermissionDenied},
		{name: "weak new password", access: access, current: goodPassword, next: "short", want: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, change(tc.access, tc.current, tc.next), tc.want)
		})
	}
	// The wrong guess counts toward the login lockout.
	var failures int
	e.scan(`SELECT count(*) FROM login_attempts la JOIN users u ON u.id = la.user_id
	        WHERE u.username = 'Writer_One' AND NOT la.succeeded`, nil, &failures)
	if failures != 1 {
		t.Fatalf("failed attempts recorded = %d, want 1", failures)
	}

	if err := change(access, goodPassword, newPassword); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, _, err := e.login("Writer_One", newPassword); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	_, _, err := e.login("Writer_One", goodPassword)
	wantCode(t, err, connect.CodeUnauthenticated)
	// This device stays signed in; the other one is signed out.
	if _, _, err := e.refresh(cookie, ""); err != nil {
		t.Fatalf("refresh on the changing device: %v", err)
	}
	_, _, err = e.refresh(otherCookie, "")
	wantCode(t, err, connect.CodeUnauthenticated)
}

func TestChangePasswordHonoursLockout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	access, _ := e.mustLogin("Writer_One")
	e.exec(`INSERT INTO login_attempts (user_id, succeeded) SELECT $1, false FROM generate_series(1, 10)`, id)
	_, err := e.authAs(access).ChangePassword(context.Background(), connect.NewRequest(&perspectivv1.ChangePasswordRequest{
		CurrentPassword: goodPassword, NewPassword: "a different long passphrase",
	}))
	wantCode(t, err, connect.CodeResourceExhausted)
}

func TestEmailChange(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	e.signUp("Writer_Two")
	access, _ := e.mustLogin("Writer_One")
	request := func(newEmail, current string) error {
		_, err := e.authAs(access).RequestEmailChange(context.Background(), connect.NewRequest(&perspectivv1.RequestEmailChangeRequest{
			NewEmail: newEmail, CurrentPassword: current,
		}))
		return err
	}
	confirm := func(token string) error {
		_, err := e.auth().ConfirmEmailChange(context.Background(), connect.NewRequest(&perspectivv1.ConfirmEmailChangeRequest{Token: token}))
		return err
	}
	const moved = "moved@example.test"

	tests := []struct {
		name, email, current string
		want                 connect.Code
	}{
		{name: "malformed", email: "not an email", current: goodPassword, want: connect.CodeInvalidArgument},
		{name: "wrong password", email: moved, current: "not the password at all", want: connect.CodePermissionDenied},
		{name: "same address", email: "WRITER_ONE@example.test", current: goodPassword, want: connect.CodeInvalidArgument},
		{name: "taken", email: emailOf("Writer_Two"), current: goodPassword, want: connect.CodeAlreadyExists},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, request(tc.email, tc.current), tc.want)
		})
	}
	if n := len(e.emailJobs(moved)); n != 0 {
		t.Fatalf("refused requests queued %d emails to the new address", n)
	}

	if err := request(moved, goodPassword); err != nil {
		t.Fatalf("RequestEmailChange: %v", err)
	}
	// The old address hears about it, without the new address in the mail.
	var notices int
	for _, j := range e.emailJobs(emailOf("Writer_One")) {
		if j.Template == "email_change_notice" {
			notices++
			for k, v := range j.Data {
				if v == moved {
					t.Fatalf("the notice to the old address carries the new one in %q", k)
				}
			}
		}
	}
	if notices != 1 {
		t.Fatalf("notices to the old address = %d, want 1", notices)
	}
	tok := e.linkToken(moved, "change_email", "/confirm-email")
	// Nothing changes until the link is followed.
	if _, _, err := e.login("Writer_One", goodPassword); err != nil {
		t.Fatalf("login with the old address before confirming: %v", err)
	}

	if err := confirm(tok); err != nil {
		t.Fatalf("ConfirmEmailChange: %v", err)
	}
	var email string
	var verified bool
	e.scan(`SELECT email, email_verified_at IS NOT NULL FROM users WHERE id = $1`, []any{id}, &email, &verified)
	if email != moved || !verified {
		t.Fatalf("after confirming: email %q verified %v, want %q verified", email, verified, moved)
	}
	wantCode(t, confirm(tok), connect.CodeInvalidArgument)
	wantCode(t, confirm(""), connect.CodeInvalidArgument)
}

// A second request replaces the first, and an address taken by another
// account between request and confirmation is refused and spends the link.
func TestEmailChangeRaces(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	access, _ := e.mustLogin("Writer_One")
	request := func(newEmail string) {
		t.Helper()
		if _, err := e.authAs(access).RequestEmailChange(context.Background(), connect.NewRequest(&perspectivv1.RequestEmailChangeRequest{
			NewEmail: newEmail, CurrentPassword: goodPassword,
		})); err != nil {
			t.Fatalf("RequestEmailChange(%q): %v", newEmail, err)
		}
	}
	confirm := func(token string) error {
		_, err := e.auth().ConfirmEmailChange(context.Background(), connect.NewRequest(&perspectivv1.ConfirmEmailChangeRequest{Token: token}))
		return err
	}

	request("first@example.test")
	first := e.linkToken("first@example.test", "change_email", "/confirm-email")
	request("second@example.test")
	second := e.linkToken("second@example.test", "change_email", "/confirm-email")
	wantCode(t, confirm(first), connect.CodeInvalidArgument)

	e.signUp("Second") // takes second@example.test
	wantCode(t, confirm(second), connect.CodeAlreadyExists)
	wantCode(t, confirm(second), connect.CodeInvalidArgument)
	var email string
	e.scan(`SELECT email FROM users WHERE id = $1`, []any{id}, &email)
	if email != emailOf("Writer_One") {
		t.Fatalf("email = %q after refused changes, want it unchanged", email)
	}
}

func TestSignOutEverywhere(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	access, cookie := e.mustLogin("Writer_One")
	_, otherCookie := e.mustLogin("Writer_One")

	_, err := e.auth().SignOutEverywhere(context.Background(), connect.NewRequest(&perspectivv1.SignOutEverywhereRequest{}))
	wantCode(t, err, connect.CodeUnauthenticated)

	res, err := e.authAs(access).SignOutEverywhere(context.Background(), connect.NewRequest(&perspectivv1.SignOutEverywhereRequest{}))
	if err != nil {
		t.Fatalf("SignOutEverywhere: %v", err)
	}
	if v := setCookieValue(t, res.Header()); v != "" {
		t.Fatalf("refresh cookie = %q, want it cleared", v)
	}
	for name, c := range map[string]string{"this device": cookie, "other device": otherCookie} {
		_, _, err := e.refresh(c, "")
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("refresh on %s: %v, want Unauthenticated", name, err)
		}
	}
	// The access token dies with its session.
	_, err = e.authAs(access).SignOutEverywhere(context.Background(), connect.NewRequest(&perspectivv1.SignOutEverywhereRequest{}))
	wantCode(t, err, connect.CodeUnauthenticated)
}
