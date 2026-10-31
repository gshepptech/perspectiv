package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/notify/dbq"
	"perspectiv/internal/notify/mailer"
)

// FanoutArgs is the River job args a notify event is worked as. It decodes
// the job a producer inserted with jobs.Enqueuer.NotifyTx.
type FanoutArgs struct {
	jobs.NotifyArgs
}

// Kind is the River job kind; it shadows the promoted NotifyArgs.Kind field.
func (FanoutArgs) Kind() string { return "notify" }

// AddWorkers registers the fan-out and email workers on w.
func AddWorkers(w *river.Workers, pool *pgxpool.Pool, m mailer.Mailer, met *metrics.Metrics, log zerolog.Logger, publicBaseURL string, unsub Unsubscriber) {
	river.AddWorker(w, NewFanoutWorker(pool, log))
	river.AddWorker(w, NewEmailWorker(m, met, log, publicBaseURL, unsub))
	river.AddWorker(w, NewDigestWorker(pool, log))
}

type fanoutWorker struct {
	river.WorkerDefaults[FanoutArgs]
	pool *pgxpool.Pool
	log  zerolog.Logger
}

// NewFanoutWorker returns the worker that delivers a notify event: it
// resolves the recipients, applies blocks and each recipient's toggles, and
// writes inbox rows and email jobs.
//
// Recipients by kind:
//   - claim received: the author of the claimed character's perspective.
//   - claim accepted: the claimant.
//   - new perspective in arc: every author of another visible published
//     perspective in the same arc.
//   - vote-out opened: the author of the target perspective and the authors
//     of the other members of its lore, so the target hears their
//     perspective is challenged and fellow members can vote. The voter is
//     not named in the notification.
//   - canon change: every author of a visible published perspective in the
//     arc, since canon decides what all of them see first. It is a system
//     event with no actor.
//   - report resolved: the reporter. The resolving admin is not the actor: a
//     moderation outcome is always delivered and never names the moderator.
//   - new story at a followed place: the followers of the arc's place or any
//     place above it, except the arc's authors.
//   - new story by a followed writer: the author's followers with no block
//     either way, except the arc's authors and the place followers above.
//   - thread reply: the author of the post the reply answered, unless
//     either of the two has blocked the other.
//
// For every kind the actor never hears about their own action, a recipient
// who has blocked the actor hears nothing from them, and a deleted account
// hears nothing. An event whose subject no longer exists (a deleted claim,
// a hidden new perspective, an unknown report) completes without delivering.
//
// The inbox rows, the email jobs and the job's completion commit in one
// transaction, so a retried or duplicated attempt never delivers twice.
func NewFanoutWorker(pool *pgxpool.Pool, log zerolog.Logger) river.Worker[FanoutArgs] {
	return &fanoutWorker{pool: pool, log: log.With().Str("component", "notify").Logger()}
}

func (w *fanoutWorker) Work(ctx context.Context, job *river.Job[FanoutArgs]) error {
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	enq := jobs.NewEnqueuer(client)
	args := job.Args.NotifyArgs
	err = pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		ev, ok, err := resolve(ctx, q, args)
		if err != nil {
			return err
		}
		if ok {
			if err := deliver(ctx, q, tx, enq, ev); err != nil {
				return err
			}
		} else {
			w.log.Info().Int64("job_id", job.ID).Str("kind", args.Kind.String()).
				Msg("notification subject gone or incomplete; nothing delivered")
		}
		_, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job)
		return err
	})
	if err != nil {
		return fmt.Errorf("notify %s: %w", args.Kind, err)
	}
	return nil
}

// event is a resolved notification: who may receive it and what it says.
type event struct {
	kind       perspectivv1.NotificationKind
	actor      *uuid.UUID // excluded and checked for blocks; nil for system events
	showActor  bool       // whether the payload and email name the actor
	candidates []uuid.UUID
	payload    Payload
	path       string // app path the email links to
	// quietLore, when set, emails a recipient about this lore at most once
	// an hour; the inbox still gets every event.
	quietLore *uuid.UUID
}

