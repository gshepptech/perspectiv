//go:build integration

package lore_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// thread is a world with a lore of ana and ben, a third writer in the arc,
// cal, who may post but is not a member, and a stranger who may only read.
type thread struct {
	*world
	ana, ben, cal author
	stranger      uuid.UUID
	lore          string
}

func newThread(t *testing.T) *thread {
	t.Helper()
	w := newWorld(t)
	th := &thread{world: w, ana: w.author("ana"), ben: w.author("ben"), cal: w.author("cal"), stranger: w.user("stranger")}
	w.link(th.ana, th.ben)
	th.lore = w.loreOf(th.ana.perspective).String()
	return th
}

func (th *thread) add(user uuid.UUID, body, replyTo string) (*perspectivv1.LorePost, error) {
	th.t.Helper()
	res, err := th.client.AddLorePost(context.Background(),
		asUser(user, &perspectivv1.AddLorePostRequest{LoreId: th.lore, Body: body, ReplyToId: replyTo}))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetPost(), nil
}

func (th *thread) mustAdd(user uuid.UUID, body, replyTo string) *perspectivv1.LorePost {
	th.t.Helper()
	p, err := th.add(user, body, replyTo)
	if err != nil {
		th.t.Fatalf("add %q: %v", body, err)
	}
	return p
}

