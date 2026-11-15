package apiutil

import (
	"strings"
	"unicode"
)

// OneLine collapses every run of whitespace in s, line breaks included, to
// one space and trims the ends: a label or title always reads as one line.
func OneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// HasHiddenRune reports whether s holds a character that changes how the
// text around it reads without showing itself: a control character other
// than whitespace, a zero-width character (U+200B to U+200F, U+FEFF), or a
// bidi override or isolate (U+202A to U+202E, U+2066 to U+2069). Text a
// writer names something with, and that other people or an email show,
// refuses them.
func HasHiddenRune(s string) bool {
	return strings.IndexFunc(s, hiddenRune) >= 0
}

func hiddenRune(r rune) bool {
	switch {
	case unicode.IsControl(r):
		return !unicode.IsSpace(r)
	case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0xFEFF:
		return true
	default:
		return false
	}
}

// PlainLine is s with hidden characters (see HasHiddenRune) removed and its
// whitespace collapsed (see OneLine): how text stored before those rules
// existed is shown where it could otherwise fake a line, as in an email.
func PlainLine(s string) string {
	return OneLine(strings.Map(func(r rune) rune {
		if hiddenRune(r) {
			return -1
		}
		return r
	}, s))
}
