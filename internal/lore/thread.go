package lore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/lore/dbq"
)

// A lore's thread is two levels deep: top-level posts, and replies under
// them. A reply to a reply sits under the same top-level post and records
// which post it answered. Top-level posts are listed by score (TOP) or
// newest first (NEW); replies always oldest first, the first repliesShown
// of them inline with their post.

// repliesShown is how many replies come inline with each top-level post.
const repliesShown = 3

// readableLore returns the lore's arc, or NotFound when the lore is unknown
// or its arc is at or below a hidden place.
func readableLore(ctx context.Context, q *dbq.Queries, loreID uuid.UUID) (uuid.UUID, error) {
	row, err := q.GetLore(ctx, loreID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apiutil.NotFound("lore not found")
	}
	if err != nil {
		return uuid.Nil, apiutil.Internal(fmt.Errorf("read lore: %w", err))
	}
	if err := refuseHiddenArc(ctx, q, row.ArcID, apiutil.NotFound("lore not found")); err != nil {
		return uuid.Nil, err
	}
	return row.ArcID, nil
}

// threadKeyset is a thread page's position: the last row of the previous
// page. tag says which listing minted it ("top", "new" or "reply"), so a
// token never continues a different order. upvotes is read only by "top".
type threadKeyset struct {
	tag     string
	upvotes int32
	at      time.Time
	id      uuid.UUID
}

func keysetOf(tag string, r dbq.LoreThreadPost) threadKeyset {
	return threadKeyset{tag: tag, upvotes: r.Upvotes, at: r.CreatedAt.Time, id: r.ID}
}

func (k threadKeyset) encode() string {
	return fmt.Sprintf("%s|%d|%d|%s", k.tag, k.upvotes, k.at.UnixNano(), k.id)
}

// decodeThreadKeyset parses after, which must have been minted with tag. An
// empty after is the first page and comes back as nil.
func decodeThreadKeyset(after, tag string) (*threadKeyset, error) {
	if after == "" {
		return nil, nil
	}
	bad := apiutil.InvalidArg("invalid page token")
	parts := strings.Split(after, "|")
	if len(parts) != 4 || parts[0] != tag {
		return nil, bad
	}
	upvotes, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return nil, bad
	}
	nanos, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, bad
	}
	id, err := uuid.Parse(parts[3])
	if err != nil {
		return nil, bad
	}
	return &threadKeyset{tag: tag, upvotes: int32(upvotes), at: time.Unix(0, nanos), id: id}, nil
}

// sortTag maps the requested order to its keyset tag. UNSPECIFIED is TOP.
func sortTag(sort perspectivv1.LorePostSort) (string, error) {
	switch sort {
	case perspectivv1.LorePostSort_LORE_POST_SORT_UNSPECIFIED, perspectivv1.LorePostSort_LORE_POST_SORT_TOP:
		return "top", nil
	case perspectivv1.LorePostSort_LORE_POST_SORT_NEW:
		return "new", nil
	}
	return "", apiutil.InvalidArg("unknown sort")
}

// postFromRow builds the wire post. A removed post keeps only its place in
// the thread: no author, body or upvotes.
func postFromRow(r dbq.LoreThreadPost) *perspectivv1.LorePost {
	p := &perspectivv1.LorePost{
		Id:         r.ID.String(),
		CreatedAt:  timestampFromDB(r.CreatedAt),
		ReplyCount: r.ReplyCount,
	}
	if r.ReplyToID.Valid {
		p.ReplyToId = r.ReplyToID.UUID.String()
	}
	if r.Removed {
		p.Deleted = true
		return p
	}
	p.Author = &perspectivv1.UserRef{UserId: r.AuthorID.String(), Username: r.Username}
	p.Body = r.Body
	p.ByMember = r.ByMember
	p.EditedAt = timestampFromDB(r.EditedAt)
	p.Upvotes = r.Upvotes
	if r.ReplyingTo != "" {
		p.ReplyingTo = &perspectivv1.UserRef{Username: r.ReplyingTo}
	}
	return p
}

// LorePosts pages a lore's top-level posts in the requested order, each
// with its reply count and first replies. viewer is uuid.Nil when signed
// out; otherwise each post says whether the viewer upvoted it.
func (s *Service) LorePosts(ctx context.Context, viewer, loreID uuid.UUID, sort perspectivv1.LorePostSort, pageToken string, pageSize int32) ([]*perspectivv1.LorePost, string, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return nil, "", err
	}
	tag, err := sortTag(sort)
	if err != nil {
		return nil, "", err
	}
	after, err := decodeThreadKeyset(page.After, tag)
	if err != nil {
		return nil, "", err
	}
	q := dbq.New(s.pool)
	if _, err := readableLore(ctx, q, loreID); err != nil {
		return nil, "", err
	}
	rows, err := listTopLevel(ctx, q, loreID, tag, after, page.Limit+1)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list lore posts: %w", err))
	}
	next := ""
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		next = apiutil.NextPageToken(keysetOf(tag, rows[len(rows)-1]).encode())
	}
	posts, err := withFirstReplies(ctx, q, rows)
	if err != nil {
		return nil, "", err
	}
	if err := markViewerVotes(ctx, q, viewer, posts); err != nil {
		return nil, "", err
	}
	return posts, next, nil
}

