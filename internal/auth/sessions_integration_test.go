//go:build integration

package auth_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// loginFrom signs in with a User-Agent and returns the access token and the
// refresh cookie value.
func (e *env) loginFrom(username, userAgent string) (string, string) {
	e.t.Helper()
	req := connect.NewRequest(&perspectivv1.LoginRequest{Email: emailOf(username), Password: goodPassword})
	req.Header().Set("User-Agent", userAgent)
	res, err := e.auth().Login(context.Background(), req)
	if err != nil {
		e.t.Fatalf("Login: %v", err)
	}
	return res.Msg.GetAccessToken(), setCookieValue(e.t, res.Header())
}

const (
	firefoxMac    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14.6; rv:131.0) Gecko/20100101 Firefox/131.0"
	safariIPhone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
	chromeWindows = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
)

// A member sees each signed-in device by browser and system, which one they
// are using, and can sign another one out, but not someone else's.
func TestSessions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.signUp("Writer_One")
	e.signUp("Writer_Two")
	laptop, _ := e.loginFrom("Writer_One", firefoxMac)
	_, phoneCookie := e.loginFrom("Writer_One", safariIPhone)
	other, _ := e.loginFrom("Writer_Two", chromeWindows)

	list := func(access string) []*perspectivv1.SessionInfo {
		t.Helper()
		res, err := e.authAs(access).ListSessions(context.Background(), connect.NewRequest(&perspectivv1.ListSessionsRequest{}))
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		return res.Msg.GetSessions()
	}
	revoke := func(access, sid string) error {
		_, err := e.authAs(access).RevokeSession(context.Background(), connect.NewRequest(&perspectivv1.RevokeSessionRequest{SessionId: sid}))
		return err
	}

	got := list(laptop)
	devices := map[string]bool{}
	var phone, current string
	for _, s := range got {
		devices[s.GetDevice()] = s.GetCurrent()
		if s.GetDevice() == "Safari on iPhone" {
			phone = s.GetSessionId()
		}
		if s.GetCurrent() {
			current = s.GetSessionId()
		}
	}
	// Sign-up started a session too, from a client with no browser name.
	if len(got) != 3 || !devices["Firefox on macOS"] || devices["Safari on iPhone"] || devices[""] {
		t.Fatalf("sessions = %v, want the sign-up's, the laptop (current) and the phone", got)
	}

	tests := []struct {
		name, access, sid string
		want              connect.Code
	}{
		{name: "the device in use", access: laptop, sid: current, want: connect.CodeFailedPrecondition},
		{name: "another account's device", access: other, sid: phone, want: connect.CodeNotFound},
		{name: "not an id", access: laptop, sid: "phone", want: connect.CodeInvalidArgument},
		{name: "signed out", sid: phone, want: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, revoke(tc.access, tc.sid), tc.want)
		})
	}

	if err := revoke(laptop, phone); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	_, _, err := e.refresh(phoneCookie, "")
	wantCode(t, err, connect.CodeUnauthenticated)
	if got := list(laptop); len(got) != 2 {
		t.Fatalf("sessions after signing the phone out = %v, want the laptop and the sign-up's", got)
	}
	wantCode(t, revoke(laptop, phone), connect.CodeNotFound)
}
