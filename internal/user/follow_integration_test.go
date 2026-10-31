//go:build integration

package user_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// follows reports whether follower follows followee.
func (e *env) follows(follower, followee uuid.UUID) bool {
	e.t.Helper()
	var ok bool
	e.scan(`SELECT EXISTS (SELECT 1 FROM user_follows WHERE follower_id = $1 AND followee_id = $2)`, []any{follower, followee}, &ok)
	return ok
}

func followReq(username string, on bool) *connect.Request[perspectivv1.FollowUserRequest] {
	return connect.NewRequest(&perspectivv1.FollowUserRequest{Username: username, On: on})
}

// profileOf reads username's profile as viewer (uuid.Nil is signed out).
func (e *env) profileOf(viewer uuid.UUID, username string) *perspectivv1.GetProfileResponse {
	e.t.Helper()
	res, err := e.client(viewer).GetProfile(context.Background(), connect.NewRequest(&perspectivv1.GetProfileRequest{Username: username}))
	if err != nil {
		e.t.Fatalf("GetProfile %s: %v", username, err)
	}
	return res.Msg
}

// Following a writer shows on their profile, to the follower as following
// and to everyone as the follower count; following again and unfollowing
// twice are no-ops.
func TestFollowUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	fan, other, writer := e.user("fan"), e.user("other"), e.user("writer")
	c := e.client(fan)

	for range 2 {
		res, err := c.FollowUser(ctx, followReq("WRITER", true))
		if err != nil {
			t.Fatalf("FollowUser: %v", err)
		}
		if !res.Msg.GetFollowing() || !e.follows(fan, writer) {
			t.Fatalf("following = %v, row = %v", res.Msg.GetFollowing(), e.follows(fan, writer))
		}
	}
	if _, err := e.client(other).FollowUser(ctx, followReq("writer", true)); err != nil {
		t.Fatalf("FollowUser other: %v", err)
	}

	views := []struct {
		name          string
		viewer        uuid.UUID
		wantFollowing bool
	}{
		{name: "the follower sees they follow", viewer: fan, wantFollowing: true},
		{name: "a signed-out reader sees the count only", viewer: uuid.Nil},
		{name: "the writer sees their own count", viewer: writer},
	}
	for _, v := range views {
		got := e.profileOf(v.viewer, "writer")
		if got.GetFollowing() != v.wantFollowing || got.GetFollowers() != 2 {
			t.Fatalf("%s: following = %v, followers = %d", v.name, got.GetFollowing(), got.GetFollowers())
		}
	}

	for range 2 {
		res, err := c.FollowUser(ctx, followReq("writer", false))
		if err != nil {
			t.Fatalf("unfollow: %v", err)
		}
		if res.Msg.GetFollowing() || e.follows(fan, writer) {
			t.Fatal("unfollow left the follow in place")
		}
	}
	if got := e.profileOf(fan, "writer"); got.GetFollowing() || got.GetFollowers() != 1 {
		t.Fatalf("after unfollow: following = %v, followers = %d", got.GetFollowing(), got.GetFollowers())
	}
}

// Unconfirmed and suspended followers do not count, so throwaway accounts
// cannot inflate a writer's number.
func TestFollowerCountLeavesOutUnconfirmedAndSuspended(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	other, writer := e.user("other"), e.user("writer")
	if _, err := e.client(other).FollowUser(context.Background(), followReq("writer", true)); err != nil {
		t.Fatalf("FollowUser: %v", err)
	}
	if !e.follows(other, writer) {
		t.Fatal("follow not recorded")
	}
	e.exec(`UPDATE users SET email_verified_at = NULL WHERE id = $1`, other)
	if got := e.profileOf(uuid.Nil, "writer").GetFollowers(); got != 0 {
		t.Fatalf("followers with an unconfirmed follower = %d, want 0", got)
	}
	e.exec(`UPDATE users SET email_verified_at = now(), suspended_at = now() WHERE id = $1`, other)
	if got := e.profileOf(uuid.Nil, "writer").GetFollowers(); got != 0 {
		t.Fatalf("followers with a suspended follower = %d, want 0", got)
	}
	e.exec(`UPDATE users SET suspended_at = NULL WHERE id = $1`, other)
	if got := e.profileOf(uuid.Nil, "writer").GetFollowers(); got != 1 {
		t.Fatalf("followers once reinstated = %d, want 1", got)
	}
}

