package jobs

import (
	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// The database stores enum columns as the full proto value name; these are
// the only places this package turns a proto enum into its column text.

func loreStatusText(s perspectivv1.LoreStatus) string { return s.String() }

func claimStatusText(s perspectivv1.ClaimStatus) string { return s.String() }

// validNotificationKind reports whether k is a defined, non-zero kind.
func validNotificationKind(k perspectivv1.NotificationKind) bool {
	if k == perspectivv1.NotificationKind_NOTIFICATION_KIND_UNSPECIFIED {
		return false
	}
	_, ok := perspectivv1.NotificationKind_name[int32(k)]
	return ok
}
