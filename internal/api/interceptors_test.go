package api_test

import (
	"errors"
	"slices"
	"testing"

	"connectrpc.com/connect"

	"perspectiv/internal/api"
)

const (
	getPlace    = "/perspectiv.v1.PlaceService/GetPlace"
	updateProf  = "/perspectiv.v1.UserService/UpdateProfile"
	signUp      = "/perspectiv.v1.AuthService/SignUp"
	publish     = "/perspectiv.v1.PerspectiveService/Publish"
	listReports = "/perspectiv.v1.ModerationService/ListReports"
	resolve     = "/perspectiv.v1.ModerationService/Resolve"
	report      = "/perspectiv.v1.ModerationService/Report"
	notInRoutes = "/perspectiv.v1.TestService/NotInRoutes"
)

func codeOf(err error) connect.Code {
	if err == nil {
		return 0
	}
	return connect.CodeOf(err)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func connectMessage(err error) string {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr.Message()
	}
	return ""
}

// checkDenial asserts a denied call wrote exactly one authz_denied line
// carrying the audit fields and want, and an allowed call wrote none.
func checkDenial(t *testing.T, denials []map[string]any, denied bool, want map[string]string) {
	t.Helper()
	if !denied {
		if len(denials) != 0 {
			t.Fatalf("unexpected denial lines: %v", denials)
		}
		return
	}
	if len(denials) != 1 {
		t.Fatalf("got %d authz_denied lines, want 1: %v", len(denials), denials)
	}
	for _, k := range []string{"sub", "method", "required", "role"} {
		if _, ok := denials[0][k]; !ok {
			t.Fatalf("denial line lacks %q: %v", k, denials[0])
		}
	}
	for k, v := range want {
		if denials[0][k] != v {
			t.Fatalf("denial %s = %v, want %q (line %v)", k, denials[0][k], v, denials[0])
		}
	}
}

func TestInterceptorPolicy(t *testing.T) {
	t.Parallel()
	gateDown := errors.New("db down")

	tests := []struct {
		name        string
		procedure   string
		token       string
		headers     map[string]string
		gate        api.Gate
		lim         *fakeLimiter
		wantCode    connect.Code // 0 = success
		wantMsg     string
		wantDenial  map[string]string // fields the one authz_denied line of a denied call must carry
		wantLimiter []string          // Allow calls, in order; nil = not checked
	}{
		{name: "public procedure anonymous", procedure: getPlace},
		{name: "public procedure signed in", procedure: getPlace, token: "user"},
		{
			name: "user procedure anonymous", procedure: updateProf, wantCode: connect.CodeUnauthenticated,
			wantDenial: map[string]string{"sub": "", "method": updateProf, "required": "user", "role": "anonymous"},
		},
		{name: "user procedure signed in", procedure: updateProf, token: "user"},
		{
			name: "non-admin calling Resolve is denied and audited", procedure: resolve, token: "user",
			wantCode:   connect.CodePermissionDenied,
			wantDenial: map[string]string{"sub": userID.String(), "method": resolve, "required": "admin", "role": "ROLE_USER"},
		},
		{
			name: "non-admin opening the report queue is denied and audited", procedure: listReports, token: "user",
			wantCode:   connect.CodePermissionDenied,
			wantDenial: map[string]string{"sub": userID.String(), "method": listReports, "required": "admin", "role": "ROLE_USER"},
		},
		{
			name: "anonymous calling Resolve", procedure: resolve, wantCode: connect.CodeUnauthenticated,
			wantDenial: map[string]string{"method": resolve, "required": "admin", "role": "anonymous"},
		},
		{name: "admin calling Resolve", procedure: resolve, token: "admin"},
		{name: "admin opening the report queue", procedure: listReports, token: "admin"},
		{
			name: "unverified email cannot publish", procedure: publish, token: "unverified",
			wantCode: connect.CodeFailedPrecondition, wantMsg: "verify your email first",
			wantDenial: map[string]string{"sub": unverID.String(), "required": "verified_email", "reason": "email_unverified"},
		},
		{
			name: "unverified email cannot report", procedure: report, token: "unverified",
			wantCode: connect.CodeFailedPrecondition, wantMsg: "verify your email first",
		},
		{
			name: "gate error fails closed", procedure: publish, token: "user", gate: fakeGate{err: gateDown},
			wantCode: connect.CodeUnavailable, wantDenial: map[string]string{"reason": "gate_error"},
		},
		{
			name: "signed-in rate limit counts user and ip", procedure: publish, token: "user",
			wantLimiter: []string{"publish user:" + userID.String(), "publish ip:127.0.0.1"},
		},
		{
			name: "anonymous rate limit counts ip only", procedure: signUp,
			wantLimiter: []string{"signup ip:127.0.0.1"},
		},
		{
			name: "forwarded client ip behind a local proxy", procedure: signUp,
			headers:     map[string]string{"X-Forwarded-For": "6.6.6.6, 203.0.113.9"},
			wantLimiter: []string{"signup ip:203.0.113.9"},
		},
		{
			name: "user over the limit", procedure: publish, token: "user",
			lim:      &fakeLimiter{deny: map[string]bool{"user:" + userID.String(): true}},
			wantCode: connect.CodeResourceExhausted, wantMsg: "slow down",
			wantDenial: map[string]string{"reason": "rate_limited", "required": "rate_limit:publish"},
		},
		{
			name: "ip over the limit", procedure: signUp,
			lim:      &fakeLimiter{deny: map[string]bool{"ip:127.0.0.1": true}},
			wantCode: connect.CodeResourceExhausted, wantMsg: "slow down",
		},
		{
			name: "limiter error fails closed", procedure: signUp, lim: &fakeLimiter{err: errors.New("db down")},
			wantCode: connect.CodeUnavailable, wantDenial: map[string]string{"reason": "limiter_error"},
		},
		{
			name: "unverified user is not rate counted", procedure: publish, token: "unverified",
			wantCode: connect.CodeFailedPrecondition, wantLimiter: []string{},
		},
		{
			name: "invalid credentials are unauthenticated", procedure: getPlace, token: "expired",
			wantCode: connect.CodeUnauthenticated, wantDenial: map[string]string{"reason": "invalid_credentials"},
		},
		{
			name: "authenticator error fails closed", procedure: getPlace, token: "broken",
			wantCode: connect.CodeUnavailable, wantDenial: map[string]string{"reason": "authenticator_error"},
		},
		{
			name: "procedure missing from Routes is denied", procedure: notInRoutes, token: "admin",
			wantCode: connect.CodePermissionDenied, wantDenial: map[string]string{"required": "unknown", "reason": "unknown_procedure"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStack(t, stackOpts{gate: tc.gate, lim: tc.lim})
			_, err := s.call(t, tc.procedure, tc.token, tc.headers)

			if got := codeOf(err); got != tc.wantCode {
				t.Fatalf("code = %v (%v), want %v", got, err, tc.wantCode)
			}
			if tc.wantMsg != "" && connectMessage(err) != tc.wantMsg {
				t.Fatalf("message = %v, want %q", err, tc.wantMsg)
			}
			if got, want := s.handlerHits(tc.procedure), boolToInt(tc.wantCode == 0); got != want {
				t.Fatalf("handler ran %d times, want %d", got, want)
			}
			checkDenial(t, s.logs.lines(t, "authz_denied"), tc.wantCode != 0, tc.wantDenial)
			if tc.wantLimiter != nil {
				if got := s.lim.seen(); !slices.Equal(append([]string{}, got...), tc.wantLimiter) {
					t.Fatalf("limiter calls = %v, want %v", got, tc.wantLimiter)
				}
			}
		})
	}
}

