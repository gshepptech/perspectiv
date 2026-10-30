package perspective

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/perspective/dbq"
)

// Save saves the perspective id for caller to read later when on is true,
// or unsaves it, and returns whether it is now saved. Either is a no-op when
// already so. The perspective must be readable to the caller (see
// readable); otherwise the result is NotFound, for an unsave too, so a
// story's state never leaks through this call.
func (s *Service) Save(ctx context.Context, caller, id uuid.UUID, on bool) (bool, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		rows, err := q.GetPerspectives(ctx, dbq.GetPerspectivesParams{Viewer: &caller, Ids: []uuid.UUID{id}})
		if err != nil {
			return apiutil.Internal(fmt.Errorf("read perspective: %w", err))
		}
		if len(rows) == 0 || !readable(rows[0]) {
			return errNotFound
		}
		args := dbq.SavePerspectiveParams{UserID: caller, PerspectiveID: id}
		if on {
			err = q.SavePerspective(ctx, args)
		} else {
			err = q.UnsavePerspective(ctx, dbq.UnsavePerspectiveParams(args))
		}
		if err != nil {
			return apiutil.Internal(fmt.Errorf("save perspective: %w", err))
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return on, nil
}

// readable is whether anyone may read the perspective a row describes: it is
// published, neither deleted nor hidden, and not filed at or below a hidden
// place. Get shows anything else as a placeholder (or NotFound for a draft).
func readable(r dbq.GetPerspectivesRow) bool {
	return r.Status == statusPublished && r.DeletedAt == nil && r.HiddenAt == nil && !r.PlaceHidden
}

// ListSaved pages the caller's saved perspectives that are readable now, most
// recently saved first, each as ListByAuthor returns it. A saved perspective
// that is not readable is left out but stays saved, so it returns once it is
// restored. It returns the page and the next page token, empty on the last
// page.
func (s *Service) ListSaved(ctx context.Context, caller uuid.UUID, pageToken string, pageSize int32) ([]*perspectivv1.Perspective, string, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return nil, "", err
	}
	params := dbq.ListSavedParams{UserID: caller, RowLimit: page.Limit + 1}
	if page.After != "" {
		at, id, err := parseCursor(page.After)
		if err != nil {
			return nil, "", err
		}
		params.AfterAt, params.AfterID = &at, &id
	}
	var (
		out  []*perspectivv1.Perspective
		next string
	)
	err = s.readTx(ctx, func(q *dbq.Queries) error {
		rows, err := q.ListSaved(ctx, params)
		if err != nil {
			return apiutil.Internal(fmt.Errorf("list saved perspectives: %w", err))
		}
		if len(rows) > int(page.Limit) {
			rows = rows[:page.Limit]
			last := rows[len(rows)-1]
			next = apiutil.NextPageToken(last.SavedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String())
		}
		ids := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		out, err = load(ctx, q, caller, ids)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	return out, next, nil
}
