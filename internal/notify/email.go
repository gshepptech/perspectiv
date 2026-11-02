package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/notify/dbq"
	"perspectiv/internal/notify/mailer"
)

// Email retry policy. A send that fails is retried after 30 seconds, then
// with the delay doubling each attempt and capped at one hour: 30s, 1m, 2m,
// 4m, 8m, 16m, 32m. The eighth failed attempt, about an hour after the first,
// cancels the job; by then the provider outage or the bad address is not
// transient, and a verification or reset link has gone stale enough that the
// user will ask for a new one.
const (
	// EmailMaxAttempts is how many sends an email job gets before it is
	// cancelled.
	EmailMaxAttempts = 8
	emailFirstRetry  = 30 * time.Second
	emailMaxBackoff  = time.Hour
	// emailTimeout bounds one send.
	emailTimeout = 30 * time.Second
)

// Data keys of an EmailArgs. A verify_email or password_reset job carries
// either the whole link or the token to build it from. A notification job
// carries the kind, the display fields and the app path to link to.
const (
	dataLink     = "link"
	dataToken    = "token"
	dataKind     = "kind"
	dataUserID   = "user_id"
	dataActor    = "actor"
	dataArcTitle = "arc_title"
	dataPath     = "path"
)

type emailWorker struct {
	river.WorkerDefaults[jobs.EmailArgs]
	pool    *pgxpool.Pool
	mailer  mailer.Mailer
	metrics *metrics.Metrics
	log     zerolog.Logger
	baseURL string
	unsub   Unsubscriber
}

// NewEmailWorker returns the worker for email jobs. It renders the job's
// template, with links under publicBaseURL, and sends it through m. A new
// story's unclaimed characters are read from pool as each attempt sends, so
// a story no longer readable by then names nobody; no other email reads the
// database.
//
// A provider failure never reaches the request that queued the email: the
// request only inserted the job. The failure increments
// email_send_failures_total and the job is retried on the schedule above.
func NewEmailWorker(pool *pgxpool.Pool, m mailer.Mailer, met *metrics.Metrics, log zerolog.Logger, publicBaseURL string, unsub Unsubscriber) river.Worker[jobs.EmailArgs] {
	return &emailWorker{
		pool:    pool,
		mailer:  m,
		metrics: met,
		log:     log.With().Str("component", "email").Logger(),
		baseURL: strings.TrimRight(publicBaseURL, "/"),
		unsub:   unsub,
	}
}

func (w *emailWorker) Work(ctx context.Context, job *river.Job[jobs.EmailArgs]) error {
	c, err := w.castFor(ctx, job.Args)
	if err != nil {
		return fmt.Errorf("read unclaimed characters: %w", err)
	}
	msg, err := w.render(job.Args, c)
	if err != nil {
		// Retrying cannot fix a job that does not render.
		w.log.Error().Err(err).Int64("job_id", job.ID).Str("template", job.Args.Template).Msg("email job cannot be rendered")
		return river.JobCancel(fmt.Errorf("render email: %w", err))
	}
	if err := w.mailer.Send(ctx, msg); err != nil {
		w.metrics.EmailSendFailures.Inc()
		ev := w.log.Warn()
		if job.Attempt >= EmailMaxAttempts {
			ev = w.log.Error()
		}
		// The recipient address is not logged; the job id identifies it.
		ev.Err(err).Int64("job_id", job.ID).Int("attempt", job.Attempt).Str("template", job.Args.Template).
			Msg("email send failed")
		err = fmt.Errorf("send email: %w", err)
		if job.Attempt >= EmailMaxAttempts {
			return river.JobCancel(err)
		}
		return err
	}
	return nil
}

// castFor reads, for a new-story notification email, the story's unclaimed
// characters as they stand now (see unclaimedCast). Any other email, and a
// new-story job that does not name its story, names nobody.
func (w *emailWorker) castFor(ctx context.Context, a jobs.EmailArgs) (cast, error) {
	if a.Template != jobs.TemplateNotification ||
		a.Data[dataKind] != perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC.String() {
		return cast{}, nil
	}
	id, err := uuid.Parse(a.Data[dataPerspectiveID])
	if err != nil {
		return cast{}, nil
	}
	casts, err := unclaimedCast(ctx, dbq.New(w.pool), []uuid.UUID{id})
	if err != nil {
		return cast{}, err
	}
	return casts[id], nil
}

