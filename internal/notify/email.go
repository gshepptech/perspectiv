package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

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
}

// NewEmailWorker returns the worker for email jobs. It renders the job's
// template, with links under publicBaseURL, and sends it through m.
//
// A provider failure never reaches the request that queued the email: the
// request only inserted the job. The failure increments
// email_send_failures_total and the job is retried on the schedule above.
func NewEmailWorker(m mailer.Mailer, met *metrics.Metrics, log zerolog.Logger, publicBaseURL string) river.Worker[jobs.EmailArgs] {
	return &emailWorker{
		mailer:  m,
		metrics: met,
		log:     log.With().Str("component", "email").Logger(),
		baseURL: strings.TrimRight(publicBaseURL, "/"),
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
	case jobs.TemplateNotification:
		c, err = w.notificationContent(a.Data)
	default:
		err = fmt.Errorf("unknown template %q", a.Template)
	}
	if err != nil {
		return mailer.Message{}, err
	}
	return c.message(a.To)
}

// content is what every email shows: a subject, a paragraph, one link and an
// optional footer.
type content struct {
	Subject, Body, Action, Link, Footer string
}

var htmlBody = template.Must(template.New("email").Parse(
	`<!doctype html><html><body style="font-family:Georgia,serif;color:#1d1b18;line-height:1.5">` +
		`<p>{{.Body}}</p><p><a href="{{.Link}}">{{.Action}}</a></p>` +
		`<p style="font-size:13px;color:#5b564e">Or paste this link into your browser: {{.Link}}</p>` +
		`{{if .Footer}}<p style="font-size:13px;color:#5b564e">{{.Footer}}</p>{{end}}</body></html>`))

func (c content) message(to string) (mailer.Message, error) {
	var html bytes.Buffer
	if err := htmlBody.Execute(&html, c); err != nil {
		return mailer.Message{}, err
	}
	text := c.Body + "\n\n" + c.Action + ": " + c.Link + "\n"
	if c.Footer != "" {
		text += "\n" + c.Footer + "\n"
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
		Subject: subject,
		Body:    body,
		Action:  "Open Perspectiv",
		Link:    w.baseURL + path,
		Footer:  "Choose which emails you get at " + w.baseURL + "/settings",
	}, nil
}
