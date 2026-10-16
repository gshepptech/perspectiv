package user

import (
	"fmt"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// roleFromDB converts users.role, which holds the full proto value name.
func roleFromDB(s string) (perspectivv1.Role, error) {
	v, ok := perspectivv1.Role_value[s]
	if !ok || perspectivv1.Role(v) == perspectivv1.Role_ROLE_UNSPECIFIED {
		return perspectivv1.Role_ROLE_UNSPECIFIED, fmt.Errorf("users.role %q is not a Role", s)
	}
	return perspectivv1.Role(v), nil
}
