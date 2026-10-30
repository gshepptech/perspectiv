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
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
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
	mailer  mailer.Mailer
	metrics *metrics.Metrics
	log     zerolog.Logger
	baseURL string
	unsub   Unsubscriber
}

// NewEmailWorker returns the worker for email jobs. It renders the job's
// template, with links under publicBaseURL, and sends it through m.
//
// A provider failure never reaches the request that queued the email: the
// request only inserted the job. The failure increments
// email_send_failures_total and the job is retried on the schedule above.
func NewEmailWorker(m mailer.Mailer, met *metrics.Metrics, log zerolog.Logger, publicBaseURL string, unsub Unsubscriber) river.Worker[jobs.EmailArgs] {
	return &emailWorker{
		mailer:  m,
		metrics: met,
		log:     log.With().Str("component", "email").Logger(),
		baseURL: strings.TrimRight(publicBaseURL, "/"),
		unsub:   unsub,
	}
}

func (w *emailWorker) Work(ctx context.Context, job *river.Job[jobs.EmailArgs]) error {
	msg, err := w.render(job.Args)
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

// render turns an email job into a message.
func (w *emailWorker) render(a jobs.EmailArgs) (mailer.Message, error) {
	if a.To == "" {
		return mailer.Message{}, errors.New("no recipient")
	}
	var c content
	var err error
	switch a.Template {
	case jobs.TemplateVerifyEmail:
		c, err = w.accountContent(a.Data, "/verify-email",
			"Verify your email",
			"Confirm this address to start writing perspectives on Perspectiv.",
			"Verify email")
	case jobs.TemplatePasswordReset:
		c, err = w.accountContent(a.Data, "/reset-password",
			"Reset your password",
			"Someone asked to reset the password for this Perspectiv account. If it was not you, ignore this email.",
			"Choose a new password")
	case jobs.TemplateBetaInvite:
		c, err = w.accountContent(a.Data, "/signup",
			"You're invited to Perspectiv",
			"A place in the Perspectiv beta is yours. Perspectiv is where people who were at the same place on the same day each write what they saw, and find the strangers who were in their story.",
			"Create your account")
	case jobs.TemplateChangeEmail:
		c, err = w.accountContent(a.Data, "/confirm-email",
			"Confirm your new email",
			"Confirm this address to make it the email of your Perspectiv account. Until you do, your account keeps its old address.",
			"Confirm new email")
	case jobs.TemplateEmailChangeNotice:
		c, err = w.accountContent(a.Data, "/forgot-password",
			"Your Perspectiv email is being changed",
			"Someone signed in to your Perspectiv account asked to change its email away from this address. If it was you, there is nothing to do. If it was not, reset your password now: that also cancels the change.",
			"Reset your password")
	case jobs.TemplateDigest:
		c, err = w.digestContent(a.Data)
	case jobs.TemplateNotification:
		c, err = w.notificationContent(a.Data)
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
		unsubscribe, err = w.withUnsubscribe(&c, a.Data)
		if err != nil {
			return mailer.Message{}, err
		}
	}
	msg, err := c.message(a.To)
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

func (w *emailWorker) notificationContent(data map[string]string) (content, error) {
	kind := perspectivv1.NotificationKind(perspectivv1.NotificationKind_value[data[dataKind]])
	actor := data[dataActor]
	if actor == "" {
		actor = "Someone"
	}
	arc := data[dataArcTitle]
	if arc == "" {
		arc = "your arc"
	}
	var subject, body string
	switch kind {
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED:
		subject = actor + " says they are in your story"
		body = actor + " says they are one of the characters in your perspective in " + arc + ". Accept or reject the claim."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED:
		subject = "Your claim was accepted"
		body = actor + " accepted your claim in " + arc + ". Your perspectives are now linked."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC:
		subject = "A new perspective in " + arc
		body = actor + " published a new perspective in " + arc + "."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED:
		subject = "A remove vote opened in " + arc
		body = "A member of your lore in " + arc + " opened a vote to remove a perspective from it."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE:
		subject = "The canon changed in " + arc
		body = "A different lore is now canon in " + arc + ", or the arc no longer has one."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_INVITE_ANSWERED:
		subject = actor + " wrote their side of " + arc
		body = actor + " followed your invite and wrote their perspective in " + arc + ". Read it, and claim each other's characters if you recognise yourselves."
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE:
		subject = actor + " wrote about " + arc
		body = actor + " wrote a new story in " + arc + ", at a place you follow. Were you there too?"
	case perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER:
		subject = actor + " wrote a new story"
		body = actor + ", a writer you follow, wrote a new story in " + arc + "."
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
		c, err := w.notificationContent(item)
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
