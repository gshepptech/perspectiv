package place

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
)

// A search can name a place and the city, town or state it is in:
// "starbucks tampa", "tampa starbucks", "starbucks in tampa", "starbucks,
// tampa", "tampa florida", "starbucks fl" or "starbucks tampa fl". Each way of splitting the words into a name
// and a locality that names a visible state, city or town is searched under
// those localities, best locality first, beside the plain search of the
// whole text.
const (
	// maxLocalityWords is the longest locality read from the first or last
	// words: "new port richey".
	maxLocalityWords = 3
	// maxLocalitiesPerName bounds the same-named cities and towns one split
	// is searched under ("valley" names 15).
	maxLocalitiesPerName = 10
	// maxSplitSearches bounds the searches under localities per request.
	maxSplitSearches = 3
	// maxSearchQuery bounds a search's text, in characters as sent: room
	// for the longest place name (120) and the town it is in. The web
	// search boxes stop typing at the same length.
	maxSearchQuery = 200
)

// split is one reading of a search as a place name and the city or town it
// is in. state, when set, is the name of the state a code in the search
// named ("fl"): the locality is within that state, or, when locality is
// empty, the state is the locality.
type split struct {
	name, locality, state string
}

// Split is how a search read its text as a place's name and the locality
// it is in: the first split whose locality the search found, and the best
// locality of that name.
type Split struct {
	// Name is the name part as typed, case kept and spaces collapsed.
	Name string
	// LocalityID is the best state, city or town of the locality's name.
	LocalityID uuid.UUID
	// LocalityLabel is that locality and the places it is in, nearest
	// first: "Tampa, Florida".
	LocalityLabel string
}

// SearchResult is one typeahead answer: at most SearchLimit places, whether
// more match, and the split the search read, if any.
type SearchResult struct {
	Places []Place
	More   bool
	// Split is nil when no split named a visible locality, or when a
	// place's name starts with the whole text, which is then a name.
	Split *Split
}

// Search runs the typeahead. parentID uuid.Nil searches every place; a
// parent that is missing or hidden is NotFound. kind UNSPECIFIED matches
// every kind. A query longer than maxSearchQuery characters is
// InvalidArgument before anything is read: each search can run up to
// maxSplitSearches more under the towns it names.
//
// The places whose name starts with the whole text come first, then the
// matches under each locality the text names, then the rest of the plain
// search: "miami beach" still leads with Miami Beach, while "starbucks
// tampa", which no name starts with, leads with the Starbucks in Tampa.
func (s *Service) Search(ctx context.Context, query string, parentID uuid.UUID, kind perspectivv1.PlaceKind) (SearchResult, error) {
	if utf8.RuneCountInString(query) > maxSearchQuery {
		return SearchResult{}, apiutil.InvalidArg(fmt.Sprintf("search for at most %d characters", maxSearchQuery))
	}
	display := searchDisplay(query)
	text := strings.ToLower(display)
	if text == "" {
		return SearchResult{}, apiutil.InvalidArg("query is required")
	}
	q := SearchQuery{Text: text, Kind: kind, Limit: SearchLimit + 1, Candidates: searchCandidates}
	if parentID != uuid.Nil {
		if _, err := s.store.Get(ctx, parentID); err != nil {
			return SearchResult{}, notFound(err)
		}
		q.ParentIDs = []uuid.UUID{parentID}
	}
	plain, err := s.store.Search(ctx, q)
	if err != nil {
		return SearchResult{}, apiutil.Internal(err)
	}
	scoped, read, err := s.searchLocalities(ctx, display, parentID, kind)
	if err != nil {
		return SearchResult{}, apiutil.Internal(err)
	}
	ps, more := mergeSearch(plain, scoped)
	if slices.ContainsFunc(plain, func(p Place) bool { return p.namePrefix }) {
		read = nil
	}
	// One ancestors read fills the places' chains and the split locality's.
	chained := ps
	if read != nil {
		chained = append(slices.Clip(ps), Place{ID: read.locality.ID, ParentID: read.locality.ParentID})
	}
	if err := s.store.withAncestors(ctx, chained); err != nil {
		return SearchResult{}, apiutil.Internal(err)
	}
	out := SearchResult{Places: chained[:len(ps)], More: more}
	if read != nil {
		out.Split = &Split{
			Name:          read.name,
			LocalityID:    read.locality.ID,
			LocalityLabel: localityLabel(read.locality.DisplayName, chained[len(ps)].Ancestors),
		}
	}
	return out, nil
}

// searchDisplay collapses a query's whitespace and drops commas at either
// end ("starbucks," is "starbucks"), keeping its case; lowercased, it is the
// text searched.
func searchDisplay(query string) string {
	return normalizeName(strings.Trim(normalizeName(query), ", "))
}

