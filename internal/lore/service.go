// Package lore owns characters' claims and lore membership. An author claims
// a character in another perspective in the same arc ("that was me"); when
// the character's author accepts, both perspectives are IN_LORE and lore
// membership is recomputed as connected groups of accepted claims in that arc.
//
// Lock order, shared by every lore writer so they cannot deadlock one another:
// the arc's advisory lock, then perspective rows in id order, then claim rows,
// and the arc row (lore_dirty_at) last.
//
// The lore service never writes canon state from a request: lores.status,
// lores.rank_score, lores.upvotes and arcs.canon_lore_id belong to the rank
// job.
package lore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore/dbq"
)

// Notifier enqueues a domain event inside the caller's transaction;
// *jobs.Enqueuer satisfies it. Recipients are resolved by notify.
type Notifier interface {
	NotifyTx(ctx context.Context, tx pgx.Tx, a jobs.NotifyArgs) error
}

// Service is the lore domain's logic over its store.
type Service struct {
	pool    *pgxpool.Pool
	notify  Notifier
	metrics *metrics.Metrics
}

// NewService returns a Service writing through pool, enqueueing claim events
// through notify and counting accepted claims on m.
func NewService(pool *pgxpool.Pool, notify Notifier, m *metrics.Metrics) *Service {
	return &Service{pool: pool, notify: notify, metrics: m}
}

// lockedPerspective is a perspective row read under its row lock.
type lockedPerspective = dbq.LockPerspectivesRow

// readable reports a published perspective that is neither deleted nor
// hidden.
func readable(p lockedPerspective) bool {
	return p.Status == perspectivePublished && p.ArcID.Valid && !p.DeletedAt.Valid && !p.HiddenAt.Valid
}

func removed(p lockedPerspective) bool {
	return p.LoreStatus.String == loreStatusRemoved
}

// lockPair locks two perspectives in id order and returns them as (a, b).
// A perspective that no longer exists comes back as the zero row.
func lockPair(ctx context.Context, q *dbq.Queries, a, b uuid.UUID) (lockedPerspective, lockedPerspective, error) {
	rows, err := q.LockPerspectives(ctx, []uuid.UUID{a, b})
	if err != nil {
		return lockedPerspective{}, lockedPerspective{}, apiutil.Internal(fmt.Errorf("lock perspectives: %w", err))
	}
	var pa, pb lockedPerspective
	for _, r := range rows {
		switch r.ID {
		case a:
			pa = r
		case b:
			pb = r
		}
	}
	return pa, pb, nil
}

// ClaimCharacter opens a PENDING claim by caller on a character and enqueues
// a claim-received event for the character's author.
//
// Refusals, checked in this order:
//   - NotFound when the character does not exist or its perspective is not
//     published, or is deleted or hidden.
//   - FailedPrecondition when the caller wrote the character (a user has one
//     perspective per arc, so there is nothing of theirs to link it to).
//   - FailedPrecondition when the caller has no readable published
//     perspective in the character's arc.
//   - FailedPrecondition when either perspective is REMOVED.
//   - PermissionDenied when the character's author has blocked the caller.
//     The caller is signed in but not allowed to interact with that author,
//     which is what PermissionDenied means; reading is unaffected.
//   - FailedPrecondition when the caller's earlier claim on this character
//     was rejected.
//   - AlreadyExists when the character has a PENDING or ACCEPTED claim,
//     the caller's own included.
func (s *Service) ClaimCharacter(ctx context.Context, caller, characterID uuid.UUID) (uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbq.New(tx)

	ch, err := q.GetCharacter(ctx, characterID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !ch.ArcID.Valid) {
		return uuid.Nil, apiutil.NotFound("character not found")
	}
	if err != nil {
		return uuid.Nil, apiutil.Internal(fmt.Errorf("read character: %w", err))
	}
	arcID := ch.ArcID.UUID
	if err := q.LockArcLore(ctx, arcID); err != nil {
		return uuid.Nil, apiutil.Internal(fmt.Errorf("lock arc: %w", err))
	}
	mine, err := claimantPerspective(ctx, q, ch, caller)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.checkClaimable(ctx, q, ch.AuthorID, caller, characterID); err != nil {
		return uuid.Nil, err
	}

	claimID, err := q.InsertClaim(ctx, dbq.InsertClaimParams{
		CharacterID: characterID, ClaimantUserID: caller, ClaimantPerspectiveID: mine, Status: claimPending,
	})
	if isUniqueViolation(err) {
		return uuid.Nil, apiutil.AlreadyExists("this character has already been claimed")
	}
	if err != nil {
		return uuid.Nil, apiutil.Internal(fmt.Errorf("insert claim: %w", err))
	}
	if err := s.notify.NotifyTx(ctx, tx, jobs.NotifyArgs{
		Kind:          perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED,
		ActorID:       caller,
		ArcID:         arcID,
		PerspectiveID: ch.PerspectiveID,
		ClaimID:       claimID,
	}); err != nil {
		return uuid.Nil, apiutil.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, apiutil.Internal(fmt.Errorf("commit: %w", err))
	}
	return claimID, nil
}

