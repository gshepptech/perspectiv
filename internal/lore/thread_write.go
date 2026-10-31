package lore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore/dbq"
)

// maxPostLen is the longest thread post, in characters.
const maxPostLen = 1000

var errPostNotFound = apiutil.NotFound("post not found")

// postBody trims a post and checks its length.
func postBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	switch n := utf8.RuneCountInString(body); {
	case n == 0:
		return "", apiutil.InvalidArg("write something first")
	case n > maxPostLen:
		return "", apiutil.InvalidArg(fmt.Sprintf("keep a post to %d characters", maxPostLen))
	}
	return body, nil
}

// lockedPost is a thread post read under its row lock.
type lockedPost = dbq.LockLorePostRow

// lockPost locks one post; NotFound when there is none.
func lockPost(ctx context.Context, q *dbq.Queries, id uuid.UUID) (lockedPost, error) {
	p, err := q.LockLorePost(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedPost{}, errPostNotFound
	}
	if err != nil {
		return lockedPost{}, apiutil.Internal(fmt.Errorf("lock post: %w", err))
	}
	return p, nil
}

// readablePost reports a post readers see: not deleted, its author's account
// not deleted, and neither it nor its top-level post hidden.
func readablePost(p lockedPost) bool {
	return !p.DeletedAt.Valid && !p.AuthorDeletedAt.Valid && !p.HiddenAt.Valid && !p.BranchHidden
}

// canPost refuses a caller with no readable published perspective in the
// lore's arc, the rule for posting, replying and voting alike (A-012).
func canPost(ctx context.Context, q *dbq.Queries, loreID, caller uuid.UUID, refusal error) error {
	can, err := q.CanPostToLore(ctx, dbq.CanPostToLoreParams{LoreID: loreID, UserID: caller})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("check thread access: %w", err))
	}
	if !can {
		return refusal
	}
	return nil
}

// blockedBy refuses the caller when author has blocked them.
func blockedBy(ctx context.Context, q *dbq.Queries, author, caller uuid.UUID, refusal error) error {
	b, err := q.IsBlocked(ctx, dbq.IsBlockedParams{BlockerID: author, BlockedID: caller})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read block: %w", err))
	}
	if b {
		return refusal
	}
	return nil
}

// AddLorePost posts to a lore's thread, or, when replyTo is set, replies to
// that post. Only someone with a readable published perspective in the
// lore's arc may post; members have one.
//
// A reply to a top-level post sits under it; a reply to a reply sits under
// the same top-level post and records the reply it answered. Replying is
// refused with NotFound when the answered post is not in this lore or is no
// longer readable, and with PermissionDenied when its author has blocked the
// caller. A reply enqueues a THREAD_REPLY event for the answered post's
// author in the same transaction.
func (s *Service) AddLorePost(ctx context.Context, caller, loreID uuid.UUID, body string, replyTo uuid.UUID) (*perspectivv1.LorePost, error) {
	body, err := postBody(body)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		arcID, err := readableLore(ctx, q, loreID)
		if err != nil {
			return err
		}
		if err := canPost(ctx, q, loreID, caller, apiutil.PermissionDenied("only people who wrote about this day can post here")); err != nil {
			return err
		}
		params := dbq.InsertLorePostParams{LoreID: loreID, AuthorID: caller, Body: body}
		if replyTo != uuid.Nil {
			top, err := replyTarget(ctx, q, caller, loreID, replyTo)
			if err != nil {
				return err
			}
			params.ReplyToID = uuid.NullUUID{UUID: top, Valid: true}
			params.AnsweredID = uuid.NullUUID{UUID: replyTo, Valid: true}
		}
		id, err = q.InsertLorePost(ctx, params)
		if err != nil {
			return apiutil.Internal(fmt.Errorf("insert lore post: %w", err))
		}
		if replyTo == uuid.Nil {
			return nil
		}
		if err := s.notify.NotifyTx(ctx, tx, jobs.NotifyArgs{
			Kind:    perspectivv1.NotificationKind_NOTIFICATION_KIND_THREAD_REPLY,
			ActorID: caller, ArcID: arcID, LoreID: loreID, LorePostID: id,
		}); err != nil {
			return apiutil.Internal(fmt.Errorf("enqueue reply notification: %w", err))
		}
		return nil
	})
	if err != nil {
		return nil, typed(err, "add lore post")
	}
	return readPost(ctx, dbq.New(s.pool), caller, id)
}

// replyTarget checks the post being answered and returns the top-level post
// the reply sits under.
func replyTarget(ctx context.Context, q *dbq.Queries, caller, loreID, answered uuid.UUID) (uuid.UUID, error) {
	p, err := lockPost(ctx, q, answered)
	if err != nil {
		return uuid.Nil, err
	}
	if p.LoreID != loreID || !readablePost(p) {
		return uuid.Nil, errPostNotFound
	}
	if err := blockedBy(ctx, q, p.AuthorID, caller, apiutil.PermissionDenied("you cannot reply to this post")); err != nil {
		return uuid.Nil, err
	}
	if p.ReplyToID.Valid {
		return p.ReplyToID.UUID, nil
	}
	return p.ID, nil
}

