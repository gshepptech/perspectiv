package user_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/user"
)

// AC-002 (user part): the username rule shared by signup and ChangeUsername.
func TestValidateUsername(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "three chars", in: "abc"},
		{name: "twenty chars", in: strings.Repeat("a", 20)},
		{name: "letters digits underscore", in: "Night_Owl_42"},
		{name: "leading underscore", in: "_owl"},
		{name: "empty", in: "", wantErr: true},
		{name: "two chars", in: "ab", wantErr: true},
		{name: "twenty one chars", in: strings.Repeat("a", 21), wantErr: true},
		{name: "space", in: "night owl", wantErr: true},
		{name: "hyphen", in: "night-owl", wantErr: true},
		{name: "dot", in: "night.owl", wantErr: true},
		{name: "non ascii letter", in: "café_owl", wantErr: true},
		{name: "at sign", in: "owl@example", wantErr: true},
		{name: "deleted placeholder", in: "[deleted]", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := user.ValidateUsername(tc.in)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("ValidateUsername(%q) = %v, want nil", tc.in, err)
				}
				return
			}
			if code, typed := apiutil.CodeOf(err); err == nil || !typed || code != connect.CodeInvalidArgument {
				t.Fatalf("ValidateUsername(%q) = %v, want InvalidArg", tc.in, err)
			}
		})
	}
}

func TestPublicUsername(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		stored  string
		deleted bool
		want    string
	}{
		{name: "live account shows its username", stored: "night_owl", want: "night_owl"},
		{name: "deleted account shows the placeholder", stored: "~deleted~0193", deleted: true, want: user.DeletedUsername},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := user.PublicUsername(tc.stored, tc.deleted); got != tc.want {
				t.Fatalf("PublicUsername = %q, want %q", got, tc.want)
			}
		})
	}
}
