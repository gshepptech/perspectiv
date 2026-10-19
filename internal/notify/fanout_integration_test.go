//go:build integration

package notify_test

import (
	"context"

	"connectrpc.com/connect"

	"testing"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/jobs"
)

const (
	kClaimReceived = perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_RECEIVED
	kClaimAccepted = perspectivv1.NotificationKind_NOTIFICATION_KIND_CLAIM_ACCEPTED
	kNewInArc      = perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC
	kVoteOut       = perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED
	kCanon         = perspectivv1.NotificationKind_NOTIFICATION_KIND_CANON_CHANGE
	kReport        = perspectivv1.NotificationKind_NOTIFICATION_KIND_REPORT_RESOLVED
)

// story is an arc with four authors, each with one published perspective.
type story struct {
	*env
	arc                uuid.UUID
	ann, ben, cat, dan uuid.UUID // users
	pAnn, pBen         uuid.UUID // perspectives
	pCat, pDan         uuid.UUID
}

func newStory(t *testing.T) *story {
	t.Helper()
	s := &story{env: newEnv(t)}
	s.arc = s.arc0()
	s.ann, s.ben, s.cat, s.dan = s.user("ann"), s.user("ben"), s.user("cat"), s.user("dan")
	s.pAnn, s.pBen = s.published(s.arc, s.ann), s.published(s.arc, s.ben)
	s.pCat, s.pDan = s.published(s.arc, s.cat), s.published(s.arc, s.dan)
	return s
}

func (s *story) arc0() uuid.UUID { return s.env.arc("Rainy Tuesday") }

// expect checks one user's inbox holds exactly the given kinds and that they
// have exactly that many email jobs.
func (s *story) expect(t *testing.T, user uuid.UUID, name string, inbox []perspectivv1.NotificationKind, emails int) []inboxRow {
	t.Helper()
	rows := s.inbox(user)
	if len(rows) != len(inbox) {
		t.Fatalf("%s inbox = %v, want %v", name, rows, inbox)
	}
	for i, k := range inbox {
		if rows[i].kind != k.String() {
			t.Fatalf("%s inbox[%d] = %s, want %s", name, i, rows[i].kind, k)
		}
	}
	if got := s.emailJobs(name + "@example.test"); len(got) != emails {
		t.Fatalf("%s email jobs = %d, want %d", name, len(got), emails)
	}
	return rows
}

// A claim notifies the author of the claimed character, not the claimant
// (FR-037, AC-041).
func TestFanoutClaimReceived(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	claim := s.claim(s.pAnn, s.pBen, "CLAIM_STATUS_PENDING")
	s.notifyNow(jobs.NotifyArgs{Kind: kClaimReceived, ActorID: s.ben, ClaimID: claim})

	rows := s.expect(t, s.ann, "ann", []perspectivv1.NotificationKind{kClaimReceived}, 1)
	s.expect(t, s.ben, "ben", nil, 0)
	want := map[string]string{
		"actor_id": s.ben.String(), "arc_id": s.arc.String(),
		"claim_id": claim.String(), "perspective_id": s.pAnn.String(),
	}
	if len(rows[0].payload) != len(want) {
		t.Fatalf("payload = %v, want %v", rows[0].payload, want)
	}
	for k, v := range want {
		if rows[0].payload[k] != v {
			t.Fatalf("payload[%s] = %q, want %q", k, rows[0].payload[k], v)
		}
	}
	job := s.emailJobs("ann@example.test")[0]
	if job.args.Template != jobs.TemplateNotification || job.args.Data["kind"] != kClaimReceived.String() ||
		job.args.Data["actor"] != "ben" || job.args.Data["arc_title"] != "Rainy Tuesday" || job.args.Data["path"] == "" {
		t.Fatalf("email job = %+v", job.args)
	}
}

