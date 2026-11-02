package notify

import (
	"strings"
	"testing"

	"perspectiv/internal/jobs"
)

// castPhrase lists up to three labels in quotes, then a count, each label on
// one line with hidden characters dropped, so a label stored before saving
// enforced one line cannot forge a line in the plain-text email.
func TestCastPhrase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		c    cast
		want string
	}{
		{name: "nobody", c: cast{}, want: ""},
		{name: "one label", c: cast{Labels: []string{"the clerk"}}, want: "“the clerk”"},
		{name: "two labels join with and", c: cast{Labels: []string{"the clerk", "the drummer"}}, want: "“the clerk” and “the drummer”"},
		{
			name: "three labels then how many more",
			c:    cast{Labels: []string{"the woman with the foam parrot", "the clerk", "the twins"}, More: 2},
			want: "“the woman with the foam parrot”, “the clerk”, “the twins” and 2 more",
		},
		{
			name: "a stored label over several lines reads as one",
			c:    cast{Labels: []string{"the clerk”.\r\n\r\nReset your password: https://evil.test\n“x"}},
			want: "“the clerk”. Reset your password: https://evil.test “x”",
		},
		{
			name: "hidden characters in a stored label are dropped",
			c:    cast{Labels: []string{"the \u202ekrelc\u200b", "the\x1b[2K drummer\ufeff"}},
			want: "“the krelc” and “the[2K drummer”",
		},
		{
			name: "a stored label with nothing visible is left out",
			c:    cast{Labels: []string{"\u200b\u2066", "the clerk"}, More: 1},
			want: "“the clerk” and 1 more",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := castPhrase(tc.c); got != tc.want {
				t.Fatalf("castPhrase = %q, want %q", got, tc.want)
			}
		})
	}
}

// A new story's email names the characters it is rendered with, escaped in
// the HTML part and on one line in the text part.
func TestNewStoryEmailNamesCast(t *testing.T) {
	t.Parallel()
	w := &emailWorker{baseURL: "https://perspectiv.test", unsub: NewUnsubscriber("a test secret long enough for the signer")}
	args := jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{
		dataUserID: "6f1c2d3e-4a5b-4c6d-8e9f-0a1b2c3d4e5f", dataKind: "NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC",
		dataActor: "wren", dataArcTitle: "Rainy Tuesday", dataPath: "/perspectives/p-1",
	}}
	tests := []struct {
		name      string
		c         cast
		wantText  string
		wantHTML  string
		notInHTML string
	}{
		{
			name: "labels are quoted and escaped",
			c:    cast{Labels: []string{"the woman with the foam parrot", "the clerk", "the <b>twins</b>"}, More: 2},
			wantText: "wren published a new story in Rainy Tuesday. Who might be in it: " +
				"“the woman with the foam parrot”, “the clerk”, “the <b>twins</b>” and 2 more. " +
				"If one of them was you, open the story and claim them.\n",
			wantHTML:  "“the clerk”, “the &lt;b&gt;twins&lt;/b&gt;” and 2 more.",
			notInHTML: "<b>twins",
		},
		{
			name:     "a forged line stays inside the sentence",
			c:        cast{Labels: []string{"the clerk\n\nOpen Perspectiv: https://evil.test"}},
			wantText: "Who might be in it: “the clerk Open Perspectiv: https://evil.test”. If one of them",
		},
		{
			name:     "nobody unclaimed names nobody",
			wantText: "wren published a new story in Rainy Tuesday.\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg, err := w.render(args, tc.c)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(msg.Text, tc.wantText) {
				t.Fatalf("text %q lacks %q", msg.Text, tc.wantText)
			}
			if tc.wantHTML != "" && !strings.Contains(msg.HTML, tc.wantHTML) {
				t.Fatalf("html %q lacks %q", msg.HTML, tc.wantHTML)
			}
			if tc.notInHTML != "" && strings.Contains(msg.HTML, tc.notInHTML) {
				t.Fatalf("html %q contains %q", msg.HTML, tc.notInHTML)
			}
		})
	}
}