// NextRetry doubles the delay from emailFirstRetry, capped at emailMaxBackoff.
func (w *emailWorker) NextRetry(job *river.Job[jobs.EmailArgs]) time.Time {
	return time.Now().Add(emailBackoff(job.Attempt))
}

func emailBackoff(attempt int) time.Duration {
	d := emailFirstRetry
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= emailMaxBackoff {
			return emailMaxBackoff
		}
	}
	return d
}

func (w *emailWorker) Timeout(*river.Job[jobs.EmailArgs]) time.Duration { return emailTimeout }

// render turns an email job into a message; c is the unclaimed characters
// a new-story notification names.
func (w *emailWorker) render(a jobs.EmailArgs, c cast) (mailer.Message, error) {
	if a.To == "" {
		return mailer.Message{}, errors.New("no recipient")
	}
	var body content
	var err error
	switch a.Template {
	case jobs.TemplateVerifyEmail:
		body, err = w.accountContent(a.Data, "/verify-email",
			"Verify your email",
			"Confirm this address to start writing stories on Perspectiv.",
			"Verify email")
	case jobs.TemplatePasswordReset:
		body, err = w.accountContent(a.Data, "/reset-password",
			"Reset your password",
			"Someone asked to reset the password for this Perspectiv account. If it was not you, ignore this email.",
			"Choose a new password")
	case jobs.TemplateBetaInvite:
		body, err = w.accountContent(a.Data, "/signup",
			"You're invited to Perspectiv",
			"A place in the Perspectiv beta is yours. Perspectiv is where people who were at the same place on the same day each write what they saw, and find the strangers who were in their story.",
			"Create your account")
	case jobs.TemplateChangeEmail:
		body, err = w.accountContent(a.Data, "/confirm-email",
			"Confirm your new email",
			"Confirm this address to make it the email of your Perspectiv account. Until you do, your account keeps its old address.",
			"Confirm new email")
	case jobs.TemplateEmailChangeNotice:
		body, err = w.accountContent(a.Data, "/forgot-password",
			"Your Perspectiv email is being changed",
			"Someone signed in to your Perspectiv account asked to change its email away from this address. If it was you, there is nothing to do. If it was not, reset your password now: that also cancels the change.",
			"Reset your password")
	case jobs.TemplateTwoFactorEnabled:
		body, err = w.accountContent(a.Data, "/forgot-password",
			"Two-factor sign-in is on",
			"Two-factor sign-in is now on for your Perspectiv account: signing in asks for a code from your authenticator app after your password, and every other device signed in to the account was signed out. If you did not turn it on, reset your password now.",
			"Reset your password")
	case jobs.TemplateTwoFactorDisabled:
		body, err = w.accountContent(a.Data, "/forgot-password",
			"Two-factor sign-in is off",
			"Two-factor sign-in was turned off for your Perspectiv account, so signing in asks only for your password. If you did not do this, reset your password now, then turn two-factor sign-in on again in Settings.",
			"Reset your password")
	case jobs.TemplateRecoveryCodeUsed:
		subject, text := recoveryCodeUsed(a.Data)
		body, err = w.accountContent(a.Data, "/forgot-password", subject, text, "Reset your password")
	case jobs.TemplateRecoveryCodesReplaced:
		body, err = w.accountContent(a.Data, "/forgot-password",
			"Your recovery codes were replaced",
			"A new set of recovery codes was made for your Perspectiv account, and the old codes no longer work. If you did not do this, reset your password now, then make new recovery codes in Settings.",
			"Reset your password")
	case jobs.TemplateDigest:
		body, err = w.digestContent(a.Data)
	case jobs.TemplateNotification:
		body, err = w.notificationContent(a.Data, c)
	default:
		err = fmt.Errorf("unknown template %q", a.Template)
	}
	if err != nil {
		return mailer.Message{}, err
	}
	var unsubscribe string
	if a.Template == jobs.TemplateDigest || a.Template == jobs.TemplateNotification {
		// Notification email is the kind a reader can turn off, so it never
		// goes out without a working unsubscribe link.
		unsubscribe, err = w.withUnsubscribe(&body, a.Data)
		if err != nil {
			return mailer.Message{}, err
		}
	}
	msg, err := body.message(a.To)
	msg.Unsubscribe = unsubscribe
	return msg, err
}