// Accepting a claim notifies the claimant by their toggles and never the
// accepting author (OT-041, AC-041).
func TestFanoutClaimAccepted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		inApp      *bool
		email      *bool
		wantInbox  int
		wantEmails int
	}{
		{name: "defaults: inbox row and email job", wantInbox: 1, wantEmails: 1},
		{name: "in-app off: email job only", inApp: new(false), email: new(true), wantInbox: 0, wantEmails: 1},
		{name: "email off: inbox row only", inApp: new(true), email: new(false), wantInbox: 1, wantEmails: 0},
		{name: "both off: nothing", inApp: new(false), email: new(false), wantInbox: 0, wantEmails: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStory(t)
			if tc.inApp != nil {
				s.pref(s.ben, kClaimAccepted, *tc.inApp, *tc.email)
			}
			// Toggles for another kind do not apply.
			s.pref(s.ben, kClaimReceived, false, false)
			claim := s.claim(s.pAnn, s.pBen, "CLAIM_STATUS_ACCEPTED")
			s.notifyNow(jobs.NotifyArgs{Kind: kClaimAccepted, ActorID: s.ann, ClaimID: claim})

			var inbox []perspectivv1.NotificationKind
			if tc.wantInbox == 1 {
				inbox = []perspectivv1.NotificationKind{kClaimAccepted}
			}
			s.expect(t, s.ben, "ben", inbox, tc.wantEmails)
			s.expect(t, s.ann, "ann", nil, 0)
		})
	}
}

// A user who blocked the actor gets nothing from them (FR-053, FR-065).
func TestFanoutDropsBlockedActor(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	s.block(s.ann, s.ben)
	claim := s.claim(s.pAnn, s.pBen, "CLAIM_STATUS_PENDING")
	s.notifyNow(jobs.NotifyArgs{Kind: kClaimReceived, ActorID: s.ben, ClaimID: claim})
	s.expect(t, s.ann, "ann", nil, 0)

	// The block is one-way: ben has not blocked ann, so ann's acceptance of
	// a claim by ben reaches him.
	accepted := s.claim(s.pAnn, s.pCat, "CLAIM_STATUS_ACCEPTED")
	s.block(s.cat, s.dan) // an unrelated block changes nothing
	s.notifyNow(jobs.NotifyArgs{Kind: kClaimAccepted, ActorID: s.ann, ClaimID: accepted})
	s.expect(t, s.cat, "cat", []perspectivv1.NotificationKind{kClaimAccepted}, 1)
}

// A new perspective notifies the other authors with a visible perspective in
// the arc; its own author, deleted and hidden perspectives' authors, deleted
// accounts and blockers of the author are left out.
func TestFanoutNewPerspectiveInArc(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	eve, fay := s.user("eve"), s.user("fay")
	pEve, pFay := s.published(s.arc, eve), s.published(s.arc, fay)
	gus := s.user("gus")
	s.published(s.arc, gus)
	s.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, pEve)
	s.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, pFay)
	s.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, gus)
	s.block(s.dan, s.ann)
	// An author elsewhere hears nothing.
	elsewhere := s.user("hal")
	s.published(s.env.arc("Another day"), elsewhere)

	s.notifyNow(jobs.NotifyArgs{Kind: kNewInArc, ActorID: s.ann, ArcID: s.arc, PerspectiveID: s.pAnn})

	one := []perspectivv1.NotificationKind{kNewInArc}
	s.expect(t, s.ben, "ben", one, 1)
	s.expect(t, s.cat, "cat", one, 1)
	s.expect(t, s.ann, "ann", nil, 0)
	s.expect(t, s.dan, "dan", nil, 0)
	s.expect(t, eve, "eve", nil, 0)
	s.expect(t, fay, "fay", nil, 0)
	s.expect(t, gus, "gus", nil, 0)
	s.expect(t, elsewhere, "hal", nil, 0)

	rows := s.inbox(s.ben)
	if rows[0].payload["perspective_id"] != s.pAnn.String() || rows[0].payload["actor_id"] != s.ann.String() {
		t.Fatalf("payload = %v", rows[0].payload)
	}
}

