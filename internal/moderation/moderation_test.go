package moderation

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
)

func wantCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	if got := connect.CodeOf(apiutil.ToConnect(err)); err == nil || got != code {
		t.Fatalf("err = %v (code %v), want %v", err, got, code)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		at   time.Time
	}{
		{"now", time.Now()},
		{"epoch", time.Unix(0, 0)},
		{"micro precision", time.Date(2026, 10, 16, 13, 49, 13, 123456000, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id := uuid.New()
			at, gotID, err := decodeCursor(encodeCursor(tc.at, id))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if gotID != id || !at.Equal(tc.at.Truncate(time.Microsecond)) {
				t.Fatalf("got %v %v, want %v %v", at, gotID, tc.at, id)
			}
		})
	}
}

func TestDecodeCursorRejects(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "123", "abc_" + uuid.NewString(), "123_not-a-uuid"} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			_, _, err := decodeCursor(s)
			wantCode(t, err, connect.CodeInvalidArgument)
		})
	}
}

func TestReportInputValidate(t *testing.T) {
	t.Parallel()
	ok := ReportInput{
		TargetKind: perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE,
		TargetID:   uuid.New(),
		Reason:     perspectivv1.ReportReason_REPORT_REASON_OTHER,
	}
	cases := []struct {
		name  string
		edit  func(*ReportInput)
		valid bool
	}{
		{"valid", func(*ReportInput) {}, true},
		{"details at limit", func(in *ReportInput) { in.Details = strings.Repeat("a", MaxDetailsRunes) }, true},
		{"details over limit after trim", func(in *ReportInput) { in.Details = " " + strings.Repeat("a", MaxDetailsRunes+1) }, false},
		{"no kind", func(in *ReportInput) { in.TargetKind = 0 }, false},
		{"no target", func(in *ReportInput) { in.TargetID = uuid.Nil }, false},
		{"no reason", func(in *ReportInput) { in.Reason = 0 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := ok
			tc.edit(&in)
			err := in.validate()
			if tc.valid && err != nil {
				t.Fatalf("validate: %v", err)
			}
			if !tc.valid {
				wantCode(t, err, connect.CodeInvalidArgument)
			}
		})
	}
}

func TestResolveInputValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		in     ResolveInput
		valid  bool
		reason string
	}{
		{"valid, reason trimmed", ResolveInput{uuid.New(), perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS, " why "}, true, "why"},
		{"no report", ResolveInput{uuid.Nil, perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS, "why"}, false, ""},
		{"no action", ResolveInput{uuid.New(), 0, "why"}, false, ""},
		{"blank reason", ResolveInput{uuid.New(), perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS, " \n "}, false, ""},
		{"long reason", ResolveInput{uuid.New(), perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS, strings.Repeat("x", MaxReasonRunes+1)}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := tc.in
			err := in.validate()
			if !tc.valid {
				wantCode(t, err, connect.CodeInvalidArgument)
				return
			}
			if err != nil || in.Reason != tc.reason {
				t.Fatalf("validate: %v, reason %q", err, in.Reason)
			}
		})
	}
}

// checkEnum asserts that every non-zero value of an enum parses back from its
// stored name, that the zero value is refused, and that valid agrees.
func checkEnum[T ~int32](t *testing.T, names map[int32]string, parse func(string) (T, error), valid func(T) bool) {
	t.Helper()
	for v, name := range names {
		got, err := parse(name)
		if v == 0 {
			if err == nil || valid(T(v)) {
				t.Fatalf("%s accepted", name)
			}
			continue
		}
		if err != nil || got != T(v) || !valid(T(v)) {
			t.Fatalf("%s: got %v err %v", name, got, err)
		}
	}
	if _, err := parse("NOT_A_VALUE"); err == nil {
		t.Fatal("unknown name accepted")
	}
	if valid(T(99)) {
		t.Fatal("out-of-range value accepted")
	}
}

// Every enum the service stores round-trips through its stored name.
func TestEnumsRoundTrip(t *testing.T) {
	t.Parallel()
	t.Run("target kind", func(t *testing.T) {
		t.Parallel()
		checkEnum(t, perspectivv1.ReportTargetKind_name, targetKindFromDB, validTargetKind)
	})
	t.Run("reason", func(t *testing.T) {
		t.Parallel()
		checkEnum(t, perspectivv1.ReportReason_name, reasonFromDB, validReason)
	})
	t.Run("status", func(t *testing.T) {
		t.Parallel()
		// UNSPECIFIED is a valid filter (every status) but never a stored value.
		checkEnum(t, perspectivv1.ReportStatus_name, statusFromDB, func(s perspectivv1.ReportStatus) bool {
			return s != 0 && validStatusFilter(s)
		})
		if !validStatusFilter(perspectivv1.ReportStatus_REPORT_STATUS_UNSPECIFIED) {
			t.Fatal("unspecified status filter refused")
		}
	})
	t.Run("action", func(t *testing.T) {
		t.Parallel()
		checkEnum(t, perspectivv1.ModerationAction_name, actionFromDB, func(a perspectivv1.ModerationAction) bool {
			_, _, ok := actionTargets(a)
			return ok
		})
	})
	t.Run("role", func(t *testing.T) {
		t.Parallel()
		checkEnum(t, perspectivv1.Role_name, roleFromDB, func(r perspectivv1.Role) bool {
			return r == perspectivv1.Role_ROLE_USER || r == perspectivv1.Role_ROLE_ADMIN
		})
	})
}

// Each action acts on exactly one target kind; DISMISS fits every report.
func TestActionTargets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		action  perspectivv1.ModerationAction
		kind    perspectivv1.ReportTargetKind
		dismiss bool
	}{
		{perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PERSPECTIVE, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, false},
		{perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PLACE, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE, false},
		{perspectivv1.ModerationAction_MODERATION_ACTION_SUSPEND_USER, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, false},
		{perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_UNSPECIFIED, true},
	}
	for _, tc := range cases {
		t.Run(tc.action.String(), func(t *testing.T) {
			t.Parallel()
			kind, dismiss, ok := actionTargets(tc.action)
			if !ok || kind != tc.kind || dismiss != tc.dismiss {
				t.Fatalf("got %v %v %v", kind, dismiss, ok)
			}
		})
	}
}
