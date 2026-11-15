//go:build integration

package notify_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/notify"
)

// unsubscribeLink renders a notification email to user and returns its
// one-click URL's query: the link a real email would carry.
func unsubscribeLink(t *testing.T, user uuid.UUID) url.Values {
	t.Helper()
	rec := &recorder{}
	w := notify.NewEmailWorker(nil, rec, metrics.New(prometheus.NewRegistry()), zerolog.New(io.Discard), baseURL, unsub)
	if err := w.Work(context.Background(), emailJob(1, jobs.EmailArgs{To: "a@example.test", Template: jobs.TemplateNotification, Data: map[string]string{
		"user_id": user.String(), "kind": "NOTIFICATION_KIND_CLAIM_ACCEPTED", "path": "/arcs/a-1",
	}})); err != nil {
		t.Fatalf("render: %v", err)
	}
	u, err := url.Parse(rec.messages()[0].Unsubscribe)
	if err != nil {
		t.Fatalf("one-click URL: %v", err)
	}
	return u.Query()
}

// emailOn returns each kind's email toggle as GetPreferences reports it.
func (e *env) emailOn(user uuid.UUID) map[perspectivv1.NotificationKind]bool {
	e.t.Helper()
	res, err := e.client(user).GetPreferences(context.Background(), connect.NewRequest(&perspectivv1.GetPreferencesRequest{}))
	if err != nil {
		e.t.Fatalf("GetPreferences: %v", err)
	}
	out := map[perspectivv1.NotificationKind]bool{}
	for _, p := range res.Msg.GetPreferences() {
		out[p.GetKind()] = p.GetEmail()
	}
	return out
}

func TestUnsubscribeLink(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	wren, ben := e.user("wren"), e.user("ben")
	e.pref(wren, perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED, false, true)
	link := unsubscribeLink(t, wren)
	unsubscribe := func(userID, token string) error {
		_, err := e.client(uuid.Nil).Unsubscribe(context.Background(), connect.NewRequest(&perspectivv1.UnsubscribeRequest{UserId: userID, Token: token}))
		return err
	}

	tests := []struct {
		name, user, sig string
	}{
		{name: "another account's id", user: ben.String(), sig: link.Get("t")},
		{name: "a tampered signature", user: wren.String(), sig: "bm90LWEtcmVhbC1zaWc"},
		{name: "no signature", user: wren.String()},
		{name: "not an id", user: "wren", sig: link.Get("t")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := unsubscribe(tc.user, tc.sig); connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("Unsubscribe: %v, want InvalidArgument", err)
			}
		})
	}
	for kind, on := range e.emailOn(wren) {
		if !on {
			t.Fatalf("a refused link turned %v email off", kind)
		}
	}

	if err := unsubscribe(link.Get("u"), link.Get("t")); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	for kind, on := range e.emailOn(wren) {
		if on {
			t.Errorf("%v email still on after unsubscribing", kind)
		}
	}
	// In-app stays as the reader set it: off for claims received, else on.
	res, err := e.client(wren).GetPreferences(context.Background(), connect.NewRequest(&perspectivv1.GetPreferencesRequest{}))
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	for _, p := range res.Msg.GetPreferences() {
		want := p.GetKind() != perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED
		if p.GetInApp() != want {
			t.Errorf("%v in-app = %v, want %v", p.GetKind(), p.GetInApp(), want)
		}
	}
	for kind, on := range e.emailOn(ben) {
		if !on {
			t.Fatalf("unsubscribing wren turned ben's %v email off", kind)
		}
	}
}

// RFC 8058: a mail client POSTs List-Unsubscribe=One-Click to the header's
// URL with no cookies. A GET, as a link scanner sends, changes nothing.
func TestOneClickUnsubscribe(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	wren := e.user("wren")
	link := unsubscribeLink(t, wren)
	endpoint := e.srv.URL + "/api/email/unsubscribe?" + link.Encode()
	send := func(method, target string) int {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), method, target, strings.NewReader("List-Unsubscribe=One-Click"))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := e.srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	post := func(target string) int { return send(http.MethodPost, target) }

	if got := send(http.MethodGet, endpoint); got != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", got)
	}
	tampered := url.Values{"u": {wren.String()}, "t": {"bm90LWEtcmVhbC1zaWc"}}
	if got := post(e.srv.URL + "/api/email/unsubscribe?" + tampered.Encode()); got != http.StatusBadRequest {
		t.Fatalf("tampered POST status = %d, want 400", got)
	}
	for kind, on := range e.emailOn(wren) {
		if !on {
			t.Fatalf("a GET or tampered POST turned %v email off", kind)
		}
	}
	if got := post(endpoint); got != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", got)
	}
	for kind, on := range e.emailOn(wren) {
		if on {
			t.Errorf("%v email still on after one-click", kind)
		}
	}
}