func listTopLevel(ctx context.Context, q *dbq.Queries, loreID uuid.UUID, tag string, after *threadKeyset, limit int32) ([]dbq.LoreThreadPost, error) {
	if tag == "new" {
		params := dbq.ListThreadNewParams{LoreID: loreID, RowLimit: limit}
		if after != nil {
			params.HasAfter, params.AfterAt, params.AfterID = true, pgtype.Timestamptz{Time: after.at, Valid: true}, after.id
		}
		return q.ListThreadNew(ctx, params)
	}
	params := dbq.ListThreadTopParams{LoreID: loreID, RowLimit: limit}
	if after != nil {
		params.HasAfter, params.AfterUpvotes = true, after.upvotes
		params.AfterAt, params.AfterID = pgtype.Timestamptz{Time: after.at, Valid: true}, after.id
	}
	return q.ListThreadTop(ctx, params)
}

// withFirstReplies converts the top-level rows and hangs the first
// repliesShown replies under each, with the token for the rest.
func withFirstReplies(ctx context.Context, q *dbq.Queries, rows []dbq.LoreThreadPost) ([]*perspectivv1.LorePost, error) {
	posts := make([]*perspectivv1.LorePost, 0, len(rows))
	byID := make(map[uuid.UUID]*perspectivv1.LorePost, len(rows))
	var withReplies []uuid.UUID
	for _, r := range rows {
		p := postFromRow(r)
		posts = append(posts, p)
		byID[r.ID] = p
		if r.ReplyCount > 0 {
			withReplies = append(withReplies, r.ID)
		}
	}
	if len(withReplies) == 0 {
		return posts, nil
	}
	replies, err := q.ListFirstReplies(ctx, dbq.ListFirstRepliesParams{PostIds: withReplies, PerPost: repliesShown})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list first replies: %w", err))
	}
	last := make(map[uuid.UUID]dbq.LoreThreadPost, len(withReplies))
	for _, r := range replies {
		parent := byID[r.ReplyToID.UUID]
		parent.Replies = append(parent.Replies, postFromRow(r))
		last[r.ReplyToID.UUID] = r
	}
	for id, r := range last {
		if p := byID[id]; int(p.GetReplyCount()) > len(p.GetReplies()) {
			p.MoreRepliesToken = apiutil.NextPageToken(keysetOf("reply", r).encode())
		}
	}
	return posts, nil
}

// markViewerVotes sets viewer_upvoted on the posts and their inline replies
// that the viewer upvoted. A signed-out viewer has none.
func markViewerVotes(ctx context.Context, q *dbq.Queries, viewer uuid.UUID, posts []*perspectivv1.LorePost) error {
	if viewer == uuid.Nil || len(posts) == 0 {
		return nil
	}
	byID := map[uuid.UUID]*perspectivv1.LorePost{}
	var ids []uuid.UUID
	add := func(p *perspectivv1.LorePost) {
		id := uuid.MustParse(p.GetId())
		byID[id] = p
		ids = append(ids, id)
	}
	for _, p := range posts {
		add(p)
		for _, r := range p.GetReplies() {
			add(r)
		}
	}
	voted, err := q.ViewerUpvoted(ctx, dbq.ViewerUpvotedParams{UserID: viewer, PostIds: ids})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read viewer votes: %w", err))
	}
	for _, id := range voted {
		if p := byID[id]; !p.GetDeleted() {
			p.ViewerUpvoted = true
		}
	}
	return nil
}

// LorePostReplies pages the readable replies under a top-level post, oldest
// first. NotFound when the post is unknown, hidden, or in a lore no longer
// readable; InvalidArg when it is itself a reply.
func (s *Service) LorePostReplies(ctx context.Context, viewer, postID uuid.UUID, pageToken string, pageSize int32) ([]*perspectivv1.LorePost, string, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return nil, "", err
	}
	after, err := decodeThreadKeyset(page.After, "reply")
	if err != nil {
		return nil, "", err
	}
	q := dbq.New(s.pool)
	top, err := q.GetThreadPost(ctx, postID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && top.HiddenAt.Valid) {
		return nil, "", apiutil.NotFound("post not found")
	}
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("read post: %w", err))
	}
	if top.ReplyToID.Valid {
		return nil, "", apiutil.InvalidArg("only a top-level post has replies")
	}
	if _, err := readableLore(ctx, q, top.LoreID); err != nil {
		return nil, "", err
	}
	params := dbq.ListRepliesParams{PostID: postID, RowLimit: page.Limit + 1}
	if after != nil {
		params.HasAfter, params.AfterAt, params.AfterID = true, pgtype.Timestamptz{Time: after.at, Valid: true}, after.id
	}
	rows, err := q.ListReplies(ctx, params)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list replies: %w", err))
	}
	next := ""
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		next = apiutil.NextPageToken(keysetOf("reply", rows[len(rows)-1]).encode())
	}
	replies := make([]*perspectivv1.LorePost, 0, len(rows))
	for _, r := range rows {
		replies = append(replies, postFromRow(r))
	}
	if err := markViewerVotes(ctx, q, viewer, replies); err != nil {
		return nil, "", err
	}
	return replies, next, nil
}

// readPost returns one post as the viewer sees it, without its replies.
func readPost(ctx context.Context, q *dbq.Queries, viewer, id uuid.UUID) (*perspectivv1.LorePost, error) {
	r, err := q.GetThreadPost(ctx, id)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read post: %w", err))
	}
	p := postFromRow(r)
	if err := markViewerVotes(ctx, q, viewer, []*perspectivv1.LorePost{p}); err != nil {
		return nil, err
	}
	return p, nil
}
