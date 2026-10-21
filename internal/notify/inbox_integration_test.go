//go:build integration

package notify_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// seed inserts n notifications for user, one second apart, oldest first, and
// returns their ids newest first (the inbox order).
func (e *env) seed(user uuid.UUID, n int, payload string) []uuid.UUID {
	e.t.Helper()
	ids := make([]uuid.UUID, n)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := range n {
		ids[n-1-i] = e.id(`INSERT INTO notifications (user_id, kind, payload, created_at)
			VALUES ($1, 'NOTIFICATION_KIND_CANON_CHANGE', $2::jsonb, $3) RETURNING id`,
			user, payload, base.Add(time.Duration(i)*time.Second))
	}
	return ids
}

// The inbox and preference calls need a signed-in caller (CT-014).
func TestNotifyServiceRequiresSignIn(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.client(uuid.Nil)
	ctx := context.Background()
	calls := map[string]func() error{
		"ListInbox": func() error {
			_, err := c.ListInbox(ctx, connect.NewRequest(&perspectivv1.ListInboxRequest{}))
			return err
		},
		"MarkRead": func() error {
			_, err := c.MarkRead(ctx, connect.NewRequest(&perspectivv1.MarkReadRequest{All: true}))
			return err
		},
		"GetPreferences": func() error {
			_, err := c.GetPreferences(ctx, connect.NewRequest(&perspectivv1.GetPreferencesRequest{}))
			return err
		},
		"UpdatePreferences": func() error {
			_, err := c.UpdatePreferences(ctx, connect.NewRequest(&perspectivv1.UpdatePreferencesRequest{}))
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wantCode(t, call(), connect.CodeUnauthenticated)
		})
	}
}

// The inbox lists only the caller's notifications, newest first, with the
// actor as a username and the arc title, through cursor pages (CT-014,
// NFR-008).
func TestListInbox(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other, actor := e.user("me"), e.user("other"), e.user("sam")
	arc := e.arc("Rainy Tuesday")
	payload := `{"actor_id":"` + actor.String() + `","arc_id":"` + arc.String() + `","lore_id":"` + uuid.NewString() + `"}`
	ids := e.seed(me, 5, payload)
	e.seed(other, 3, `{}`)

	c := e.client(me)
	var got []uuid.UUID
	token := ""
	pages := 0
	for {
		resp, err := c.ListInbox(ctx, connect.NewRequest(&perspectivv1.ListInboxRequest{PageSize: 2, PageToken: token}))
		if err != nil {
			t.Fatalf("ListInbox: %v", err)
		}
		pages++
		if resp.Msg.GetUnreadCount() != 5 {
			t.Fatalf("unread_count = %d, want 5", resp.Msg.GetUnreadCount())
		}
		for _, n := range resp.Msg.GetNotifications() {
			got = append(got, uuid.MustParse(n.GetId()))
			checkSeeded(t, n, actor, arc)
		}
		token = resp.Msg.GetNextPageToken()
		if token == "" {
			break
		}
		if pages > 5 {
			t.Fatal("pagination does not terminate")
		}
	}
	if len(got) != len(ids) {
		t.Fatalf("listed %d notifications, want %d", len(got), len(ids))
	}
	for i := range ids {
		if got[i] != ids[i] {
			t.Fatalf("order = %v, want %v", got, ids)
		}
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3", pages)
	}
}

// checkSeeded checks one listed notification seeded by TestListInbox.
func checkSeeded(t *testing.T, n *perspectivv1.Notification, actor, arc uuid.UUID) {
	t.Helper()
	if n.GetKind() != perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE {
		t.Fatalf("kind = %v", n.GetKind())
	}
	if n.GetActor().GetUsername() != "sam" || n.GetActor().GetUserId() != actor.String() {
		t.Fatalf("actor = %v, want sam", n.GetActor())
	}
	if n.GetArcId() != arc.String() || n.GetArcTitle() != "Rainy Tuesday" {
		t.Fatalf("arc = %s %q", n.GetArcId(), n.GetArcTitle())
	}
	if n.GetLoreId() == "" || n.GetClaimId() != "" || n.GetReadAt() != nil || n.GetCreatedAt() == nil {
		t.Fatalf("notification fields = %v", n)
	}
}

