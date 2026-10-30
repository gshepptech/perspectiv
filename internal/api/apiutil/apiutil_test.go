package apiutil_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
)

func checkConnectError(t *testing.T, got error, wantCode connect.Code, wantMsg string) {
	t.Helper()
	var cerr *connect.Error
	if !errors.As(got, &cerr) {
		t.Fatalf("ToConnect returned %T, want *connect.Error", got)
	}
	if cerr.Code() != wantCode {
		t.Fatalf("code = %v, want %v", cerr.Code(), wantCode)
	}
	if wantMsg != "" && cerr.Message() != wantMsg {
		t.Fatalf("message = %q, want %q", cerr.Message(), wantMsg)
	}
	if strings.Contains(cerr.Message(), "users_secret_column") {
		t.Fatalf("internal detail leaked: %q", cerr.Message())
	}
}

func TestToConnect(t *testing.T) {
	t.Parallel()
	secret := errors.New("pq: relation users_secret_column leaked")
	existing := connect.NewError(connect.CodeUnavailable, errors.New("down"))

	tests := []struct {
		name     string
		err      error
		wantNil  bool
		wantCode connect.Code
		wantMsg  string
	}{
		{name: "nil", err: nil, wantNil: true},
		{name: "invalid argument", err: apiutil.InvalidArg("part limit exceeded"), wantCode: connect.CodeInvalidArgument, wantMsg: "part limit exceeded"},
		{name: "not found", err: apiutil.NotFound("place not found"), wantCode: connect.CodeNotFound, wantMsg: "place not found"},
		{name: "already exists", err: apiutil.AlreadyExists("you already have a perspective in this arc"), wantCode: connect.CodeAlreadyExists, wantMsg: "you already have a perspective in this arc"},
		{name: "permission denied", err: apiutil.PermissionDenied("admin only"), wantCode: connect.CodePermissionDenied, wantMsg: "admin only"},
		{name: "failed precondition", err: apiutil.FailedPrecondition("verify your email first"), wantCode: connect.CodeFailedPrecondition, wantMsg: "verify your email first"},
		{name: "unauthenticated", err: apiutil.Unauthenticated("sign in required"), wantCode: connect.CodeUnauthenticated, wantMsg: "sign in required"},
		{name: "resource exhausted", err: apiutil.ResourceExhausted("slow down"), wantCode: connect.CodeResourceExhausted, wantMsg: "slow down"},
		{name: "wrapped typed error keeps its code", err: fmt.Errorf("publish: %w", apiutil.NotFound("arc not found")), wantCode: connect.CodeNotFound, wantMsg: "arc not found"},
		{name: "internal hides its cause", err: apiutil.Internal(secret), wantCode: connect.CodeInternal, wantMsg: "internal error"},
		{name: "raw error hides its text", err: secret, wantCode: connect.CodeInternal, wantMsg: "internal error"},
		{name: "connect error passes through", err: existing, wantCode: connect.CodeUnavailable, wantMsg: "down"},
		{name: "canceled", err: fmt.Errorf("query: %w", context.Canceled), wantCode: connect.CodeCanceled},
		{name: "deadline", err: context.DeadlineExceeded, wantCode: connect.CodeDeadlineExceeded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := apiutil.ToConnect(tc.err)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("ToConnect(nil) = %v", got)
				}
				return
			}
			checkConnectError(t, got, tc.wantCode, tc.wantMsg)
		})
	}
}

func TestInternalKeepsCauseForLogs(t *testing.T) {
	t.Parallel()
	cause := errors.New("disk full")
	err := apiutil.Internal(cause)
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is cannot reach the wrapped cause")
	}
	if code, typed := apiutil.CodeOf(err); !typed || code != connect.CodeInternal {
		t.Fatalf("CodeOf = %v, %v", code, typed)
	}
	if _, typed := apiutil.CodeOf(cause); typed {
		t.Fatal("a raw error reports a typed code")
	}
}

func TestCaller(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		ctx    context.Context
		want   apiutil.Caller
		wantOK bool
	}{
		{name: "anonymous", ctx: context.Background()},
		{
			name:   "signed in",
			ctx:    apiutil.WithCaller(context.Background(), apiutil.Caller{UserID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Role: perspectivv1.Role_ROLE_ADMIN, SessionID: uuid.MustParse("22222222-2222-2222-2222-222222222222")}),
			want:   apiutil.Caller{UserID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Role: perspectivv1.Role_ROLE_ADMIN, SessionID: uuid.MustParse("22222222-2222-2222-2222-222222222222")},
			wantOK: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := apiutil.CallerFrom(tc.ctx)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("CallerFrom = %+v, %v; want %+v, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestParsePageToken(t *testing.T) {
	t.Parallel()
	next := apiutil.NextPageToken("2026-10-16T12:00:00Z|0b8e")

	tests := []struct {
		name      string
		token     string
		limit     int32
		want      apiutil.PageToken
		wantError bool
	}{
		{name: "first page default limit", token: "", limit: 0, want: apiutil.PageToken{Limit: 20}},
		{name: "negative limit means default", token: "", limit: -5, want: apiutil.PageToken{Limit: 20}},
		{name: "explicit limit", token: "", limit: 50, want: apiutil.PageToken{Limit: 50}},
		{name: "limit clamped to max", token: "", limit: 1000, want: apiutil.PageToken{Limit: 100}},
		{name: "limit at max", token: "", limit: 100, want: apiutil.PageToken{Limit: 100}},
		{name: "round trip", token: next, limit: 10, want: apiutil.PageToken{After: "2026-10-16T12:00:00Z|0b8e", Limit: 10}},
		{name: "not base64", token: "!!!", limit: 10, wantError: true},
		{name: "base64 but not minted here", token: "aGVsbG8", limit: 10, wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := apiutil.ParsePageToken(tc.token, tc.limit)
			if tc.wantError {
				if code, _ := apiutil.CodeOf(err); err == nil || code != connect.CodeInvalidArgument {
					t.Fatalf("err = %v, want InvalidArg", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParsePageToken = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNextPageTokenEmpty(t *testing.T) {
	t.Parallel()
	if got := apiutil.NextPageToken(""); got != "" {
		t.Fatalf("NextPageToken(\"\") = %q, want \"\"", got)
	}
}

func TestMaskEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{name: "long local part", in: "pat@example.test", want: "pa•••@example.test"},
		{name: "short local part", in: "p@example.test", want: "p•••@example.test"},
		{name: "multibyte", in: "zoë@example.test", want: "zo•••@example.test"},
		{name: "no at sign", in: "not-an-address", want: "•••"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := apiutil.MaskEmail(tc.in); got != tc.want {
				t.Fatalf("MaskEmail(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
