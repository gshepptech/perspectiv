//go:build integration

package notify_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"connectrpc.com/connect"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/jobs"
	"perspectiv/internal/notify"
)

// scan runs a one-row query.
func (e *env) scan(query string, args []any, dest ...any) {
	e.t.Helper()
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(dest...); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
}

// runDigest runs one digest pass and waits for it.
func (e *env) runDigest() {
	e.t.Helper()
	res, err := e.river.Insert(context.Background(), notify.DigestArgs{}, nil)
	if err != nil {
		e.t.Fatalf("insert digest job: %v", err)
	}
	if state := e.waitFinal(res.Job.ID); state != "completed" {
		e.t.Fatalf("digest job state = %s, want completed", state)
	}
}

// A digest account's email-worthy events wait as digest items instead of
// emails; the digest pass sends one summary of them, clears them, and sends
// nothing again until the next day. Others still get one email per event.
func TestDigest(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	s.exec(`UPDATE users SET email_digest = true WHERE id = $1`, s.ben)
	kind := perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC
	s.notifyNow(jobs.NotifyArgs{Kind: kind, ActorID: s.ann, ArcID: s.arc, PerspectiveID: s.pAnn})
	s.notifyNow(jobs.NotifyArgs{Kind: kind, ActorID: s.cat, ArcID: s.arc, PerspectiveID: s.pCat})

	// Ben still sees both in the inbox, but no email yet; Dan, without the
	// digest, has one email per event.
	s.expect(t, s.ben, "ben", []perspectivv1.NotificationKind{kind, kind}, 0)
	if got := len(s.emailJobs("dan@example.test")); got != 2 {
		t.Fatalf("dan email jobs = %d, want 2", got)
	}
	var waiting int
	s.scan(`SELECT count(*) FROM digest_items WHERE user_id = $1`, []any{s.ben}, &waiting)
	if waiting != 2 {
		t.Fatalf("ben digest items = %d, want 2", waiting)
	}

	s.runDigest()
	mails := s.emailJobs("ben@example.test")
	if len(mails) != 1 || mails[0].args.Template != jobs.TemplateDigest || mails[0].args.Data["count"] != "2" {
		t.Fatalf("ben digest emails = %+v", mails)
	}
	var items []map[string]string
	if err := json.Unmarshal([]byte(mails[0].args.Data["items"]), &items); err != nil || len(items) != 2 {
		t.Fatalf("digest items = %v, %v", items, err)
	}
	var sent *time.Time
	s.scan(`SELECT count(*) FROM digest_items WHERE user_id = $1`, []any{s.ben}, &waiting)
	s.scan(`SELECT digest_sent_at FROM users WHERE id = $1`, []any{s.ben}, &sent)
	if waiting != 0 || sent == nil {
		t.Fatalf("after the digest: items %d, sent at %v", waiting, sent)
	}

	// A new event the same day waits for tomorrow's digest.
	s.notifyNow(jobs.NotifyArgs{Kind: kind, ActorID: s.dan, ArcID: s.arc, PerspectiveID: s.pDan})
	s.runDigest()
	if got := len(s.emailJobs("ben@example.test")); got != 1 {
		t.Fatalf("ben emails after a second pass = %d, want still 1", got)
	}
	// A day later it goes out.
	s.exec(`UPDATE users SET digest_sent_at = now() - interval '1 day' WHERE id = $1`, s.ben)
	s.runDigest()
	if got := len(s.emailJobs("ben@example.test")); got != 2 {
		t.Fatalf("ben emails the next day = %d, want 2", got)
	}
}

// The digest setting reads and writes through the preferences API; leaving
// it out of an update keeps it.
func TestDigestSetting(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := e.user("pat")
	c := e.client(me)
	get := func() bool {
		t.Helper()
		res, err := c.GetPreferences(ctx, connect.NewRequest(&perspectivv1.GetPreferencesRequest{}))
		if err != nil {
			t.Fatalf("GetPreferences: %v", err)
		}
		return res.Msg.GetEmailDigest()
	}
	if get() {
		t.Fatal("digest is on by default")
	}
	on := true
	res, err := c.UpdatePreferences(ctx, connect.NewRequest(&perspectivv1.UpdatePreferencesRequest{EmailDigest: &on}))
	if err != nil || !res.Msg.GetEmailDigest() {
		t.Fatalf("turn on = %v, %v", res, err)
	}
	if _, err := c.UpdatePreferences(ctx, connect.NewRequest(&perspectivv1.UpdatePreferencesRequest{
		Preferences: []*perspectivv1.Preference{{Kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE, InApp: true}},
	})); err != nil {
		t.Fatalf("toggle update: %v", err)
	}
	if !get() {
		t.Fatal("an update without the digest field turned it off")
	}
}
