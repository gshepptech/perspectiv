//go:build integration

package lore_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

func TestGetLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b, c := w.author("alice"), w.author("bob"), w.author("carol")
	ab := w.link(a, b)
	bc := w.link(b, c)
	loreID := w.loreOf(a.perspective)

	resp, err := w.client.GetLore(context.Background(), asUser(uuid.Nil, &perspectivv1.GetLoreRequest{LoreId: loreID.String()}))
	if err != nil {
		t.Fatalf("GetLore: %v", err)
	}
	l := resp.Msg.GetLore()
	if l.GetId() != loreID.String() || l.GetArcId() != w.arc.String() || l.GetMemberCount() != 3 ||
		l.GetStanding() != perspectivv1.LoreStanding_LORE_STANDING_SUBLORE {
		t.Errorf("lore = %v", l)
	}
	names := map[string]string{}
	for _, m := range l.GetMembers() {
		names[m.GetPerspectiveId()] = m.GetUser().GetUsername()
	}
	if len(names) != 3 || names[a.perspective.String()] != "alice" || names[b.perspective.String()] != "bob" ||
		names[c.perspective.String()] != "carol" {
		t.Errorf("members = %v", names)
	}
	enc := map[string]*perspectivv1.Encounter{}
	for _, e := range l.GetEncounters() {
		enc[e.GetClaimId()] = e
	}
	if len(enc) != 2 || enc[ab].GetPerspectiveId() != a.perspective.String() ||
		enc[ab].GetClaimantPerspectiveId() != b.perspective.String() || enc[bc] == nil {
		t.Errorf("encounters = %v", l.GetEncounters())
	}
	raw, _ := protojson.Marshal(resp.Msg)
	if strings.Contains(string(raw), "@") {
		t.Errorf("lore leaks an email: %s", raw)
	}

	_, err = w.client.GetLore(context.Background(), asUser(uuid.Nil, &perspectivv1.GetLoreRequest{LoreId: uuid.NewString()}))
	wantCode(t, err, connect.CodeNotFound)
	_, err = w.client.GetLore(context.Background(), asUser(uuid.Nil, &perspectivv1.GetLoreRequest{LoreId: "x"}))
	wantCode(t, err, connect.CodeInvalidArgument)
}

// A lore in an arc at or below a hidden place is not found, like the arc.
func TestGetLoreUnderHiddenPlace(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, b := w.author("ana"), w.author("ben")
	w.link(a, b)
	loreID := w.loreOf(a.perspective)
	get := func() error {
		_, err := w.client.GetLore(context.Background(), asUser(uuid.Nil, &perspectivv1.GetLoreRequest{LoreId: loreID.String()}))
		return err
	}
	if err := get(); err != nil {
		t.Fatalf("GetLore before the hide: %v", err)
	}
	w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, w.place)
	wantCode(t, get(), connect.CodeNotFound)
}

// A lore's thread: anyone reads it, members and other writers in the arc post
// to it, an outsider or an empty or overlong post is refused, and a hidden
// place takes the thread with it.
func TestLoreThread(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ctx := context.Background()
	a, b, c := w.author("ana"), w.author("ben"), w.author("cal")
	w.link(a, b)
	loreID := w.loreOf(a.perspective).String()
	post := func(user uuid.UUID, body string) error {
		_, err := w.client.AddLorePost(ctx, asUser(user, &perspectivv1.AddLorePostRequest{LoreId: loreID, Body: body}))
		return err
	}
	if err := post(a.user, "  I can't believe you were the one singing.  "); err != nil {
		t.Fatalf("member post: %v", err)
	}
	if err := post(c.user, "I was there too, by the door."); err != nil {
		t.Fatalf("arc writer post: %v", err)
	}
	wantCode(t, post(w.user("stranger"), "hello"), connect.CodePermissionDenied)
	wantCode(t, post(a.user, "   "), connect.CodeInvalidArgument)
	wantCode(t, post(a.user, strings.Repeat("x", 1001)), connect.CodeInvalidArgument)

	res, err := w.client.ListLorePosts(ctx, asUser(uuid.Nil, &perspectivv1.ListLorePostsRequest{LoreId: loreID}))
	if err != nil {
		t.Fatalf("ListLorePosts signed out: %v", err)
	}
	got := res.Msg.GetPosts()
	if len(got) != 2 || got[0].GetBody() != "I can't believe you were the one singing." || !got[0].GetByMember() || got[1].GetByMember() {
		t.Fatalf("posts = %v", got)
	}

	w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, w.place)
	_, err = w.client.ListLorePosts(ctx, asUser(uuid.Nil, &perspectivv1.ListLorePostsRequest{LoreId: loreID}))
	wantCode(t, err, connect.CodeNotFound)
}

// TestNameLore covers naming a lore: only its members may, whitespace runs
// collapse, the latest name wins, and GetLore shows the name and who set it.
func TestNameLore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ctx := context.Background()
	a, b, c := w.author("ana"), w.author("ben"), w.author("cal")
	w.link(a, b)
	loreID := w.loreOf(a.perspective).String()
	name := func(user uuid.UUID, n string) (string, error) {
		res, err := w.client.NameLore(ctx, asUser(user, &perspectivv1.NameLoreRequest{LoreId: loreID, Name: n}))
		if err != nil {
			return "", err
		}
		return res.Msg.GetName(), nil
	}

	tests := []struct {
		name string
		user uuid.UUID
		in   string
		want string
		code connect.Code
	}{
		{name: "a member names it, whitespace collapsed", user: a.user, in: "  The   Lot 7  tailgate ", want: "The Lot 7 tailgate"},
		{name: "a writer in the arc outside the lore may not", user: c.user, in: "Mine now", code: connect.CodePermissionDenied},
		{name: "a stranger may not", user: w.user("stranger"), in: "Mine now", code: connect.CodePermissionDenied},
		{name: "blank", user: a.user, in: "   ", code: connect.CodeInvalidArgument},
		{name: "too long", user: a.user, in: strings.Repeat("x", 61), code: connect.CodeInvalidArgument},
		{name: "another member renames it", user: b.user, in: "Jumper cables and burgers", want: "Jumper cables and burgers"},
	}
	// Sequential: each case builds on the name the one before left.
	for _, tc := range tests {
		got, err := name(tc.user, tc.in)
		if tc.code != 0 {
			wantCode(t, err, tc.code)
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%s: name = %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}

	res, err := w.client.GetLore(ctx, asUser(uuid.Nil, &perspectivv1.GetLoreRequest{LoreId: loreID}))
	if err != nil {
		t.Fatalf("GetLore: %v", err)
	}
	if l := res.Msg.GetLore(); l.GetName() != "Jumper cables and burgers" || l.GetNamedBy().GetUserId() != b.user.String() {
		t.Fatalf("lore name = %q by %v", l.GetName(), l.GetNamedBy())
	}

	// A moderator's clear locks naming for a week.
	w.exec(`UPDATE lores SET name = NULL, named_by = NULL, name_cleared_at = now() WHERE id = $1`, loreID)
	_, err = name(a.user, "Put straight back")
	wantCode(t, err, connect.CodeFailedPrecondition)
	w.exec(`UPDATE lores SET name_cleared_at = now() - interval '8 days' WHERE id = $1`, loreID)
	if got, err := name(a.user, "A fair new name"); err != nil || got != "A fair new name" {
		t.Fatalf("after the lock = %q, %v", got, err)
	}

	w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, w.place)
	_, err = name(a.user, "After hiding")
	wantCode(t, err, connect.CodeNotFound)
}