func (th *thread) list(viewer uuid.UUID, sort perspectivv1.LorePostSort, token string, size int32) (*perspectivv1.ListLorePostsResponse, error) {
	th.t.Helper()
	res, err := th.client.ListLorePosts(context.Background(), asUser(viewer, &perspectivv1.ListLorePostsRequest{
		LoreId: th.lore, Sort: sort, PageToken: token, PageSize: size,
	}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (th *thread) mustList(viewer uuid.UUID, sort perspectivv1.LorePostSort) []*perspectivv1.LorePost {
	th.t.Helper()
	res, err := th.list(viewer, sort, "", 50)
	if err != nil {
		th.t.Fatalf("list: %v", err)
	}
	return res.GetPosts()
}

func (th *thread) replies(viewer uuid.UUID, post, token string, size int32) (*perspectivv1.ListLorePostRepliesResponse, error) {
	th.t.Helper()
	res, err := th.client.ListLorePostReplies(context.Background(), asUser(viewer, &perspectivv1.ListLorePostRepliesRequest{
		PostId: post, PageToken: token, PageSize: size,
	}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (th *thread) vote(user uuid.UUID, post string, on bool) (int32, error) {
	th.t.Helper()
	res, err := th.client.VoteLorePost(context.Background(), asUser(user, &perspectivv1.VoteLorePostRequest{PostId: post, On: on}))
	if err != nil {
		return 0, err
	}
	return res.Msg.GetUpvotes(), nil
}

func (th *thread) edit(user uuid.UUID, post, body string) (*perspectivv1.LorePost, error) {
	th.t.Helper()
	res, err := th.client.EditLorePost(context.Background(), asUser(user, &perspectivv1.EditLorePostRequest{PostId: post, Body: body}))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetPost(), nil
}

func (th *thread) remove(user uuid.UUID, post string) error {
	th.t.Helper()
	_, err := th.client.DeleteLorePost(context.Background(), asUser(user, &perspectivv1.DeleteLorePostRequest{PostId: post}))
	return err
}

func bodies(posts []*perspectivv1.LorePost) []string {
	out := make([]string, 0, len(posts))
	for _, p := range posts {
		if p.GetDeleted() {
			out = append(out, "[deleted]")
			continue
		}
		out = append(out, p.GetBody())
	}
	return out
}

func find(posts []*perspectivv1.LorePost, id string) *perspectivv1.LorePost {
	for _, p := range posts {
		if p.GetId() == id {
			return p
		}
	}
	return nil
}

// replyJobs counts THREAD_REPLY notify jobs about the post.
func (th *thread) replyJobs(post string) int {
	th.t.Helper()
	return th.count(`SELECT count(*) FROM river_job
		WHERE kind = 'notify' AND (args->>'Kind')::int = $1 AND args->>'LorePostID' = $2`,
		int32(perspectivv1.NotificationKind_NOTIFICATION_KIND_THREAD_REPLY), post)
}

// TestThreadReplies covers the two levels: a reply sits under its post, a
// reply to a reply sits under the same top-level post and names whom it
// answered, the first three replies come inline oldest first and the rest
// page through ListLorePostReplies. Each reply enqueues one notification.
func TestThreadReplies(t *testing.T) {
	t.Parallel()
	th := newThread(t)
	top := th.mustAdd(th.ana.user, "Who brought the jumper cables?", "")
	if th.replyJobs(top.GetId()) != 0 {
		t.Fatal("a top-level post enqueued a reply notification")
	}
	first := th.mustAdd(th.ben.user, "That was me.", top.GetId())
	if first.GetReplyToId() != top.GetId() || first.GetReplyingTo() != nil {
		t.Fatalf("direct reply = %v", first)
	}
	if th.replyJobs(first.GetId()) != 1 {
		t.Fatal("a reply enqueued no notification")
	}
	// Cal answers ben's reply: it flattens under ana's post and names ben.
	nested := th.mustAdd(th.cal.user, "You saved my evening.", first.GetId())
	if nested.GetReplyToId() != top.GetId() || nested.GetReplyingTo().GetUsername() != "ben" {
		t.Fatalf("reply to a reply = %v", nested)
	}
	for i := range 3 {
		th.mustAdd(th.ana.user, fmt.Sprintf("Thanks again %d", i), top.GetId())
	}

	posts := th.mustList(uuid.Nil, perspectivv1.LorePostSort_LORE_POST_SORT_TOP)
	if len(posts) != 1 {
		t.Fatalf("top-level posts = %v", bodies(posts))
	}
	got := posts[0]
	if got.GetReplyCount() != 5 || !slices.Equal(bodies(got.GetReplies()), []string{"That was me.", "You saved my evening.", "Thanks again 0"}) {
		t.Fatalf("inline replies = %d %v", got.GetReplyCount(), bodies(got.GetReplies()))
	}
	if got.GetMoreRepliesToken() == "" {
		t.Fatal("no token for the replies after the first three")
	}
	more, err := th.replies(uuid.Nil, top.GetId(), got.GetMoreRepliesToken(), 1)
	if err != nil {
		t.Fatalf("replies page 1: %v", err)
	}
	rest, err := th.replies(uuid.Nil, top.GetId(), more.GetNextPageToken(), 10)
	if err != nil {
		t.Fatalf("replies page 2: %v", err)
	}
	if b := append(bodies(more.GetReplies()), bodies(rest.GetReplies())...); !slices.Equal(b, []string{"Thanks again 1", "Thanks again 2"}) || rest.GetNextPageToken() != "" {
		t.Fatalf("paged replies = %v, next %q", b, rest.GetNextPageToken())
	}

	// Refusals.
	other := newThread(t)
	elsewhere := other.mustAdd(other.ana.user, "A post in another lore", "")
	th.block(th.ben.user, th.cal.user)
	deleted := th.mustAdd(th.ben.user, "Soon gone", "")
	if err := th.remove(th.ben.user, deleted.GetId()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	tests := []struct {
		name    string
		user    uuid.UUID
		replyTo string
		code    connect.Code
	}{
		{name: "someone with no story in the arc", user: th.stranger, replyTo: top.GetId(), code: connect.CodePermissionDenied},
		{name: "a post in another lore", user: th.ben.user, replyTo: elsewhere.GetId(), code: connect.CodeNotFound},
		{name: "a deleted post", user: th.ana.user, replyTo: deleted.GetId(), code: connect.CodeNotFound},
		{name: "an unknown post", user: th.ana.user, replyTo: uuid.NewString(), code: connect.CodeNotFound},
		{name: "a malformed id", user: th.ana.user, replyTo: "nope", code: connect.CodeInvalidArgument},
		{name: "the author of the answered post blocked the replier", user: th.cal.user, replyTo: first.GetId(), code: connect.CodePermissionDenied},
	}
	for _, tc := range tests {
		_, err := th.add(tc.user, "A reply", tc.replyTo)
		if err == nil || connect.CodeOf(err) != tc.code {
			t.Errorf("%s: error = %v, want %v", tc.name, err, tc.code)
		}
	}
	_, err = th.replies(uuid.Nil, first.GetId(), "", 10)
	wantCode(t, err, connect.CodeInvalidArgument)
}

func (w *world) block(blocker, blocked uuid.UUID) {
	w.t.Helper()
	w.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, blocker, blocked)
}

// TestVoteLorePost covers who may upvote a post (the people who may post,
// never on their own post, not when its author blocked them), that a vote
// counts once, and that the voter sees their own vote pressed.
func TestVoteLorePost(t *testing.T) {
	t.Parallel()
	th := newThread(t)
	post := th.mustAdd(th.ana.user, "Best night of the year.", "").GetId()
	blocked := th.user("blocked")
	th.published(th.arc, blocked, statusPending)
	th.block(th.ana.user, blocked)
	gone := th.mustAdd(th.ben.user, "Never mind.", "").GetId()
	if err := th.remove(th.ben.user, gone); err != nil {
		t.Fatalf("delete: %v", err)
	}

	tests := []struct {
		name string
		user uuid.UUID
		post string
		on   bool
		want int32
		code connect.Code
	}{
		{name: "a member upvotes", user: th.ben.user, post: post, on: true, want: 1},
		{name: "the same vote again counts once", user: th.ben.user, post: post, on: true, want: 1},
		{name: "a writer in the arc outside the lore upvotes", user: th.cal.user, post: post, on: true, want: 2},
		{name: "not on your own post", user: th.ana.user, post: post, on: true, code: connect.CodeInvalidArgument},
		{name: "not without a story in the arc", user: th.stranger, post: post, on: true, code: connect.CodePermissionDenied},
		{name: "not when the author blocked you", user: blocked, post: post, on: true, code: connect.CodePermissionDenied},
		{name: "not on a deleted post", user: th.ana.user, post: gone, on: true, code: connect.CodeNotFound},
		{name: "not signed in", user: uuid.Nil, post: post, on: true, code: connect.CodeUnauthenticated},
		{name: "clearing takes it back", user: th.cal.user, post: post, on: false, want: 1},
		{name: "clearing an absent vote changes nothing", user: th.cal.user, post: post, on: false, want: 1},
	}
	// Sequential: each case builds on the votes the ones before left.
	for _, tc := range tests {
		got, err := th.vote(tc.user, tc.post, tc.on)
		if tc.code != 0 {
			if err == nil || connect.CodeOf(err) != tc.code {
				t.Fatalf("%s: error = %v, want %v", tc.name, err, tc.code)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%s: upvotes = %d, %v; want %d", tc.name, got, err, tc.want)
		}
	}
	if n := th.count(`SELECT count(*) FROM lore_post_votes WHERE post_id = $1`, post); n != 1 {
		t.Fatalf("vote rows = %d, want 1", n)
	}
	for _, v := range []struct {
		viewer  uuid.UUID
		pressed bool
	}{{th.ben.user, true}, {th.cal.user, false}, {uuid.Nil, false}} {
		p := find(th.mustList(v.viewer, perspectivv1.LorePostSort_LORE_POST_SORT_TOP), post)
		if p.GetUpvotes() != 1 || p.GetViewerUpvoted() != v.pressed {
			t.Errorf("viewer %s sees %d upvotes, pressed %v; want 1, %v", v.viewer, p.GetUpvotes(), p.GetViewerUpvoted(), v.pressed)
		}
	}
}

// TestLorePostSort covers both orders with paging: TOP is most upvoted
// first with ties oldest first, NEW newest first, and a token only
// continues the order that made it.
func TestLorePostSort(t *testing.T) {
	t.Parallel()
	th := newThread(t)
	ids := map[string]string{}
	for i, body := range []string{"oldest", "older", "newer", "newest"} {
		p := th.mustAdd(th.ana.user, body, "")
		ids[body] = p.GetId()
		th.exec(`UPDATE lore_posts SET created_at = now() - make_interval(hours => $2) WHERE id = $1`, p.GetId(), 10-i)
	}
	for _, v := range []struct {
		user uuid.UUID
		post string
	}{{th.ben.user, "newer"}, {th.cal.user, "newer"}, {th.ben.user, "older"}, {th.cal.user, "newest"}} {
		if _, err := th.vote(v.user, ids[v.post], true); err != nil {
			t.Fatalf("vote: %v", err)
		}
	}

	tests := []struct {
		name string
		sort perspectivv1.LorePostSort
		want []string
	}{
		{name: "unspecified reads as top", sort: perspectivv1.LorePostSort_LORE_POST_SORT_UNSPECIFIED, want: []string{"newer", "older", "newest", "oldest"}},
		{name: "top", sort: perspectivv1.LorePostSort_LORE_POST_SORT_TOP, want: []string{"newer", "older", "newest", "oldest"}},
		{name: "new", sort: perspectivv1.LorePostSort_LORE_POST_SORT_NEW, want: []string{"newest", "newer", "older", "oldest"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			token := ""
			for range 3 {
				res, err := th.list(uuid.Nil, tc.sort, token, 3)
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				got = append(got, bodies(res.GetPosts())...)
				if token = res.GetNextPageToken(); token == "" {
					break
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("order = %v, want %v", got, tc.want)
			}
		})
	}

	top, err := th.list(uuid.Nil, perspectivv1.LorePostSort_LORE_POST_SORT_TOP, "", 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	_, err = th.list(uuid.Nil, perspectivv1.LorePostSort_LORE_POST_SORT_NEW, top.GetNextPageToken(), 2)
	wantCode(t, err, connect.CodeInvalidArgument)
}

// TestEditLorePost covers editing: only your own post, a changed text is
// stamped edited, an unchanged one is not, and a hidden or deleted post
// cannot be edited.
func TestEditLorePost(t *testing.T) {
	t.Parallel()
	th := newThread(t)
	post := th.mustAdd(th.ana.user, "We left at ten.", "").GetId()
	same, err := th.edit(th.ana.user, post, "  We left at ten. ")
	if err != nil || same.GetEditedAt() != nil {
		t.Fatalf("unchanged edit = %v, %v; want no edited_at", same, err)
	}
	edited, err := th.edit(th.ana.user, post, "We left at eleven.")
	if err != nil || edited.GetBody() != "We left at eleven." || edited.GetEditedAt() == nil {
		t.Fatalf("edit = %v, %v", edited, err)
	}
	if p := find(th.mustList(uuid.Nil, perspectivv1.LorePostSort_LORE_POST_SORT_NEW), post); p.GetBody() != "We left at eleven." || p.GetEditedAt() == nil {
		t.Fatalf("listed after edit = %v", p)
	}

	hidden := th.mustAdd(th.ana.user, "Something rude.", "").GetId()
	th.exec(`UPDATE lore_posts SET hidden_at = now() WHERE id = $1`, hidden)
	gone := th.mustAdd(th.ana.user, "Deleted.", "").GetId()
	if err := th.remove(th.ana.user, gone); err != nil {
		t.Fatalf("delete: %v", err)
	}
	tests := []struct {
		name string
		user uuid.UUID
		post string
		body string
		code connect.Code
	}{
		{name: "someone else's post", user: th.ben.user, post: post, body: "Mine now", code: connect.CodePermissionDenied},
		{name: "blank", user: th.ana.user, post: post, body: "  ", code: connect.CodeInvalidArgument},
		{name: "hidden by a moderator", user: th.ana.user, post: hidden, body: "Sorry", code: connect.CodeFailedPrecondition},
		{name: "deleted", user: th.ana.user, post: gone, body: "Back", code: connect.CodeNotFound},
		{name: "unknown", user: th.ana.user, post: uuid.NewString(), body: "Hello", code: connect.CodeNotFound},
	}
	for _, tc := range tests {
		_, err := th.edit(tc.user, tc.post, tc.body)
		if err == nil || connect.CodeOf(err) != tc.code {
			t.Errorf("%s: error = %v, want %v", tc.name, err, tc.code)
		}
	}
}

// TestDeleteLorePost covers deleting: a post with no replies leaves the
// thread, one with replies stays as "[deleted]" above them, and it goes once
// its last reply goes.
func TestDeleteLorePost(t *testing.T) {
	t.Parallel()
	th := newThread(t)
	alone := th.mustAdd(th.ana.user, "Nobody answered this.", "").GetId()
	parent := th.mustAdd(th.ana.user, "Who was singing?", "").GetId()
	reply := th.mustAdd(th.ben.user, "Me, badly.", parent).GetId()

	wantCode(t, th.remove(th.ben.user, alone), connect.CodePermissionDenied)
	for _, id := range []string{alone, parent} {
		if err := th.remove(th.ana.user, id); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	wantCode(t, th.remove(th.ana.user, alone), connect.CodeNotFound)

	posts := th.mustList(uuid.Nil, perspectivv1.LorePostSort_LORE_POST_SORT_TOP)
	if len(posts) != 1 || posts[0].GetId() != parent {
		t.Fatalf("posts after delete = %v", bodies(posts))
	}
	p := posts[0]
	if !p.GetDeleted() || p.GetBody() != "" || p.GetAuthor() != nil || p.GetUpvotes() != 0 ||
		!slices.Equal(bodies(p.GetReplies()), []string{"Me, badly."}) {
		t.Fatalf("deleted parent = %v", p)
	}
	// The body stays stored for moderation; it is never served.
	if b := th.text(`SELECT body FROM lore_posts WHERE id = $1`, parent); b != "Who was singing?" {
		t.Fatalf("stored body = %q", b)
	}

	if err := th.remove(th.ben.user, reply); err != nil {
		t.Fatalf("delete reply: %v", err)
	}
	if posts := th.mustList(uuid.Nil, perspectivv1.LorePostSort_LORE_POST_SORT_TOP); len(posts) != 0 {
		t.Fatalf("posts after the last reply went = %v", bodies(posts))
	}
}

// TestThreadHideAndRestore covers moderation with replies: hiding a
// top-level post hides its replies from readers and closes the branch to
// replies and votes; restoring brings both back. Hiding one reply hides
// only that reply.
func TestThreadHideAndRestore(t *testing.T) {
	t.Parallel()
	th := newThread(t)
	top := th.mustAdd(th.ana.user, "The cashier's name was on her badge.", "").GetId()
	reply := th.mustAdd(th.ben.user, "Let's not.", top).GetId()
	second := th.mustAdd(th.cal.user, "Agreed.", top).GetId()

	th.exec(`UPDATE lore_posts SET hidden_at = now() WHERE id = $1`, top)
	if posts := th.mustList(uuid.Nil, perspectivv1.LorePostSort_LORE_POST_SORT_TOP); len(posts) != 0 {
		t.Fatalf("hidden branch listed = %v", bodies(posts))
	}
	_, err := th.replies(uuid.Nil, top, "", 10)
	wantCode(t, err, connect.CodeNotFound)
	_, err = th.add(th.cal.user, "Under a hidden post", reply)
	wantCode(t, err, connect.CodeNotFound)
	_, err = th.vote(th.ana.user, reply, true)
	wantCode(t, err, connect.CodeNotFound)

	th.exec(`UPDATE lore_posts SET hidden_at = NULL WHERE id = $1`, top)
	th.exec(`UPDATE lore_posts SET hidden_at = now() WHERE id = $1`, second)
	posts := th.mustList(uuid.Nil, perspectivv1.LorePostSort_LORE_POST_SORT_TOP)
	if len(posts) != 1 || posts[0].GetReplyCount() != 1 || !slices.Equal(bodies(posts[0].GetReplies()), []string{"Let's not."}) {
		t.Fatalf("restored branch = %v", posts)
	}
}
