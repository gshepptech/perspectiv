// Package perspective owns perspectives: one user's entry in one arc, written
// as an autosaved draft, published explicitly into the arc for a place, a
// place-local day and an optional hour, then editable until deleted.
//
// A perspective holds at most 10 parts of at most 300 words and declares at
// most 10 characters that other authors in its arc may claim. Once published
// its place, day, hour and arc are fixed. Publishing schedules the expiry job
// and the new-perspective event in the publish transaction.
//
// The perspective domain never writes canon state or lore standing; a delete
// only withdraws the perspective's lore inputs through the lore service and
// marks the arc for the rank job, which owns both.
package perspective

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/arc"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore"
	"perspectiv/internal/perspective/dbq"
)

// Enqueuer inserts jobs inside the caller's transaction; *jobs.Enqueuer
// satisfies it.
type Enqueuer interface {
	ExpiryTx(ctx context.Context, tx pgx.Tx, perspectiveID uuid.UUID, runAt time.Time) error
	NotifyTx(ctx context.Context, tx pgx.Tx, a jobs.NotifyArgs) error
}

// Service is the perspective domain's logic over its store.
type Service struct {
	pool         *pgxpool.Pool
	arcs         *arc.Service
	lore         *lore.Service
	enq          Enqueuer
	metrics      *metrics.Metrics
	expiryWindow time.Duration
}

// NewService returns a Service writing through pool. It resolves arcs through
// arcs, withdraws deleted perspectives from lore through lore, schedules jobs
// through enq and counts publishes on m. A published perspective's expiry job
// runs expiryWindow after publish; zero or less means jobs.DefaultExpiryWindow.
func NewService(pool *pgxpool.Pool, arcs *arc.Service, lore *lore.Service, enq Enqueuer, m *metrics.Metrics, expiryWindow time.Duration) *Service {
	if expiryWindow <= 0 {
		expiryWindow = jobs.DefaultExpiryWindow
	}
	return &Service{pool: pool, arcs: arcs, lore: lore, enq: enq, metrics: m, expiryWindow: expiryWindow}
}

// DraftInput is an autosaved draft. ID is uuid.Nil for a new draft. Every
// other field may be unset while the author is still writing.
type DraftInput struct {
	ID         uuid.UUID
	PlaceID    *uuid.UUID
	LocalDate  *time.Time
	Hour       *int32
	Title      *string
	Genre      *string
	Parts      []string
	Characters []characterInput
}

// PublishInput is a publish. ID is the draft being published, or uuid.Nil to
// publish without a saved draft.
type PublishInput struct {
	ID         uuid.UUID
	PlaceID    uuid.UUID
	LocalDate  time.Time
	Hour       *int32
	Title      string
	Genre      string
	Parts      []string
	Characters []characterInput
	// InviteCode is the invite link the author followed here; "" for none.
	InviteCode string
}

// UpdateInput is an edit of a published perspective.
//
// An edit replaces the text (all parts), the genre and the declared
// characters: characters may be added, relabelled, reordered and removed,
// except that a character holding an ACCEPTED claim cannot be removed. The
// place, day, hour, arc and arc title are fixed at publish and an edit has
// no way to name them.
type UpdateInput struct {
	ID         uuid.UUID
	Genre      string
	Parts      []string
	Characters []characterInput
}

var errNotFound = apiutil.NotFound("perspective not found")

