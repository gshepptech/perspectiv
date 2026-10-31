package user

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/user/dbq"
)

// postExcerptLen is how many characters of a post the Thread posts tab
// quotes; ListUserPosts reads one more, so a cut can tell a post that ends
// there from one that goes on.
const postExcerptLen = 240

// dateLayout is how a place-local day goes on the wire.
const dateLayout = "2006-01-02"

// Post is one of a writer's lore thread posts as their profile lists it.
type Post struct {
	ID        uuid.UUID
	LoreID    uuid.UUID
	LoreName  string
	ArcID     uuid.UUID
	ArcTitle  string
	LocalDate string
	// Hour is nil for a day arc.
	Hour      *int32
	PlaceName string
	Excerpt   string
	Reply     bool
	CreatedAt time.Time
}

// ListUserPosts pages the writer's lore thread posts that a reader of the
// lore page sees, newest first. A deleted account, or an id no account
// holds, has none.
func (s *Service) ListUserPosts(ctx context.Context, author uuid.UUID, pageToken string, pageSize int32) ([]Post, string, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return nil, "", err
	}
	params := dbq.ListUserPostsParams{AuthorID: author, RowLimit: page.Limit + 1}
	if page.After != "" {
		k, err := parsePostKeyset(page.After)
		if err != nil {
			return nil, "", err
		}
		params.HasAfter, params.AfterAt, params.AfterID = true, k.createdAt, k.id
	}
	rows, err := s.q.ListUserPosts(ctx, params)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list user posts: %w", err))
	}
	var next string
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		last := rows[len(rows)-1]
		next = apiutil.NextPageToken(postKeyset{createdAt: last.CreatedAt, id: last.ID}.encode())
	}
	out := make([]Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, postOf(r))
	}
	return out, next, nil
}

func postOf(r dbq.ListUserPostsRow) Post {
	p := Post{
		ID:        r.ID,
		LoreID:    r.LoreID,
		LoreName:  r.LoreName,
		ArcID:     r.ArcID,
		ArcTitle:  r.ArcTitle,
		LocalDate: r.LocalDate.Time.Format(dateLayout),
		PlaceName: r.PlaceName,
		Excerpt:   postExcerpt(r.Opening),
		Reply:     r.Reply,
		CreatedAt: r.CreatedAt,
	}
	if r.Hour != nil {
		h := int32(*r.Hour)
		p.Hour = &h
	}
	return p
}

// postExcerpt cuts a post, whose whitespace the query folded, at the last
// word that fits and marks the cut with an ellipsis, so the tab never
// quotes half a word. A single word longer than half the limit is cut
// where the limit falls.
func postExcerpt(opening string) string {
	r := []rune(opening)
	if len(r) <= postExcerptLen {
		return opening
	}
	cut := postExcerptLen
	for i := postExcerptLen; i > postExcerptLen/2; i-- {
		if unicode.IsSpace(r[i]) {
			cut = i
			break
		}
	}
	return strings.TrimRightFunc(string(r[:cut]), func(c rune) bool { return unicode.IsSpace(c) || c == ',' || c == ';' || c == ':' }) + "…"
}

// postKeyset is ListUserPosts' position: the (created_at, id) of the last
// row of the previous page, encoded as "<unix nanoseconds>_<uuid>".
type postKeyset struct {
	createdAt time.Time
	id        uuid.UUID
}

func (k postKeyset) encode() string {
	return fmt.Sprintf("%d_%s", k.createdAt.UnixNano(), k.id)
}

func parsePostKeyset(s string) (postKeyset, error) {
	nanos, idText, ok := strings.Cut(s, "_")
	if !ok {
		return postKeyset{}, apiutil.InvalidArg("invalid page token")
	}
	n, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil {
		return postKeyset{}, apiutil.InvalidArg("invalid page token")
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return postKeyset{}, apiutil.InvalidArg("invalid page token")
	}
	return postKeyset{createdAt: time.Unix(0, n), id: id}, nil
}
