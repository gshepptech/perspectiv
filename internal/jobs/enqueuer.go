package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// Enqueuer inserts jobs inside the caller's transaction, so a job exists
// exactly when the domain write that asked for it commits.
type Enqueuer struct {
	client *river.Client[pgx.Tx]
}

// NewEnqueuer returns an Enqueuer inserting through c, which may be an
// insert-only client.
func NewEnqueuer(c *river.Client[pgx.Tx]) *Enqueuer {
	return &Enqueuer{client: c}
}

// ExpiryTx schedules the expiry check for a perspective at runAt, which the
// publisher sets to the publish time plus the configured expiry window.
func (e *Enqueuer) ExpiryTx(ctx context.Context, tx pgx.Tx, perspectiveID uuid.UUID, runAt time.Time) error {
	if perspectiveID == uuid.Nil {
		return errors.New("enqueue expiry: perspective id is required")
	}
	if runAt.IsZero() {
		return errors.New("enqueue expiry: run time is required")
	}
	return e.insert(ctx, tx, ExpiryArgs{PerspectiveID: perspectiveID}, &river.InsertOpts{ScheduledAt: runAt})
}

// EmailTx enqueues one email.
func (e *Enqueuer) EmailTx(ctx context.Context, tx pgx.Tx, a EmailArgs) error {
	if a.To == "" {
		return errors.New("enqueue email: recipient is required")
	}
	switch a.Template {
	case TemplateVerifyEmail, TemplatePasswordReset, TemplateNotification, TemplateBetaInvite, TemplateDigest,
		TemplateChangeEmail, TemplateEmailChangeNotice, TemplateTwoFactorEnabled, TemplateTwoFactorDisabled,
		TemplateRecoveryCodeUsed, TemplateRecoveryCodesReplaced:
	default:
		return fmt.Errorf("enqueue email: unknown template %q", a.Template)
	}
	return e.insert(ctx, tx, a, nil)
}

// NotifyTx enqueues a domain event for notify to deliver.
func (e *Enqueuer) NotifyTx(ctx context.Context, tx pgx.Tx, a NotifyArgs) error {
	if !validNotificationKind(a.Kind) {
		return fmt.Errorf("enqueue notify: invalid notification kind %d", a.Kind)
	}
	return e.insert(ctx, tx, notifyJob{NotifyArgs: a}, nil)
}

func (e *Enqueuer) insert(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) error {
	if _, err := e.client.InsertTx(ctx, tx, args, opts); err != nil {
		return fmt.Errorf("enqueue %s: %w", args.Kind(), err)
	}
	return nil
}
