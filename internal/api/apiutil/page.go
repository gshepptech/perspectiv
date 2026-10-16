package apiutil

import (
	"encoding/base64"
	"strings"
)

// Page size bounds shared by every list endpoint.
const (
	DefaultPageLimit int32 = 20
	MaxPageLimit     int32 = 100
)

// pageTokenPrefix marks a token this package minted. A token that does not
// decode to it is rejected instead of being handed to a domain as a keyset.
const pageTokenPrefix = "p1:"

// PageToken is a parsed page request. After is the domain's own keyset
// encoding of the last row of the previous page ("" for the first page);
// apiutil never interprets it.
type PageToken struct {
	After string
	Limit int32
}

// ParsePageToken parses a client's page token and page size.
//
// A limit of zero or less means the default of 20; a limit above 100 is
// clamped to 100 rather than rejected, so a client asking for "as many as
// possible" still gets a page. An empty token is the first page. A token that
// was not minted by NextPageToken is InvalidArg.
func ParsePageToken(token string, limit int32) (PageToken, error) {
	switch {
	case limit <= 0:
		limit = DefaultPageLimit
	case limit > MaxPageLimit:
		limit = MaxPageLimit
	}
	if token == "" {
		return PageToken{Limit: limit}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return PageToken{}, InvalidArg("invalid page token")
	}
	after, ok := strings.CutPrefix(string(raw), pageTokenPrefix)
	if !ok || after == "" {
		return PageToken{}, InvalidArg("invalid page token")
	}
	return PageToken{After: after, Limit: limit}, nil
}

// NextPageToken mints the token for the page after the row whose keyset is
// after. It returns "" when after is "", meaning there is no next page.
func NextPageToken(after string) string {
	if after == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(pageTokenPrefix + after))
}
