// Package user owns the account as other users see it and as its owner
// manages it: the public profile, the username and its change window, the
// avatar on local disk, blocks, and account deletion.
//
// Accounts are pseudonymous. Another user is only ever shown by id and
// chosen username; the email appears only in the owner's own Account.
package user

import (
	"time"

	"perspectiv/internal/api/apiutil"
)

// Username rule: 3 to 20 characters, each an ASCII letter, digit or
// underscore. Uniqueness is case-insensitive and enforced by the
// users_username_lower_key index.
const (
	MinUsernameLen = 3
	MaxUsernameLen = 20
)

// UsernameChangeWindowDays is how long a username change locks out the next
// one, as a fixed 24-hour-day duration. The username chosen at signup is not
// a change: the first change may happen at once.
const UsernameChangeWindowDays = 30

const usernameChangeWindow = UsernameChangeWindowDays * 24 * time.Hour

// DeletedUsername is how a deleted account's username reads everywhere: in
// its profile, in blocked lists and in any reading view that names an author.
// It fails the username rule, so no live account can hold it.
//
// A suspended account is not renamed: its username reads unchanged and its
// profile carries the suspended flag. Suspension restricts what the account
// may do, and hiding content is a separate moderation action.
const DeletedUsername = "[deleted]"

// deletedUsernamePrefix starts the username stored on a deleted account's
// row. "~" fails the username rule, so the stored value can never collide
// with a chosen username, and the account id suffix keeps the unique index
// satisfied while freeing the old name.
const deletedUsernamePrefix = "~deleted~"

// ValidateUsername checks the username rule. A failure is apiutil.InvalidArg.
func ValidateUsername(name string) error {
	if len(name) < MinUsernameLen || len(name) > MaxUsernameLen {
		return apiutil.InvalidArg("username must be 3 to 20 characters")
	}
	for i := range len(name) {
		c := name[i]
		ok := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			return apiutil.InvalidArg("username may contain only letters, digits and underscore")
		}
	}
	return nil
}

// PublicUsername is the username a reader sees for an account whose stored
// username is stored: the placeholder when the account is deleted, else the
// stored name.
func PublicUsername(stored string, deleted bool) string {
	if deleted {
		return DeletedUsername
	}
	return stored
}