// localityLabel is a locality's name and then its ancestors' names, nearest
// first: "Tampa, Florida". ancestors run from the state down.
func localityLabel(name string, ancestors []Ref) string {
	parts := []string{name}
	for _, a := range slices.Backward(ancestors) {
		parts = append(parts, a.Name)
	}
	return strings.Join(parts, ", ")
}

// splitRead is the split a search read: the name part as typed and the best
// locality found for it.
type splitRead struct {
	name     string
	locality Locality
}

// searchLocalities searches each split of display whose locality names a
// visible state, city or town (under parentID when it is set), up to
// maxSplitSearches of them, and returns their results in split order with
// the first of those splits and its best locality. display keeps the
// query's case; each split is searched lowercased.
func (s *Service) searchLocalities(ctx context.Context, display string, parentID uuid.UUID, kind perspectivv1.PlaceKind) ([][]Place, *splitRead, error) {
	splits := localitySplits(display)
	if len(splits) == 0 {
		return nil, nil, nil
	}
	found, inState, err := s.findLocalities(ctx, splits, parentID)
	if err != nil {
		return nil, nil, err
	}
	var lists [][]Place
	var read *splitRead
	for _, sp := range splits {
		locs := splitLocalities(sp, found, inState)
		if len(locs) == 0 {
			continue
		}
		if read == nil {
			read = &splitRead{name: sp.name, locality: locs[0]}
		}
		ids := make([]uuid.UUID, len(locs))
		for i, l := range locs {
			ids[i] = l.ID
		}
		ps, err := s.store.Search(ctx, SearchQuery{
			Text: strings.ToLower(sp.name), ParentIDs: ids, Kind: kind,
			Limit: SearchLimit + 1, Candidates: searchCandidates,
		})
		if err != nil {
			return nil, nil, err
		}
		lists = append(lists, ps)
		if len(lists) == maxSplitSearches {
			break
		}
	}
	return lists, read, nil
}

// findLocalities reads the localities splits name, under parentID when it
// is set: found holds the states and the localities named without one, and
// inState the localities named within a state, read under that state once
// found holds it. Every split names the same state, if any (localitySplits).
func (s *Service) findLocalities(ctx context.Context, splits []split, parentID uuid.UUID) (found, inState []Locality, err error) {
	var names, inStateNames []string
	for _, sp := range splits {
		switch {
		case sp.locality == "":
			names = append(names, strings.ToLower(sp.state))
		case sp.state != "":
			inStateNames = append(inStateNames, localityNames(strings.ToLower(sp.locality))...)
		default:
			names = append(names, localityNames(strings.ToLower(sp.locality))...)
		}
	}
	found, err = s.store.Localities(ctx, names, parentID)
	if err != nil || len(inStateNames) == 0 {
		return found, nil, err
	}
	state, ok := stateLocality(found, splits[len(splits)-1].state)
	if !ok {
		return found, nil, nil
	}
	inState, err = s.store.Localities(ctx, inStateNames, state.ID)
	return found, inState, err
}

// splitLocalities picks the localities a split names, best first: the
// state itself, the localities found within its state, or the localities
// found by name alone.
func splitLocalities(sp split, found, inState []Locality) []Locality {
	switch {
	case sp.locality == "":
		if l, ok := stateLocality(found, sp.state); ok {
			return []Locality{l}
		}
		return nil
	case sp.state != "":
		return localityMatches(inState, strings.ToLower(sp.locality))
	default:
		return localityMatches(found, strings.ToLower(sp.locality))
	}
}

// stateLocality finds in found the state named state: the locality of that
// name with no parent, so a city named for a state ("Washington") is not
// taken for it.
func stateLocality(found []Locality, state string) (Locality, bool) {
	name := strings.ToLower(state)
	i := slices.IndexFunc(found, func(l Locality) bool { return l.Name == name && l.ParentID == uuid.Nil })
	if i < 0 {
		return Locality{}, false
	}
	return found[i], true
}

// localityMatches picks from found, which is best first, up to
// maxLocalitiesPerName localities that locality (lowercased) names.
func localityMatches(found []Locality, locality string) []Locality {
	names := localityNames(locality)
	var out []Locality
	for _, l := range found {
		if slices.Contains(names, l.Name) && !slices.ContainsFunc(out, func(o Locality) bool { return o.ID == l.ID }) {
			out = append(out, l)
			if len(out) == maxLocalitiesPerName {
				break
			}
		}
	}
	return out
}

// abbreviations are the spellings of a word that start place names: "st
// petersburg" finds Saint Petersburg and "fort myers" finds Ft. Myers.
var abbreviations = [][3]string{
	{"st", "st.", "saint"},
	{"ft", "ft.", "fort"},
	{"mt", "mt.", "mount"},
}

