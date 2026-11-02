package place

import "perspectiv/internal/place/importer"

// stateCodes maps the USPS codes of the states, the District of Columbia
// and the territories the importer files places under to the state's
// lowercase name, so "starbucks fl" searches Florida. The importer's list is
// the one source of these.
var stateCodes = importer.StateNamesByCode()

// wordCodes are the codes that are also words a search may hold: "starbucks
// in tampa", "ok google", "mt dora" (Mount Dora). One is read as a state
// only as the last word after another, never as the first.
var wordCodes = []string{
	"al", "as", "co", "de", "hi", "id", "in", "la", "ma", "me", "mt", "oh", "ok", "or", "pa", "wa",
}
