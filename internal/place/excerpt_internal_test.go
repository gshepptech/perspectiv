package place

import (
	"strings"
	"testing"
)

func TestClipExcerpt(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("rain ", 60) // 300 runes
	tests := []struct {
		name, in, want string
	}{
		{name: "short stays whole", in: "  It rained.  ", want: "It rained."},
		{name: "exactly the limit stays whole", in: strings.Repeat("a", excerptRunes), want: strings.Repeat("a", excerptRunes)},
		{name: "long ends at a word with an ellipsis", in: long, want: strings.TrimSpace(long[:200]) + "…"},
		{name: "a comma before the cut is dropped", in: strings.Repeat("ab, ", 70), want: strings.TrimSuffix(strings.TrimSpace(strings.Repeat("ab, ", 70)[:200]), ",") + "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := clipExcerpt(tc.in)
			if got != tc.want {
				t.Fatalf("clipExcerpt = %q, want %q", got, tc.want)
			}
			if strings.HasSuffix(got, "…") && len([]rune(got)) > excerptRunes+1 {
				t.Fatalf("clipped excerpt is %d runes", len([]rune(got)))
			}
		})
	}
}
