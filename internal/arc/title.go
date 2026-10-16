package arc

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"perspectiv/internal/api/apiutil"
)

// Title length bounds in characters (Unicode code points), matching the
// arcs_title_check constraint's char_length.
const (
	minTitleLen = 3
	maxTitleLen = 80
)

// dateLayouts are the spellings of an arc's date a title may not consist of.
// Comparison ignores case, spacing and punctuation (see titleKey), so
// "2026-10-16", "2026/10/16" and "2026 10 16" are one spelling.
var dateLayouts = []string{
	"2006-01-02",
	"01/02/2006",
	"1/2/2006",
	"02/01/2006",
	"2/1/2006",
	"January 2, 2006",
	"Jan 2, 2006",
	"2 January 2006",
	"2 Jan 2006",
	"Monday, January 2, 2006",
	"Mon, Jan 2, 2006",
	"Monday, 2 January 2006",
	"January 2",
	"Jan 2",
	"2 January",
	"2 Jan",
	"01/02",
	"1/2",
}

// checkTitle validates a title an author gives an arc and returns it with
// surrounding whitespace trimmed. A title is required, 3 to 80 characters
// after trimming, and may not be the place name, the date, or the place name
// and the date together in either order.
//
// "Equal" is decided on titleKey: lower case with everything but letters and
// digits removed, so "Joe's Diner, 2026-10-16" equals the place "Joe's Diner"
// plus the date, and "joes   DINER" equals the place name. A title that merely
// contains the place name ("Last call at Joe's Diner") is accepted.
func checkTitle(raw, placeName string, date time.Time) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", apiutil.InvalidArg("a new arc needs a title")
	}
	if n := utf8.RuneCountInString(title); n < minTitleLen || n > maxTitleLen {
		return "", apiutil.InvalidArg(fmt.Sprintf("title must be %d to %d characters", minTitleLen, maxTitleLen))
	}
	key := titleKey(title)
	place := titleKey(placeName)
	if key == place {
		return "", apiutil.InvalidArg("title must not be the place name")
	}
	for _, layout := range dateLayouts {
		d := titleKey(date.Format(layout))
		switch key {
		case d:
			return "", apiutil.InvalidArg("title must not be the date")
		case place + d, d + place:
			return "", apiutil.InvalidArg("title must not be the place name and the date")
		}
	}
	return title, nil
}

// titleKey reduces s to its lower-cased letters and digits.
func titleKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// hourTitle is the title an hour arc gets when it is created: the day arc's
// title plus the hour label, for example "The long night · 14:00". The day
// title is shortened so the result stays within maxTitleLen characters. The
// hour arc's creator may change it later with RetitleHourArc.
func hourTitle(dayTitle string, hour int32) string {
	label := fmt.Sprintf(" · %02d:00", hour)
	room := maxTitleLen - utf8.RuneCountInString(label)
	if r := []rune(dayTitle); len(r) > room {
		dayTitle = strings.TrimRightFunc(string(r[:room]), unicode.IsSpace)
	}
	return dayTitle + label
}
