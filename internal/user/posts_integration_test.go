//go:build integration

package user_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// lore inserts a lore in the arc, named when name is not empty.
func (e *env) lore(arc uuid.UUID, name string) uuid.UUID {
	e.t.Helper()
	if name == "" {
		return e.id(`INSERT INTO lores (arc_id) VALUES ($1) RETURNING id`, arc)
	}
	return e.id(`INSERT INTO lores (arc_id, name) VALUES ($1, $2) RETURNING id`, arc, name)
}

// post inserts a top-level post (replyTo is uuid.Nil) or a reply, minutes
// before now so tests can order them.
func (e *env) post(lore, author uuid.UUID, body string, replyTo uuid.UUID, minutesAgo int) uuid.UUID {
	e.t.Helper()
	var reply *uuid.UUID
	if replyTo != uuid.Nil {
		reply = &replyTo
	}
	return e.id(`INSERT INTO lore_posts (lore_id, author_id, body, reply_to_id, answered_id, created_at)
		VALUES ($1, $2, $3, $4, $4, now() - make_interval(mins => $5)) RETURNING id`, lore, author, body, reply, minutesAgo)
}

// listPosts reads every page of the writer's posts signed out, pageSize at
// a time, and returns their excerpts in order.
func listPosts(t *testing.T, e *env, writer uuid.UUID, pageSize int32) []*perspectivv1.UserPost {
	t.Helper()
	var out []*perspectivv1.UserPost
	token := ""
	for range 20 {
		res, err := e.client(uuid.Nil).ListUserPosts(context.Background(), connect.NewRequest(&perspectivv1.ListUserPostsRequest{
			UserId: writer.String(), PageToken: token, PageSize: pageSize,
		}))
		if err != nil {
			t.Fatalf("ListUserPosts: %v", err)
		}
		out = append(out, res.Msg.GetPosts()...)
		if token = res.Msg.GetNextPageToken(); token == "" {
			return out
		}
	}
	t.Fatal("ListUserPosts never ran out of pages")
	return nil
}

func excerpts(posts []*perspectivv1.UserPost) []string {
	out := make([]string, len(posts))
	for i, p := range posts {
		out[i] = p.GetExcerpt()
	}
	return out
}

// ListUserPosts lists only the posts a reader of the lore page sees. Each
// case seeds one writer's posts and lists the excerpts, newest first.
func TestListUserPostsVisibility(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tests := []struct {
		name  string
		build func(e *env, w, other, lore uuid.UUID)
		want  []string
	}{
		{name: "posts and replies, newest first", want: []string{"my reply", "my post"}, build: func(e *env, w, other, lore uuid.UUID) {
			top := e.post(lore, w, "my post", uuid.Nil, 3)
			e.post(lore, w, "my reply", top, 1)
		}},
		{name: "another writer's posts are not listed", want: []string{"mine"}, build: func(e *env, w, other, lore uuid.UUID) {
			theirs := e.post(lore, other, "theirs", uuid.Nil, 3)
			e.post(lore, w, "mine", theirs, 2)
			e.post(lore, other, "their reply", theirs, 1)
		}},
		{name: "a deleted post is gone", want: []string{"kept"}, build: func(e *env, w, other, lore uuid.UUID) {
			e.post(lore, w, "kept", uuid.Nil, 2)
			gone := e.post(lore, w, "deleted", uuid.Nil, 1)
			e.exec(`UPDATE lore_posts SET deleted_at = now() WHERE id = $1`, gone)
		}},
		{name: "a hidden post is gone", want: []string{"kept"}, build: func(e *env, w, other, lore uuid.UUID) {
			e.post(lore, w, "kept", uuid.Nil, 2)
			hidden := e.post(lore, w, "hidden", uuid.Nil, 1)
			e.exec(`UPDATE lore_posts SET hidden_at = now() WHERE id = $1`, hidden)
		}},
		{name: "a reply under a hidden post is gone", want: []string{"under a live post"}, build: func(e *env, w, other, lore uuid.UUID) {
			hidden := e.post(lore, other, "hidden top", uuid.Nil, 4)
			e.post(lore, w, "under a hidden post", hidden, 3)
			e.exec(`UPDATE lore_posts SET hidden_at = now() WHERE id = $1`, hidden)
			live := e.post(lore, other, "live top", uuid.Nil, 2)
			e.post(lore, w, "under a live post", live, 1)
		}},
		{name: "a reply under a deleted post stays, as the thread keeps it", want: []string{"under a deleted post"}, build: func(e *env, w, other, lore uuid.UUID) {
			top := e.post(lore, other, "deleted top", uuid.Nil, 2)
			e.post(lore, w, "under a deleted post", top, 1)
			e.exec(`UPDATE lore_posts SET deleted_at = now() WHERE id = $1`, top)
		}},
		{name: "a post in a lore under a hidden place is gone", want: []string{"visible lore"}, build: func(e *env, w, other, lore uuid.UUID) {
			e.post(lore, w, "visible lore", uuid.Nil, 2)
			hidden := e.lore(e.dayArc(e.hiddenStore(), w), "")
			e.post(hidden, w, "hidden place", uuid.Nil, 1)
		}},
		{name: "a deleted account has no posts", build: func(e *env, w, other, lore uuid.UUID) {
			e.post(lore, w, "before deleting", uuid.Nil, 1)
			e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, w)
		}},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			se := e.as(t)
			w := se.user("poster_" + strconv.Itoa(i))
			other := se.user("other_" + tag(w))
			lore := se.lore(se.dayArc(se.store(), w), "")
			tc.build(se, w, other, lore)
			got := excerpts(listPosts(t, se, w, 0))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("posts = %q, want %q", got, tc.want)
			}
		})
	}
}

