package perspective

import (
	"strings"
	"testing"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// The 300-word limit counts words as runs of non-whitespace separated by
// Unicode whitespace.
func TestCountWords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "empty", in: "", want: 0},
		{name: "only whitespace", in: " \t\n  ", want: 0},
		{name: "plain sentence", in: "I ordered pancakes at midnight.", want: 5},
		{name: "runs of whitespace", in: "  one\n\n two\tthree  ", want: 3},
		{name: "hyphen and apostrophe stay in a word", in: "short-order cook's", want: 2},
		{name: "punctuation alone is a word", in: "wait — what", want: 3},
		{name: "non-breaking space separates", in: "one two", want: 2},
		{name: "emoji is a word", in: "nice 🔥", want: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := countWords(tc.in); got != tc.want {
				t.Fatalf("countWords(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestCheckParts(t *testing.T) {
	t.Parallel()
	w300 := strings.Repeat("w ", 300)
	tests := []struct {
		name    string
		parts   []string
		publish bool
		wantErr bool
	}{
		{name: "ten parts of 300 words", parts: repeat(w300, 10), publish: true},
		{name: "eleven parts", parts: repeat("x", 11), publish: true, wantErr: true},
		{name: "301 words", parts: []string{w300 + "w"}, publish: true, wantErr: true},
		{name: "publish needs a part", parts: nil, publish: true, wantErr: true},
		{name: "publish refuses a blank part", parts: []string{"x", " "}, publish: true, wantErr: true},
		{name: "a draft may be empty", parts: nil},
		{name: "a draft may hold blank parts", parts: []string{"", ""}},
		{name: "a draft still has the word limit", parts: []string{w300 + "w"}, wantErr: true},
		{name: "an overlong part", parts: []string{strings.Repeat("a", maxPartRunes+1)}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkParts(tc.parts, tc.publish)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkParts err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// A label is put on one line and refused when it holds a character that
// hides itself; see TestCharacterLabelsReadAsOneLine for save and publish.
// A draft is stored only once some part holds a word; blank parts beside a
// written one are fine.
func TestAnyText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		parts []string
		want  bool
	}{
		{name: "no parts", parts: nil, want: false},
		{name: "one empty part", parts: []string{""}, want: false},
		{name: "only whitespace", parts: []string{" \n\t", "  "}, want: false},
		{name: "one word", parts: []string{"bubbles"}, want: true},
		{name: "blank part beside a written one", parts: []string{"", "We came for the market."}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := anyText(tc.parts); got != tc.want {
				t.Fatalf("anyText(%q) = %v, want %v", tc.parts, got, tc.want)
			}
		})
	}
}

func TestCheckCharacters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, label, want string
		wantErr           bool
	}{
		{name: "a plain label is kept", label: "the cook", want: "the cook"},
		{name: "a line break becomes one space", label: "the\r\ncook", want: "the cook"},
		{name: "a unicode line separator becomes one space", label: "the\u2028cook", want: "the cook"},
		{name: "blank after collapsing is refused", label: " \n ", wantErr: true},
		{name: "eighty characters after collapsing is kept", label: strings.Repeat("x", 40) + "\n\n\n" + strings.Repeat("y", 39), want: strings.Repeat("x", 40) + " " + strings.Repeat("y", 39)},
		{name: "a U+200D zero-width character is refused", label: "the\u200dcook", wantErr: true},
		{name: "a left-to-right mark is refused", label: "the\u200ecook", wantErr: true},
		{name: "a pop directional formatting is refused", label: "the\u202ccook", wantErr: true},
		{name: "a first strong isolate is refused", label: "the\u2068cook", wantErr: true},
		{name: "a NUL is refused", label: "the\x00cook", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := checkCharacters([]*perspectivv1.CharacterInput{{Label: tc.label}})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("checkCharacters(%q) = %v, want an error", tc.label, got)
				}
				return
			}
			if err != nil || len(got) != 1 || got[0].label != tc.want {
				t.Fatalf("checkCharacters(%q) = %v, %v, want %q", tc.label, got, err, tc.want)
			}
		})
	}
}
