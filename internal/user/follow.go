package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/user/dbq"
)

// maxFollowedListed caps ListFollowedWriters, newest follow first.
const maxFollowedListed = 100

// FollowedWriter is a writer the caller follows, as their profile shows them.
type FollowedWriter struct {
	UserID     uuid.UUID
	Username   string
	AvatarURL  string
	FollowedAt time.Time
}

// FollowUser follows (on) or unfollows (!on) the writer named username for
// the caller and returns whether the caller now follows them. Either is a
// no-op when already so.
//
// Refusals: NotFound for a username no active account holds (a deleted
// account has given its name up), InvalidArg for the caller's own name, and
// PermissionDenied for a follow when either has blocked the other, the code
// claims, votes and reacts return for a block. Unfollowing is always
// allowed. A suspended writer may be followed: their profile stays public.
func (s *Service) FollowUser(ctx context.Context, caller uuid.UUID, username string, on bool) (bool, error) {
	if err := s.requireActive(ctx, s.q, caller); err != nil {
		return false, err
	}
	target, err := s.q.GetActiveUserIDByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, apiutil.NotFound("user not found")
	}
	if err != nil {
		return false, apiutil.Internal(fmt.Errorf("find user by username: %w", err))
	}
	if target == caller {
		return false, apiutil.InvalidArg("you cannot follow yourself")
	}
	if !on {
		if err := s.q.UnfollowUser(ctx, dbq.UnfollowUserParams{FollowerID: caller, FolloweeID: target}); err != nil {
			return false, apiutil.Internal(fmt.Errorf("unfollow user: %w", err))
		}
		return false, nil
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockPair(ctx, dbq.LockPairParams{A: caller, B: target}); err != nil {
			return apiutil.Internal(fmt.Errorf("lock pair: %w", err))
		}
		blocked, err := q.BlockedEitherWay(ctx, dbq.BlockedEitherWayParams{A: caller, B: target})
		if err != nil {
			return apiutil.Internal(fmt.Errorf("read block: %w", err))
		}
		if blocked {
			return apiutil.PermissionDenied("you cannot follow this writer")
		}
		if err := q.FollowUser(ctx, dbq.FollowUserParams{FollowerID: caller, FolloweeID: target}); err != nil {
			return apiutil.Internal(fmt.Errorf("follow user: %w", err))
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListFollowedWriters returns the writers the caller follows, most recently
// followed first, at most maxFollowedListed. Deleted accounts are left out.
func (s *Service) ListFollowedWriters(ctx context.Context, caller uuid.UUID) ([]FollowedWriter, error) {
	if err := s.requireActive(ctx, s.q, caller); err != nil {
		return nil, err
	}
	rows, err := s.q.ListFollowedWriters(ctx, dbq.ListFollowedWritersParams{FollowerID: caller, MaxRows: maxFollowedListed})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list followed writers: %w", err))
	}
	out := make([]FollowedWriter, 0, len(rows))
	for _, r := range rows {
		out = append(out, FollowedWriter{
			UserID: r.ID, Username: r.Username, AvatarURL: avatarURL(r.AvatarPath), FollowedAt: r.FollowedAt,
		})
	}
	return out, nil
}

// followInfo sets a live profile's follower count and, for a signed-in
// caller looking at someone else, whether the caller follows them.
func (s *Service) followInfo(ctx context.Context, caller *uuid.UUID, p *Profile) error {
	n, err := s.q.CountFollowers(ctx, p.UserID)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("count followers: %w", err))
	}
	p.Followers = n
	if caller == nil || *caller == p.UserID {
		return nil
	}
	p.Following, err = s.q.IsFollowingUser(ctx, dbq.IsFollowingUserParams{FollowerID: *caller, FolloweeID: p.UserID})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read follow: %w", err))
	}
	return nil
}