func TestHandlerSeesCaller(t *testing.T) {
	t.Parallel()
	s := newStack(t, stackOpts{})
	if _, err := s.call(t, resolve, "admin", nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	s.mu.Lock()
	got := s.seen[resolve]
	s.mu.Unlock()
	if got.UserID != adminID {
		t.Fatalf("handler saw caller %v, want %v", got.UserID, adminID)
	}
}

func TestRecoveryKeepsServing(t *testing.T) {
	t.Parallel()
	s := newStack(t, stackOpts{})
	_, err := s.call(t, getPlace, "", map[string]string{"X-Test-Panic": "1"})
	if codeOf(err) != connect.CodeInternal {
		t.Fatalf("panic code = %v, want internal", err)
	}
	if lines := s.logs.lines(t, "panic"); len(lines) != 1 {
		t.Fatalf("got %d panic lines, want 1", len(lines))
	}
	if _, err := s.call(t, getPlace, "", nil); err != nil {
		t.Fatalf("call after panic: %v", err)
	}
}

func TestRequestIDAndLogging(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		sent     string
		keepSent bool
	}{
		{name: "client id kept", sent: "req-abc-123", keepSent: true},
		{name: "missing id minted"},
		{name: "unprintable id replaced", sent: "bad id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStack(t, stackOpts{})
			headers := map[string]string{}
			if tc.sent != "" {
				headers[api.RequestIDHeader] = tc.sent
			}
			resp, err := s.call(t, getPlace, "", headers)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			id := resp.Header().Get(api.RequestIDHeader)
			if id == "" || (tc.keepSent && id != tc.sent) || (!tc.keepSent && id == tc.sent) {
				t.Fatalf("response request id = %q (sent %q)", id, tc.sent)
			}
			rpc := s.logs.lines(t, "rpc")
			if len(rpc) != 1 || rpc[0]["request_id"] != id || rpc[0]["method"] != getPlace || rpc[0]["code"] != "ok" {
				t.Fatalf("rpc log = %v, want one ok line with request_id %q", rpc, id)
			}
		})
	}
}

// TestDenialIsLoggedAsRPC shows logging wraps authorization: the denied
// call's rpc line carries the denial code and the denial's request id.
func TestDenialIsLoggedAsRPC(t *testing.T) {
	t.Parallel()
	s := newStack(t, stackOpts{})
	_, err := s.call(t, resolve, "user", map[string]string{api.RequestIDHeader: "deny-1"})
	var cerr *connect.Error
	if !errors.As(err, &cerr) || cerr.Meta().Get(api.RequestIDHeader) != "deny-1" {
		t.Fatalf("error %v lacks the request id", err)
	}
	rpc := s.logs.lines(t, "rpc")
	if len(rpc) != 1 || rpc[0]["code"] != "permission_denied" {
		t.Fatalf("rpc log = %v", rpc)
	}
	denials := s.logs.lines(t, "authz_denied")
	if len(denials) != 1 || denials[0]["request_id"] != "deny-1" {
		t.Fatalf("denial log = %v", denials)
	}
}
