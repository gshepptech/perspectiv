package auth

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// rfcSecret is the SHA-1 seed of RFC 6238 Appendix B: the ASCII string
// "12345678901234567890".
var rfcSecret = []byte("12345678901234567890")

// TestTOTPMatchesRFC6238Vectors checks the generator against every SHA-1
// test vector in RFC 6238 Appendix B (eight digits), and checks that the six
// digits an app shows are the vector's last six.
func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		unix int64
		step string // T as RFC 6238 prints it, in hex
		want string
	}{
		{59, "0000000000000001", "94287082"},
		{1111111109, "00000000023523EC", "07081804"},
		{1111111111, "00000000023523ED", "14050471"},
		{1234567890, "000000000273EF07", "89005924"},
		{2000000000, "0000000003F940AA", "69279037"},
		{20000000000, "0000000027BC86AA", "65353130"},
	}
	for _, tc := range tests {
		t.Run(tc.step, func(t *testing.T) {
			t.Parallel()
			at := time.Unix(tc.unix, 0).UTC()
			if got := totpCode(rfcSecret, at, 8); got != tc.want {
				t.Errorf("8-digit code at %d = %s, want %s", tc.unix, got, tc.want)
			}
			if got := totpCode(rfcSecret, at, totpDigits); got != tc.want[2:] {
				t.Errorf("6-digit code at %d = %s, want %s", tc.unix, got, tc.want[2:])
			}
		})
	}
}

// TestMatchTOTP covers the window and replay rules: a code is good for its
// own step and one step either side, and only for a step after the last
// one used.
func TestMatchTOTP(t *testing.T) {
	t.Parallel()
	now := time.Unix(1111111111, 0) // step 0x23523ED
	cur := totpStep(now)
	codeAt := func(step int64) string { return hotp(rfcSecret, uint64(step), totpDigits) } //nolint:gosec // G115: positive steps
	used := func(step int64) *int64 { return &step }
	tests := []struct {
		name     string
		code     string
		lastUsed *int64
		want     totpResult
		wantStep int64
	}{
		{name: "the current step", code: codeAt(cur), want: totpAccepted, wantStep: cur},
		{name: "one step behind (a slow phone clock)", code: codeAt(cur - 1), want: totpAccepted, wantStep: cur - 1},
		{name: "one step ahead (a fast phone clock)", code: codeAt(cur + 1), want: totpAccepted, wantStep: cur + 1},
		{name: "two steps behind is outside the window", code: codeAt(cur - 2), want: totpWrong},
		{name: "two steps ahead is outside the window", code: codeAt(cur + 2), want: totpWrong},
		{name: "a wrong code", code: "000000", want: totpWrong},
		{name: "the step already used is a replay", code: codeAt(cur), lastUsed: used(cur), want: totpReplayed},
		{name: "an older step than the last used is a replay", code: codeAt(cur - 1), lastUsed: used(cur), want: totpReplayed},
		{name: "a newer step than the last used is accepted", code: codeAt(cur + 1), lastUsed: used(cur), want: totpAccepted, wantStep: cur + 1},
		{name: "the current step after an older one was used", code: codeAt(cur), lastUsed: used(cur - 1), want: totpAccepted, wantStep: cur},
		{name: "a code of the wrong length", code: codeAt(cur)[:5], want: totpWrong},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			step, got := matchTOTP(rfcSecret, tc.code, now, tc.lastUsed)
			if got != tc.want {
				t.Fatalf("result = %v, want %v", got, tc.want)
			}
			if got == totpAccepted && step != tc.wantStep {
				t.Fatalf("step = %d, want %d", step, tc.wantStep)
			}
		})
	}
}

func TestIsTOTPCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
		ok       bool
	}{
		{in: "123456", want: "123456", ok: true},
		{in: " 123 456 ", want: "123456", ok: true},
		{in: "12345", ok: false},
		{in: "1234567", ok: false},
		{in: "12345a", ok: false},
		{in: "abcd-efgh-jkmn-pqrs", ok: false},
		{in: "", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, ok := isTOTPCode(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("isTOTPCode(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestOtpauthURI checks the URI carries what an authenticator app reads:
// the issuer and username label, the secret in unpadded base32 and the
// RFC 6238 parameters.
func TestOtpauthURI(t *testing.T) {
	t.Parallel()
	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != totpSecretBytes {
		t.Fatalf("secret is %d bytes, want %d", len(secret), totpSecretBytes)
	}
	u, err := url.Parse(otpauthURI(secret, "wren_writes"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" || u.Path != "/Perspectiv:wren_writes" {
		t.Fatalf("uri = %s", u)
	}
	q := u.Query()
	want := map[string]string{"issuer": "Perspectiv", "algorithm": "SHA1", "digits": "6", "period": "30"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	got, err := secretEncoding.DecodeString(q.Get("secret"))
	if err != nil || string(got) != string(secret) || strings.Contains(q.Get("secret"), "=") {
		t.Errorf("secret %q does not round-trip as unpadded base32: %v", q.Get("secret"), err)
	}
}

// TestRecoveryCodes checks the codes' shape, that they are distinct, and
// that a code typed back in any of the forms a person might use normalises
// to the same stored hash.
func TestRecoveryCodes(t *testing.T) {
	t.Parallel()
	codes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("%d codes, want %d", len(codes), RecoveryCodeCount)
	}
	shape := regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{4}(-[0-9a-hjkmnp-tv-z]{4}){3}$`)
	seen := map[string]bool{}
	for _, c := range codes {
		if !shape.MatchString(c) {
			t.Errorf("code %q is not four groups of four Crockford base32 characters", c)
		}
		if seen[c] {
			t.Errorf("code %q repeated", c)
		}
		seen[c] = true
	}

	norm, ok := normalizeRecoveryCode(codes[0])
	if !ok {
		t.Fatalf("a generated code does not normalise: %q", codes[0])
	}
	owner := uuid.New()
	stored := hashRecoveryCode(owner, norm)
	typed := []string{
		strings.ToUpper(codes[0]),
		strings.ReplaceAll(codes[0], "-", ""),
		strings.ReplaceAll(codes[0], "-", " "),
		" " + codes[0] + " ",
	}
	for _, in := range typed {
		n, ok := normalizeRecoveryCode(in)
		if !ok || string(hashRecoveryCode(owner, n)) != string(stored) {
			t.Errorf("%q does not match its code", in)
		}
	}
	// Crockford's base32 reads i and l as 1 and o as 0.
	if n, ok := normalizeRecoveryCode("1010-0000-0000-0000"); !ok || n != "1010000000000000" {
		t.Fatalf("normalise digits: %q %v", n, ok)
	}
	if n, _ := normalizeRecoveryCode("IoLo-oooo-OOOO-0000"); n != "1010000000000000" {
		t.Errorf("i, l and o are not read as digits: %q", n)
	}
	for _, bad := range []string{"", "123456", "abcd-efgh-jkmn-pqr", "abcd-efgh-jkmn-pqrsu", "abcd-efgh-jkmn-pqr!"} {
		if _, ok := normalizeRecoveryCode(bad); ok {
			t.Errorf("%q normalised as a code", bad)
		}
	}
}

// The stored hash of a recovery code is the account's own: the same code
// hashes differently for two accounts, and differently from the unsalted
// version 1 hash it replaced.
func TestRecoveryCodeHashIsPerAccount(t *testing.T) {
	t.Parallel()
	const norm = "0123456789abcdef"
	a, b := uuid.New(), uuid.New()
	tests := []struct {
		name      string
		got, want []byte
		same      bool
	}{
		{name: "same account, same code", got: hashRecoveryCode(a, norm), want: hashRecoveryCode(a, norm), same: true},
		{name: "two accounts, same code", got: hashRecoveryCode(a, norm), want: hashRecoveryCode(b, norm)},
		{name: "salted against unsalted", got: hashRecoveryCode(a, norm), want: legacyRecoveryCodeHash(norm)},
		{name: "two codes, same account", got: hashRecoveryCode(a, norm), want: hashRecoveryCode(a, "0123456789abcdeg")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := string(tc.got) == string(tc.want); got != tc.same {
				t.Errorf("hashes equal = %v, want %v", got, tc.same)
			}
		})
	}
}
