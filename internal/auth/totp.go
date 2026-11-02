package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: RFC 6238 TOTP is HMAC-SHA-1, the algorithm authenticator apps expect; HMAC-SHA-1 is not broken.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TOTP parameters (RFC 6238): HMAC-SHA-1, six digits, 30-second steps. They
// are what every common authenticator app assumes, and they are written into
// the otpauth URI so an app that reads them agrees.
const (
	totpDigits = 6
	totpPeriod = 30 // seconds
	// totpSecretBytes is the shared secret's length: 160 bits, the length
	// RFC 4226 recommends for HMAC-SHA-1.
	totpSecretBytes = 20
	// totpDrift is how many steps either side of now a code may be for, so a
	// phone clock up to about 30 seconds off still signs in.
	totpDrift  = 1
	totpIssuer = "Perspectiv"
)

// hotp is RFC 4226 HOTP: HMAC-SHA-1 of the big-endian counter, dynamically
// truncated to digits decimal digits.
func hotp(secret []byte, counter uint64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

// totpStep is the RFC 6238 time step t falls in (T0 = 0).
func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// totpCode is the code for the step t falls in.
func totpCode(secret []byte, t time.Time, digits int) string {
	return hotp(secret, uint64(totpStep(t)), digits) //nolint:gosec // G115: a step after 1970 is never negative
}

// totpResult is what checking a code found.
type totpResult int

const (
	totpWrong    totpResult = iota // matches no step in the window
	totpAccepted                   // matches a step newer than the last one used
	totpReplayed                   // matches only a step already used, or older
)

// matchTOTP checks code against the steps within totpDrift of now. A code
// is accepted only for a step after lastUsed (nil when none has been used),
// so a code that signed in once cannot sign in again (RFC 6238 section
// 5.2). It returns the step accepted. Every step in the window is compared,
// in constant time, whatever an earlier one matched.
func matchTOTP(secret []byte, code string, now time.Time, lastUsed *int64) (int64, totpResult) {
	cur := totpStep(now)
	var best int64
	result := totpWrong
	for step := cur - totpDrift; step <= cur+totpDrift; step++ {
		want := hotp(secret, uint64(step), totpDigits) //nolint:gosec // G115: steps after 1970 are positive
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) != 1 {
			continue
		}
		if lastUsed != nil && step <= *lastUsed {
			if result == totpWrong {
				result = totpReplayed
			}
			continue
		}
		best, result = step, totpAccepted
	}
	return best, result
}

// isTOTPCode reports whether s, spaces removed, is six digits: what an app
// shows. Anything else is read as a recovery code.
func isTOTPCode(s string) (string, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if len(s) != totpDigits {
		return "", false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return s, true
}

var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a fresh random shared secret.
func newTOTPSecret() ([]byte, error) {
	b := make([]byte, totpSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate totp secret: %w", err)
	}
	return b, nil
}

// otpauthURI is the Key URI an authenticator app scans: the issuer and the
// member's username as its label, the secret in base32 and the parameters
// spelled out.
func otpauthURI(secret []byte, account string) string {
	q := url.Values{}
	q.Set("secret", secretEncoding.EncodeToString(secret))
	q.Set("issuer", totpIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	label := url.PathEscape(totpIssuer) + ":" + url.PathEscape(account)
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// Recovery codes: RecoveryCodeCount per account, each 16 characters of
// Crockford's base32 (80 random bits) shown as four groups of four. Only a
// SHA-256 of the account's id and the normalised code is stored
// (hashRecoveryCode); at 80 bits a stolen hash cannot be searched back to its
// code, and the id makes every account's hashes its own, so one table of
// precomputed hashes cannot be tried against every account at once.
const (
	RecoveryCodeCount    = 10
	recoveryCodeChars    = 16
	recoveryCodeGroup    = 4
	recoveryCodeAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"
)

// newRecoveryCodes returns RecoveryCodeCount fresh codes, formatted.
func newRecoveryCodes() ([]string, error) {
	out := make([]string, RecoveryCodeCount)
	buf := make([]byte, recoveryCodeChars)
	for i := range out {
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("generate recovery code: %w", err)
		}
		var b strings.Builder
		for j, v := range buf {
			if j > 0 && j%recoveryCodeGroup == 0 {
				b.WriteByte('-')
			}
			// 256 is a multiple of 32, so the low five bits are uniform.
			b.WriteByte(recoveryCodeAlphabet[v&0x1f])
		}
		out[i] = b.String()
	}
	return out, nil
}

// normalizeRecoveryCode reads a code as a person types it back: any case,
// with or without dashes and spaces, and with the letters Crockford's base32
// reads as digits (i and l as 1, o as 0). ok is false for anything that
// cannot be a code.
func normalizeRecoveryCode(s string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case '-', ' ', '\t':
			continue
		case 'i', 'l':
			r = '1'
		case 'o':
			r = '0'
		}
		if !strings.ContainsRune(recoveryCodeAlphabet, r) {
			return "", false
		}
		b.WriteRune(r)
	}
	if b.Len() != recoveryCodeChars {
		return "", false
	}
	return b.String(), true
}

// hashRecoveryCode is the stored form of a normalised recovery code for the
// account userID: hash version 2 in recovery_codes.hash_version.
//
// The salt is the account id rather than an HMAC key derived from
// ENCRYPTION_KEYS: those keys rotate, and a hash keyed by one would stop
// every stored code working when it is retired. At 80 random bits a code
// needs no secret key to resist search; the per-account salt is what stops
// one search covering every account.
func hashRecoveryCode(userID uuid.UUID, normalized string) []byte {
	h := sha256.New()
	h.Write([]byte("perspectiv.recovery.v2\x00"))
	h.Write(userID[:])
	h.Write([]byte(normalized))
	return h.Sum(nil)
}

// legacyRecoveryCodeHash is hash version 1, unsalted: how codes were stored
// before migration 41. It is only compared, never written; a version 1 code
// keeps working until it is used or its set is replaced.
func legacyRecoveryCodeHash(normalized string) []byte {
	sum := sha256.Sum256([]byte("perspectiv.recovery:" + normalized))
	return sum[:]
}
