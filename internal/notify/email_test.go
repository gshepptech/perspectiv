package notify_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/rs/zerolog"

	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/notify"
	"perspectiv/internal/notify/mailer"
)

const (
	baseURL     = "https://perspectiv.test"
	recipientID = "6f1c2d3e-4a5b-4c6d-8e9f-0a1b2c3d4e5f"
)

var unsub = notify.NewUnsubscriber("a test secret long enough for the signer")

// recorder is a Mailer that keeps what it was asked to send and fails with
// err when set.
type recorder struct {
	mu   sync.Mutex
	err  error
	sent []mailer.Message
}

func (r *recorder) Send(_ context.Context, m mailer.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.sent = append(r.sent, m)
	return nil
}

func (r *recorder) messages() []mailer.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mailer.Message(nil), r.sent...)
}

func emailJob(attempt int, args jobs.EmailArgs) *river.Job[jobs.EmailArgs] {
	return &river.Job[jobs.EmailArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: 25, Kind: "email"},
		Args:   args,
	}
}

// TestEmailWorkerRendersTemplates covers the three templates: each produces a
// subject, a text body and an HTML body carrying the link.
func TestEmailWorkerRendersTemplates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		args        jobs.EmailArgs
		wantSubject string
		wantText    []string
		wantHTML    []string
		notInHTML   []string
	}{
		{
			name: "a digest lists each event with its own link, and links the inbox",
			args: jobs.EmailArgs{To: "ben@example.test", Template: jobs.TemplateDigest, Data: map[string]string{
				"user_id": recipientID,
				"count":   "2",
				"items": `[{"kind":"NOTIFICATION_KIND_CLAIM_RECEIVED","path":"/claims","actor":"otter","arc_title":"Rainy Tuesday"},` +
					`{"kind":"NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE","path":"/perspectives/p-1","actor":"wren","arc_title":"Rainy Tuesday"}]`,
			}},
			wantSubject: "Your day on Perspectiv: 2 updates",
			wantText:    []string{"- otter says they are in your story", baseURL + "/claims", "- wren wrote about Rainy Tuesday", baseURL + "/perspectives/p-1", baseURL + "/inbox"},
			wantHTML:    []string{"<li><a href=\"" + baseURL + "/claims\">otter says they are in your story</a></li>", baseURL + "/inbox"},
		},
		{
			name:        "verification link built from a token (AC-043)",
			args:        jobs.EmailArgs{To: "new@example.test", Template: jobs.TemplateVerifyEmail, Data: map[string]string{"token": "tok-123"}},
			wantSubject: "Verify your email",
			wantText:    []string{baseURL + "/verify-email?token=tok-123"},
			wantHTML:    []string{baseURL + "/verify-email?token=tok-123"},
		},
		{
			name:        "verification link passed whole",
			args:        jobs.EmailArgs{To: "new@example.test", Template: jobs.TemplateVerifyEmail, Data: map[string]string{"link": baseURL + "/verify-email?token=abc"}},
			wantSubject: "Verify your email",
			wantText:    []string{baseURL + "/verify-email?token=abc"},
		},
		{
			name:        "token is query-escaped",
			args:        jobs.EmailArgs{To: "new@example.test", Template: jobs.TemplateVerifyEmail, Data: map[string]string{"token": "a+b/c="}},
			wantSubject: "Verify your email",
			wantText:    []string{baseURL + "/verify-email?token=a%2Bb%2Fc%3D"},
		},
		{
			name:        "password reset",
			args:        jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplatePasswordReset, Data: map[string]string{"token": "r1"}},
			wantSubject: "Reset your password",
			wantText:    []string{baseURL + "/reset-password?token=r1"},
		},
		{
			name:        "email change confirmation goes to the new address's page",
			args:        jobs.EmailArgs{To: "new@example.test", Template: jobs.TemplateChangeEmail, Data: map[string]string{"token": "c1"}},
			wantSubject: "Confirm your new email",
			wantText:    []string{baseURL + "/confirm-email?token=c1"},
		},
		{
			name: "email change notice points the old address at a password reset",
			args: jobs.EmailArgs{To: "old@example.test", Template: jobs.TemplateEmailChangeNotice, Data: map[string]string{
				"link": baseURL + "/forgot-password",
			}},
			wantSubject: "Your Perspectiv email is being changed",
			wantText:    []string{"If it was not, reset your password now: that also cancels the change.", baseURL + "/forgot-password"},
		},
		{
			name: "claim received names the claimant and the arc (FR-037)",
			args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{
				"user_id": recipientID, "kind": "NOTIFICATION_KIND_CLAIM_RECEIVED", "actor": "sam", "arc_title": "Rainy Tuesday", "path": "/claims",
			}},
			wantSubject: "sam says they are in your story",
			wantText:    []string{"Rainy Tuesday", baseURL + "/claims", baseURL + "/settings"},
		},
		{
			name: "user content is escaped in HTML",
			args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{
				"user_id": recipientID, "kind": "NOTIFICATION_KIND_CANON_CHANGE", "arc_title": "<script>x</script>", "path": "/arcs/1",
			}},
			wantSubject: "The canon changed in <script>x</script>",
			wantHTML:    []string{"&lt;script&gt;x&lt;/script&gt;"},
			notInHTML:   []string{"<script>"},
		},
		{
			name: "every kind has its own subject",
			args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{
				"user_id": recipientID, "kind": "NOTIFICATION_KIND_REPORT_RESOLVED", "path": "/inbox",
			}},
			wantSubject: "Your report was resolved",
			wantText:    []string{baseURL + "/inbox"},
		},
		{
			name: "a followed writer's new story names the writer and the day",
			args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{
				"user_id": recipientID, "kind": "NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER", "actor": "wren",
				"arc_title": "Rainy Tuesday", "path": "/perspectives/p-1",
			}},
			wantSubject: "wren wrote a new story",
			wantText:    []string{"wren, a writer you follow, wrote a new story in Rainy Tuesday.", baseURL + "/perspectives/p-1"},
		},
		{
			name: "a thread reply names the replier and links the lore",
			args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{
				"user_id": recipientID, "kind": "NOTIFICATION_KIND_THREAD_REPLY", "actor": "otter",
				"arc_title": "Rainy Tuesday", "path": "/lores/l-1",
			}},
			wantSubject: "otter replied to your post",
			wantText:    []string{"otter replied to your post in a lore's thread in Rainy Tuesday.", baseURL + "/lores/l-1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			w := notify.NewEmailWorker(rec, metrics.New(prometheus.NewRegistry()), zerolog.New(io.Discard), baseURL, unsub)
			if err := w.Work(context.Background(), emailJob(1, tc.args)); err != nil {
				t.Fatalf("Work: %v", err)
			}
			sent := rec.messages()
			if len(sent) != 1 {
				t.Fatalf("sent %d messages, want 1", len(sent))
			}
			m := sent[0]
			if m.To != tc.args.To || m.Subject != tc.wantSubject {
				t.Fatalf("To/Subject = %q/%q, want %q/%q", m.To, m.Subject, tc.args.To, tc.wantSubject)
			}
			wantContains(t, "text", m.Text, tc.wantText, true)
			wantContains(t, "html", m.HTML, tc.wantHTML, true)
			wantContains(t, "html", m.HTML, tc.notInHTML, false)
		})
	}
}

