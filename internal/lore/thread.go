package lore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/lore/dbq"
)

// maxPostLen is the longest thread post, in characters.
const maxPostLen = 1000

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

// LorePosts pages a lore's thread, oldest first.
func (s *Service) LorePosts(ctx context.Context, loreID uuid.UUID, pageToken string, pageSize int32) ([]*perspectivv1.LorePost, string, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return nil, "", err
	}
	q := dbq.New(s.pool)
	if _, err := readableLore(ctx, q, loreID); err != nil {
		return nil, "", err
	}
	params := dbq.ListLorePostsParams{LoreID: loreID, RowLimit: page.Limit + 1}
	if page.After != "" {
		atText, idText, ok := strings.Cut(page.After, "|")
		at, terr := time.Parse(time.RFC3339Nano, atText)
		id, ierr := uuid.Parse(idText)
		if !ok || terr != nil || ierr != nil {
			return nil, "", apiutil.InvalidArg("invalid page token")
		}
		params.AfterAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.AfterID = uuid.NullUUID{UUID: id, Valid: true}
	}
	rows, err := q.ListLorePosts(ctx, params)
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list lore posts: %w", err))
	}
	next := ""
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		last := rows[len(rows)-1]
		next = apiutil.NextPageToken(last.CreatedAt.Time.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String())
	}
	out := make([]*perspectivv1.LorePost, 0, len(rows))
	for _, r := range rows {
		out = append(out, &perspectivv1.LorePost{
			Id:        r.ID.String(),
			Author:    &perspectivv1.UserRef{UserId: r.AuthorID.String(), Username: r.Username},
			Body:      r.Body,
			CreatedAt: timestampFromDB(r.CreatedAt),
			ByMember:  r.ByMember,
		})
	}
	return out, next, nil
}

// AddLorePost posts to a lore's thread. Only someone with a readable
// published perspective in the lore's arc may post; members have one.
func (s *Service) AddLorePost(ctx context.Context, caller, loreID uuid.UUID, body string) (*perspectivv1.LorePost, error) {
	body = strings.TrimSpace(body)
	switch n := utf8.RuneCountInString(body); {
	case n == 0:
		return nil, apiutil.InvalidArg("write something first")
	case n > maxPostLen:
		return nil, apiutil.InvalidArg(fmt.Sprintf("keep a post to %d characters", maxPostLen))
	}
	q := dbq.New(s.pool)
	if _, err := readableLore(ctx, q, loreID); err != nil {
		return nil, err
	}
	can, err := q.CanPostToLore(ctx, dbq.CanPostToLoreParams{LoreID: loreID, UserID: caller})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("check thread access: %w", err))
	}
	if !can {
		return nil, apiutil.PermissionDenied("only people with a perspective in this arc can post here")
	}
	row, err := q.InsertLorePost(ctx, dbq.InsertLorePostParams{LoreID: loreID, AuthorID: caller, Body: body})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("insert lore post: %w", err))
	}
	r, err := q.GetLorePost(ctx, row.ID)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read new post: %w", err))
	}
	return &perspectivv1.LorePost{
		Id:        r.ID.String(),
		Author:    &perspectivv1.UserRef{UserId: r.AuthorID.String(), Username: r.Username},
		Body:      r.Body,
		CreatedAt: timestampFromDB(r.CreatedAt),
		ByMember:  r.ByMember,
	}, nil
}

// maxNameLen bounds a lore name, as lores_name_check does.
const maxNameLen = 60

// NameLore sets a lore's name. Only a member of the lore may name it, and
// the latest name wins. Runs of whitespace collapse to one space.
func (s *Service) NameLore(ctx context.Context, caller, loreID uuid.UUID, name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		return "", apiutil.InvalidArg("give the lore a name")
	case n > maxNameLen:
		return "", apiutil.InvalidArg(fmt.Sprintf("keep a name to %d characters", maxNameLen))
	}
	q := dbq.New(s.pool)
	if _, err := readableLore(ctx, q, loreID); err != nil {
		return "", err
	}
	member, err := q.IsLoreMember(ctx, dbq.IsLoreMemberParams{LoreID: loreID, UserID: caller})
	if err != nil {
		return "", apiutil.Internal(fmt.Errorf("check lore membership: %w", err))
	}
	if !member {
		return "", apiutil.PermissionDenied("only the people in this lore can name it")
	}
	if err := q.SetLoreName(ctx, dbq.SetLoreNameParams{ID: loreID, Name: name, NamedBy: caller}); err != nil {
		return "", apiutil.Internal(fmt.Errorf("name lore: %w", err))
	}
	return name, nil
}
