package auth

import (
	_ "embed"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"perspectiv/internal/api/apiutil"
)

// Password strength. A password is weak, and refused at sign up and at reset,
// when it:
//
//   - has fewer than MinPasswordRunes characters;
//   - is longer than MaxPasswordBytes bytes, the most bcrypt reads (a longer
//     password would be silently truncated, so it is refused instead);
//   - uses fewer than minDistinctRunes distinct characters ("aaaaaaaaaa",
//     "abcabcabca");
//   - equals, ignoring case, the account's username, its email, or the
//     email's local part.
//
// There is no character-class rule: a long passphrase of lower-case words is
// stronger than a short mixed-class password.
const (
	MinPasswordRunes = 10
	MaxPasswordBytes = 72
	minDistinctRunes = 5
)

// DefaultBcryptCost is the bcrypt work factor for new password hashes.
const DefaultBcryptCost = 12

// maxEmailBytes is the longest address SMTP can carry (RFC 5321 path limit).
const maxEmailBytes = 254

// ValidatePassword checks password against the strength rule for an account
// with email and username. A failure is apiutil.InvalidArg.
func ValidatePassword(password, email, username string) error {
	if utf8.RuneCountInString(password) < MinPasswordRunes {
		return apiutil.InvalidArg(fmt.Sprintf("password must be at least %d characters", MinPasswordRunes))
	}
	if len(password) > MaxPasswordBytes {
		return apiutil.InvalidArg(fmt.Sprintf("password must be at most %d bytes", MaxPasswordBytes))
	}
	distinct := map[rune]struct{}{}
	for _, r := range password {
		distinct[r] = struct{}{}
	}
	if len(distinct) < minDistinctRunes {
		return apiutil.InvalidArg("password repeats too few characters; use a longer phrase")
	}
	if commonPasswords()[strings.ToLower(password)] {
		return apiutil.InvalidArg("that password is one of the most common ones; choose a phrase of your own")
	}
	local, _, _ := strings.Cut(email, "@")
	for _, personal := range []string{username, email, local} {
		if personal != "" && strings.EqualFold(password, personal) {
			return apiutil.InvalidArg("password must not be your username or email")
		}
	}
	return nil
}

// validateEmail trims s and checks it is a bare address (no display name)
// with a dotted domain, short enough to deliver to. It returns the trimmed
// address; a failure is apiutil.InvalidArg.
func validateEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	bad := apiutil.InvalidArg("enter a valid email address")
	if s == "" || len(s) > maxEmailBytes {
		return "", bad
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || addr.Address != s {
		return "", bad
	}
	_, domain, _ := strings.Cut(s, "@")
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", bad
	}
	return s, nil
}

// Passwords hashes and verifies passwords with bcrypt. It satisfies the
// user domain's PasswordVerifier, so password confirmation elsewhere checks
// the same hash format sign up writes.
type Passwords struct {
	cost int
}

// NewPasswords returns a hasher at cost; 0 selects DefaultBcryptCost and a
// cost below bcrypt's minimum is raised to it.
func NewPasswords(cost int) Passwords {
	switch {
	case cost == 0:
		cost = DefaultBcryptCost
	case cost < bcrypt.MinCost:
		cost = bcrypt.MinCost
	}
	return Passwords{cost: cost}
}

// Hash returns the bcrypt hash of password, which must already have passed
// ValidatePassword.
func (p Passwords) Hash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), p.cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

// VerifyPassword reports whether password matches hash. ok is false for a
// wrong password; err is for a hash that cannot be checked. A password longer
// than bcrypt reads can never have been set, so it never matches.
func (p Passwords) VerifyPassword(hash, password string) (bool, error) {
	if len(password) > MaxPasswordBytes {
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return false, nil
	}
	return false, fmt.Errorf("verify password: %w", err)
}

//go:embed common_passwords.txt
var commonPasswordList string

// commonPasswords is the bundled list of common passwords as a set, built
// once on first use. It is read-only.
var commonPasswords = sync.OnceValue(func() map[string]bool {
	set := map[string]bool{}
	for _, line := range strings.Split(commonPasswordList, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			set[line] = true
		}
	}
	return set
})
