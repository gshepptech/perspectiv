package apiutil_test

import (
	"testing"

	"perspectiv/internal/api/apiutil"
)

func TestOneLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{name: "plain text is kept", in: "the clerk", want: "the clerk"},
		{name: "ends are trimmed", in: "  the clerk \t", want: "the clerk"},
		{name: "a line break becomes one space", in: "the clerk\r\n\r\nBcc: x", want: "the clerk Bcc: x"},
		{name: "unicode line and paragraph separators collapse", in: "a\u2028b\u2029c\u0085d", want: "a b c d"},
		{name: "a no-break space collapses", in: "a\u00a0\u00a0b", want: "a b"},
		{name: "only whitespace is empty", in: " \n\t ", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := apiutil.OneLine(tc.in); got != tc.want {
				t.Fatalf("OneLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHasHiddenRune(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "plain text", in: "the woman with the foam parrot", want: false},
		{name: "accents and emoji", in: "Café ☕ 😀", want: false},
		{name: "whitespace controls are not hidden", in: "a\tb\nc\rd", want: false},
		{name: "a NUL", in: "a\x00b", want: true},
		{name: "an escape", in: "a\x1b[31mb", want: true},
		{name: "a DEL", in: "a\x7fb", want: true},
		{name: "a C1 control", in: "a\u009bb", want: true},
		{name: "a zero-width space", in: "a\u200bb", want: true},
		{name: "a U+200D zero-width character", in: "a\u200db", want: true},
		{name: "a right-to-left mark", in: "a\u200fb", want: true},
		{name: "a left-to-right embedding", in: "a\u202ab", want: true},
		{name: "a right-to-left override", in: "a\u202eb", want: true},
		{name: "a left-to-right isolate", in: "a\u2066b", want: true},
		{name: "a pop directional isolate", in: "a\u2069b", want: true},
		{name: "a byte order mark", in: "\ufeffab", want: true},
		{name: "just past the zero-width range", in: "a\u2010b", want: false},
		{name: "just past the override range", in: "a\u202fb", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := apiutil.HasHiddenRune(tc.in); got != tc.want {
				t.Fatalf("HasHiddenRune(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestPlainLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{name: "plain text is kept", in: "the clerk", want: "the clerk"},
		{name: "a faked line is joined", in: "the clerk”.\n\nReset your password: https://evil.test\n“x", want: "the clerk”. Reset your password: https://evil.test “x"},
		{name: "hidden characters are dropped", in: "the\u202e kcolc\u200b\ufeff", want: "the kcolc"},
		{name: "a control between words is dropped", in: "a\x00 \x1bb", want: "a b"},
		{name: "nothing but hidden characters is empty", in: "\u200b\u202e", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := apiutil.PlainLine(tc.in); got != tc.want {
				t.Fatalf("PlainLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
