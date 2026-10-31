//go:build integration

package notify_test

import (
	"testing"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/jobs"
)

// Email abuse limits: a burst of thread replies reaches the inbox every time
// but is one email an hour per lore, and an account whose address was never
// confirmed gets inbox notifications and no email at all.
func TestNotificationEmailLimits(t *testing.T) {
	t.Parallel()
	kReply := perspectivv1.NotificationKind_NOTIFICATION_KIND_THREAD_REPLY
	s := newStory(t)
	lore := s.lore(s.arc, s.pAnn, s.pBen)
	otherLore := s.lore(s.arc, s.pCat)
	replyTo := func(lore, top, author uuid.UUID) {
		t.Helper()
		id := s.id(`INSERT INTO lore_posts (lore_id, author_id, body, reply_to_id, answered_id)
			VALUES ($1, $2, 'a reply', $3, $3) RETURNING id`, lore, author, top)
		s.notifyNow(jobs.NotifyArgs{Kind: kReply, ActorID: author, ArcID: s.arc, LoreID: lore, LorePostID: id})
	}

	// Three replies to ann's post in one lore, then one in another lore.
	annTop := s.id(`INSERT INTO lore_posts (lore_id, author_id, body) VALUES ($1, $2, 'mine') RETURNING id`, lore, s.ann)
	for range 3 {
		replyTo(lore, annTop, s.ben)
	}
	annOther := s.id(`INSERT INTO lore_posts (lore_id, author_id, body) VALUES ($1, $2, 'also mine') RETURNING id`, otherLore, s.ann)
	replyTo(otherLore, annOther, s.cat)
	if n := len(s.inbox(s.ann)); n != 4 {
		t.Fatalf("ann's inbox has %d notifications, want all 4 replies", n)
	}
	if n := len(s.emailJobs("ann@example.test")); n != 2 {
		t.Fatalf("ann got %d reply emails, want 2: one per lore in the hour", n)
	}

	// An unconfirmed address: the inbox, never the email.
	s.exec(`UPDATE users SET email_verified_at = NULL WHERE id = $1`, s.dan)
	danTop := s.id(`INSERT INTO lore_posts (lore_id, author_id, body) VALUES ($1, $2, 'unconfirmed') RETURNING id`, lore, s.dan)
	replyTo(lore, danTop, s.ben)
	if n := len(s.inbox(s.dan)); n != 1 {
		t.Fatalf("dan's inbox has %d notifications, want 1", n)
	}
	if n := len(s.emailJobs("dan@example.test")); n != 0 {
		t.Fatalf("an unconfirmed address got %d emails", n)
	}
}
