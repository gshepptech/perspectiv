// Package jobs owns the River job queue: the job argument types every domain
// enqueues, the enqueuer that inserts them inside the caller's transaction,
// the client constructor the composition root starts per ROLE, and the
// perspective expiry worker.
package jobs

import (
	"time"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// DefaultExpiryWindow is how long after publish a perspective with no accepted
// claim stays PENDING before the expiry job marks it SOLO.
const DefaultExpiryWindow = 7 * 24 * time.Hour

// Email templates an EmailArgs may name.
const (
	TemplateVerifyEmail   = "verify_email"
	TemplatePasswordReset = "password_reset"
	TemplateNotification  = "notification"
	// TemplateBetaInvite invites someone from the waitlist; Data carries the
	// sign-up link with their access code.
	TemplateBetaInvite = "beta_invite"
	// TemplateDigest is a daily summary; Data carries the notification items
	// as a JSON list and their count.
	TemplateDigest = "digest"
	// TemplateChangeEmail confirms a new address; TemplateEmailChangeNotice
	// tells the old one. Data carries the link.
	TemplateChangeEmail       = "change_email"
	TemplateEmailChangeNotice = "email_change_notice"
)

// ExpiryArgs schedules the PENDING to SOLO check for one perspective.
type ExpiryArgs struct {
	PerspectiveID uuid.UUID
}

// Kind is the River job kind.
func (ExpiryArgs) Kind() string { return "perspective_expiry" }

// RankArgs is the periodic lore rank pass; it carries no arguments.
type RankArgs struct{}

// Kind is the River job kind.
func (RankArgs) Kind() string { return "lore_rank" }

// EmailArgs is one email to send. Template is one of the Template constants
// and Data carries its substitutions.
type EmailArgs struct {
	To       string
	Template string
	Data     map[string]string
}

// Kind is the River job kind.
func (EmailArgs) Kind() string { return "email" }

// NotifyArgs is a domain event for notify to fan out to recipients. A field
// set to uuid.Nil is absent. Producers name the event; notify resolves who
// receives it.
type NotifyArgs struct {
	Kind          perspectivv1.NotificationKind
	ActorID       uuid.UUID
	ArcID         uuid.UUID
	PerspectiveID uuid.UUID
	ClaimID       uuid.UUID
	LoreID        uuid.UUID
	ReportID      uuid.UUID
}

// notifyKind is the River job kind of a notify job.
const notifyKind = "notify"

// notifyJob is the River job args a NotifyArgs is inserted as. NotifyArgs
// cannot be River job args itself: Go does not allow its Kind field and a
// Kind method on one type. The embedding marshals to exactly the NotifyArgs
// fields, so the stored job args decode straight into a NotifyArgs.
type notifyJob struct {
	NotifyArgs
}

// Kind is the River job kind; it shadows the promoted Kind field.
func (notifyJob) Kind() string { return notifyKind }