// EditLorePost rewrites the caller's own post. An unchanged text is not an
// edit and leaves edited_at alone.
//
// Refusals: NotFound for an unknown or deleted post, or one in a lore no
// longer readable; PermissionDenied for someone else's post;
// FailedPrecondition for a post a moderator hid, alone or with its branch.
func (s *Service) EditLorePost(ctx context.Context, caller, postID uuid.UUID, body string) (*perspectivv1.LorePost, error) {
	body, err := postBody(body)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		p, err := lockPost(ctx, q, postID)
		if err != nil {
			return err
		}
		switch {
		case p.DeletedAt.Valid:
			return errPostNotFound
		case p.AuthorID != caller:
			return apiutil.PermissionDenied("you can only edit your own post")
		case p.HiddenAt.Valid || p.BranchHidden:
			return apiutil.FailedPrecondition("a moderator hid this post, so it cannot be edited")
		}
		if _, err := readableLore(ctx, q, p.LoreID); err != nil {
			return err
		}
		if err := q.EditLorePost(ctx, dbq.EditLorePostParams{ID: postID, Body: body}); err != nil {
			return apiutil.Internal(fmt.Errorf("edit lore post: %w", err))
		}
		return nil
	})
	if err != nil {
		return nil, typed(err, "edit lore post")
	}
	return readPost(ctx, dbq.New(s.pool), caller, postID)
}

// DeleteLorePost deletes the caller's own post. It is a soft delete: the
// thread keeps a deleted top-level post as "[deleted]" while it has readable
// replies, and drops it otherwise. Deleting is allowed even after a
// moderator hid the post, since it only takes text away.
//
// Refusals: NotFound for an unknown or already deleted post;
// PermissionDenied for someone else's.
func (s *Service) DeleteLorePost(ctx context.Context, caller, postID uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		p, err := lockPost(ctx, q, postID)
		if err != nil {
			return err
		}
		if p.DeletedAt.Valid {
			return errPostNotFound
		}
		if p.AuthorID != caller {
			return apiutil.PermissionDenied("you can only delete your own post")
		}
		if err := q.DeleteLorePost(ctx, postID); err != nil {
			return apiutil.Internal(fmt.Errorf("delete lore post: %w", err))
		}
		return nil
	})
	return typed(err, "delete lore post")
}

// VoteLorePost sets (on) or clears (!on) the caller's upvote on a thread
// post and returns its total. The post's row lock serialises votes on it, so
// the stored total is always its vote rows' count.
//
// Refusals, in order, as for a story's upvote: NotFound for a post that is
// unknown or not readable, or in a lore no longer readable; InvalidArg on
// the caller's own post; PermissionDenied when the caller has no readable
// published perspective in the arc; PermissionDenied when the post's author
// has blocked the caller and the caller is adding a vote. Setting a held
// vote or clearing an absent one changes nothing.
func (s *Service) VoteLorePost(ctx context.Context, caller, postID uuid.UUID, on bool) (int32, error) {
	var upvotes int32
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		p, err := lockPost(ctx, q, postID)
		if err != nil {
			return err
		}
		if !readablePost(p) {
			return errPostNotFound
		}
		if _, err := readableLore(ctx, q, p.LoreID); err != nil {
			return err
		}
		if p.AuthorID == caller {
			return apiutil.InvalidArg("you cannot upvote your own post")
		}
		if err := canPost(ctx, q, p.LoreID, caller, apiutil.PermissionDenied("only people who wrote about this day can upvote here")); err != nil {
			return err
		}
		if err := writePostVote(ctx, q, p, caller, on); err != nil {
			return err
		}
		upvotes, err = q.RecountPostVotes(ctx, postID)
		if err != nil {
			return apiutil.Internal(fmt.Errorf("count post votes: %w", err))
		}
		return nil
	})
	if err != nil {
		return 0, typed(err, "vote on lore post")
	}
	return upvotes, nil
}

func writePostVote(ctx context.Context, q *dbq.Queries, p lockedPost, caller uuid.UUID, on bool) error {
	ids := dbq.InsertPostVoteParams{PostID: p.ID, UserID: caller}
	var err error
	if on {
		if err := blockedBy(ctx, q, p.AuthorID, caller, apiutil.PermissionDenied("you cannot upvote this post")); err != nil {
			return err
		}
		err = q.InsertPostVote(ctx, ids)
	} else {
		err = q.DeletePostVote(ctx, dbq.DeletePostVoteParams(ids))
	}
	if err != nil {
		return apiutil.Internal(fmt.Errorf("write post vote: %w", err))
	}
	return nil
}

// typed passes a domain error through and wraps anything else (a failed
// begin or commit) as Internal.
func typed(err error, op string) error {
	if err == nil {
		return nil
	}
	if _, ok := apiutil.CodeOf(err); ok {
		return err
	}
	return apiutil.Internal(fmt.Errorf("%s: %w", op, err))
}
