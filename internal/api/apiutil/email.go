package apiutil

import "strings"

// MaskEmail keeps the first two characters of the local part and the domain:
// "pat@example.test" is "pa•••@example.test". It is how an admin view shows
// an address that is not the caller's own.
func MaskEmail(addr string) string {
	local, domain, ok := strings.Cut(addr, "@")
	if !ok {
		return "•••"
	}
	keep := []rune(local)
	if len(keep) > 2 {
		keep = keep[:2]
	}
	return string(keep) + "•••@" + domain
}
