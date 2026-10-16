// Package arc owns arcs: one place and one place-local day, optionally split
// into hour child arcs. It resolves the arc a perspective publishes into,
// assembles the arc page (canon, sublores, tales and the day's hour timeline)
// and lets an hour arc's creator retitle it.
//
// The arc domain never writes canon state (arcs.canon_lore_id, canon_score,
// member_count, lore standing); it only reads what the rank job wrote.
package arc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc/dbq"
)

// Service is the arc domain's logic over its store.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service reading and writing through pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ResolveInput says where a perspective is being published.
type ResolveInput struct {
	PlaceID uuid.UUID
	// LocalDate is the place-local calendar day; only its date is used.
	LocalDate time.Time
	// Hour is 0 to 23 for an hour child arc, nil for the day arc.
	Hour *int32
	// Title names the day arc when this publish creates it. It is ignored
	// when the day arc already exists.
	Title     string
	CreatedBy uuid.UUID
}

// ResolveForPublish returns the arc a perspective at in's place, day and hour
// files under, creating it inside tx when it does not exist yet.
//
//   - With no hour it joins or creates the day arc. Creating the day arc needs
//     a valid Title (see checkTitle); joining an existing one ignores Title, so
//     a second writer is never asked for one.
//   - With an hour it first joins or creates the day arc the same way, then
//     joins or creates the hour child of that day arc. When one publish creates
//     both, Title names the day arc and the hour child is titled from it (see
//     hourTitle); the hour arc's creator may retitle it later.
//
// created reports whether the returned arc (the hour arc when an hour is given)
// was inserted by this call. Concurrent first writers converge on one row: the
// insert does nothing on the (place_id, local_date, hour) unique index, whose
// NULLS NOT DISTINCT makes two day arcs collide, and the loser reads the
// winner's row. tx must run at READ COMMITTED (the default) so that read sees
// the row committed by the concurrent winner.
func (s *Service) ResolveForPublish(ctx context.Context, tx pgx.Tx, in ResolveInput) (arcID uuid.UUID, created bool, err error) {
	if err := in.validate(); err != nil {
		return uuid.Nil, false, err
	}
	q := dbq.New(tx)
	place, err := q.GetPlaceForArc(ctx, in.PlaceID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && place.HiddenAt.Valid) {
		return uuid.Nil, false, apiutil.NotFound("place not found")
	}
	if err != nil {
		return uuid.Nil, false, apiutil.Internal(fmt.Errorf("read place: %w", err))
	}
	date := dateToDB(in.LocalDate)

	day, dayCreated, err := resolveDay(ctx, q, place.Name, date, in)
	if err != nil || in.Hour == nil {
		return day.ID, dayCreated, err
	}
	return resolveHour(ctx, q, day, in.PlaceID, date, *in.Hour, in.CreatedBy)
}

func (in ResolveInput) validate() error {
	switch {
	case in.PlaceID == uuid.Nil:
		return apiutil.InvalidArg("place is required")
	case in.LocalDate.IsZero():
		return apiutil.InvalidArg("local date is required")
	case in.CreatedBy == uuid.Nil:
		return apiutil.InvalidArg("author is required")
	case in.Hour != nil && (*in.Hour < 0 || *in.Hour > 23):
		return apiutil.InvalidArg("hour must be 0 to 23")
	}
	return nil
}

func resolveDay(ctx context.Context, q *dbq.Queries, placeName string, date pgtype.Date, in ResolveInput) (dbq.GetDayArcRow, bool, error) {
	key := dbq.GetDayArcParams{PlaceID: in.PlaceID, LocalDate: date}
	day, err := q.GetDayArc(ctx, key)
	if err == nil {
		return day, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return day, false, apiutil.Internal(fmt.Errorf("read day arc: %w", err))
	}
	title, err := checkTitle(in.Title, placeName, date.Time)
	if err != nil {
		return day, false, err
	}
	id, err := q.InsertArc(ctx, dbq.InsertArcParams{
		PlaceID: in.PlaceID, LocalDate: date, Title: title, CreatedBy: in.CreatedBy,
	})
	if err == nil {
		return dbq.GetDayArcRow{ID: id, Title: title}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return day, false, apiutil.Internal(fmt.Errorf("insert day arc: %w", err))
	}
	// A concurrent first writer created it; join theirs.
	day, err = q.GetDayArc(ctx, key)
	if err != nil {
		return day, false, apiutil.Internal(fmt.Errorf("read day arc after conflict: %w", err))
	}
	return day, false, nil
}

// resolveHour joins or creates the hour child of day, which sits at the same
// place and date.
func resolveHour(
	ctx context.Context, q *dbq.Queries, day dbq.GetDayArcRow, placeID uuid.UUID, date pgtype.Date, hour int32, by uuid.UUID,
) (uuid.UUID, bool, error) {
	// hour is 0 to 23 (ResolveInput.validate), so it fits in an int16.
	key := dbq.GetHourArcParams{PlaceID: placeID, LocalDate: date, Hour: pgtype.Int2{Int16: int16(hour), Valid: true}} //nolint:gosec // bounded above
	h, err := q.GetHourArc(ctx, key)
	if err == nil {
		return h.ID, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, apiutil.Internal(fmt.Errorf("read hour arc: %w", err))
	}
	id, err := q.InsertArc(ctx, dbq.InsertArcParams{
		PlaceID:     placeID,
		LocalDate:   date,
		Title:       hourTitle(day.Title, hour),
		CreatedBy:   by,
		Hour:        key.Hour,
		ParentArcID: uuid.NullUUID{UUID: day.ID, Valid: true},
	})
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, apiutil.Internal(fmt.Errorf("insert hour arc: %w", err))
	}
	h, err = q.GetHourArc(ctx, key)
	if err != nil {
		return uuid.Nil, false, apiutil.Internal(fmt.Errorf("read hour arc after conflict: %w", err))
	}
	return h.ID, false, nil
}

// RetitleHourArc sets a new title on an hour arc.
//
// Only the hour arc's creator, the writer whose publish created it with the
// derived "<day title> · HH:00" title, may retitle it; anyone else, admins
// included, gets PermissionDenied. A day arc keeps the title its first writer
// gave it, so retitling one is FailedPrecondition. The new title follows the
// same rules as a day arc title, checked against the arc's place and date.
func (s *Service) RetitleHourArc(ctx context.Context, caller, arcID uuid.UUID, title string) (*perspectivv1.Arc, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := dbq.New(tx)
	row, err := q.LockArcForRetitle(ctx, arcID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.PlaceHiddenAt.Valid) {
		return nil, apiutil.NotFound("arc not found")
	}
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("lock arc: %w", err))
	}
	if !row.Hour.Valid {
		return nil, apiutil.FailedPrecondition("only an hour arc can be retitled")
	}
	if row.CreatedBy != caller {
		return nil, apiutil.PermissionDenied("only the hour arc's creator can retitle it")
	}
	clean, err := checkTitle(title, row.PlaceName, row.LocalDate.Time)
	if err != nil {
		return nil, err
	}
	if err := q.UpdateArcTitle(ctx, dbq.UpdateArcTitleParams{ID: arcID, Title: clean}); err != nil {
		return nil, apiutil.Internal(fmt.Errorf("update title: %w", err))
	}
	arc, err := loadArc(ctx, q, arcID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apiutil.Internal(fmt.Errorf("commit: %w", err))
	}
	return arc, nil
}
