package notify

import (
	"fmt"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// The database stores a notification kind as the full proto value name; these
// are the only places this package converts between the two.

// allKinds is every defined kind in enum order, the order GetPreferences
// returns them in.
var allKinds = []perspectivv1.NotificationKind{
	perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED,
	perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED,
	perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC,
	perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED,
	perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE,
	perspectivv1.NotificationKind_NOTIFICATION_KIND_REPORT_RESOLVED,
}

// validKind reports whether k is a defined, non-zero kind.
func validKind(k perspectivv1.NotificationKind) bool {
	switch k {
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_REPORT_RESOLVED:
		return true
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_UNSPECIFIED:
		return false
	default:
		return false
	}
}

func kindText(k perspectivv1.NotificationKind) string { return k.String() }

func kindFromText(s string) (perspectivv1.NotificationKind, error) {
	k := perspectivv1.NotificationKind(perspectivv1.NotificationKind_value[s])
	if !validKind(k) {
		return 0, fmt.Errorf("unknown notification kind %q", s)
	}
	return k, nil
}