// resolve finds an event's recipients and payload from the database, the
// authority for who wrote what. ok is false when there is nothing to deliver.
func resolve(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	var (
		ev  event
		ok  bool
		err error
	)
	switch a.Kind {
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED:
		ev, ok, err = resolveClaim(ctx, q, a)
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC:
		ev, ok, err = resolveNewPerspective(ctx, q, a)
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED:
		ev, ok, err = resolveVoteOut(ctx, q, a)
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE:
		ev, ok, err = resolveCanonChange(ctx, q, a)
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_REPORT_RESOLVED:
		ev, ok, err = resolveReport(ctx, q, a)
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_INVITE_ANSWERED:
		ev, ok, err = resolveInviteAnswered(ctx, q, a)
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE:
		ev, ok, err = resolveFollowedPlaceStory(ctx, q, a)
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER:
		ev, ok, err = resolveFollowedWriterStory(ctx, q, a)
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_THREAD_REPLY:
		ev, ok, err = resolveThreadReply(ctx, q, a)
		ev.quietLore = ev.payload.LoreID
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_UNSPECIFIED:
		return event{}, false, nil
	default:
		return event{}, false, nil
	}
	if !ok || err != nil {
		return event{}, false, err
	}
	ev.kind = a.Kind
	if ev.payload.ArcID != nil {
		// An event about an arc at or below a hidden place is not delivered:
		// moderation removed what it is about, and neither the inbox nor the
		// email may name the arc.
		hidden, err := q.ArcPlaceHidden(ctx, *ev.payload.ArcID)
		if err != nil {
			return event{}, false, fmt.Errorf("arc place hidden: %w", err)
		}
		if hidden {
			return event{}, false, nil
		}
	}
	return ev, true, nil
}

