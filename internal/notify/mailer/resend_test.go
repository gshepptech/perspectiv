package mailer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/resend/resend-go/v3"

	"perspectiv/internal/notify/mailer"
)

// resendAPI is an HTTP server speaking the Resend send-email endpoint. It
// records the last request and answers with status.
type resendAPI struct {
	status int
	got    struct {
		path, auth string
		body       map[string]any
	}
}

func (a *resendAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.got.path = r.URL.Path
	a.got.auth = r.Header.Get("Authorization")
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &a.got.body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(a.status)
	if a.status == http.StatusOK {
		_, _ = w.Write([]byte(`{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`))
		return
	}
	_, _ = w.Write([]byte(`{"statusCode":` + http.StatusText(a.status) + `,"name":"application_error","message":"provider down"}`))
}

func resendAt(t *testing.T, api *resendAPI) mailer.Mailer {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	c := resend.NewCustomClient(srv.Client(), "re_test_key")
	u, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	c.BaseURL = u
	return mailer.NewResendClient(c, "Perspectiv <no-reply@perspectiv.test>")
}

func TestResendSend(t *testing.T) {
	t.Parallel()
	msg := mailer.Message{
		To:      "reader@example.test",
		Subject: "Verify your email",
		Text:    "Open https://perspectiv.test/verify-email?token=abc",
		HTML:    "<p>Open the link</p>",
	}
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "accepted", status: http.StatusOK},
		{name: "provider error is returned", status: http.StatusInternalServerError, wantErr: true},
		{name: "validation error is returned", status: http.StatusUnprocessableEntity, wantErr: true},
		{name: "rate limit is returned", status: http.StatusTooManyRequests, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := &resendAPI{status: tc.status}
			err := resendAt(t, api).Send(context.Background(), msg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Send err = %v, wantErr %v", err, tc.wantErr)
			}
			if api.got.path != "/emails" {
				t.Fatalf("path = %q, want /emails", api.got.path)
			}
			if api.got.auth != "Bearer re_test_key" {
				t.Fatalf("Authorization = %q", api.got.auth)
			}
			b := api.got.body
			if b["from"] != "Perspectiv <no-reply@perspectiv.test>" || b["subject"] != msg.Subject ||
				b["text"] != msg.Text || b["html"] != msg.HTML {
				t.Fatalf("body = %v", b)
			}
			to, _ := b["to"].([]any)
			if len(to) != 1 || to[0] != msg.To {
				t.Fatalf("to = %v, want [%s]", b["to"], msg.To)
			}
		})
	}
}

func TestResendRefusesBadMessageWithoutCalling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  mailer.Message
	}{
		{name: "no recipient", msg: mailer.Message{Subject: "s", Text: "t"}},
		{name: "unparsable recipient", msg: mailer.Message{To: "not an address", Subject: "s", Text: "t"}},
		{name: "no body", msg: mailer.Message{To: "a@example.test", Subject: "s"}},
		{name: "subject with a line break", msg: mailer.Message{To: "a@example.test", Subject: "s\r\nBcc: x@example.test", Text: "t"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := &resendAPI{status: http.StatusOK}
			if err := resendAt(t, api).Send(context.Background(), tc.msg); err == nil {
				t.Fatal("Send = nil, want an error")
			}
			if api.got.path != "" {
				t.Fatalf("the provider was called for an invalid message")
			}
		})
	}
}

func TestNewResendTargetsTheResendAPI(t *testing.T) {
	t.Parallel()
	// NewResend is the production constructor; with an unreachable context it
	// must fail fast rather than block, and never panic on a nil client.
	m := mailer.NewResend("re_key", "no-reply@perspectiv.test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := m.Send(ctx, mailer.Message{To: "a@example.test", Subject: "s", Text: "t"})
	if err == nil || !strings.Contains(err.Error(), "resend") {
		t.Fatalf("Send with a cancelled context = %v, want a resend error", err)
	}
}
