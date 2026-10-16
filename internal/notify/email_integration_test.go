//go:build integration

package notify_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/resend/resend-go/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/jobs"
	"perspectiv/internal/notify/mailer"
)

// mailpitImage matches the dev loop's Mailpit.
const mailpitImage = "axllent/mailpit:v1.31.6"

// mailpit is a running Mailpit container: SMTP for the mailer, HTTP for
// reading what arrived.
type mailpit struct {
	smtpAddr string
	apiURL   string
}

func startMailpit(t *testing.T) mailpit {
	t.Helper()
	ctx := context.Background()
	ctr, err := testcontainers.Run(ctx, mailpitImage,
		testcontainers.WithExposedPorts("1025/tcp", "8025/tcp"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/api/v1/messages").WithPort("8025/tcp").
			WithStartupTimeout(2*time.Minute)),
	)
	if ctr != nil {
		t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	}
	if err != nil {
		t.Fatalf("start %s: %v", mailpitImage, err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("mailpit host: %v", err)
	}
	smtpPort, err := ctr.MappedPort(ctx, "1025/tcp")
	if err != nil {
		t.Fatalf("mailpit smtp port: %v", err)
	}
	apiPort, err := ctr.MappedPort(ctx, "8025/tcp")
	if err != nil {
		t.Fatalf("mailpit api port: %v", err)
	}
	return mailpit{
		smtpAddr: fmt.Sprintf("%s:%s", host, smtpPort.Port()),
		apiURL:   fmt.Sprintf("http://%s:%s", host, apiPort.Port()),
	}
}

// message waits for a message to `to` and returns its subject and text body,
// read through Mailpit's HTTP API the way the E2E suite reads it.
func (m mailpit) message(t *testing.T, to string) (string, string) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		var list struct {
			Messages []struct {
				ID      string
				Subject string
			} `json:"messages"`
		}
		m.get(t, "/api/v1/search?query="+url.QueryEscape("to:"+to), &list)
		if len(list.Messages) > 0 {
			var msg struct{ Text string }
			m.get(t, "/api/v1/message/"+list.Messages[0].ID, &msg)
			return list.Messages[0].Subject, msg.Text
		}
		if time.Now().After(deadline) {
			t.Fatalf("no message to %s in Mailpit after %v", to, waitTimeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (m mailpit) get(t *testing.T, path string, into any) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, m.apiURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mailpit %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mailpit %s: %s", path, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatalf("mailpit %s: decode: %v", path, err)
	}
}

// enqueueEmail inserts an email job as a request handler does, inside a
// committed transaction, and returns its id.
func (e *env) enqueueEmail(a jobs.EmailArgs) int64 {
	e.t.Helper()
	ctx := context.Background()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		e.t.Fatalf("begin: %v", err)
	}
	if err := jobs.NewEnqueuer(e.river).EmailTx(ctx, tx, a); err != nil {
		_ = tx.Rollback(ctx)
		e.t.Fatalf("EmailTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		e.t.Fatalf("the originating transaction failed: %v", err)
	}
	var id int64
	if err := e.pool.QueryRow(ctx, `SELECT max(id) FROM river_job WHERE kind = 'email'`).Scan(&id); err != nil {
		e.t.Fatalf("find email job: %v", err)
	}
	return id
}

// A verification email sent through the SMTP mailer lands in Mailpit, and its
// link can be read back from Mailpit's API (AC-043, OT-001, FR-038).
func TestVerificationEmailReachesMailpit(t *testing.T) {
	t.Parallel()
	mp := startMailpit(t)
	e := newEnvWithMailer(t, mailer.NewSMTP(mp.smtpAddr, "Perspectiv <no-reply@perspectiv.test>"))

	id := e.enqueueEmail(jobs.EmailArgs{
		To: "newcomer@example.test", Template: jobs.TemplateVerifyEmail,
		Data: map[string]string{"token": "verify-token-1"},
	})
	if state := e.waitFinal(id); state != "completed" {
		t.Fatalf("email job = %s, want completed", state)
	}
	subject, text := mp.message(t, "newcomer@example.test")
	if subject != "Verify your email" {
		t.Fatalf("subject = %q", subject)
	}
	if want := baseURL + "/verify-email?token=verify-token-1"; !strings.Contains(text, want) {
		t.Fatalf("text %q lacks the verification link %q", text, want)
	}
	if got := testutil.ToFloat64(e.metrics.EmailSendFailures); got != 0 {
		t.Fatalf("email_send_failures_total = %v, want 0", got)
	}
}

// A notification fanned out end to end arrives in Mailpit for the recipient
// (FR-037, FR-048).
func TestNotificationEmailReachesMailpit(t *testing.T) {
	t.Parallel()
	mp := startMailpit(t)
	e := newEnvWithMailer(t, mailer.NewSMTP(mp.smtpAddr, "no-reply@perspectiv.test"))
	arc := e.arc("Rainy Tuesday")
	ann, ben := e.user("ann"), e.user("ben")
	pAnn, pBen := e.published(arc, ann), e.published(arc, ben)
	claim := e.claim(pAnn, pBen, "CLAIM_STATUS_ACCEPTED")

	e.notifyNow(jobs.NotifyArgs{Kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED, ActorID: ann, ClaimID: claim})

	subject, text := mp.message(t, "ben@example.test")
	if subject != "Your claim was accepted" || !strings.Contains(text, "ann accepted your claim in Rainy Tuesday") {
		t.Fatalf("message = %q / %q", subject, text)
	}
}

// A failing Resend call leaves the originating transaction committed and the
// email job retryable, counted in email_send_failures_total (OT-042, AC-042,
// NFR-009).
func TestResendFailureLeavesJobRetryable(t *testing.T) {
	t.Parallel()
	calls := make(chan struct{}, 10)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"statusCode":503,"name":"internal_server_error","message":"down"}`))
	}))
	t.Cleanup(api.Close)
	rc := resend.NewCustomClient(api.Client(), "re_test")
	u, err := url.Parse(api.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	rc.BaseURL = u
	e := newEnvWithMailer(t, mailer.NewResendClient(rc, "no-reply@perspectiv.test"))

	// The originating write and its email job commit together; the provider
	// is not called inside the request.
	user := e.user("newcomer")
	id := e.enqueueEmail(jobs.EmailArgs{
		To: "newcomer@example.test", Template: jobs.TemplateVerifyEmail,
		Data: map[string]string{"token": "t"},
	})
	var exists bool
	if err := e.pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, user).Scan(&exists); err != nil || !exists {
		t.Fatalf("originating row missing: %v", err)
	}

	if state := e.waitState(id, "retryable", "completed", "cancelled", "discarded"); state != "retryable" {
		t.Fatalf("email job = %s, want retryable", state)
	}
	select {
	case <-calls:
	default:
		t.Fatal("the Resend API was never called")
	}
	var attempt int
	var scheduledAt time.Time
	var errCount int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT attempt, scheduled_at, coalesce(array_length(errors, 1), 0) FROM river_job WHERE id = $1`, id).
		Scan(&attempt, &scheduledAt, &errCount); err != nil {
		t.Fatal(err)
	}
	if attempt != 1 || errCount != 1 {
		t.Fatalf("attempt = %d, errors = %d, want 1 and 1", attempt, errCount)
	}
	if wait := time.Until(scheduledAt); wait < 20*time.Second || wait > 31*time.Second {
		t.Fatalf("next attempt in %v, want about 30s", wait)
	}
	if got := testutil.ToFloat64(e.metrics.EmailSendFailures); got != 1 {
		t.Fatalf("email_send_failures_total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(e.metrics.JobFailures); got != 1 {
		t.Fatalf("job_failures_total = %v, want 1", got)
	}
}
