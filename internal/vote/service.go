// Package vote owns up-votes and remove votes on perspectives.
//
// An up-vote is a toggle: a votes row exists while the caller holds the vote
// and clearing it deletes the row. There is no downvote. Only participants,
// users with a published perspective in the target's arc, may vote. Vote
// totals feed the rank job; this package never writes lore standing or canon
// state.
//
// A remove vote is cast by a member of the target's own lore. When the remove
// votes reach a majority of that lore's members excluding the target's
// author, the target is taken out of lore for good through the lore service.
package vote

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore"
	"perspectiv/internal/vote/dbq"
)

// MinRemoveLore is the smallest lore whose members may vote one of their own
// out.
const MinRemoveLore = 4

var statusPublished = perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_PUBLISHED.String()

// Notifier enqueues a domain event inside the caller's transaction;
// *jobs.Enqueuer satisfies it. Recipients are resolved by notify.
type Notifier interface {
	NotifyTx(ctx context.Context, tx pgx.Tx, a jobs.NotifyArgs) error
}

// Service is the vote domain's logic over its store.
type Service struct {
	pool   *pgxpool.Pool
	lore   *lore.Service
	notify Notifier
}

// NewService returns a Service writing through pool, reading lore membership
// and removing voted-out perspectives through lore, and enqueueing vote-out
// events through notify.
func NewService(pool *pgxpool.Pool, lore *lore.Service, notify Notifier) *Service {
	return &Service{pool: pool, lore: lore, notify: notify}
}

// VoteResult is the target's up-vote total after a Vote and whether the
// caller now holds a vote on it.
type VoteResult struct {
	Upvotes int32
	On      bool
}

// RemoveResult is the state of the remove vote on a target after a
// VoteRemove: the counted remove votes, the members eligible to vote, and
// whether this vote removed the target.
type RemoveResult struct {
	RemoveVotes    int32
	EligibleVoters int32
	Removed        bool
}

type target = dbq.GetTargetRow

// visible reports a published perspective that is neither deleted nor
// hidden; anything else is not found to a voter.
func visible(t target) bool {
	return t.Status == statusPublished && t.ArcID != nil && t.DeletedAt == nil && t.HiddenAt == nil
}

func readTarget(ctx context.Context, q *dbq.Queries, id uuid.UUID) (target, error) {
	t, err := q.GetTarget(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !visible(t)) {
		return target{}, apiutil.NotFound("perspective not found")
	}
	if err != nil {
		return target{}, apiutil.Internal(fmt.Errorf("read perspective: %w", err))
	}
	return t, nil
}

// blocked reports whether the target's author has blocked the caller.
//
// A vote or react refused because the author blocked the caller is
// PermissionDenied: the caller is signed in and verified but not allowed to
// interact with that author, the same code lore returns for a blocked claim.
// Reading is unaffected.
func blocked(ctx context.Context, q *dbq.Queries, author, caller uuid.UUID) error {
	b, err := q.IsBlocked(ctx, dbq.IsBlockedParams{BlockerID: author, BlockedID: caller})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read block: %w", err))
	}
	if b {
		return apiutil.PermissionDenied("you cannot vote on this perspective")
	}
	return nil
}