// localityNames is locality and, when it has a word that abbreviations
// lists, the same name with each of the word's spellings.
func localityNames(locality string) []string {
	words := strings.Fields(locality)
	names := []string{locality}
	for form := range 3 {
		changed := slices.Clone(words)
		for i, w := range changed {
			for _, spellings := range abbreviations {
				if slices.Contains(spellings[:], w) {
					changed[i] = spellings[form]
				}
			}
		}
		if name := strings.Join(changed, " "); !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// localitySplits lists the readings of text as a name and a locality, most
// likely first, in text's own case. When text starts or ends with a state
// code (stateOf), the rest of it is read as townSplits reads a text, each
// locality within that state, and last as a name in the state itself:
// "publix tampa fl" tries Publix in Tampa, Florida before "publix tampa" in
// Florida. Otherwise the readings are townSplits'.
func localitySplits(text string) []split {
	state, rest, ok := stateOf(text)
	if !ok {
		return townSplits(text)
	}
	var out []split
	for _, sp := range townSplits(rest) {
		sp.state = state
		out = append(out, sp)
	}
	return append(out, split{name: nameWords(strings.Fields(strings.ReplaceAll(rest, ",", " "))), state: state})
}

// stateOf reads a USPS code (stateCodes), in any case and with or without
// a full stop, as the last word of text after a space or a comma, or as
// its first word when the code is not also a word (wordCodes), and returns
// the state's name and the rest of text, which is never empty: "Tampa,
// FL." is Florida and "Tampa"; "ok" alone and "in tampa" name no state.
func stateOf(text string) (state, rest string, ok bool) {
	if i := strings.LastIndexAny(text, " ,"); i >= 0 {
		name, found := stateCodes[codeKey(text[i+1:])]
		if r := strings.TrimRight(text[:i], " ,"); found && r != "" {
			return name, r, true
		}
	}
	if i := strings.IndexAny(text, " ,"); i >= 0 {
		code := codeKey(text[:i])
		name, found := stateCodes[code]
		if r := strings.TrimLeft(text[i+1:], " ,"); found && r != "" && !slices.Contains(wordCodes, code) {
			return name, r, true
		}
	}
	return "", "", false
}

// codeKey is a word as stateCodes keys it: lowercase, without a last full
// stop.
func codeKey(word string) string {
	return strings.ToLower(strings.TrimSuffix(word, "."))
}

// townSplits lists the readings of text as a name and a locality, most
// likely first, in text's own case. With a comma, the name comes before it
// and the locality after it ("starbucks, tampa, florida"), or the other
// way round. Without one, the locality is the last or the first one to
// maxLocalityWords words, longer first and the last words before the
// first: "starbucks st petersburg" tries "st petersburg" before
// "petersburg". A last name word "in", in any case, is dropped ("starbucks
// in tampa").
func townSplits(text string) []split {
	if before, after, ok := strings.Cut(text, ","); ok {
		town, _, _ := strings.Cut(after, ",")
		name, town := normalizeName(before), normalizeName(town)
		if name == "" || town == "" {
			return nil
		}
		return []split{{name: name, locality: town}, {name: town, locality: name}}
	}
	words := strings.Fields(text)
	var out []split
	for k := min(maxLocalityWords, len(words)-1); k >= 1; k-- {
		n := len(words) - k
		out = append(out,
			split{name: nameWords(words[:n]), locality: strings.Join(words[n:], " ")},
			split{name: strings.Join(words[k:], " "), locality: strings.Join(words[:k], " ")})
	}
	return out
}

// nameWords joins the words of a name read before its locality, without a
// last "in".
func nameWords(words []string) string {
	if len(words) > 1 && strings.EqualFold(words[len(words)-1], "in") {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

// mergeSearch orders the plain search's name-start matches first, then each
// locality search's matches in turn, then the plain search's other
// matches, each place once, and cuts the list at SearchLimit. Every search
// asks for one more than SearchLimit, so more says that some list held more
// than the cut keeps.
func mergeSearch(plain []Place, scoped [][]Place) ([]Place, bool) {
	var head, tail []Place
	for _, p := range plain {
		if p.namePrefix {
			head = append(head, p)
		} else {
			tail = append(tail, p)
		}
	}
	more := len(plain) > SearchLimit
	groups := append(append([][]Place{head}, scoped...), tail)
	seen := make(map[uuid.UUID]bool)
	out := make([]Place, 0, SearchLimit+1)
	for _, g := range groups {
		if len(g) > SearchLimit {
			more = true
		}
		for _, p := range g {
			if !seen[p.ID] {
				seen[p.ID] = true
				out = append(out, p)
			}
		}
	}
	if len(out) > SearchLimit {
		return out[:SearchLimit], true
	}
	return out, more
}