// SaveDraft creates or replaces the caller's draft and returns it. Drafts
// hold the same part, word and character limits as a publish, but may be
// empty. A published perspective is edited through Update instead.
func (s *Service) SaveDraft(ctx context.Context, caller uuid.UUID, in DraftInput) (*perspectivv1.Perspective, error) {
	id, err := s.inTx(ctx, func(tx pgx.Tx) (uuid.UUID, error) {
		q := dbq.New(tx)
		id, err := saveDraftRow(ctx, q, caller, in)
		if err != nil {
			return uuid.Nil, err
		}
		return id, writeContent(ctx, q, id, in.Parts, in.Characters)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, caller, id)
}

// saveDraftRow inserts a new draft row or rewrites the caller's existing one.
func saveDraftRow(ctx context.Context, q *dbq.Queries, caller uuid.UUID, in DraftInput) (uuid.UUID, error) {
	if in.ID == uuid.Nil {
		id, err := q.InsertDraft(ctx, dbq.InsertDraftParams{
			AuthorID: caller, PlaceID: in.PlaceID, LocalDate: dateToDB(in.LocalDate),
			Hour: hourToDB(in.Hour), TitleIfNewArc: in.Title, Genre: in.Genre,
		})
		if err != nil {
			return uuid.Nil, storeError("insert draft", err)
		}
		return id, nil
	}
	row, err := lockOwn(ctx, q, caller, in.ID)
	if err != nil {
		return uuid.Nil, err
	}
	if row.Status != statusDraft {
		return uuid.Nil, apiutil.FailedPrecondition("this perspective is published; edit it with Update")
	}
	if err := q.UpdateDraft(ctx, dbq.UpdateDraftParams{
		ID: in.ID, PlaceID: in.PlaceID, LocalDate: dateToDB(in.LocalDate),
		Hour: hourToDB(in.Hour), TitleIfNewArc: in.Title, Genre: in.Genre,
	}); err != nil {
		return uuid.Nil, storeError("update draft", err)
	}
	return in.ID, nil
}

// Publish files a perspective into the arc for its place, day and optional
// hour, creating the arc (and the hour child) in the same transaction when
// absent, and returns the perspective and arc ids.
//
// Refusals:
//   - FailedPrecondition when the caller's email is unverified.
//   - NotFound for an unknown draft id, another user's draft, or an unknown
//     or hidden place.
//   - InvalidArgument for a bad title when this publish creates the arc.
//   - AlreadyExists when the caller already has a perspective in that arc,
//     including the draft id itself once published; the client then opens
//     that perspective for editing.
//   - FailedPrecondition when the caller's perspective in that arc was
//     deleted or hidden: the arc's one slot for the user stays taken, so
//     there is nothing to open and no second perspective to publish.
//
// In the same transaction it schedules the expiry job at publish time plus
// the expiry window and enqueues the new-perspective event.
func (s *Service) Publish(ctx context.Context, caller uuid.UUID, in PublishInput) (perspectiveID, arcID uuid.UUID, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		perspectiveID, arcID, err = s.publishTx(ctx, tx, caller, in)
		return err
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	s.metrics.PerspectivesPublished.Inc()
	return perspectiveID, arcID, nil
}

func (s *Service) publishTx(ctx context.Context, tx pgx.Tx, caller uuid.UUID, in PublishInput) (uuid.UUID, uuid.UUID, error) {
	q := dbq.New(tx)
	if err := checkAuthor(ctx, q, caller); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if in.ID != uuid.Nil {
		row, err := lockOwn(ctx, q, caller, in.ID)
		if err != nil {
			return uuid.Nil, uuid.Nil, err
		}
		if row.Status != statusDraft {
			return uuid.Nil, uuid.Nil, apiutil.AlreadyExists("this perspective is already published; edit it instead")
		}
	}
	arcID, _, err := s.arcs.ResolveForPublish(ctx, tx, arc.ResolveInput{
		PlaceID: in.PlaceID, LocalDate: in.LocalDate, Hour: in.Hour, Title: in.Title, CreatedBy: caller,
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := checkSlot(ctx, q, arcID, caller); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	id := in.ID
	var publishedAt *time.Time
	if id == uuid.Nil {
		row, err := q.InsertPublished(ctx, dbq.InsertPublishedParams{
			ArcID: arcID, AuthorID: caller, PlaceID: in.PlaceID, LocalDate: dateToDB(&in.LocalDate),
			Hour: hourToDB(in.Hour), Genre: in.Genre, Status: statusPublished, LoreStatus: lorePending,
		})
		if err != nil {
			return uuid.Nil, uuid.Nil, storeError("insert perspective", err)
		}
		id, publishedAt = row.ID, row.PublishedAt
	} else {
		publishedAt, err = q.PublishDraft(ctx, dbq.PublishDraftParams{
			ID: id, ArcID: arcID, PlaceID: in.PlaceID, LocalDate: dateToDB(&in.LocalDate),
			Hour: hourToDB(in.Hour), Genre: in.Genre, Status: statusPublished, LoreStatus: lorePending,
		})
		if err != nil {
			return uuid.Nil, uuid.Nil, storeError("publish draft", err)
		}
	}
	if publishedAt == nil {
		return uuid.Nil, uuid.Nil, apiutil.Internal(errors.New("publish did not stamp published_at"))
	}
	if err := writeContent(ctx, q, id, in.Parts, in.Characters); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := s.enq.ExpiryTx(ctx, tx, id, publishedAt.Add(s.expiryWindow)); err != nil {
		return uuid.Nil, uuid.Nil, apiutil.Internal(err)
	}
	for _, kind := range []perspectivv1.NotificationKind{
		perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER,
	} {
		if err := s.enq.NotifyTx(ctx, tx, jobs.NotifyArgs{Kind: kind, ActorID: caller, ArcID: arcID, PerspectiveID: id}); err != nil {
			return uuid.Nil, uuid.Nil, apiutil.Internal(err)
		}
	}
	if err := s.recordInvite(ctx, tx, caller, id, arcID, in.InviteCode); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return id, arcID, nil
}

// checkAuthor refuses a caller who may not publish. The route policy already
// requires a verified email; this re-checks inside the publish transaction so
// the guard holds whatever mounted the handler.
func checkAuthor(ctx context.Context, q *dbq.Queries, caller uuid.UUID) error {
	u, err := q.GetAuthor(ctx, caller)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && u.DeletedAt != nil) {
		return apiutil.Unauthenticated("sign in to publish")
	}
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read author: %w", err))
	}
	if u.SuspendedAt != nil {
		return apiutil.PermissionDenied("your account is suspended")
	}
	if u.EmailVerifiedAt == nil {
		return apiutil.FailedPrecondition("verify your email first")
	}
	return nil
}

// checkSlot refuses a second perspective by caller in arcID.
func checkSlot(ctx context.Context, q *dbq.Queries, arcID, caller uuid.UUID) error {
	row, err := q.FindInArc(ctx, dbq.FindInArcParams{ArcID: arcID, AuthorID: caller})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return apiutil.Internal(fmt.Errorf("find perspective in arc: %w", err))
	case row.HiddenAt != nil:
		// A moderator's hide stands; deleting is the author's own choice
		// and frees the place (FindInArc skips deleted rows).
		return apiutil.FailedPrecondition("your story about this day was hidden by a moderator; you cannot publish another there")
	default:
		return errAlreadyInArc
	}
}