// unlessMissing drops a not-found error, which means the event's subject is
// gone, and passes any other error through.
func unlessMissing(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func ref(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func resolveClaim(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	if a.ClaimID == uuid.Nil {
		return event{}, false, nil
	}
	c, err := q.ClaimFacts(ctx, a.ClaimID)
	if err != nil {
		return event{}, false, unlessMissing(err)
	}
	ev := event{
		showActor: true,
		path:      "/claims",
		payload: Payload{
			ArcID: c.ArcID, ClaimID: ref(a.ClaimID), PerspectiveID: ref(c.CharacterPerspectiveID),
		},
	}
	if a.Kind == perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED {
		ev.actor, ev.candidates = ref(c.ClaimantUserID), []uuid.UUID{c.CharacterAuthorID}
	} else {
		ev.actor, ev.candidates = ref(c.CharacterAuthorID), []uuid.UUID{c.ClaimantUserID}
	}
	ev.payload.ActorID = ev.actor
	return ev, true, nil
}

// visiblePerspective returns a published perspective that is neither deleted
// nor hidden; ok is false otherwise.
func visiblePerspective(ctx context.Context, q *dbq.Queries, id uuid.UUID) (dbq.PerspectiveFactsRow, bool, error) {
	if id == uuid.Nil {
		return dbq.PerspectiveFactsRow{}, false, nil
	}
	p, err := q.PerspectiveFacts(ctx, id)
	if err != nil {
		return p, false, unlessMissing(err)
	}
	published := perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_PUBLISHED.String()
	ok := p.Status == published && p.ArcID != nil && p.DeletedAt == nil && p.HiddenAt == nil
	return p, ok, nil
}

func resolveNewPerspective(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	p, ok, err := visiblePerspective(ctx, q, a.PerspectiveID)
	if !ok || err != nil {
		return event{}, false, err
	}
	authors, err := q.ArcAuthors(ctx, p.ArcID)
	if err != nil {
		return event{}, false, fmt.Errorf("arc authors: %w", err)
	}
	actor := ref(p.AuthorID)
	return event{
		actor:      actor,
		showActor:  true,
		candidates: authors,
		path:       "/perspectives/" + a.PerspectiveID.String(),
		payload:    Payload{ActorID: actor, ArcID: p.ArcID, PerspectiveID: ref(a.PerspectiveID)},
	}, true, nil
}

func resolveVoteOut(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	p, ok, err := visiblePerspective(ctx, q, a.PerspectiveID)
	if !ok || err != nil {
		return event{}, false, err
	}
	lore := ref(a.LoreID)
	if lore == nil {
		lore = p.LoreID
	}
	candidates := []uuid.UUID{p.AuthorID}
	if lore != nil {
		members, err := q.LoreAuthors(ctx, *lore)
		if err != nil {
			return event{}, false, fmt.Errorf("lore authors: %w", err)
		}
		candidates = append(candidates, members...)
	}
	return event{
		actor:      ref(a.ActorID),
		candidates: candidates,
		path:       "/arcs/" + p.ArcID.String(),
		payload:    Payload{ArcID: p.ArcID, PerspectiveID: ref(a.PerspectiveID), LoreID: lore},
	}, true, nil
}

func resolveCanonChange(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	arc := ref(a.ArcID)
	if arc == nil && a.LoreID != uuid.Nil {
		id, err := q.LoreArc(ctx, a.LoreID)
		if err != nil {
			return event{}, false, unlessMissing(err)
		}
		arc = &id
	}
	if arc == nil {
		return event{}, false, nil
	}
	authors, err := q.ArcAuthors(ctx, arc)
	if err != nil {
		return event{}, false, fmt.Errorf("arc authors: %w", err)
	}
	return event{
		candidates: authors,
		path:       "/arcs/" + arc.String(),
		payload:    Payload{ArcID: arc, LoreID: ref(a.LoreID)},
	}, len(authors) > 0, nil
}

// resolveInviteAnswered tells an inviter that someone who followed their
// invite published in the arc. The new perspective must still be readable.
func resolveInviteAnswered(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	p, ok, err := visiblePerspective(ctx, q, a.PerspectiveID)
	if !ok || err != nil {
		return event{}, false, err
	}
	inviter, err := q.InviteAnswer(ctx, a.PerspectiveID)
	if err != nil {
		return event{}, false, unlessMissing(err)
	}
	actor := ref(p.AuthorID)
	return event{
		actor:      actor,
		showActor:  true,
		candidates: []uuid.UUID{inviter},
		path:       "/perspectives/" + a.PerspectiveID.String(),
		payload:    Payload{ActorID: actor, ArcID: p.ArcID, PerspectiveID: ref(a.PerspectiveID)},
	}, true, nil
}

// resolveFollowedPlaceStory tells the followers of the arc's place, or of
// any place above it, that someone wrote there.
func resolveFollowedPlaceStory(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	p, ok, err := visiblePerspective(ctx, q, a.PerspectiveID)
	if !ok || err != nil {
		return event{}, false, err
	}
	// visiblePerspective only returns a perspective that is in an arc.
	followers, err := q.FollowersOfArcPlaces(ctx, *p.ArcID)
	if err != nil {
		return event{}, false, fmt.Errorf("followers of arc places: %w", err)
	}
	actor := ref(p.AuthorID)
	return event{
		actor:      actor,
		showActor:  true,
		candidates: followers,
		path:       "/perspectives/" + a.PerspectiveID.String(),
		payload:    Payload{ActorID: actor, ArcID: p.ArcID, PerspectiveID: ref(a.PerspectiveID)},
	}, true, nil
}

// resolveFollowedWriterStory tells the followers of a story's author that
// they wrote it. FollowersOfWriter leaves out anyone with a block either way
// and anyone who hears of the story as a place follower or an arc author, so
// one story makes one notification per person.
func resolveFollowedWriterStory(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	p, ok, err := visiblePerspective(ctx, q, a.PerspectiveID)
	if !ok || err != nil {
		return event{}, false, err
	}
	// visiblePerspective only returns a perspective that is in an arc.
	followers, err := q.FollowersOfWriter(ctx, dbq.FollowersOfWriterParams{ArcID: *p.ArcID, AuthorID: p.AuthorID})
	if err != nil {
		return event{}, false, fmt.Errorf("followers of writer: %w", err)
	}
	actor := ref(p.AuthorID)
	return event{
		actor:      actor,
		showActor:  true,
		candidates: followers,
		path:       "/perspectives/" + a.PerspectiveID.String(),
		payload:    Payload{ActorID: actor, ArcID: p.ArcID, PerspectiveID: ref(a.PerspectiveID)},
	}, true, nil
}

// resolveThreadReply tells the author of a thread post that someone
// replied to it. Recipients drops only a recipient who blocked the actor;
// a reply never notifies across a block in either direction, so the other
// direction is checked here.
func resolveThreadReply(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	if a.LorePostID == uuid.Nil {
		return event{}, false, nil
	}
	f, err := q.ThreadReplyFacts(ctx, a.LorePostID)
	if err != nil {
		return event{}, false, unlessMissing(err)
	}
	if f.Blocked {
		return event{}, false, nil
	}
	actor := ref(f.ReplierID)
	return event{
		actor:      actor,
		showActor:  true,
		candidates: []uuid.UUID{f.AnsweredAuthorID},
		path:       "/lores/" + f.LoreID.String(),
		payload:    Payload{ActorID: actor, ArcID: ref(f.ArcID), LoreID: ref(f.LoreID)},
	}, true, nil
}

func resolveReport(ctx context.Context, q *dbq.Queries, a jobs.NotifyArgs) (event, bool, error) {
	if a.ReportID == uuid.Nil {
		return event{}, false, nil
	}
	reporter, err := q.ReportReporter(ctx, a.ReportID)
	if err != nil {
		return event{}, false, unlessMissing(err)
	}
	return event{
		candidates: []uuid.UUID{reporter},
		path:       "/inbox",
		payload:    Payload{ReportID: ref(a.ReportID)},
	}, true, nil
}

// deliver writes an inbox row for each recipient whose in-app toggle is on
// and an email job for each whose email toggle is on.
func deliver(ctx context.Context, q *dbq.Queries, tx pgx.Tx, enq *jobs.Enqueuer, ev event) error {
	recipients, err := q.Recipients(ctx, dbq.RecipientsParams{
		Kind: kindText(ev.kind), Candidates: ev.candidates, ActorID: ev.actor, QuietLoreID: ev.quietLore,
	})
	if err != nil {
		return fmt.Errorf("recipients: %w", err)
	}
	if len(recipients) == 0 {
		return nil
	}
	payload, err := json.Marshal(ev.payload)
	if err != nil {
		return err
	}
	data, err := emailData(ctx, q, ev)
	if err != nil {
		return err
	}
	for _, r := range recipients {
		if r.InApp {
			if err := q.InsertNotification(ctx, dbq.InsertNotificationParams{
				UserID: r.ID, Kind: kindText(ev.kind), Payload: payload,
			}); err != nil {
				return fmt.Errorf("insert notification: %w", err)
			}
		}
		if r.EmailOn && !r.EmailedRecently {
			if err := deliverEmail(ctx, q, tx, enq, r, data); err != nil {
				return err
			}
		}
	}
	return nil
}

// deliverEmail emails one recipient about the event, or, when they take a
// daily summary, adds it to their next one.
func deliverEmail(ctx context.Context, q *dbq.Queries, tx pgx.Tx, enq *jobs.Enqueuer, r dbq.RecipientsRow, data map[string]string) error {
	if !r.EmailDigest {
		mail := maps.Clone(data)
		mail[dataUserID] = r.ID.String()
		return enq.EmailTx(ctx, tx, jobs.EmailArgs{To: r.Email, Template: jobs.TemplateNotification, Data: mail})
	}
	item, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := q.InsertDigestItem(ctx, dbq.InsertDigestItemParams{UserID: r.ID, Data: item}); err != nil {
		return fmt.Errorf("insert digest item: %w", err)
	}
	return nil
}

// emailData is the notification template's data: the kind, the link path,
// the arc title and actor username when the event has them, and for a new
// story its unclaimed characters, read from the database as it is
// delivered.
func emailData(ctx context.Context, q *dbq.Queries, ev event) (map[string]string, error) {
	data := map[string]string{dataKind: kindText(ev.kind), dataPath: ev.path}
	if ev.payload.ArcID != nil {
		title, err := q.ArcTitle(ctx, *ev.payload.ArcID)
		if err := unlessMissing(err); err != nil {
			return nil, fmt.Errorf("arc title: %w", err)
		}
		if title != "" {
			data[dataArcTitle] = title
		}
	}
	if ev.showActor && ev.actor != nil {
		name, err := q.Username(ctx, *ev.actor)
		if err := unlessMissing(err); err != nil {
			return nil, fmt.Errorf("actor username: %w", err)
		}
		if name != "" {
			data[dataActor] = name
		}
	}
	if err := addCastData(ctx, q, ev, data); err != nil {
		return nil, err
	}
	return data, nil
}