// wantContains fails unless body contains (present) or lacks (!present) each
// of subs.
func wantContains(t *testing.T, label, body string, subs []string, present bool) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(body, sub) != present {
			t.Fatalf("%s %q: contains %q = %v, want %v", label, body, sub, !present, present)
		}
	}
}

// A job that can never render is cancelled, not retried, and never reaches
// the provider.
func TestEmailWorkerCancelsUnrenderableJobs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args jobs.EmailArgs
	}{
		{name: "unknown template", args: jobs.EmailArgs{To: "a@example.test", Template: "welcome"}},
		{name: "verification without a token or link", args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateVerifyEmail}},
		{name: "notification of an unknown kind", args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{"kind": "NOTIFICATION_KIND_UNSPECIFIED"}}},
		{name: "no recipient", args: jobs.EmailArgs{Template: jobs.TemplatePasswordReset, Data: map[string]string{"token": "x"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			m := metrics.New(prometheus.NewRegistry())
			w := notify.NewEmailWorker(rec, m, zerolog.New(io.Discard), baseURL, unsub)
			err := w.Work(context.Background(), emailJob(1, tc.args))
			var cancel *rivertype.JobCancelError
			if !errors.As(err, &cancel) {
				t.Fatalf("Work = %v, want a job cancel", err)
			}
			if len(rec.messages()) != 0 {
				t.Fatal("an unrenderable job reached the provider")
			}
			if got := testutil.ToFloat64(m.EmailSendFailures); got != 0 {
				t.Fatalf("email_send_failures_total = %v, want 0 (nothing was sent)", got)
			}
		})
	}
}

