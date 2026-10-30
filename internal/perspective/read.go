package perspective

import (
	"context"

	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/perspective/dbq"
)

// Get returns one perspective as viewer sees it; viewer is uuid.Nil when
// signed out.
//
// A published perspective is public. A draft is visible only to its author;
// to anyone else it is NotFound, as is a deleted draft. A deleted or hidden
// published perspective is a placeholder (see placeholder).
func (s *Service) Get(ctx context.Context, viewer, id uuid.UUID) (*perspectivv1.Perspective, error) {
	var out *perspectivv1.Perspective
	err := s.readTx(ctx, func(q *dbq.Queries) error {
		got, err := load(ctx, q, viewer, []uuid.UUID{id})
		if err != nil {
			return err
		}
		if len(got) == 0 {
			return errNotFound
		}
		out = got[0]
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListFilter narrows ListMine. A nil field does not filter.
//
// The hour filter follows the composer's lookup after ALREADY_EXISTS: when a
// LocalDate is given, Hour is matched exactly, so a nil Hour finds only the
// day-arc perspective and not the hour-arc ones of that day. Without a
// LocalDate a nil Hour does not filter and a set Hour matches that hour on
// any day.
type ListFilter struct {
	Status    *string
	PlaceID   *uuid.UUID
	LocalDate *time.Time
	Hour      *int32
}

// ListMine pages the caller's drafts and published perspectives, deleted ones
// excluded, newest edit first. It returns the page and the next page token,
// empty on the last page.
func (s *Service) ListMine(ctx context.Context, caller uuid.UUID, f ListFilter, pageToken string, pageSize int32) ([]*perspectivv1.Perspective, string, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return nil, "", err
	}
	params := dbq.ListMineParams{
		AuthorID: caller, Status: f.Status, PlaceID: f.PlaceID, LocalDate: dateToDB(f.LocalDate),
		MatchHour: f.LocalDate != nil || f.Hour != nil, Hour: hourToDB(f.Hour),
		RowLimit: page.Limit + 1,
	}
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
		rows, err := q.ListMine(ctx, params)
		if err != nil {
			return apiutil.Internal(fmt.Errorf("list perspectives: %w", err))
		}
		if len(rows) > int(page.Limit) {
			rows = rows[:page.Limit]
			last := rows[len(rows)-1]
			next = apiutil.NextPageToken(last.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String())
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

// ListByAuthor pages one author's readable published perspectives for their
// profile, newest first: tales (SOLO and REMOVED) included; drafts, deleted
// and hidden perspectives, and those at or below a hidden place, left out.
// viewer is uuid.Nil for a signed-out reader.
func (s *Service) ListByAuthor(ctx context.Context, viewer, author uuid.UUID, pageToken string, pageSize int32) ([]*perspectivv1.Perspective, string, error) {
	page, err := apiutil.ParsePageToken(pageToken, pageSize)
	if err != nil {
		return nil, "", err
	}
	params := dbq.ListByAuthorParams{AuthorID: author, RowLimit: page.Limit + 1}
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
		rows, err := q.ListByAuthor(ctx, params)
		if err != nil {
			return apiutil.Internal(fmt.Errorf("list perspectives by author: %w", err))
		}
		if len(rows) > int(page.Limit) {
			rows = rows[:page.Limit]
			last := rows[len(rows)-1]
			next = apiutil.NextPageToken(last.PublishedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String())
		}
		ids := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		out, err = load(ctx, q, viewer, ids)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	return out, next, nil
}

func parseCursor(after string) (time.Time, uuid.UUID, error) {
	ts, idText, ok := strings.Cut(after, "|")
	if !ok {
		return time.Time{}, uuid.Nil, apiutil.InvalidArg("invalid page token")
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, apiutil.InvalidArg("invalid page token")
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return time.Time{}, uuid.Nil, apiutil.InvalidArg("invalid page token")
	}
	return at, id, nil
}

// readTx runs fn in one read-only REPEATABLE READ snapshot, so a perspective
// is never assembled from two states of its parts, characters or counts.
func (s *Service) readTx(ctx context.Context, fn func(q *dbq.Queries) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return fn(dbq.New(tx))
}

// load assembles the perspectives ids names, in that order, as viewer sees
// them. A perspective viewer may not see (a draft of someone else's, or a
// deleted draft) or one that does not exist is left out.
func load(ctx context.Context, q *dbq.Queries, viewer uuid.UUID, ids []uuid.UUID) ([]*perspectivv1.Perspective, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var viewerArg *uuid.UUID
	if viewer != uuid.Nil {
		viewerArg = &viewer
	}
	rows, err := q.GetPerspectives(ctx, dbq.GetPerspectivesParams{Viewer: viewerArg, Ids: ids})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read perspectives: %w", err))
	}
	content, err := loadContent(ctx, q, viewerArg, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]*perspectivv1.Perspective, len(rows))
	for _, r := range rows {
		if r.Status == statusDraft && (r.AuthorID != viewer || r.DeletedAt != nil) {
			continue
		}
		p, err := assemble(r, content)
		if err != nil {
			return nil, apiutil.Internal(err)
		}
		byID[r.ID] = p
	}
	out := make([]*perspectivv1.Perspective, 0, len(ids))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// contents holds the parts, characters and reacts of a set of perspectives.
type contents struct {
	parts      map[uuid.UUID][]*perspectivv1.Part
	characters map[uuid.UUID][]*perspectivv1.Character
	reacts     map[uuid.UUID][]*perspectivv1.ReactCount
}

func loadContent(ctx context.Context, q *dbq.Queries, viewer *uuid.UUID, ids []uuid.UUID) (contents, error) {
	c := contents{
		parts:      map[uuid.UUID][]*perspectivv1.Part{},
		characters: map[uuid.UUID][]*perspectivv1.Character{},
		reacts:     map[uuid.UUID][]*perspectivv1.ReactCount{},
	}
	parts, err := q.ListPartsOf(ctx, ids)
	if err != nil {
		return c, apiutil.Internal(fmt.Errorf("read parts: %w", err))
	}
	for _, p := range parts {
		c.parts[p.PerspectiveID] = append(c.parts[p.PerspectiveID], &perspectivv1.Part{Position: int32(p.Position), Body: p.Body})
	}
	chars, err := q.ListCharactersOf(ctx, dbq.ListCharactersOfParams{Viewer: viewer, Ids: ids})
	if err != nil {
		return c, apiutil.Internal(fmt.Errorf("read characters: %w", err))
	}
	for _, ch := range chars {
		mine, err := claimStatusFromDB(ch.MyClaim)
		if err != nil {
			return c, apiutil.Internal(err)
		}
		out := &perspectivv1.Character{
			Id: ch.ID.String(), Position: int32(ch.Position), Label: ch.Label,
			HasAcceptedClaim: ch.HasAccepted, HasPendingClaim: ch.HasPending, MyClaim: mine,
		}
		if ch.ClaimedByID != nil && ch.ClaimedByUsername != nil {
			out.ClaimedBy = &perspectivv1.UserRef{UserId: ch.ClaimedByID.String(), Username: *ch.ClaimedByUsername}
		}
		c.characters[ch.PerspectiveID] = append(c.characters[ch.PerspectiveID], out)
	}
	reacts, err := q.ReactCountsOf(ctx, dbq.ReactCountsOfParams{Viewer: viewer, Ids: ids})
	if err != nil {
		return c, apiutil.Internal(fmt.Errorf("read reacts: %w", err))
	}
	for _, r := range reacts {
		e, err := emojiFromDB(r.Emoji)
		if err != nil {
			return c, apiutil.Internal(err)
		}
		c.reacts[r.PerspectiveID] = append(c.reacts[r.PerspectiveID], &perspectivv1.ReactCount{Emoji: e, Count: r.Total, Mine: r.Mine})
	}
	return c, nil
}

// assemble builds the wire Perspective from its row and content. A deleted or
// hidden perspective becomes a placeholder.
func assemble(r dbq.GetPerspectivesRow, c contents) (*perspectivv1.Perspective, error) {
	p, err := placeholder(r)
	if err != nil || p.Deleted || p.Hidden {
		return p, err
	}
	if p.Genre, err = genreFromDB(r.Genre); err != nil {
		return nil, err
	}
	p.Author = &perspectivv1.UserRef{UserId: r.AuthorID.String(), Username: r.AuthorUsername}
	if r.Status == statusDraft && r.TitleIfNewArc != nil {
		p.TitleIfNewArc = *r.TitleIfNewArc
	}
	p.Parts = c.parts[r.ID]
	p.Characters = c.characters[r.ID]
	p.Upvotes = r.Upvotes
	p.Voted = r.Voted
	p.Saved = r.Saved
	p.Reacts = c.reacts[r.ID]
	return p, nil
}

// placeholder is what every reader gets for a deleted or hidden perspective:
// its ids, where it is filed, its state flags and timestamps, and no text,
// characters, genre, counts or author. assemble fills the rest for a live
// one.
//
// A published perspective's place, day and hour are its arc's, fixed at
// publish; a draft's are the ones it saved.
//
// A perspective filed at a hidden place, or below one, is hidden with it: a
// published one reads as a hidden placeholder that names neither the place
// nor the arc, which is NotFound on the arc page. A draft keeps its text for
// its author but no longer names the place, as publishing there is refused.
func placeholder(r dbq.GetPerspectivesRow) (*perspectivv1.Perspective, error) {
	p, err := filed(r)
	if err != nil || !r.PlaceHidden {
		return p, err
	}
	p.Place = nil
	if r.Status != statusDraft {
		p.Hidden = true
		p.ArcId, p.ArcTitle, p.LoreId = "", "", ""
	}
	return p, nil
}

// filed is placeholder before the hidden-place rule.
func filed(r dbq.GetPerspectivesRow) (*perspectivv1.Perspective, error) {
	status, err := statusFromDB(r.Status)
	if err != nil {
		return nil, err
	}
	loreStatus, err := loreStatusFromDB(r.LoreStatus)
	if err != nil {
		return nil, err
	}
	p := &perspectivv1.Perspective{
		Id:          r.ID.String(),
		Status:      status,
		LoreStatus:  loreStatus,
		LocalDate:   dateFromDB(r.DraftLocalDate),
		Hour:        hourFromDB(r.DraftHour),
		CreatedAt:   timestampFromDB(&r.CreatedAt),
		PublishedAt: timestampFromDB(r.PublishedAt),
		UpdatedAt:   timestampFromDB(&r.UpdatedAt),
		Deleted:     r.DeletedAt != nil,
		Hidden:      r.HiddenAt != nil,
	}
	placeID := r.DraftPlaceID
	if r.ArcID != nil {
		p.ArcId = r.ArcID.String()
		placeID = r.ArcPlaceID
		p.LocalDate = dateFromDB(r.ArcLocalDate)
		p.Hour = hourFromDB(r.ArcHour)
		if r.ArcTitle != nil {
			p.ArcTitle = *r.ArcTitle
		}
	}
	if r.LoreID != nil && loreStatus == perspectivv1.LoreStatus_LORE_STATUS_IN_LORE {
		p.LoreId = r.LoreID.String()
	}
	if placeID != nil && r.PlaceKind != nil && r.PlaceName != nil {
		kind, err := placeKindFromDB(*r.PlaceKind)
		if err != nil {
			return nil, err
		}
		p.Place = &perspectivv1.PlaceRef{Id: placeID.String(), Kind: kind, Name: *r.PlaceName}
	}
	return p, nil
}
