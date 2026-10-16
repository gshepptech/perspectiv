package auth

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	"golang.org/x/crypto/bcrypt"

	"perspectiv/internal/api/apiutil"
)

func TestValidatePassword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		candidate string
		wantOK    bool
	}{
		{name: "ten distinct characters is strong enough", candidate: "kettle-drum", wantOK: true},
		{name: "a passphrase is strong enough", candidate: "correct horse battery staple", wantOK: true},
		{name: "exactly 72 bytes is accepted", candidate: strings.Repeat("abcdefgh", 9), wantOK: true},
		{name: "multibyte characters count as characters", candidate: "ñandú-über-çà", wantOK: true},
		{name: "empty is refused", candidate: ""},
		{name: "nine characters is refused", candidate: "abcdefgh1"},
		{name: "73 bytes is refused", candidate: strings.Repeat("abcdefgh", 9) + "x"},
		{name: "one repeated character is refused", candidate: "aaaaaaaaaaaa"},
		{name: "four distinct characters is refused", candidate: "abcdabcdabcd"},
		{name: "the username is refused", candidate: "Writer_One_99"},
		{name: "the username in another case is refused", candidate: "writer_one_99"},
		{name: "the email is refused", candidate: "writer@example.test"},
		{name: "the email local part is refused", candidate: "Writer.Person"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			email := "writer@example.test"
			if strings.Contains(tc.name, "local part") {
				email = "writer.person@example.test"
			}
			err := ValidatePassword(tc.candidate, email, "Writer_One_99")
			if tc.wantOK {
				if err != nil {
					t.Fatalf("ValidatePassword(%q) = %v, want nil", tc.candidate, err)
				}
				return
			}
			if code, typed := apiutil.CodeOf(err); !typed || code != connect.CodeInvalidArgument {
				t.Fatalf("ValidatePassword(%q) = %v, want InvalidArgument", tc.candidate, err)
			}
		})
	}
}

func TestPasswordsHashAndVerify(t *testing.T) {
	t.Parallel()
	p := NewPasswords(bcrypt.MinCost)
	hash, err := p.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	tests := []struct {
		name     string
		hash     string
		password string
		wantOK   bool
		wantErr  bool
	}{
		{name: "the right password verifies", hash: hash, password: "correct horse battery staple", wantOK: true},
		{name: "a wrong password does not verify", hash: hash, password: "correct horse battery stapler"},
		{name: "an over-long password never verifies", hash: hash, password: strings.Repeat("x", 100)},
		{name: "a corrupt hash is an error", hash: "not-a-hash", password: "whatever", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, err := p.VerifyPassword(tc.hash, tc.password)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

func TestNewPasswordsCost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cost int
		want int
	}{
		{name: "zero selects the default", cost: 0, want: DefaultBcryptCost},
		{name: "below the minimum is raised to it", cost: 1, want: bcrypt.MinCost},
		{name: "an explicit cost is kept", cost: 5, want: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NewPasswords(tc.cost).cost; got != tc.want {
				t.Fatalf("cost = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    string
		want  string
		valid bool
	}{
		{name: "a plain address", in: "a.writer@example.test", want: "a.writer@example.test", valid: true},
		{name: "surrounding space is trimmed", in: "  a@example.test ", want: "a@example.test", valid: true},
		{name: "empty", in: ""},
		{name: "no at sign", in: "example.test"},
		{name: "a display name", in: "Writer <a@example.test>"},
		{name: "a dotless domain", in: "a@localhost"},
		{name: "too long", in: strings.Repeat("a", 250) + "@example.test"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := validateEmail(tc.in)
			if !tc.valid {
				if code, typed := apiutil.CodeOf(err); !typed || code != connect.CodeInvalidArgument {
					t.Fatalf("validateEmail(%q) = %v, want InvalidArgument", tc.in, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("validateEmail(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
}

func TestRoleFromDB(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		wantErr bool
	}{
		{in: "ROLE_USER"},
		{in: "ROLE_ADMIN"},
		{in: "ROLE_UNSPECIFIED", wantErr: true},
		{in: "admin", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := roleFromDB(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("roleFromDB(%q) = %v, %v", tc.in, got, err)
			}
			if err == nil && got.String() != tc.in {
				t.Fatalf("roleFromDB(%q) = %v", tc.in, got)
			}
		})
	}
}