// withUnsubscribe adds the unsubscribe page to c's footer and returns the
// one-click URL for the List-Unsubscribe header.
func (w *emailWorker) withUnsubscribe(c *content, data map[string]string) (string, error) {
	id, err := uuid.Parse(data[dataUserID])
	if err != nil {
		return "", errors.New("notification email without its recipient's user id")
	}
	q := w.unsub.query(id)
	c.Unsubscribe = w.baseURL + unsubscribePage + "?" + q
	return w.baseURL + "/api" + unsubscribeOneClick + "?" + q, nil
}

// content is what every email shows: a subject, a paragraph, one link and an
// optional footer. A digest also lists its items, each with its own link.
type content struct {
	Subject, Body, Action, Link string
	// Footer is a closing sentence; SettingsLink, when set, follows it as
	// a "Change it in Settings" link, and Unsubscribe as "Stop these
	// emails". In the text part each link stands on its own line.
	Footer, SettingsLink, Unsubscribe string
	Items                             []digestLine
}

// digestLine is one event in a digest: what happened and where to see it.
type digestLine struct {
	Text, Link string
}

var htmlBody = template.Must(template.New("email").Parse(
	`<!doctype html><html><body style="font-family:Georgia,serif;color:#1d1b18;line-height:1.5">` +
		`<p>{{.Body}}</p>{{if .Items}}<ul>{{range .Items}}<li><a href="{{.Link}}">{{.Text}}</a></li>{{end}}</ul>{{end}}` +
		`<p><a href="{{.Link}}">{{.Action}}</a></p>` +
		`<p style="font-size:13px;color:#5b564e">Or paste this link into your browser: {{.Link}}</p>` +
		`{{if .Footer}}<p style="font-size:13px;color:#5b564e">{{.Footer}}` +
		`{{if .SettingsLink}} <a href="{{.SettingsLink}}">Change it in Settings</a>.{{end}}` +
		`{{if .Unsubscribe}} <a href="{{.Unsubscribe}}">Stop these emails</a>.{{end}}</p>{{end}}</body></html>`))

func (c content) message(to string) (mailer.Message, error) {
	var html bytes.Buffer
	if err := htmlBody.Execute(&html, c); err != nil {
		return mailer.Message{}, err
	}
	text := c.Body + "\n\n"
	for _, it := range c.Items {
		text += "- " + it.Text + "\n  " + it.Link + "\n"
	}
	if len(c.Items) > 0 {
		text += "\n"
	}
	text += c.Action + ": " + c.Link + "\n"
	if c.Footer != "" {
		text += "\n" + c.Footer + "\n"
		if c.SettingsLink != "" {
			text += "Change it in Settings: " + c.SettingsLink + "\n"
		}
		if c.Unsubscribe != "" {
			text += "Stop these emails: " + c.Unsubscribe + "\n"
		}
	}
	return mailer.Message{To: to, Subject: c.Subject, Text: text, HTML: html.String()}, nil
}

// accountContent builds a verification or reset email. The link is taken
// whole when the job carries one, else built from the token as
// <base><path>?token=<token>.
func (w *emailWorker) accountContent(data map[string]string, path, subject, body, action string) (content, error) {
	link := data[dataLink]
	if link == "" {
		token := data[dataToken]
		if token == "" {
			return content{}, errors.New("neither a link nor a token")
		}
		link = w.baseURL + path + "?token=" + url.QueryEscape(token)
	}
	return content{Subject: subject, Body: body, Action: action, Link: link}, nil
}

// recoveryCodeUsed is the subject and paragraph of a recovery-code-used
// email: what the code was used for and, after a sign-in, how many are left.
func recoveryCodeUsed(data map[string]string) (subject, body string) {
	switch data["used_for"] {
	case jobs.RecoveryCodeUsedForTurnOff:
		return "A recovery code was used to turn off two-factor sign-in",
			"One of your Perspectiv account's recovery codes was used to turn two-factor sign-in off. " +
				"If it was not you, reset your password now, then turn two-factor sign-in on again in Settings."
	case jobs.RecoveryCodeUsedForNewCodes:
		return "A recovery code was used to make new recovery codes",
			"One of your Perspectiv account's recovery codes was used to make a new set of recovery codes, so the old ones no longer work. " +
				"If it was not you, reset your password now, then make new recovery codes in Settings."
	}
	return "A recovery code was used to sign in", recoveryCodeUsedBody(data["left"])
}