// FollowUser refuses what cannot be followed, and follows a suspended writer,
// whose profile stays public.
func TestFollowUserRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	fan, writer := e.user("fan"), e.user("writer")
	sus := e.user("sus_sam")
	e.exec(`UPDATE users SET suspended_at = now() WHERE id = $1`, sus)
	gone := e.user("gone_gina")
	if _, err := e.client(gone).DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{Password: goodPassword})); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	blockedBy, blocking := e.user("blocked_by"), e.user("blocking")
	e.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2), ($2, $3)`, blockedBy, fan, blocking)

	tests := []struct {
		name     string
		as       uuid.UUID
		username string
		on       bool
		want     connect.Code // 0 means success
		target   uuid.UUID
	}{
		{name: "signed out", as: uuid.Nil, username: "writer", on: true, want: connect.CodeUnauthenticated},
		{name: "empty username", as: fan, username: "  ", on: true, want: connect.CodeInvalidArgument},
		{name: "yourself", as: fan, username: "fan", on: true, want: connect.CodeInvalidArgument},
		{name: "no such writer", as: fan, username: "nobody_here", on: true, want: connect.CodeNotFound},
		{name: "a deleted account", as: fan, username: "gone_gina", on: true, want: connect.CodeNotFound},
		{name: "a writer who blocked you", as: fan, username: "blocked_by", on: true, want: connect.CodePermissionDenied, target: blockedBy},
		{name: "a writer you blocked", as: fan, username: "blocking", on: true, want: connect.CodePermissionDenied, target: blocking},
		{name: "unfollowing across a block is allowed", as: fan, username: "blocked_by", on: false, target: blockedBy},
		{name: "a suspended writer", as: fan, username: "sus_sam", on: true, target: sus},
		{name: "an active writer", as: fan, username: "writer", on: true, target: writer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := e.client(tc.as).FollowUser(ctx, followReq(tc.username, tc.on))
			if tc.want != 0 {
				wantCode(t, err, tc.want)
				if tc.target != uuid.Nil && e.follows(tc.as, tc.target) {
					t.Fatal("a refused follow wrote a row")
				}
				return
			}
			if err != nil {
				t.Fatalf("FollowUser: %v", err)
			}
			if res.Msg.GetFollowing() != tc.on || e.follows(tc.as, tc.target) != tc.on {
				t.Fatalf("following = %v, row = %v, want %v", res.Msg.GetFollowing(), e.follows(tc.as, tc.target), tc.on)
			}
		})
	}
}

// Blocking removes a follow in both directions, in the block's transaction,
// and leaves follows with anyone else alone.
func TestBlockRemovesFollows(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ann, bob, cat := e.user("ann"), e.user("bob_b"), e.user("cat")
	e.exec(`INSERT INTO user_follows (follower_id, followee_id) VALUES ($1, $2), ($2, $1), ($1, $3), ($3, $2)`, ann, bob, cat)

	if _, err := e.client(ann).BlockUser(ctx, connect.NewRequest(&perspectivv1.BlockUserRequest{UserId: bob.String()})); err != nil {
		t.Fatalf("BlockUser: %v", err)
	}
	tests := []struct {
		name               string
		follower, followee uuid.UUID
		want               bool
	}{
		{name: "the blocker's follow of the blocked is gone", follower: ann, followee: bob},
		{name: "the blocked's follow of the blocker is gone", follower: bob, followee: ann},
		{name: "the blocker's other follow stays", follower: ann, followee: cat, want: true},
		{name: "a third party's follow of the blocked stays", follower: cat, followee: bob, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := e.follows(tc.follower, tc.followee); got != tc.want {
				t.Fatalf("follows = %v, want %v", got, tc.want)
			}
		})
	}
	if _, err := e.client(bob).FollowUser(ctx, followReq("ann", true)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("follow after block: err = %v, want PermissionDenied", err)
	}
}

// ListFollowedWriters returns the caller's writers newest follow first, as
// their profiles show them, and drops an account deleted since; deleting an
// account removes the follows it made and received.
func TestListFollowedWriters(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	fan, older, newer, gone := e.user("fan"), e.user("older"), e.user("newer"), e.user("gone_gina")
	e.exec(`UPDATE users SET avatar_path = 'abc.png' WHERE id = $1`, newer)
	e.exec(`INSERT INTO user_follows (follower_id, followee_id, created_at) VALUES
		($1, $2, now() - interval '2 days'), ($1, $3, now() - interval '1 day'), ($1, $4, now()), ($4, $1, now())`,
		fan, older, newer, gone)
	if _, err := e.client(gone).DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{Password: goodPassword})); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	var left int
	e.scan(`SELECT count(*) FROM user_follows WHERE follower_id = $1 OR followee_id = $1`, []any{gone}, &left)
	if left != 0 {
		t.Fatalf("follows left on a deleted account = %d", left)
	}

	res, err := e.client(fan).ListFollowedWriters(ctx, connect.NewRequest(&perspectivv1.ListFollowedWritersRequest{}))
	if err != nil {
		t.Fatalf("ListFollowedWriters: %v", err)
	}
	ws := res.Msg.GetWriters()
	if len(ws) != 2 {
		t.Fatalf("writers = %v, want newer and older", ws)
	}
	if ws[0].GetUsername() != "newer" || ws[0].GetUserId() != newer.String() || ws[0].GetAvatarUrl() == "" || ws[0].GetFollowedAt() == nil {
		t.Fatalf("first = %v", ws[0])
	}
	if ws[1].GetUsername() != "older" || ws[1].GetAvatarUrl() != "" {
		t.Fatalf("second = %v", ws[1])
	}
	_, err = e.client(uuid.Nil).ListFollowedWriters(ctx, connect.NewRequest(&perspectivv1.ListFollowedWritersRequest{}))
	wantCode(t, err, connect.CodeUnauthenticated)
}