// checkClaimable applies the block, prior-rejection and one-active-claim
// rules.
func (s *Service) checkClaimable(ctx context.Context, q *dbq.Queries, author, caller, characterID uuid.UUID) error {
	blocked, err := q.IsBlocked(ctx, dbq.IsBlockedParams{BlockerID: author, BlockedID: caller})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read block: %w", err))
	}
	if blocked {
		return apiutil.PermissionDenied("you cannot claim this character")
	}
	prior, err := q.GetClaimByClaimant(ctx, dbq.GetClaimByClaimantParams{CharacterID: characterID, ClaimantUserID: caller})
	switch {
	case err == nil && prior.Status == claimRejected:
		return apiutil.FailedPrecondition("your claim on this character was rejected")
	case err == nil:
		return apiutil.AlreadyExists("you have already claimed this character")
	case !errors.Is(err, pgx.ErrNoRows):
		return apiutil.Internal(fmt.Errorf("read prior claim: %w", err))
	}
	if err := oneCharacterPerStory(ctx, q, caller, characterID); err != nil {
		return err
	}
	active, err := q.HasActiveClaim(ctx, dbq.HasActiveClaimParams{CharacterID: characterID, ActiveStatuses: activeClaims})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read active claims: %w", err))
	}
	if active {
		return apiutil.AlreadyExists("this character has already been claimed")
	}
	return nil
}

// oneCharacterPerStory refuses a claim when the caller already has a PENDING
// or ACCEPTED claim on any character in the same perspective: a person can
// only be one of the people in someone else's story. A rejected claim does
// not count, so a writer who guessed wrong can claim someone else there.
func oneCharacterPerStory(ctx context.Context, q *dbq.Queries, caller, characterID uuid.UUID) error {
	ch, err := q.GetCharacter(ctx, characterID)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read character: %w", err))
	}
	active, err := q.HasActiveClaimInPerspective(ctx, dbq.HasActiveClaimInPerspectiveParams{
		PerspectiveID: ch.PerspectiveID, ClaimantUserID: caller,
	})
	if err != nil {
		return apiutil.Internal(fmt.Errorf("read claims in perspective: %w", err))
	}
	if active {
		return apiutil.FailedPrecondition("you are already someone in this story; one character per story")
	}
	return nil
}