// A new perspective that was deleted or hidden before the job ran notifies
// nobody.
func TestFanoutNewPerspectiveGoneBeforeJob(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	s.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, s.pAnn)
	s.notifyNow(jobs.NotifyArgs{Kind: kNewInArc, ActorID: s.ann, ArcID: s.arc, PerspectiveID: s.pAnn})
	s.expect(t, s.ben, "ben", nil, 0)
}

// A vote-out notifies the target's author and the other members of its lore,
// never the voter, and does not name the voter.
func TestFanoutVoteOutOpened(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	lore := s.lore(s.arc, s.pAnn, s.pBen, s.pCat, s.pDan)
	outsider := s.user("eve")
	s.published(s.arc, outsider)

	s.notifyNow(jobs.NotifyArgs{Kind: kVoteOut, ActorID: s.ben, PerspectiveID: s.pAnn, LoreID: lore})

	one := []perspectivv1.NotificationKind{kVoteOut}
	rows := s.expect(t, s.ann, "ann", one, 1)
	s.expect(t, s.cat, "cat", one, 1)
	s.expect(t, s.dan, "dan", one, 1)
	s.expect(t, s.ben, "ben", nil, 0)
	s.expect(t, outsider, "eve", nil, 0)
	p := rows[0].payload
	if _, named := p["actor_id"]; named {
		t.Fatalf("vote-out payload names the voter: %v", p)
	}
	if p["perspective_id"] != s.pAnn.String() || p["lore_id"] != lore.String() || p["arc_id"] != s.arc.String() {
		t.Fatalf("payload = %v", p)
	}
	if job := s.emailJobs("ann@example.test")[0]; job.args.Data["actor"] != "" {
		t.Fatalf("vote-out email names the voter: %v", job.args.Data)
	}

	// The lore is found from the perspective when the event omits it, and a
	// member who blocked the voter is skipped.
	s2 := newStory(t)
	s2.lore(s2.arc, s2.pAnn, s2.pBen, s2.pCat, s2.pDan)
	s2.block(s2.cat, s2.ben)
	s2.notifyNow(jobs.NotifyArgs{Kind: kVoteOut, ActorID: s2.ben, PerspectiveID: s2.pAnn})
	s2.expect(t, s2.ann, "ann", one, 1)
	s2.expect(t, s2.dan, "dan", one, 1)
	s2.expect(t, s2.cat, "cat", nil, 0)
}

// A canon change notifies every author with a visible perspective in the
// arc; it is a system event with no actor.
func TestFanoutCanonChange(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	lore := s.lore(s.arc, s.pAnn, s.pBen, s.pCat, s.pDan)
	eve := s.user("eve")
	s.published(s.arc, eve)
	s.pref(s.dan, kCanon, true, false)
	s.block(s.cat, s.ann) // blocks never apply to a system event

	s.notifyNow(jobs.NotifyArgs{Kind: kCanon, LoreID: lore})

	one := []perspectivv1.NotificationKind{kCanon}
	for _, u := range []struct {
		id   uuid.UUID
		name string
	}{{s.ann, "ann"}, {s.ben, "ben"}, {s.cat, "cat"}, {eve, "eve"}} {
		s.expect(t, u.id, u.name, one, 1)
	}
	rows := s.expect(t, s.dan, "dan", one, 0)
	p := rows[0].payload
	if p["arc_id"] != s.arc.String() || p["lore_id"] != lore.String() || p["actor_id"] != "" {
		t.Fatalf("payload = %v", p)
	}

	// An arc that lost its canon (no lore) still notifies its authors.
	s.notifyNow(jobs.NotifyArgs{Kind: kCanon, ArcID: s.arc})
	s.expect(t, s.ann, "ann", []perspectivv1.NotificationKind{kCanon, kCanon}, 2)
}