var errAlreadyInArc = apiutil.AlreadyExists("you already have a story about this day")

// Update edits a published perspective (see UpdateInput) and returns it. A
// draft is FailedPrecondition (drafts save through SaveDraft), and so is a
// perspective moderation hid.
func (s *Service) Update(ctx context.Context, caller uuid.UUID, in UpdateInput) (*perspectivv1.Perspective, error) {
	id, err := s.inTx(ctx, func(tx pgx.Tx) (uuid.UUID, error) {
		q := dbq.New(tx)
		row, err := lockForWrite(ctx, q, caller, in.ID)
		if err != nil {
			return uuid.Nil, err
		}
		if row.Status != statusPublished || row.ArcID == nil {
			return uuid.Nil, apiutil.FailedPrecondition("a draft is edited with SaveDraft")
		}
		if row.HiddenAt != nil {
			return uuid.Nil, apiutil.FailedPrecondition("this perspective was hidden by a moderator")
		}
		if err := q.UpdatePublished(ctx, dbq.UpdatePublishedParams{ID: in.ID, Genre: in.Genre}); err != nil {
			return uuid.Nil, apiutil.Internal(fmt.Errorf("update perspective: %w", err))
		}
		return in.ID, writeContent(ctx, q, in.ID, in.Parts, in.Characters)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, caller, id)
}

// Delete soft-deletes the caller's perspective, draft or published.
//
// For a published perspective, in the same transaction, it withdraws the
// perspective's lore inputs through lore.RecomputeArc, which rejects every
// open or accepted claim on its characters or made from it and recomputes
// the arc's lore groups without it, and marks the arc lore-dirty so the next
// rank job recomputes standing and canon. Reading views then show a
// placeholder. Deleting an unknown, foreign or already deleted perspective is
// NotFound.
func (s *Service) Delete(ctx context.Context, caller, id uuid.UUID) error {
	_, err := s.inTx(ctx, func(tx pgx.Tx) (uuid.UUID, error) {
		q := dbq.New(tx)
		row, err := lockForWrite(ctx, q, caller, id)
		if err != nil {
			return uuid.Nil, err
		}
		if err := q.SoftDelete(ctx, id); err != nil {
			return uuid.Nil, apiutil.Internal(fmt.Errorf("soft delete: %w", err))
		}
		if row.ArcID == nil {
			return id, nil
		}
		if err := s.lore.RecomputeArc(ctx, tx, *row.ArcID); err != nil {
			return uuid.Nil, err
		}
		if err := q.MarkArcLoreDirty(ctx, *row.ArcID); err != nil {
			return uuid.Nil, apiutil.Internal(fmt.Errorf("mark arc lore dirty: %w", err))
		}
		return id, nil
	})
	return err
}

// lockForWrite locks the caller's live perspective id for an edit or delete,
// taking its arc's lore lock first when it is published: the lore domain's
// lock order (arc lock, then perspective rows), so a character removal or a
// delete and a claim resolution in the same arc serialise instead of
// interleaving. A published perspective never changes arc, so the arc read
// before the lock is the one it is in.
func lockForWrite(ctx context.Context, q *dbq.Queries, caller, id uuid.UUID) (dbq.LockPerspectiveRow, error) {
	ref, err := q.GetPerspectiveRef(ctx, id)
	if err != nil || ref.AuthorID != caller || ref.DeletedAt != nil {
		return dbq.LockPerspectiveRow{}, notFoundOr(err)
	}
	if ref.ArcID != nil {
		if err := q.LockArcLore(ctx, *ref.ArcID); err != nil {
			return dbq.LockPerspectiveRow{}, apiutil.Internal(fmt.Errorf("lock arc: %w", err))
		}
	}
	row, err := lockOwn(ctx, q, caller, id)
	if err != nil {
		return row, err
	}
	if row.ArcID != nil && ref.ArcID == nil {
		// The draft was published between the read and the row lock. Take
		// the arc lock now; Postgres breaks the rare deadlock this can meet
		// with a lore writer, and the write then fails as Internal instead
		// of interleaving.
		if err := q.LockArcLore(ctx, *row.ArcID); err != nil {
			return row, apiutil.Internal(fmt.Errorf("lock arc: %w", err))
		}
	}
	return row, nil
}

// inTx runs fn in a transaction and commits when it succeeds.
func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx) (uuid.UUID, error)) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		id, err = fn(tx)
		return err
	})
	if err != nil {
		// fn returns coded errors; anything else (begin, commit) is Internal.
		if _, ok := apiutil.CodeOf(err); !ok {
			return uuid.Nil, apiutil.Internal(err)
		}
		return uuid.Nil, err
	}
	return id, nil
}