// ResolveClaim accepts or rejects a PENDING claim for the author of the
// claimed character.
//
// Accepting requires both perspectives to be readable, in the same arc and
// not REMOVED (FailedPrecondition otherwise); it sets the claim ACCEPTED,
// recomputes the arc's lore membership, which puts both perspectives IN_LORE
// in one lore (a SOLO tale on either side is revived), counts the accept and
// enqueues a claim-accepted event. Rejecting sets the claim REJECTED, after
// which the claimant can never claim that character again. Either way the
// arc is marked lore-dirty for the rank job.
func (s *Service) ResolveClaim(ctx context.Context, caller, claimID uuid.UUID, accept bool) (perspectivv1.ClaimStatus, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, apiutil.Internal(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbq.New(tx)

	cl, err := lockPendingClaim(ctx, q, caller, claimID)
	if err != nil {
		return 0, err
	}
	next := perspectivv1.ClaimStatus_CLAIM_STATUS_REJECTED
	if accept {
		next = perspectivv1.ClaimStatus_CLAIM_STATUS_ACCEPTED
		if err := acceptable(cl.target, cl.claimant); err != nil {
			return 0, err
		}
		if err := refuseHiddenArc(ctx, q, cl.arcID, apiutil.FailedPrecondition("one of these perspectives is no longer published")); err != nil {
			return 0, err
		}
	}
	if err := q.SetClaimStatus(ctx, dbq.SetClaimStatusParams{ID: claimID, Status: next.String()}); err != nil {
		return 0, apiutil.Internal(fmt.Errorf("set claim status: %w", err))
	}
	if accept {
		if err := s.linkAccepted(ctx, tx, caller, cl); err != nil {
			return 0, err
		}
	}
	if err := q.MarkArcLoreDirty(ctx, cl.arcID); err != nil {
		return 0, apiutil.Internal(fmt.Errorf("mark arc lore dirty: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, apiutil.Internal(fmt.Errorf("commit: %w", err))
	}
	if accept {
		s.metrics.ClaimsAccepted.Inc()
	}
	return next, nil
}

// pendingClaim is a PENDING claim read under the arc lock with both its
// perspectives and its own row locked.
type pendingClaim struct {
	id, arcID, perspectiveID uuid.UUID
	target, claimant         lockedPerspective
}

// lockPendingClaim locks the claim for its character's author: NotFound when
// it does not exist, PermissionDenied for anyone but that author and
// FailedPrecondition once it is no longer PENDING.
func lockPendingClaim(ctx context.Context, q *dbq.Queries, caller, claimID uuid.UUID) (pendingClaim, error) {
	cl, err := q.GetClaim(ctx, claimID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pendingClaim{}, apiutil.NotFound("claim not found")
	}
	if err != nil {
		return pendingClaim{}, apiutil.Internal(fmt.Errorf("read claim: %w", err))
	}
	if cl.AuthorID != caller {
		return pendingClaim{}, apiutil.PermissionDenied("only the character's author can resolve this claim")
	}
	if !cl.ArcID.Valid {
		return pendingClaim{}, apiutil.FailedPrecondition("this claim's perspective is not published")
	}
	out := pendingClaim{id: claimID, arcID: cl.ArcID.UUID, perspectiveID: cl.PerspectiveID}
	if err := q.LockArcLore(ctx, out.arcID); err != nil {
		return pendingClaim{}, apiutil.Internal(fmt.Errorf("lock arc: %w", err))
	}
	out.target, out.claimant, err = lockPair(ctx, q, cl.PerspectiveID, cl.ClaimantPerspectiveID)
	if err != nil {
		return pendingClaim{}, err
	}
	status, err := q.LockClaimStatus(ctx, claimID)
	if err != nil {
		return pendingClaim{}, apiutil.Internal(fmt.Errorf("lock claim: %w", err))
	}
	if status != claimPending {
		return pendingClaim{}, apiutil.FailedPrecondition("this claim has already been resolved")
	}
	return out, nil
}

// linkAccepted recomputes the arc's lore after an accept and enqueues the
// claim-accepted event.
func (s *Service) linkAccepted(ctx context.Context, tx pgx.Tx, caller uuid.UUID, cl pendingClaim) error {
	if err := s.RecomputeArc(ctx, tx, cl.arcID); err != nil {
		return err
	}
	if err := s.notify.NotifyTx(ctx, tx, jobs.NotifyArgs{
		Kind:          perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED,
		ActorID:       caller,
		ArcID:         cl.arcID,
		PerspectiveID: cl.perspectiveID,
		ClaimID:       cl.id,
	}); err != nil {
		return apiutil.Internal(err)
	}
	return nil
}

// claimantPerspective locks the character's perspective and the caller's own
// perspective in the arc and checks that the caller may claim the character
// from it: not their own character, both perspectives readable and not
// REMOVED, and the arc not at or below a hidden place. It returns the
// caller's perspective.
func claimantPerspective(ctx context.Context, q *dbq.Queries, ch dbq.GetCharacterRow, caller uuid.UUID) (uuid.UUID, error) {
	if ch.AuthorID == caller {
		return uuid.Nil, apiutil.FailedPrecondition("you cannot claim a character in your own perspective")
	}
	mine, err := q.FindAuthorPerspectiveInArc(ctx, dbq.FindAuthorPerspectiveInArcParams{ArcID: ch.ArcID.UUID, AuthorID: caller})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apiutil.Internal(fmt.Errorf("find claimant perspective: %w", err))
	}
	target, own, err := lockPair(ctx, q, ch.PerspectiveID, mine)
	if err != nil {
		return uuid.Nil, err
	}
	if !readable(target) {
		return uuid.Nil, apiutil.NotFound("character not found")
	}
	if err := refuseHiddenArc(ctx, q, ch.ArcID.UUID, apiutil.NotFound("character not found")); err != nil {
		return uuid.Nil, err
	}
	if mine == uuid.Nil || !readable(own) {
		return uuid.Nil, apiutil.FailedPrecondition("write your perspective in this arc first")
	}
	if removed(target) || removed(own) {
		return uuid.Nil, apiutil.FailedPrecondition("this story was removed from lore")
	}
	return mine, nil
}

// refuseHiddenArc returns refusal when the arc's place, or any place above
// it, is hidden: hiding a place closes its whole subtree to lore writes.
func refuseHiddenArc(ctx context.Context, q *dbq.Queries, arcID uuid.UUID, refusal error) error {
	hidden, err := q.ArcPlaceHidden(ctx, arcID)
	if err != nil {
		return apiutil.Internal(fmt.Errorf("arc place hidden: %w", err))
	}
	if hidden {
		return refusal
	}
	return nil
}

// acceptable guards the PENDING to ACCEPTED transition.
func acceptable(target, claimant lockedPerspective) error {
	switch {
	case !readable(target) || !readable(claimant):
		return apiutil.FailedPrecondition("one of these perspectives is no longer published")
	case target.ArcID != claimant.ArcID:
		return apiutil.FailedPrecondition("both perspectives must be in the same arc")
	case removed(target) || removed(claimant):
		return apiutil.FailedPrecondition("this story was removed from lore")
	}
	return nil
}

// ListClaims lists the caller's incoming claims (on characters in their
// perspectives) or outgoing claims (made by them), newest first, optionally
// filtered by status. It returns the page and the next page token, "" on the
// last page.
func (s *Service) ListClaims(
	ctx context.Context, caller uuid.UUID, dir perspectivv1.ClaimDirection, status perspectivv1.ClaimStatus, page apiutil.PageToken,
) ([]*perspectivv1.Claim, string, error) {
	filter, err := claimStatusToDB(status)
	if err != nil {
		return nil, "", err
	}
	params := dbq.ListIncomingClaimsParams{UserID: caller, Status: filter, RowLimit: page.Limit + 1}
	if page.After != "" {
		k, err := decodeClaimKeyset(page.After)
		if err != nil {
			return nil, "", err
		}
		params.HasAfter = true
		params.AfterCreatedAt = pgtype.Timestamptz{Time: k.createdAt, Valid: true}
		params.AfterID = k.id
	}
	q := dbq.New(s.pool)
	var rows []dbq.ListIncomingClaimsRow
	switch dir {
	case perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING:
		rows, err = q.ListIncomingClaims(ctx, params)
	case perspectivv1.ClaimDirection_CLAIM_DIRECTION_OUTGOING:
		var out []dbq.ListOutgoingClaimsRow
		out, err = q.ListOutgoingClaims(ctx, dbq.ListOutgoingClaimsParams(params))
		for _, r := range out {
			rows = append(rows, dbq.ListIncomingClaimsRow(r))
		}
	case perspectivv1.ClaimDirection_CLAIM_DIRECTION_UNSPECIFIED:
		return nil, "", apiutil.InvalidArg("direction is required")
	default:
		return nil, "", apiutil.InvalidArg("unknown direction")
	}
	if err != nil {
		return nil, "", apiutil.Internal(fmt.Errorf("list claims: %w", err))
	}

	next := ""
	if len(rows) > int(page.Limit) {
		rows = rows[:page.Limit]
		last := rows[len(rows)-1]
		next = apiutil.NextPageToken(claimKeyset{createdAt: last.CreatedAt.Time, id: last.ID}.encode())
	}
	claims := make([]*perspectivv1.Claim, 0, len(rows))
	for _, r := range rows {
		c, err := claimFromRow(r)
		if err != nil {
			return nil, "", apiutil.Internal(err)
		}
		claims = append(claims, c)
	}
	return claims, next, nil
}

// GetLore returns one lore with its members and the accepted claims linking
// them. It is a public read.
func (s *Service) GetLore(ctx context.Context, loreID uuid.UUID) (*perspectivv1.Lore, error) {
	q := dbq.New(s.pool)
	row, err := q.GetLore(ctx, loreID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apiutil.NotFound("lore not found")
	}
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("read lore: %w", err))
	}
	// A lore in an arc at or below a hidden place is as gone as the arc.
	if err := refuseHiddenArc(ctx, q, row.ArcID, apiutil.NotFound("lore not found")); err != nil {
		return nil, err
	}
	standing, err := loreStandingFromDB(row.Status)
	if err != nil {
		return nil, apiutil.Internal(err)
	}
	members, err := q.ListLoreMembers(ctx, loreID)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list lore members: %w", err))
	}
	encounters, err := q.ListLoreEncounters(ctx, dbq.ListLoreEncountersParams{LoreID: loreID, Accepted: claimAccepted})
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list encounters: %w", err))
	}
	out := &perspectivv1.Lore{
		Id:          row.ID.String(),
		ArcId:       row.ArcID.String(),
		Standing:    standing,
		MemberCount: row.MemberCount,
		Upvotes:     row.Upvotes,
		RankScore:   row.RankScore,
		ComputedAt:  timestampFromDB(row.ComputedAt),
	}
	for _, m := range members {
		out.Members = append(out.Members, &perspectivv1.LoreMember{
			PerspectiveId: m.PerspectiveID.String(),
			User:          &perspectivv1.UserRef{UserId: m.AuthorID.String(), Username: m.Username},
		})
	}
	for _, e := range encounters {
		out.Encounters = append(out.Encounters, &perspectivv1.Encounter{
			ClaimId:               e.ClaimID.String(),
			PerspectiveId:         e.PerspectiveID.String(),
			ClaimantPerspectiveId: e.ClaimantPerspectiveID.String(),
		})
	}
	return out, nil
}