// Each post carries the lore and day it belongs to, and an excerpt cut at a
// word near 240 characters.
func TestListUserPostsFields(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.user("fielded")
	state := e.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', 'Fieldstate', 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`)
	store := e.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_STORE', 'Prairie Books', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, state)
	day := e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-01-14', 'Saturday night', $2) RETURNING id`, store, w)
	hour := e.id(`INSERT INTO arcs (place_id, local_date, hour, parent_arc_id, title, created_by)
		VALUES ($1, '2026-01-14', 21, $2, 'Saturday night · 21:00', $3) RETURNING id`, store, day, w)
	named := e.lore(day, "The long queue")
	unnamed := e.lore(hour, "")
	long := strings.Repeat("word ", 47) + "  and\n\nthen   some more words after the cut"
	top := e.post(named, w, long, uuid.Nil, 2)
	e.post(unnamed, w, "  short\n reply ", top, 1)

	got := listPosts(t, e, w, 0)
	if len(got) != 2 {
		t.Fatalf("posts = %v, want 2", got)
	}
	reply, first := got[0], got[1]
	switch {
	case reply.GetLoreId() != unnamed.String() || reply.GetLoreName() != "" || reply.GetArcId() != hour.String():
		t.Fatalf("reply's lore = %v", reply)
	case reply.GetArcTitle() != "Saturday night · 21:00" || reply.Hour == nil || reply.GetHour() != 21:
		t.Fatalf("reply's hour = %v", reply)
	case !reply.GetReply() || reply.GetExcerpt() != "short reply":
		t.Fatalf("reply = %v", reply)
	case first.GetLoreName() != "The long queue" || first.GetArcTitle() != "Saturday night" || first.Hour != nil:
		t.Fatalf("post's lore = %v", first)
	case first.GetLocalDate() != "2026-01-14" || first.GetPlaceName() != "Prairie Books" || first.GetReply():
		t.Fatalf("post's day = %v", first)
	case first.GetId() != top.String() || first.GetCreatedAt().AsTime().After(time.Now()):
		t.Fatalf("post = %v", first)
	}
	// Folded, 47 "word"s and "and" are 238 characters; "then" would pass 240.
	want := strings.TrimSpace(strings.Repeat("word ", 47)) + " and…"
	if first.GetExcerpt() != want {
		t.Fatalf("excerpt = %q (%d), want %q", first.GetExcerpt(), len([]rune(first.GetExcerpt())), want)
	}
}

// Pages are keyset pages: every post once, newest first, whatever the size,
// even when posts share a time.
func TestListUserPostsPaging(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.user("pager")
	lore := e.lore(e.dayArc(e.store(), w), "")
	var want []string
	for i, letter := range strings.Split("ABCDEFG", "") {
		body := "post " + letter
		e.post(lore, w, body, uuid.Nil, 10-i)
		want = append(want, body)
	}
	// C and D share a time, so the later id, D's, comes first.
	e.exec(`UPDATE lore_posts SET created_at = (SELECT created_at FROM lore_posts WHERE body = 'post C') WHERE body = 'post D'`)
	slices.Reverse(want)
	tests := []struct {
		name string
		size int32
	}{
		{name: "one at a time", size: 1},
		{name: "two at a time", size: 2},
		{name: "a page that ends exactly at the last post", size: 7},
		{name: "default size", size: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := excerpts(listPosts(t, e.as(t), w, tc.size))
			if !slices.Equal(got, want) {
				t.Fatalf("posts = %q, want %q", got, want)
			}
		})
	}
}

func TestListUserPostsRejects(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	tests := []struct {
		name string
		req  *perspectivv1.ListUserPostsRequest
		code connect.Code
	}{
		{name: "no user", req: &perspectivv1.ListUserPostsRequest{}, code: connect.CodeInvalidArgument},
		{name: "malformed user", req: &perspectivv1.ListUserPostsRequest{UserId: "nope"}, code: connect.CodeInvalidArgument},
		{name: "tampered page token", req: &perspectivv1.ListUserPostsRequest{UserId: uuid.NewString(), PageToken: "bm9wZQ"}, code: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := e.client(uuid.Nil).ListUserPosts(ctx, connect.NewRequest(tc.req))
			wantCode(t, err, tc.code)
		})
	}
	res, err := e.client(uuid.Nil).ListUserPosts(ctx, connect.NewRequest(&perspectivv1.ListUserPostsRequest{UserId: uuid.NewString()}))
	if err != nil || len(res.Msg.GetPosts()) != 0 || res.Msg.GetNextPageToken() != "" {
		t.Fatalf("unknown user = %v, %v; want an empty page", res, err)
	}
}
