package arc

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"perspectiv/internal/api/apiutil"
)

func TestCheckTitle(t *testing.T) {
	t.Parallel()

	date := time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)
	const place = "Joe's Diner"

	tests := []struct {
		name    string
		title   string
		want    string
		wantErr bool
	}{
		{name: "three characters accepted (OT-013)", title: "abc", want: "abc"},
		{name: "eighty characters accepted", title: strings.Repeat("x", 80), want: strings.Repeat("x", 80)},
		{name: "eighty multibyte characters accepted", title: strings.Repeat("é", 80), want: strings.Repeat("é", 80)},
		{name: "surrounding whitespace trimmed", title: "  A long night  ", want: "A long night"},
		{name: "ordinary title accepted", title: "The night the jukebox died", want: "The night the jukebox died"},
		{name: "place name inside a longer title accepted", title: "Last call at Joe's Diner", want: "Last call at Joe's Diner"},
		{name: "empty title rejected (FR-014)", title: "", wantErr: true},
		{name: "whitespace only rejected", title: "     ", wantErr: true},
		{name: "two characters rejected", title: "ab", wantErr: true},
		{name: "two characters after trimming rejected", title: "  ab  ", wantErr: true},
		{name: "eighty-one characters rejected", title: strings.Repeat("x", 81), wantErr: true},
		{name: "place name rejected (OT-013)", title: "Joe's Diner", wantErr: true},
		{name: "place name in other case and spacing rejected", title: "joes   DINER", wantErr: true},
		{name: "ISO date rejected", title: "2026-10-16", wantErr: true},
		{name: "US numeric date rejected", title: "10/16/2026", wantErr: true},
		{name: "long date rejected", title: "October 16, 2026", wantErr: true},
		{name: "day-first long date rejected", title: "16 Oct 2026", wantErr: true},
		{name: "weekday date rejected", title: "Friday, October 16, 2026", wantErr: true},
		{name: "month and day without year rejected", title: "October 16", wantErr: true},
		{name: "place then date rejected (AC-011)", title: "Joe's Diner 2026-10-16", wantErr: true},
		{name: "date then place rejected", title: "2026-10-16 - Joe's Diner", wantErr: true},
		{name: "place and long date rejected", title: "Joe's Diner, October 16, 2026", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := checkTitle(tc.title, place, date)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("checkTitle(%q) = %q, want an error", tc.title, got)
				}
				if code, typed := apiutil.CodeOf(err); !typed || code != connect.CodeInvalidArgument {
					t.Fatalf("checkTitle(%q) error = %v, want InvalidArg", tc.title, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("checkTitle(%q) error = %v", tc.title, err)
			}
			if got != tc.want {
				t.Fatalf("checkTitle(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestHourTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dayTitle string
		hour     int32
		want     string
	}{
		{name: "midnight", dayTitle: "The long night", hour: 0, want: "The long night · 00:00"},
		{name: "afternoon", dayTitle: "The long night", hour: 14, want: "The long night · 14:00"},
		{name: "last hour", dayTitle: "abc", hour: 23, want: "abc · 23:00"},
		{
			name:     "long day title shortened to fit eighty characters",
			dayTitle: strings.Repeat("y", 80),
			hour:     9,
			want:     strings.Repeat("y", 72) + " · 09:00",
		},
		{
			name:     "shortening never leaves a trailing space before the label",
			dayTitle: strings.Repeat("y", 71) + " zzzzzzzz",
			hour:     9,
			want:     strings.Repeat("y", 71) + " · 09:00",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hourTitle(tc.dayTitle, tc.hour)
			if got != tc.want {
				t.Fatalf("hourTitle(%q, %d) = %q, want %q", tc.dayTitle, tc.hour, got, tc.want)
			}
			if n := len([]rune(got)); n < minTitleLen || n > maxTitleLen {
				t.Fatalf("hourTitle length %d outside %d..%d", n, minTitleLen, maxTitleLen)
			}
		})
	}
}