// Vote sets (on) or clears (!on) the caller's up-vote on a perspective and
// returns its up-vote total.
//
// Refusals, in order: NotFound for a perspective that does not exist or is
// not published, or is deleted or hidden; InvalidArg on the caller's own
// perspective; PermissionDenied when the caller has no published, visible
// perspective in the target's arc; PermissionDenied when the target's author
// has blocked the caller and the caller is adding a vote. Clearing is allowed
// under a block: it only withdraws an interaction the author did not want.
// Setting a held vote or clearing an absent one changes nothing.
func (s *Service) Vote(ctx context.Context, caller, perspectiveID uuid.UUID, on bool) (VoteResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VoteResult{}, apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbq.New(tx)

	t, err := readTarget(ctx, q, perspectiveID)
	if err != nil {
		return VoteResult{}, err
	}
	if t.AuthorID == caller {
		return VoteResult{}, apiutil.InvalidArg("you cannot vote on your own perspective")
	}
	participant, err := q.IsParticipant(ctx, dbq.IsParticipantParams{ArcID: *t.ArcID, AuthorID: caller, Published: statusPublished})
	if err != nil {
		return VoteResult{}, apiutil.Internal(fmt.Errorf("read participation: %w", err))
	}
	if !participant {
		return VoteResult{}, apiutil.PermissionDenied("only people in this arc can vote")
	}
	ids := dbq.InsertVoteParams{PerspectiveID: perspectiveID, UserID: caller}
	if on {
		if err := blocked(ctx, q, t.AuthorID, caller); err != nil {
			return VoteResult{}, err
		}
		err = q.InsertVote(ctx, ids)
	} else {
		err = q.DeleteVote(ctx, dbq.DeleteVoteParams(ids))
	}
	if err != nil {
		return VoteResult{}, apiutil.Internal(fmt.Errorf("write vote: %w", err))
	}
	n, err := q.CountVotes(ctx, perspectiveID)
	if err != nil {
		return VoteResult{}, apiutil.Internal(fmt.Errorf("count votes: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return VoteResult{}, apiutil.Internal(fmt.Errorf("commit: %w", err))
	}
	return VoteResult{Upvotes: n, On: on}, nil
}

// VoteRemove casts the caller's remove vote on a perspective in their lore.
//
// It runs under the lore domain's arc lock, so membership cannot change
// between the checks and the outcome. Refusals, in order: NotFound for a
// perspective that does not exist or is not published, or is deleted or
// hidden; FailedPrecondition when the target is not IN_LORE or its lore has
// fewer than MinRemoveLore members; PermissionDenied for the target's author,
// for anyone who is not a member of the target's lore, and for a member the
// author has blocked.
//
// Counting: eligible voters are the lore's current members excluding the
// author, and only remove votes held by current eligible members count, so a
// vote from someone who has since left the lore stops counting. A repeated
// vote by the same member is accepted and counted once.
//
// When the counted votes exceed half the eligible voters the target is
// removed (REMOVED, its claims rejected, the arc's lore recomputed) in the
// same transaction and Removed is true. Otherwise, when this vote is the
// first counted remove vote on the target, a VOTE_OUT_OPENED event is
// enqueued: the vote-out opens with its first vote, and later votes do not
// notify again. With MinRemoveLore members there are at least three eligible
// voters, so the first vote never removes on its own.
func (s *Service) VoteRemove(ctx context.Context, caller, perspectiveID uuid.UUID) (RemoveResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RemoveResult{}, apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbq.New(tx)

	t, err := readTarget(ctx, q, perspectiveID)
	if err != nil {
		return RemoveResult{}, err
	}
	// A published perspective never changes arc, so the arc read before the
	// lock is the one to lock.
	if err := q.LockArcLore(ctx, *t.ArcID); err != nil {
		return RemoveResult{}, apiutil.Internal(fmt.Errorf("lock arc: %w", err))
	}
	if t, err = readTarget(ctx, q, perspectiveID); err != nil {
		return RemoveResult{}, err
	}
	loreID, members, ok, err := s.lore.MembershipOf(ctx, tx, perspectiveID)
	if err != nil {
		return RemoveResult{}, err
	}
	if !ok {
		return RemoveResult{}, apiutil.FailedPrecondition("this perspective is not in a lore")
	}
	if len(members) < MinRemoveLore {
		return RemoveResult{}, apiutil.FailedPrecondition("not enough members to vote")
	}
	if caller == t.AuthorID {
		return RemoveResult{}, apiutil.PermissionDenied("you cannot vote out your own perspective")
	}
	eligible := eligibleVoters(members, t.AuthorID)
	if !slices.Contains(eligible, caller) {
		return RemoveResult{}, apiutil.PermissionDenied("only members of this lore can vote it out")
	}
	if err := blocked(ctx, q, t.AuthorID, caller); err != nil {
		return RemoveResult{}, err
	}

	before, err := q.CountRemoveVotes(ctx, dbq.CountRemoveVotesParams{PerspectiveID: perspectiveID, UserIds: eligible})
	if err != nil {
		return RemoveResult{}, apiutil.Internal(fmt.Errorf("count remove votes: %w", err))
	}
	if err := q.UpsertRemoveVote(ctx, dbq.UpsertRemoveVoteParams{PerspectiveID: perspectiveID, UserID: caller, LoreID: loreID}); err != nil {
		return RemoveResult{}, apiutil.Internal(fmt.Errorf("write remove vote: %w", err))
	}
	votes, err := q.CountRemoveVotes(ctx, dbq.CountRemoveVotesParams{PerspectiveID: perspectiveID, UserIds: eligible})
	if err != nil {
		return RemoveResult{}, apiutil.Internal(fmt.Errorf("count remove votes: %w", err))
	}
	res := RemoveResult{RemoveVotes: votes, EligibleVoters: int32(len(eligible))} //nolint:gosec // a lore has far fewer than 2^31 members
	switch {
	case majority(votes, res.EligibleVoters):
		if err := s.lore.RemoveFromLore(ctx, tx, perspectiveID); err != nil {
			return RemoveResult{}, err
		}
		res.Removed = true
	case before == 0 && votes == 1:
		if err := s.notify.NotifyTx(ctx, tx, jobs.NotifyArgs{
			Kind:          perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED,
			ActorID:       caller,
			ArcID:         *t.ArcID,
			PerspectiveID: perspectiveID,
			LoreID:        loreID,
		}); err != nil {
			return RemoveResult{}, apiutil.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return RemoveResult{}, apiutil.Internal(fmt.Errorf("commit: %w", err))
	}
	return res, nil
}

// eligibleVoters is the lore's member users excluding the target's author.
func eligibleVoters(members []lore.Member, author uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		if m.UserID != author {
			out = append(out, m.UserID)
		}
	}
	return out
}

// majority reports whether votes are more than half of eligible.
func majority(votes, eligible int32) bool {
	return eligible > 0 && 2*votes > eligible
}
