package importer

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// localityAbbrevs expands the abbreviations a free-text address writes for
// the words a division's name spells out: "St. Petersburg" is the city
// Overture names "Saint Petersburg", "Ft. Myers" is "Fort Myers".
var localityAbbrevs = map[string]string{
	"st": "saint", "ste": "sainte", "ft": "fort", "mt": "mount", "mtn": "mountain",
}

// localityKey normalises a city, town or address locality for filing: case,
// punctuation and whitespace are ignored, and St./Ft./Mt. read as the words
// they abbreviate. An apostrophe is dropped rather than split on, so
// "Coeur d'Alene" and "Coeur dAlene" agree.
func localityKey(s string) string {
	words := strings.Fields(strings.Map(func(r rune) rune {
		switch {
		case r == '\'' || r == '’':
			return -1
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return unicode.ToLower(r)
		default:
			return ' '
		}
	}, s))
	for i, w := range words {
		if full, ok := localityAbbrevs[w]; ok {
			words[i] = full
		}
	}
	return strings.Join(words, " ")
}

// countryNames are the trailing name parts that only repeat the country.
var countryNames = map[string]bool{"usa": true, "us": true, "united states": true, "united states of america": true}

// trailingPunct is what a name may end in by accident: a list separator or
// a dangling dash left by a truncated extract.
const trailingPunct = ".,;:-–—/|*_"

// nameAbbrevs are the last words whose full stop belongs to the word.
var nameAbbrevs = map[string]bool{
	"inc": true, "co": true, "corp": true, "ltd": true, "st": true, "jr": true, "sr": true,
	"dr": true, "mt": true, "ft": true, "ave": true, "blvd": true, "rd": true, "bros": true,
	"assn": true, "no": true, "mfg": true, "intl": true, "ste": true, "llc": true,
}

// initialism matches a last word written as initials with stops: "U.S.A."
var initialism = regexp.MustCompile(`(?:^|\s)(?:\pL\.){2,}$`)

// cleanPlaceName tidies a store or mall name from the place data. It strips
// a trailing ", <locality>", ", <state>" or ", USA" that only repeats the
// record's own address (so "Cozumel, Mexico" stays), strips trailing
// punctuation unless a full stop ends an abbreviation, and title-cases a name
// written all in lower case. A name the rules would empty is kept as it was.
func cleanPlaceName(name, locality, region string) string {
	orig := cleanName(name)
	name = orig
	state, _ := stateCode(region)
	for {
		next := trimTrailingPunct(stripOwnSuffix(name, localityKey(locality), state))
		if next == name {
			break
		}
		name = next
	}
	if !hasLetter(name) {
		return orig
	}
	if strings.ToLower(name) == name {
		name = titleCase(name)
	}
	return name
}

// stripOwnSuffix removes one trailing ", part" when part names the record's
// own locality or state, or the country. It never strips the only part.
func stripOwnSuffix(name, locality, state string) string {
	i := strings.LastIndex(name, ",")
	if i <= 0 {
		return name
	}
	head, part := strings.TrimSpace(name[:i]), strings.TrimSpace(name[i+1:])
	if !hasLetter(head) {
		return name
	}
	key := localityKey(part)
	code, isState := stateCode(part)
	switch {
	case key == "":
		return head
	case locality != "" && key == locality,
		countryNames[key],
		isState && code == state:
		return head
	}
	return name
}

// trimTrailingPunct strips trailing separators, keeping a full stop that
// ends an abbreviation ("Smith & Co.") or initials ("U.S.A.").
func trimTrailingPunct(name string) string {
	for name != "" {
		r, size := utf8.DecodeLastRuneInString(name)
		if !strings.ContainsRune(trailingPunct, r) {
			break
		}
		if r == '.' && endsInAbbreviation(name) {
			break
		}
		name = strings.TrimSpace(name[:len(name)-size])
	}
	return name
}

func endsInAbbreviation(name string) bool {
	if initialism.MatchString(name) {
		return true
	}
	words := strings.Fields(name)
	if len(words) == 0 {
		return false
	}
	return nameAbbrevs[strings.ToLower(strings.TrimRight(words[len(words)-1], "."))]
}

// minorWords stay lower case inside a title-cased name.
var minorWords = map[string]bool{
	"a": true, "an": true, "and": true, "at": true, "by": true, "for": true, "in": true,
	"of": true, "on": true, "or": true, "the": true, "to": true, "de": true, "del": true, "la": true,
}

// titleCase capitalises each word of an all-lower-case name, leaving minor
// words lower case unless they open or close it. A letter after a hyphen or
// a slash starts a word; one after an apostrophe does not ("joe's").
func titleCase(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		if i > 0 && i < len(words)-1 && minorWords[w] {
			continue
		}
		words[i] = capitaliseParts(w)
	}
	return strings.Join(words, " ")
}

func capitaliseParts(w string) string {
	var b strings.Builder
	start := true
	for _, r := range w {
		if start && unicode.IsLetter(r) {
			r = unicode.ToUpper(r)
			start = false
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' || r == '’' {
			start = false
		}
		if r == '-' || r == '/' {
			start = true
		}
		b.WriteRune(r)
	}
	return b.String()
}
