package jobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/jobs/dbq"
)

// expiryWorker moves a perspective from PENDING to SOLO when its expiry job
// fires and it has no accepted claim in either direction.
type expiryWorker struct {
	river.WorkerDefaults[ExpiryArgs]
	pool *pgxpool.Pool
}

// NewExpiryWorker returns the worker for ExpiryArgs jobs.
func NewExpiryWorker(pool *pgxpool.Pool) river.Worker[ExpiryArgs] {
	return &expiryWorker{pool: pool}
}

// Work reclassifies the perspective in place: it stays readable at its place
// and day, and becomes a SOLO tale outside lore and canon. Pending claims and
// other perspectives in the arc do not keep it PENDING; only an ACCEPTED claim
// on one of its characters or made by it does.
//
// It completes without a change when the perspective is not PENDING (already
// in lore, solo or removed), when the row does not exist, and when the
// perspective was deleted or hidden before the job fired. A deleted or hidden
// perspective is out of every listing and its lore inputs were already
// withdrawn by the delete or hide; reclassifying it would dirty the arc for a
// perspective nobody can see. It stays as the delete or hide left it.
//
// Every change happens in one transaction with the perspective row locked,
// so the job is safe to run twice.
func (w *expiryWorker) Work(ctx context.Context, job *river.Job[ExpiryArgs]) error {
	err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		return expire(ctx, dbq.New(tx), job.Args)
	})
	if err != nil {
		return fmt.Errorf("expire perspective %s: %w", job.Args.PerspectiveID, err)
	}
	return nil
}

func expire(ctx context.Context, q *dbq.Queries, args ExpiryArgs) error {
	pending := loreStatusText(perspectivv1.LoreStatus_LORE_STATUS_PENDING)
	row, err := q.LockPerspectiveForExpiry(ctx, args.PerspectiveID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock perspective: %w", err)
	}
	if row.LoreStatus.String != pending || row.DeletedAt.Valid || row.HiddenAt.Valid || row.ArcID == nil {
		return nil
	}

	accepted, err := q.HasAcceptedClaim(ctx, dbq.HasAcceptedClaimParams{
		AcceptedStatus: claimStatusText(perspectivv1.ClaimStatus_CLAIM_STATUS_ACCEPTED),
		PerspectiveID:  args.PerspectiveID,
	})
	if err != nil {
		return fmt.Errorf("check accepted claims: %w", err)
	}
	if accepted {
		return nil
	}

	if err := q.TransitionLoreStatus(ctx, dbq.TransitionLoreStatusParams{
		ID:         args.PerspectiveID,
		FromStatus: pending,
		ToStatus:   loreStatusText(perspectivv1.LoreStatus_LORE_STATUS_SOLO),
	}); err != nil {
		return fmt.Errorf("mark solo: %w", err)
	}
	if err := q.MarkArcLoreDirty(ctx, *row.ArcID); err != nil {
		return fmt.Errorf("mark arc lore dirty: %w", err)
	}
	return nil
}