// A resolved report notifies its reporter, whoever resolved it.
func TestFanoutReportResolved(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	admin := s.user("admin")
	s.block(s.ben, admin) // a moderation outcome is delivered regardless
	report := s.id(`INSERT INTO reports (reporter_id, target_kind, target_id, reason, status, resolved_by, resolution, resolved_at)
		VALUES ($1, 'REPORT_TARGET_KIND_PERSPECTIVE', $2, 'REPORT_REASON_SPAM', 'REPORT_STATUS_RESOLVED', $3,
		        'MODERATION_ACTION_HIDE_PERSPECTIVE', now()) RETURNING id`, s.ben, s.pAnn, admin)

	s.notifyNow(jobs.NotifyArgs{Kind: kReport, ActorID: admin, ReportID: report})

	rows := s.expect(t, s.ben, "ben", []perspectivv1.NotificationKind{kReport}, 1)
	if rows[0].payload["report_id"] != report.String() || len(rows[0].payload) != 1 {
		t.Fatalf("payload = %v", rows[0].payload)
	}
	s.expect(t, s.ann, "ann", nil, 0)
	s.expect(t, admin, "admin", nil, 0)
}

// An event whose subject is gone, or that lacks the id its kind needs,
// completes without notifying anyone, so it is not retried forever.
func TestFanoutMissingSubject(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	for _, a := range []jobs.NotifyArgs{
		{Kind: kClaimReceived, ClaimID: uuid.New()},
		{Kind: kClaimAccepted},
		{Kind: kNewInArc, PerspectiveID: uuid.New()},
		{Kind: kVoteOut, PerspectiveID: uuid.New()},
		{Kind: kCanon, LoreID: uuid.New()},
		{Kind: kCanon},
		{Kind: kReport, ReportID: uuid.New()},
	} {
		s.notifyNow(a)
	}
	var n int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM notifications`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("notifications = %d, want 0", n)
	}
}

// Hiding a place hides its whole subtree from notifications: an inbox row
// that already names an arc below the hidden place loses the arc, and a new
// event about that arc is not delivered at all, in the inbox or by email.
func TestHiddenPlaceSilencesItsArcs(t *testing.T) {
	t.Parallel()
	s := newStory(t)
	state := s.id(`SELECT place_id FROM arcs WHERE id = $1`, s.arc)
	city := s.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_CITY', 'Hidden Springs', $1, 'America/New_York', 'PLACE_SOURCE_USER') RETURNING id`, state)
	s.exec(`UPDATE arcs SET place_id = $1 WHERE id = $2`, city, s.arc)

	s.notifyNow(jobs.NotifyArgs{Kind: kNewInArc, ActorID: s.ann, ArcID: s.arc, PerspectiveID: s.pAnn})
	s.expect(t, s.ben, "ben", []perspectivv1.NotificationKind{kNewInArc}, 1)
	ctx := context.Background()
	before, err := s.client(s.ben).ListInbox(ctx, connect.NewRequest(&perspectivv1.ListInboxRequest{}))
	if err != nil || len(before.Msg.GetNotifications()) != 1 || before.Msg.GetNotifications()[0].GetArcTitle() != "Rainy Tuesday" {
		t.Fatalf("inbox before the hide = %v, %v; want one row naming the arc", before, err)
	}

	// Moderation hides the state above the arc's city.
	s.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, state)

	after, err := s.client(s.ben).ListInbox(ctx, connect.NewRequest(&perspectivv1.ListInboxRequest{}))
	if err != nil || len(after.Msg.GetNotifications()) != 1 {
		t.Fatalf("inbox after the hide = %v, %v; want the one row kept", after, err)
	}
	if n := after.Msg.GetNotifications()[0]; n.GetArcTitle() != "" || n.GetArcId() != "" {
		t.Fatalf("row after the hide names arc %q (%q); want neither", n.GetArcTitle(), n.GetArcId())
	}

	s.notifyNow(jobs.NotifyArgs{Kind: kNewInArc, ActorID: s.cat, ArcID: s.arc, PerspectiveID: s.pCat})
	s.expect(t, s.ben, "ben", []perspectivv1.NotificationKind{kNewInArc}, 1)
	s.expect(t, s.dan, "dan", []perspectivv1.NotificationKind{kNewInArc}, 1)
}