// recoveryCodeUsedBody says a recovery code signed in and how many are left.
func recoveryCodeUsedBody(left string) string {
	codes := left + " recovery codes are left"
	switch left {
	case "":
		codes = "Each code works once"
	case "0":
		codes = "That was the last one: make new codes in Settings now"
	case "1":
		codes = "1 recovery code is left"
	}
	return "Someone signed in to your Perspectiv account with one of its recovery codes. " + codes +
		". If it was not you, reset your password now and make new recovery codes in Settings."
}

// notificationContent builds one notification's email; cast is the
// unclaimed characters a new story names. The arc title is put on one line
// with hidden characters dropped (apiutil.PlainLine), so a title stored
// before saving enforced that cannot forge a line or a header.
func (w *emailWorker) notificationContent(data map[string]string, cast cast) (content, error) {
	kind := perspectivv1.NotificationKind(perspectivv1.NotificationKind_value[data[dataKind]])
	actor := data[dataActor]
	if actor == "" {
		actor = "Someone"
	}
	arc := apiutil.PlainLine(data[dataArcTitle])
	if arc == "" {
		arc = "your day"
	}
	var subject, body string
	switch kind {
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED:
		subject = actor + " says they are in your story"
		body = actor + " says they are one of the characters in your story in " + arc + ". Accept or reject the claim."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED:
		subject = "Your claim was accepted"
		body = actor + " accepted your claim in " + arc + ". Your stories are now linked."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC:
		subject = "A new story in " + arc
		body = actor + " published a new story in " + arc + "."
		if who := castPhrase(cast); who != "" {
			body += " Who might be in it: " + who + ". If one of them was you, open the story and claim them."
		}
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED:
		subject = "A remove vote opened in " + arc
		body = "A member of your lore in " + arc + " opened a vote to remove a story from it."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE:
		subject = "The canon changed in " + arc
		body = "A different lore is now canon in " + arc + ", or the day no longer has one."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_INVITE_ANSWERED:
		subject = actor + " wrote their side of " + arc
		body = actor + " followed your invite and wrote their story in " + arc + ". Read it, and claim each other's characters if you recognise yourselves."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE:
		subject = actor + " wrote about " + arc
		body = actor + " wrote a new story in " + arc + ", at a place you follow. Were you there too?"
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER:
		subject = actor + " wrote a new story"
		body = actor + ", a writer you follow, wrote a new story in " + arc + "."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_THREAD_REPLY:
		subject = actor + " replied to your post"
		body = actor + " replied to your post in a lore's thread in " + arc + "."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_REPORT_RESOLVED:
		subject = "Your report was resolved"
		body = "A moderator reviewed and resolved a report you made. Thank you for flagging it."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_UNSPECIFIED:
		return content{}, fmt.Errorf("unknown notification kind %q", data[dataKind])
	default:
		return content{}, fmt.Errorf("unknown notification kind %q", data[dataKind])
	}
	path := data[dataPath]
	if !strings.HasPrefix(path, "/") {
		path = "/inbox"
	}
	return content{
		Subject:      subject,
		Body:         body,
		Action:       "Open Perspectiv",
		Link:         w.baseURL + path,
		Footer:       "You choose which emails you get.",
		SettingsLink: w.baseURL + "/settings",
	}, nil
}

// digestContent builds the daily summary: one line per event, worded as its
// own email's subject, each linking where the event happened.
func (w *emailWorker) digestContent(data map[string]string) (content, error) {
	var items []map[string]string
	if err := json.Unmarshal([]byte(data[dataItems]), &items); err != nil {
		return content{}, fmt.Errorf("digest items: %w", err)
	}
	if len(items) == 0 {
		return content{}, errors.New("digest has no items")
	}
	lines := make([]digestLine, 0, len(items))
	for _, item := range items {
		c, err := w.notificationContent(item, cast{})
		if err != nil {
			return content{}, err
		}
		lines = append(lines, digestLine{Text: c.Subject, Link: c.Link})
	}
	subject := "Your day on Perspectiv: 1 update"
	if len(lines) != 1 {
		subject = fmt.Sprintf("Your day on Perspectiv: %d updates", len(lines))
	}
	return content{
		Subject:      subject,
		Body:         "Here is what happened since your last summary.",
		Items:        lines,
		Action:       "Open your inbox",
		Link:         w.baseURL + "/inbox",
		Footer:       "You get one summary a day instead of an email for each.",
		SettingsLink: w.baseURL + "/settings",
	}, nil
}
