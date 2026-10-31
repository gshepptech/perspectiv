//go:build integration

package user_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// The export carries the member's lore thread posts with where each sits
// (the post it replies under and the one it answered) and when it was
// edited, and the posts the member upvoted; nobody else's posts.
func TestExportMyDataThread(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	me, other := e.user("threader"), e.user("answerer")
	arc := e.dayArc(e.store(), me)
	e.published(arc, me)
	e.published(arc, other)
	lore := e.id(`INSERT INTO lores (arc_id) VALUES ($1) RETURNING id`, arc)
	top := e.id(`INSERT INTO lore_posts (lore_id, author_id, body, edited_at) VALUES ($1, $2, 'Who had the umbrella?', now()) RETURNING id`, lore, me)
	theirs := e.id(`INSERT INTO lore_posts (lore_id, author_id, body, reply_to_id, answered_id) VALUES ($1, $2, 'Not me.', $3, $3) RETURNING id`, lore, other, top)
	mine := e.id(`INSERT INTO lore_posts (lore_id, author_id, body, reply_to_id, answered_id) VALUES ($1, $2, 'Then who?', $3, $4) RETURNING id`, lore, me, top, theirs)
	e.exec(`INSERT INTO lore_post_votes (post_id, user_id) VALUES ($1, $2), ($3, $4)`, theirs, me, top, other)

	res, err := e.client(me).ExportMyData(context.Background(), connect.NewRequest(&perspectivv1.ExportMyDataRequest{}))
	if err != nil {
		t.Fatalf("ExportMyData: %v", err)
	}
	doc := res.Msg.GetJson()
	var got struct {
		ThreadPosts []struct {
			ID         uuid.UUID  `json:"id"`
			ReplyToID  *uuid.UUID `json:"reply_to_id"`
			AnsweredID *uuid.UUID `json:"answered_id"`
			Body       string     `json:"body"`
			EditedAt   *time.Time `json:"edited_at"`
		} `json:"thread_posts"`
		ThreadUpvotes []struct {
			PostID uuid.UUID `json:"post_id"`
		} `json:"thread_upvotes"`
	}
	if err := json.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("export is not JSON: %v\n%s", err, doc)
	}
	switch p := got.ThreadPosts; {
	case len(p) != 2:
		t.Fatalf("thread posts = %+v, want the member's two", p)
	case p[0].ID != top || p[0].ReplyToID != nil || p[0].EditedAt == nil:
		t.Fatalf("top-level post = %+v", p[0])
	case p[1].ID != mine || p[1].ReplyToID == nil || *p[1].ReplyToID != top || p[1].AnsweredID == nil || *p[1].AnsweredID != theirs || p[1].EditedAt != nil:
		t.Fatalf("reply = %+v", p[1])
	case len(got.ThreadUpvotes) != 1 || got.ThreadUpvotes[0].PostID != theirs:
		t.Fatalf("thread upvotes = %+v, want only the member's own vote", got.ThreadUpvotes)
	case strings.Contains(doc, "Not me."):
		t.Fatal("the export carries another member's post")
	}
}