// lockOwn locks the caller's live perspective id. Another user's perspective
// is NotFound, like a missing or deleted one, so ids reveal nothing.
func lockOwn(ctx context.Context, q *dbq.Queries, caller, id uuid.UUID) (dbq.LockPerspectiveRow, error) {
	row, err := q.LockPerspective(ctx, id)
	if err != nil || row.AuthorID != caller || row.DeletedAt != nil {
		return row, notFoundOr(err)
	}
	return row, nil
}

func notFoundOr(err error) error {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	return apiutil.Internal(fmt.Errorf("read perspective: %w", err))
}

// writeContent replaces a perspective's parts and brings its characters in
// line with chars.
func writeContent(ctx context.Context, q *dbq.Queries, id uuid.UUID, parts []string, chars []characterInput) error {
	if err := q.DeleteParts(ctx, id); err != nil {
		return apiutil.Internal(fmt.Errorf("delete parts: %w", err))
	}
	if len(parts) > 0 {
		if err := q.InsertParts(ctx, dbq.InsertPartsParams{PerspectiveID: id, Bodies: parts}); err != nil {
			return apiutil.Internal(fmt.Errorf("insert parts: %w", err))
		}
	}
	return syncCharacters(ctx, q, id, chars)
}

// syncCharacters makes the perspective's characters exactly chars, in order.
// An input naming an id keeps that character (relabelled and moved); one
// without an id adds a character; an existing character not named is
// removed, which deletes its open or rejected claims with it. A character
// holding an ACCEPTED claim cannot be removed: that refuses the whole edit
// with FailedPrecondition. An id that is not one of this perspective's
// characters is InvalidArgument.
func syncCharacters(ctx context.Context, q *dbq.Queries, id uuid.UUID, chars []characterInput) error {
	existing, err := q.ListCharacterClaims(ctx, id)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("list characters: %w", err))
	}
	removed, err := removedCharacters(existing, chars)
	if err != nil {
		return err
	}
	if len(removed) > 0 {
		if err := q.DeleteCharacters(ctx, removed); err != nil {
			return apiutil.Internal(fmt.Errorf("delete characters: %w", err))
		}
	}
	for i, c := range chars {
		pos := int16(i + 1) //nolint:gosec // checkCharacters caps chars at 10
		if c.id != uuid.Nil {
			err = q.UpdateCharacter(ctx, dbq.UpdateCharacterParams{ID: c.id, Label: c.label, Position: pos})
		} else {
			err = q.InsertCharacter(ctx, dbq.InsertCharacterParams{PerspectiveID: id, Label: c.label, Position: pos})
		}
		if err != nil {
			return apiutil.Internal(fmt.Errorf("write character: %w", err))
		}
	}
	return nil
}