func TestListInboxRejectsBadToken(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, err := e.client(e.user("me")).ListInbox(context.Background(),
		connect.NewRequest(&perspectivv1.ListInboxRequest{PageToken: "not-a-token"}))
	wantCode(t, err, connect.CodeInvalidArgument)
}

// A system notification has no actor; an empty inbox lists nothing.
func TestListInboxEmptyAndSystem(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := e.user("me")
	resp, err := e.client(me).ListInbox(ctx, connect.NewRequest(&perspectivv1.ListInboxRequest{}))
	if err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	if len(resp.Msg.GetNotifications()) != 0 || resp.Msg.GetNextPageToken() != "" || resp.Msg.GetUnreadCount() != 0 {
		t.Fatalf("empty inbox = %v", resp.Msg)
	}
	e.seed(me, 1, `{"report_id":"`+uuid.NewString()+`"}`)
	resp, err = e.client(me).ListInbox(ctx, connect.NewRequest(&perspectivv1.ListInboxRequest{}))
	if err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	n := resp.Msg.GetNotifications()
	if len(n) != 1 || n[0].GetActor() != nil || n[0].GetReportId() == "" || n[0].GetArcTitle() != "" {
		t.Fatalf("system notification = %v", n)
	}
}

// MarkRead marks the caller's own notifications only, and unread_only then
// filters them out.
func TestMarkRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := e.user("me"), e.user("other")
	mine := e.seed(me, 3, `{}`)
	theirs := e.seed(other, 1, `{}`)
	c := e.client(me)

	resp, err := c.MarkRead(ctx, connect.NewRequest(&perspectivv1.MarkReadRequest{
		NotificationIds: []string{mine[0].String(), theirs[0].String()},
	}))
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if resp.Msg.GetUnreadCount() != 2 {
		t.Fatalf("unread after one = %d, want 2", resp.Msg.GetUnreadCount())
	}
	var otherRead bool
	if err := e.pool.QueryRow(ctx, `SELECT read_at IS NOT NULL FROM notifications WHERE id = $1`, theirs[0]).Scan(&otherRead); err != nil {
		t.Fatal(err)
	}
	if otherRead {
		t.Fatal("MarkRead marked another user's notification")
	}

	unread, err := c.ListInbox(ctx, connect.NewRequest(&perspectivv1.ListInboxRequest{UnreadOnly: true}))
	if err != nil {
		t.Fatalf("ListInbox unread: %v", err)
	}
	if len(unread.Msg.GetNotifications()) != 2 {
		t.Fatalf("unread_only listed %d, want 2", len(unread.Msg.GetNotifications()))
	}
	all, err := c.ListInbox(ctx, connect.NewRequest(&perspectivv1.ListInboxRequest{}))
	if err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	if all.Msg.GetNotifications()[0].GetReadAt() == nil {
		t.Fatal("read notification has no read_at")
	}

	resp, err = c.MarkRead(ctx, connect.NewRequest(&perspectivv1.MarkReadRequest{All: true}))
	if err != nil {
		t.Fatalf("MarkRead all: %v", err)
	}
	if resp.Msg.GetUnreadCount() != 0 {
		t.Fatalf("unread after all = %d, want 0", resp.Msg.GetUnreadCount())
	}
}

func TestMarkReadValidation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.client(e.user("me"))
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}
	tests := []struct {
		name string
		req  *perspectivv1.MarkReadRequest
	}{
		{name: "nothing named", req: &perspectivv1.MarkReadRequest{}},
		{name: "not an id", req: &perspectivv1.MarkReadRequest{NotificationIds: []string{"nope"}}},
		{name: "too many ids", req: &perspectivv1.MarkReadRequest{NotificationIds: tooMany}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := c.MarkRead(context.Background(), connect.NewRequest(tc.req))
			wantCode(t, err, connect.CodeInvalidArgument)
		})
	}
}

