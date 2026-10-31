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

// A search can name a place and the city or town it is in: "starbucks
// tampa", "tampa starbucks", "starbucks in tampa" or "starbucks, tampa".
// Each way of splitting the words into a name and a locality that names a
// visible city or town is searched under those localities, best locality
// first, beside the plain search of the whole text.
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
// is in.
type split struct {
	name, locality string
}

// SearchResult is one typeahead answer: at most SearchLimit places, and
// whether more match.
type SearchResult struct {
	Places []Place
	More   bool
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
	text := searchText(query)
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
	scoped, err := s.searchLocalities(ctx, text, parentID, kind)
	if err != nil {
		return SearchResult{}, apiutil.Internal(err)
	}
	ps, more := mergeSearch(plain, scoped)
	if err := s.store.withAncestors(ctx, ps); err != nil {
		return SearchResult{}, apiutil.Internal(err)
	}
	return SearchResult{Places: ps, More: more}, nil
}

// searchText lowercases a query, collapses its whitespace and drops commas
// at either end ("starbucks," is "starbucks").
func searchText(query string) string {
	return normalizeName(strings.Trim(strings.ToLower(normalizeName(query)), ", "))
}

// searchLocalities searches each split of text whose locality names a
// visible city or town (under parentID when it is set), up to
// maxSplitSearches of them, and returns their results in split order.
func (s *Service) searchLocalities(ctx context.Context, text string, parentID uuid.UUID, kind perspectivv1.PlaceKind) ([][]Place, error) {
	splits := localitySplits(text)
	if len(splits) == 0 {
		return nil, nil
	}
	var names []string
	for _, sp := range splits {
		names = append(names, localityNames(sp.locality)...)
	}
	found, err := s.store.Localities(ctx, names, parentID)
	if err != nil {
		return nil, err
	}
	var lists [][]Place
	for _, sp := range splits {
		ids := localityIDs(found, sp.locality)
		if len(ids) == 0 {
			continue
		}
		ps, err := s.store.Search(ctx, SearchQuery{
			Text: sp.name, ParentIDs: ids, Kind: kind,
			Limit: SearchLimit + 1, Candidates: searchCandidates,
		})
		if err != nil {
			return nil, err
		}
		lists = append(lists, ps)
		if len(lists) == maxSplitSearches {
			break
		}
	}
	return lists, nil
}

// localityIDs picks from found, which is best first, up to
// maxLocalitiesPerName localities that locality names.
func localityIDs(found []Locality, locality string) []uuid.UUID {
	names := localityNames(locality)
	var ids []uuid.UUID
	for _, l := range found {
		if slices.Contains(names, l.Name) && !slices.Contains(ids, l.ID) {
			ids = append(ids, l.ID)
			if len(ids) == maxLocalitiesPerName {
				break
			}
		}
	}
	return ids
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
// likely first. With a comma, the name comes before it and the locality
// after it ("starbucks, tampa, fl"), or the other way round. Without one,
// the locality is the last or the first one to maxLocalityWords words,
// longer first and the last words before the first: "starbucks st
// petersburg" tries "st petersburg" before "petersburg". A last name word
// "in" is dropped ("starbucks in tampa").
func localitySplits(text string) []split {
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
	if len(words) > 1 && words[len(words)-1] == "in" {
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
