package perspective

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
)

// Limits on what a perspective holds.
const (
	maxParts        = 10
	maxWordsPerPart = 300
	maxCharacters   = 10
	// maxLabelRunes bounds a character's label, in characters.
	maxLabelRunes = 80
	// maxPartRunes bounds a part's length in characters, so a part of 300
	// enormous "words" cannot be stored. 300 words of ordinary prose are
	// well under 3,000 characters.
	maxPartRunes = 10000
	// maxDraftTitleRunes bounds the arc title a draft carries; the arc's own
	// rules (3 to 80 characters, not the place or date) apply at publish.
	maxDraftTitleRunes = 80
)

// dateLayout is the wire form of a place-local calendar day.
const dateLayout = "2006-01-02"

// countWords counts the words in a part for the 300-word limit. A word is a
// maximal run of characters that are not Unicode whitespace (strings.Fields),
// so "short-order", "cook's" and a lone "—" or emoji each count as one word,
// and any amount of spacing or line breaks between words counts as nothing.
func countWords(s string) int {
	return len(strings.Fields(s))
}

// checkParts enforces the part limits. Both drafts and publishes hold at most
// 10 parts of at most 300 words. A publish also needs at least one part, and
// no part may be blank; a draft may be empty or hold blank parts while its
// author is still writing.
func checkParts(parts []string, publish bool) error {
	if len(parts) > maxParts {
		return apiutil.InvalidArg(fmt.Sprintf("a perspective has at most %d parts", maxParts))
	}
	if publish && len(parts) == 0 {
		return apiutil.InvalidArg("a perspective needs at least one part")
	}
	for i, body := range parts {
		if utf8.RuneCountInString(body) > maxPartRunes {
			return apiutil.InvalidArg(fmt.Sprintf("part %d is longer than %d characters", i+1, maxPartRunes))
		}
		n := countWords(body)
		if n > maxWordsPerPart {
			return apiutil.InvalidArg(fmt.Sprintf("part %d has %d words; a part has at most %d", i+1, n, maxWordsPerPart))
		}
		if publish && n == 0 {
			return apiutil.InvalidArg(fmt.Sprintf("part %d is empty", i+1))
		}
	}
	return nil
}

// characterInput is one declared character after validation. id is uuid.Nil
// for a new character.
type characterInput struct {
	id    uuid.UUID
	label string
}

// checkCharacters validates declared characters: at most 10, each with a
// label of 1 to 80 characters on one line (every run of whitespace, line
// breaks included, becomes one space) with no control, zero-width or bidi
// override characters, and no id named twice. A label is read out in other
// people's email, so it may not forge a line there. Whether a named id
// belongs to the perspective is checked against the store.
func checkCharacters(in []*perspectivv1.CharacterInput) ([]characterInput, error) {
	if len(in) > maxCharacters {
		return nil, apiutil.InvalidArg(fmt.Sprintf("a perspective declares at most %d characters", maxCharacters))
	}
	out := make([]characterInput, 0, len(in))
	seen := make(map[uuid.UUID]bool, len(in))
	for i, c := range in {
		label, err := checkLabel(i, c.GetLabel())
		if err != nil {
			return nil, err
		}
		ci := characterInput{label: label}
		if raw := c.GetId(); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				return nil, apiutil.InvalidArg(fmt.Sprintf("character %d has an invalid id", i+1))
			}
			if seen[id] {
				return nil, apiutil.InvalidArg(fmt.Sprintf("character %d repeats an id", i+1))
			}
			seen[id] = true
			ci.id = id
		}
		out = append(out, ci)
	}
	return out, nil
}

// checkLabel returns the i-th character's label on one line, or why it is
// refused.
func checkLabel(i int, raw string) (string, error) {
	label := apiutil.OneLine(raw)
	switch {
	case label == "":
		return "", apiutil.InvalidArg(fmt.Sprintf("character %d needs a label", i+1))
	case utf8.RuneCountInString(label) > maxLabelRunes:
		return "", apiutil.InvalidArg(fmt.Sprintf("character %d's label is longer than %d characters", i+1, maxLabelRunes))
	case apiutil.HasHiddenRune(label):
		return "", apiutil.InvalidArg(fmt.Sprintf("character %d's label has an invisible or control character; remove it", i+1))
	}
	return label, nil
}

// parseGenre returns the stored name of g. A publish or edit needs one of the
// fixed genres; a draft may leave it unset (nil).
func parseGenre(g perspectivv1.Genre, required bool) (*string, error) {
	if g == perspectivv1.Genre_GENRE_UNSPECIFIED {
		if required {
			return nil, apiutil.InvalidArg("choose a genre")
		}
		return nil, nil
	}
	name, ok := perspectivv1.Genre_name[int32(g)]
	if !ok {
		return nil, apiutil.InvalidArg("unknown genre")
	}
	return &name, nil
}

// parseDate reads a YYYY-MM-DD place-local day as UTC midnight. The day is
// stored exactly as the author picked it; it is never derived from a clock.
func parseDate(s string) (time.Time, error) {
	d, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, apiutil.InvalidArg("local_date must be a YYYY-MM-DD day")
	}
	return d, nil
}

// checkHour accepts an absent hour (the day arc) or 0 to 23.
func checkHour(h *int32) error {
	if h != nil && (*h < 0 || *h > 23) {
		return apiutil.InvalidArg("hour must be 0 to 23")
	}
	return nil
}

func parseID(field, s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, apiutil.InvalidArg(field + " is required")
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, apiutil.InvalidArg(field + " is not a valid id")
	}
	return id, nil
}
