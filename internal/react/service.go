// Package react owns emoji reacts on perspectives. Any signed-in, verified
// user may hold up to one of each emoji in the fixed set of six on a
// perspective; participation in the arc is not required.
package react

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/react/dbq"
)

// Service is the react domain's logic over its store.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service writing through pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// React adds (on) or clears (!on) the caller's emoji on a perspective and
// returns the perspective's count for every emoji in the fixed set, in enum
// order, each marked with whether the caller holds it.
//
// Refusals, in order: InvalidArg for an emoji outside the set; NotFound for a
// perspective that does not exist or is not published, or is deleted or
// hidden; PermissionDenied when the perspective's author has blocked the
// caller and the caller is adding a react. The block code matches lore's for
// a blocked claim: the caller is allowed in but not to interact with that
// author. Clearing is allowed under a block, since it only withdraws an
// interaction the author did not want. Authors may react to their own
// perspectives. Adding a held react or clearing an absent one changes
// nothing.
func (s *Service) React(ctx context.Context, caller, perspectiveID uuid.UUID, emoji perspectivv1.Emoji, on bool) ([]*perspectivv1.ReactCount, error) {
	name, err := emojiToDB(emoji)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbq.New(tx)

	t, err := q.GetTarget(ctx, perspectiveID)
	visible := err == nil && t.Status == statusPublished && t.DeletedAt == nil && t.HiddenAt == nil
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !visible) {
		return nil, apiutil.NotFound("perspective not found")
	}
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read perspective: %w", err))
	}
	if err := write(ctx, q, t.AuthorID, caller, perspectiveID, name, on); err != nil {
		return nil, err
	}
	rows, err := q.CountReacts(ctx, dbq.CountReactsParams{PerspectiveID: perspectiveID, UserID: caller})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("count reacts: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apiutil.Internal(fmt.Errorf("commit: %w", err))
	}
	return countsOf(rows)
}

// write adds or clears one react, refusing an add by a user the author
// blocked.
func write(ctx context.Context, q *dbq.Queries, author, caller, perspectiveID uuid.UUID, emoji string, on bool) error {
	if !on {
		if err := q.DeleteReact(ctx, dbq.DeleteReactParams{PerspectiveID: perspectiveID, UserID: caller, Emoji: emoji}); err != nil {
			return apiutil.Internal(fmt.Errorf("clear react: %w", err))
		}
		return nil
	}
	blocked, err := q.IsBlocked(ctx, dbq.IsBlockedParams{BlockerID: author, BlockedID: caller})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read block: %w", err))
	}
	if blocked {
		return apiutil.PermissionDenied("you cannot react to this perspective")
	}
	if err := q.InsertReact(ctx, dbq.InsertReactParams{PerspectiveID: perspectiveID, UserID: caller, Emoji: emoji}); err != nil {
		return apiutil.Internal(fmt.Errorf("write react: %w", err))
	}
	return nil
}

// countsOf lists every emoji in the fixed set, in enum order, with its total
// from rows (zero when absent).
func countsOf(rows []dbq.CountReactsRow) ([]*perspectivv1.ReactCount, error) {
	byEmoji := make(map[perspectivv1.Emoji]dbq.CountReactsRow, len(rows))
	for _, r := range rows {
		e, err := emojiFromDB(r.Emoji)
		if err != nil {
			return nil, apiutil.Internal(err)
		}
		byEmoji[e] = r
	}
	out := make([]*perspectivv1.ReactCount, 0, len(emojis))
	for _, e := range emojis {
		r := byEmoji[e]
		out = append(out, &perspectivv1.ReactCount{Emoji: e, Count: r.Total, Mine: r.Mine})
	}
	return out, nil
}
