//go:build integration

package notify_test

import (
	"testing"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/jobs"
)

// TestFanoutThreadReply covers the THREAD_REPLY notification: the author of
// the answered post hears of a reply, and nobody else; never the replier
// themselves, never across a block either way, and nothing once the reply,
// the answered post or the branch is gone.
func TestFanoutThreadReply(t *testing.T) {
	t.Parallel()
	kReply := perspectivv1.NotificationKind_NOTIFICATION_KIND_THREAD_REPLY
	s := newStory(t)
	lore := s.lore(s.arc, s.pAnn, s.pBen)
	post := func(author uuid.UUID, answered uuid.UUID) uuid.UUID {
		t.Helper()
		if answered == uuid.Nil {
			return s.id(`INSERT INTO lore_posts (lore_id, author_id, body) VALUES ($1, $2, 'a post') RETURNING id`, lore, author)
		}
		return s.id(`INSERT INTO lore_posts (lore_id, author_id, body, reply_to_id, answered_id)
			SELECT $1, $2, 'a reply', coalesce(reply_to_id, id), id FROM lore_posts WHERE id = $3 RETURNING id`,
			lore, author, answered)
	}
	reply := func(author uuid.UUID, answered uuid.UUID) uuid.UUID {
		t.Helper()
		id := post(author, answered)
		s.notifyNow(jobs.NotifyArgs{Kind: kReply, ActorID: author, ArcID: s.arc, LoreID: lore, LorePostID: id})
		return id
	}
	notifyOn := func(id, actor uuid.UUID) {
		t.Helper()
		s.notifyNow(jobs.NotifyArgs{Kind: kReply, ActorID: actor, ArcID: s.arc, LoreID: lore, LorePostID: id})
	}

	// Ann's top-level post gets a reply from ben, and cat answers ben's reply:
	// ann hears of ben's, ben of cat's, and ann not of cat's.
	annTop := post(s.ann, uuid.Nil)
	benReply := reply(s.ben, annTop)
	catReply := reply(s.cat, benReply)
	// Ann answering her own post tells nobody.
	reply(s.ann, annTop)

	// Dan blocked eve: eve's reply to dan reaches nobody. Fox blocked gus:
	// fox's reply to gus does not reach gus either.
	dan, eve, fox, gus := s.dan, s.user("eve"), s.user("fox"), s.user("gus")
	s.block(dan, eve)
	reply(eve, post(dan, uuid.Nil))
	s.block(fox, gus)
	reply(fox, post(gus, uuid.Nil))

	// A reply deleted before the job runs, and one under a hidden post,
	// deliver nothing.
	hal, ivy, jo := s.user("hal"), s.user("ivy"), s.user("jo")
	gone := post(jo, post(hal, uuid.Nil))
	s.exec(`UPDATE lore_posts SET deleted_at = now() WHERE id = $1`, gone)
	notifyOn(gone, jo)
	hiddenTop := post(ivy, uuid.Nil)
	under := post(jo, hiddenTop)
	s.exec(`UPDATE lore_posts SET hidden_at = now() WHERE id = $1`, hiddenTop)
	notifyOn(under, jo)

	// Kim turned the kind off in the inbox and in email.
	kim := s.user("kim")
	s.pref(kim, kReply, false, false)
	reply(s.ben, post(kim, uuid.Nil))

	one := []perspectivv1.NotificationKind{kReply}
	tests := []struct {
		name  string
		user  uuid.UUID
		login string
		want  []perspectivv1.NotificationKind
		actor uuid.UUID
		// The reply the notification names, so the inbox opens the lore at it.
		post uuid.UUID
	}{
		{name: "the top-level post's author hears of a direct reply, once", user: s.ann, login: "ann", want: one, actor: s.ben, post: benReply},
		{name: "the author of an answered reply hears of the reply to it", user: s.ben, login: "ben", want: one, actor: s.cat, post: catReply},
		{name: "the replier hears nothing", user: s.cat, login: "cat"},
		{name: "an author who blocked the replier hears nothing", user: dan, login: "dan"},
		{name: "an author the replier blocked hears nothing", user: gus, login: "gus"},
		{name: "an answered post whose reply was deleted hears nothing", user: hal, login: "hal"},
		{name: "an author whose post was hidden hears nothing", user: ivy, login: "ivy"},
		{name: "an author who turned the kind off hears nothing", user: kim, login: "kim"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := s.expect(t, tc.user, tc.login, tc.want, len(tc.want))
			if len(rows) == 1 && (rows[0].payload["lore_id"] != lore.String() || rows[0].payload["actor_id"] != tc.actor.String() ||
				rows[0].payload["arc_id"] != s.arc.String() || rows[0].payload["post_id"] != tc.post.String()) {
				t.Fatalf("payload = %v", rows[0].payload)
			}
		})
	}
	if got := s.emailJobs("ann@example.test"); len(got) == 1 && got[0].args.Data["path"] != "/lores/"+lore.String()+"#post-"+benReply.String() {
		t.Fatalf("email path = %q", got[0].args.Data["path"])
	}
}
