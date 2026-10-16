package moderation

import (
	"fmt"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// The reports and moderation_audit enum columns hold the full proto value
// name. This file is the one place those names cross the database boundary.

// enumFromDB parses a stored proto value name. The zero value is never stored,
// so it is refused like an unknown name.
func enumFromDB[T ~int32](column, s string, values map[string]int32) (T, error) {
	v, ok := values[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("%s %q is not a known value", column, s)
	}
	return T(v), nil
}

func targetKindFromDB(s string) (perspectivv1.ReportTargetKind, error) {
	return enumFromDB[perspectivv1.ReportTargetKind]("target_kind", s, perspectivv1.ReportTargetKind_value)
}

func reasonFromDB(s string) (perspectivv1.ReportReason, error) {
	return enumFromDB[perspectivv1.ReportReason]("reports.reason", s, perspectivv1.ReportReason_value)
}

func statusFromDB(s string) (perspectivv1.ReportStatus, error) {
	return enumFromDB[perspectivv1.ReportStatus]("reports.status", s, perspectivv1.ReportStatus_value)
}

func actionFromDB(s string) (perspectivv1.ModerationAction, error) {
	return enumFromDB[perspectivv1.ModerationAction]("reports.resolution", s, perspectivv1.ModerationAction_value)
}

func roleFromDB(s string) (perspectivv1.Role, error) {
	return enumFromDB[perspectivv1.Role]("users.role", s, perspectivv1.Role_value)
}

// validTargetKind reports whether k is a value a request may carry.
func validTargetKind(k perspectivv1.ReportTargetKind) bool {
	switch k {
	case perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE,
		perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE,
		perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER:
		return true
	case perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_UNSPECIFIED:
		return false
	}
	return false
}

func validReason(r perspectivv1.ReportReason) bool {
	switch r {
	case perspectivv1.ReportReason_REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER,
		perspectivv1.ReportReason_REPORT_REASON_HARASSMENT,
		perspectivv1.ReportReason_REPORT_REASON_HATEFUL_CONTENT,
		perspectivv1.ReportReason_REPORT_REASON_SEXUAL_CONTENT,
		perspectivv1.ReportReason_REPORT_REASON_SPAM,
		perspectivv1.ReportReason_REPORT_REASON_OTHER:
		return true
	case perspectivv1.ReportReason_REPORT_REASON_UNSPECIFIED:
		return false
	}
	return false
}

func validStatusFilter(s perspectivv1.ReportStatus) bool {
	switch s {
	case perspectivv1.ReportStatus_REPORT_STATUS_UNSPECIFIED,
		perspectivv1.ReportStatus_REPORT_STATUS_OPEN,
		perspectivv1.ReportStatus_REPORT_STATUS_RESOLVED:
		return true
	}
	return false
}

// actionTargets returns the target kind an action acts on. DISMISS acts on
// none and fits every report; ok is false for a value a request may not carry.
func actionTargets(a perspectivv1.ModerationAction) (kind perspectivv1.ReportTargetKind, dismiss, ok bool) {
	switch a {
	case perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PERSPECTIVE:
		return perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, false, true
	case perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PLACE:
		return perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE, false, true
	case perspectivv1.ModerationAction_MODERATION_ACTION_SUSPEND_USER:
		return perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, false, true
	case perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS:
		return perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_UNSPECIFIED, true, true
	case perspectivv1.ModerationAction_MODERATION_ACTION_UNSPECIFIED:
		return perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_UNSPECIFIED, false, false
	}
	return perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_UNSPECIFIED, false, false
}
