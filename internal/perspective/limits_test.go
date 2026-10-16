package perspective

import (
	"strings"
	"testing"
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