func prefMap(t *testing.T, ps []*perspectivv1.Preference) map[perspectivv1.NotificationKind][2]bool {
	t.Helper()
	// One preference per notification kind; UNSPECIFIED is not a kind.
	if want := len(perspectivv1.NotificationKind_name) - 1; len(ps) != want {
		t.Fatalf("preferences = %d entries, want %d", len(ps), want)
	}
	out := map[perspectivv1.NotificationKind][2]bool{}
	for i, p := range ps {
		if p.GetKind() != perspectivv1.NotificationKind(i+1) {
			t.Fatalf("preference %d is %v, want enum order", i, p.GetKind())
		}
		out[p.GetKind()] = [2]bool{p.GetInApp(), p.GetEmail()}
	}
	return out
}

// Both toggles default on for every kind; an update changes only the
// kinds it lists (AC-041, CT-014).
func TestPreferences(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := e.user("me")
	c := e.client(me)

	got, err := c.GetPreferences(ctx, connect.NewRequest(&perspectivv1.GetPreferencesRequest{}))
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	for k, v := range prefMap(t, got.Msg.GetPreferences()) {
		if v != [2]bool{true, true} {
			t.Fatalf("default for %v = %v, want both on", k, v)
		}
	}

	upd, err := c.UpdatePreferences(ctx, connect.NewRequest(&perspectivv1.UpdatePreferencesRequest{
		Preferences: []*perspectivv1.Preference{
			{Kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE, InApp: true, Email: false},
			{Kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED, InApp: false, Email: false},
		},
	}))
	if err != nil {
		t.Fatalf("UpdatePreferences: %v", err)
	}
	after := prefMap(t, upd.Msg.GetPreferences())
	if after[perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE] != [2]bool{true, false} ||
		after[perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED] != [2]bool{false, false} ||
		after[perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED] != [2]bool{true, true} {
		t.Fatalf("after update = %v", after)
	}

	// A second update of one kind keeps the other's earlier change.
	if _, err := c.UpdatePreferences(ctx, connect.NewRequest(&perspectivv1.UpdatePreferencesRequest{
		Preferences: []*perspectivv1.Preference{
			{Kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED, InApp: true, Email: true},
		},
	})); err != nil {
		t.Fatalf("UpdatePreferences: %v", err)
	}
	got, err = c.GetPreferences(ctx, connect.NewRequest(&perspectivv1.GetPreferencesRequest{}))
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	final := prefMap(t, got.Msg.GetPreferences())
	if final[perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE] != [2]bool{true, false} ||
		final[perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED] != [2]bool{true, true} {
		t.Fatalf("final = %v", final)
	}

	// Another user's preferences are untouched.
	others, err := e.client(e.user("other")).GetPreferences(ctx, connect.NewRequest(&perspectivv1.GetPreferencesRequest{}))
	if err != nil {
		t.Fatalf("GetPreferences other: %v", err)
	}
	if prefMap(t, others.Msg.GetPreferences())[perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE] != [2]bool{true, true} {
		t.Fatal("one user's update changed another's preferences")
	}
}

func TestUpdatePreferencesValidation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.client(e.user("me"))
	canon := perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE
	tests := []struct {
		name  string
		prefs []*perspectivv1.Preference
	}{
		{name: "unspecified kind", prefs: []*perspectivv1.Preference{{Kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_UNSPECIFIED}}},
		{name: "undefined kind", prefs: []*perspectivv1.Preference{{Kind: perspectivv1.NotificationKind(99)}}},
		{name: "kind listed twice", prefs: []*perspectivv1.Preference{{Kind: canon}, {Kind: canon, InApp: true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := c.UpdatePreferences(context.Background(),
				connect.NewRequest(&perspectivv1.UpdatePreferencesRequest{Preferences: tc.prefs}))
			wantCode(t, err, connect.CodeInvalidArgument)
		})
	}
}