// A provider failure is counted and returned so River retries it; the last
// allowed attempt cancels the job instead (AC-042, NFR-009).
func TestEmailWorkerProviderFailure(t *testing.T) {
	t.Parallel()
	args := jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplatePasswordReset, Data: map[string]string{"token": "x"}}
	tests := []struct {
		name       string
		attempt    int
		wantCancel bool
	}{
		{name: "first attempt is retryable", attempt: 1},
		{name: "seventh attempt is retryable", attempt: notify.EmailMaxAttempts - 1},
		{name: "last attempt gives up", attempt: notify.EmailMaxAttempts, wantCancel: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			provider := errors.New("provider down")
			m := metrics.New(prometheus.NewRegistry())
			w := notify.NewEmailWorker(&recorder{err: provider}, m, zerolog.New(io.Discard), baseURL, unsub)
			err := w.Work(context.Background(), emailJob(tc.attempt, args))
			if !errors.Is(err, provider) {
				t.Fatalf("Work = %v, want the provider error", err)
			}
			var cancel *rivertype.JobCancelError
			if got := errors.As(err, &cancel); got != tc.wantCancel {
				t.Fatalf("cancelled = %v, want %v", got, tc.wantCancel)
			}
			if got := testutil.ToFloat64(m.EmailSendFailures); got != 1 {
				t.Fatalf("email_send_failures_total = %v, want 1", got)
			}
		})
	}
}

// The retry schedule doubles from 30 seconds and is capped at one hour.
func TestEmailWorkerRetrySchedule(t *testing.T) {
	t.Parallel()
	w := notify.NewEmailWorker(&recorder{}, metrics.New(prometheus.NewRegistry()), zerolog.New(io.Discard), baseURL, unsub)
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 30 * time.Second},
		{2, time.Minute},
		{3, 2 * time.Minute},
		{7, 32 * time.Minute},
		{8, time.Hour},
		{40, time.Hour},
	}
	for _, tc := range tests {
		before := time.Now()
		got := w.NextRetry(emailJob(tc.attempt, jobs.EmailArgs{})).Sub(before)
		if got < tc.want-time.Second || got > tc.want+time.Second {
			t.Errorf("attempt %d: retry in %v, want %v", tc.attempt, got, tc.want)
		}
	}
	if got := w.Timeout(emailJob(1, jobs.EmailArgs{})); got <= 0 || got > time.Minute {
		t.Errorf("Timeout = %v, want a bound under a minute", got)
	}
}

// Notification and digest emails carry the RFC 8058 one-click header and an
// unsubscribe page link signed for their recipient; account emails carry
// neither, and a notification email without its recipient is not sent.
func TestNotificationEmailsCanBeUnsubscribed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      jobs.EmailArgs
		wantUnsub bool
		wantErr   bool
	}{
		{name: "notification", wantUnsub: true, args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{
			"user_id": recipientID, "kind": "NOTIFICATION_KIND_CLAIM_ACCEPTED", "path": "/arcs/a-1",
		}}},
		{name: "digest", wantUnsub: true, args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateDigest, Data: map[string]string{
			"user_id": recipientID, "count": "1", "items": `[{"kind":"NOTIFICATION_KIND_CLAIM_ACCEPTED","path":"/arcs/a-1"}]`,
		}}},
		{name: "password reset", args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplatePasswordReset, Data: map[string]string{"token": "r1"}}},
		{name: "notification without its recipient", wantErr: true, args: jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{
			"kind": "NOTIFICATION_KIND_CLAIM_ACCEPTED", "path": "/arcs/a-1",
		}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			w := notify.NewEmailWorker(rec, metrics.New(prometheus.NewRegistry()), zerolog.New(io.Discard), baseURL, unsub)
			err := w.Work(context.Background(), emailJob(1, tc.args))
			if tc.wantErr {
				if err == nil || len(rec.messages()) != 0 {
					t.Fatalf("Work = %v with %d sent, want a refusal and nothing sent", err, len(rec.messages()))
				}
				return
			}
			if err != nil {
				t.Fatalf("Work: %v", err)
			}
			checkUnsubscribe(t, rec.messages()[0], tc.wantUnsub)
		})
	}
}

// checkUnsubscribe fails unless m offers the recipient's signed unsubscribe
// links exactly when want is set.
func checkUnsubscribe(t *testing.T, m mailer.Message, want bool) {
	t.Helper()
	if !want {
		if m.Unsubscribe != "" || strings.Contains(m.Text, "/unsubscribe") {
			t.Fatalf("an account email offers unsubscribe: %q", m.Unsubscribe)
		}
		return
	}
	oneClick := baseURL + "/api/email/unsubscribe?t="
	page := baseURL + "/unsubscribe?t="
	if !strings.HasPrefix(m.Unsubscribe, oneClick) || !strings.HasSuffix(m.Unsubscribe, "&u="+recipientID) {
		t.Fatalf("one-click URL = %q", m.Unsubscribe)
	}
	wantContains(t, "text", m.Text, []string{page}, true)
	wantContains(t, "text", m.Text, []string{"Stop these emails: " + page}, true)
	// In HTML both are links with words, not bare addresses.
	wantContains(t, "html", m.HTML, []string{`">Stop these emails</a>.`, `<a href="` + baseURL + `/settings">Change it in Settings</a>.`}, true)
}
