package arc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc/dbq"
)

// FindDayArc says what already exists for a place on a day: the day arc
// with its story count and each hour arc with its own, so a writer can join
// the people who were there instead of starting an arc of their own. A day
// nobody has written yet comes back empty; a hidden place is NotFound.
func (s *Service) FindDayArc(ctx context.Context, viewer, placeID uuid.UUID, day time.Time) (*perspectivv1.FindDayArcResponse, error) {
	q := dbq.New(s.pool)
	place, err := q.GetPlaceForArc(ctx, placeID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && place.SubtreeHidden) {
		return nil, apiutil.NotFound("place not found")
	}
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read place: %w", err))
	}
	params := dbq.DayArcStoriesParams{PlaceID: placeID, LocalDate: dateToDB(day)}
	if viewer != uuid.Nil {
		params.Viewer = uuid.NullUUID{UUID: viewer, Valid: true}
	}
	rows, err := q.DayArcStories(ctx, params)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read day arcs: %w", err))
	}
	out := &perspectivv1.FindDayArcResponse{}
	for _, r := range rows {
		mine := ""
		if r.Mine != uuid.Nil {
			mine = r.Mine.String()
		}
		if !r.Hour.Valid {
			out.ArcId, out.Title, out.PerspectiveCount, out.MinePerspectiveId = r.ID.String(), r.Title, r.Stories, mine
			continue
		}
		out.Hours = append(out.Hours, &perspectivv1.DayArcHour{
			Hour: int32(r.Hour.Int16), ArcId: r.ID.String(), Title: r.Title, PerspectiveCount: r.Stories, MinePerspectiveId: mine,
		})
	}
	return out, nil
}