// removedCharacters checks chars against the existing characters and returns
// the ids chars leaves out; see syncCharacters for the refusals.
func removedCharacters(existing []dbq.ListCharacterClaimsRow, chars []characterInput) ([]uuid.UUID, error) {
	known := make(map[uuid.UUID]bool, len(existing))
	for _, c := range existing {
		known[c.ID] = true
	}
	kept := make(map[uuid.UUID]bool, len(chars))
	for i, c := range chars {
		if c.id == uuid.Nil {
			continue
		}
		if !known[c.id] {
			return nil, apiutil.InvalidArg(fmt.Sprintf("character %d is not one of this perspective's characters", i+1))
		}
		kept[c.id] = true
	}
	var removed []uuid.UUID
	for _, c := range existing {
		switch {
		case kept[c.ID]:
		case c.HasAccepted:
			return nil, apiutil.FailedPrecondition("a character with an accepted claim cannot be removed")
		default:
			removed = append(removed, c.ID)
		}
	}
	return removed, nil
}

// storeError maps a constraint failure on a perspective write to the
// caller's mistake it stands for and anything else to Internal.
func storeError(op string, err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.ConstraintName {
		case "perspectives_arc_author_key":
			// A concurrent publish by the same user into the same arc won.
			return errAlreadyInArc
		case "perspectives_place_id_fkey":
			return apiutil.NotFound("place not found")
		}
	}
	return apiutil.Internal(fmt.Errorf("%s: %w", op, err))
}
