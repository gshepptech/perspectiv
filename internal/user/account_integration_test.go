//go:build integration

package user_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/user"
)

// FR-053, FR-065 (user part): BlockUser writes the blocks row the other
// domains read; UnblockUser removes it; reading is unchanged both ways.
func TestBlockUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	bob := e.user("bob_b")
	carol := e.user("carol")
	ctx := context.Background()
	c := e.client(alice)
	blocked := func(blocker, target uuid.UUID) bool {
		var ok bool
		e.scan(`SELECT EXISTS (SELECT 1 FROM blocks WHERE blocker_id = $1 AND blocked_id = $2)`, []any{blocker, target}, &ok)
		return ok
	}

	if _, err := c.BlockUser(ctx, connect.NewRequest(&perspectivv1.BlockUserRequest{UserId: bob.String()})); err != nil {
		t.Fatalf("BlockUser: %v", err)
	}
	if !blocked(alice, bob) || blocked(bob, alice) {
		t.Fatal("blocks row not written in the blocker's direction only")
	}
	// Idempotent.
	if _, err := c.BlockUser(ctx, connect.NewRequest(&perspectivv1.BlockUserRequest{UserId: bob.String()})); err != nil {
		t.Fatalf("BlockUser again: %v", err)
	}
	if _, err := c.BlockUser(ctx, connect.NewRequest(&perspectivv1.BlockUserRequest{UserId: carol.String()})); err != nil {
		t.Fatalf("BlockUser carol: %v", err)
	}

	own, err := c.GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{}))
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	got := map[string]string{}
	for _, r := range own.Msg.GetAccount().GetBlocked() {
		got[r.GetUserId()] = r.GetUsername()
	}
	if len(got) != 2 || got[bob.String()] != "bob_b" || got[carol.String()] != "carol" {
		t.Fatalf("blocked list = %v", got)
	}

	// Reading is unchanged for both users.
	if _, err := e.client(bob).GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{UserId: alice.String()})); err != nil {
		t.Fatalf("blocked user reading the blocker: %v", err)
	}
	if _, err := c.GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{UserId: bob.String()})); err != nil {
		t.Fatalf("blocker reading the blocked user: %v", err)
	}

	if _, err := c.UnblockUser(ctx, connect.NewRequest(&perspectivv1.UnblockUserRequest{UserId: bob.String()})); err != nil {
		t.Fatalf("UnblockUser: %v", err)
	}
	if blocked(alice, bob) || !blocked(alice, carol) {
		t.Fatal("unblock removed the wrong rows")
	}
	// Unblocking someone not blocked is a no-op.
	if _, err := c.UnblockUser(ctx, connect.NewRequest(&perspectivv1.UnblockUserRequest{UserId: bob.String()})); err != nil {
		t.Fatalf("UnblockUser again: %v", err)
	}
}

func TestBlockUserRejects(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	gone := e.user("gone_gus")
	e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, gone)
	ctx := context.Background()

	tests := []struct {
		name   string
		as     uuid.UUID
		target string
		code   connect.Code
	}{
		{name: "anonymous", target: alice.String(), code: connect.CodeUnauthenticated},
		{name: "self", as: alice, target: alice.String(), code: connect.CodeInvalidArgument},
		{name: "empty", as: alice, target: "", code: connect.CodeInvalidArgument},
		{name: "malformed", as: alice, target: "bob", code: connect.CodeInvalidArgument},
		{name: "unknown", as: alice, target: uuid.NewString(), code: connect.CodeNotFound},
		{name: "deleted", as: alice, target: gone.String(), code: connect.CodeNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := e.client(tc.as).BlockUser(ctx, connect.NewRequest(&perspectivv1.BlockUserRequest{UserId: tc.target}))
			wantCode(t, err, tc.code)
		})
	}
	_, err := e.client(alice).UnblockUser(ctx, connect.NewRequest(&perspectivv1.UnblockUserRequest{UserId: "bob"}))
	wantCode(t, err, connect.CodeInvalidArgument)
}

// ST-010, AC-038 (user part): deleting an account sets deleted_at on it and on
// every perspective it wrote, marks their arcs for the next rank run, frees
// its email and username, drops its avatar and saved stories and ends its
// sessions.
func TestDeleteAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	bob := e.user("bob_b")
	ctx := context.Background()
	c := e.client(alice)

	place := e.store()
	arc := e.dayArc(place, bob)
	otherArc := e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-12', 'Another title', $2) RETURNING id`, place, bob)
	mine := e.published(arc, alice)
	draft := e.draft(alice)
	bobs := e.published(arc, bob)
	bobsElsewhere := e.published(otherArc, bob)
	session := e.id(`INSERT INTO sessions (user_id, expires_at) VALUES ($1, now() + interval '1 day') RETURNING id`, alice)
	e.exec(`INSERT INTO refresh_tokens (session_id, token_hash, expires_at) VALUES ($1, 'h1', now() + interval '1 day')`, session)
	e.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, bob, alice)
	e.exec(`INSERT INTO saved_perspectives (user_id, perspective_id) VALUES ($1, $2), ($3, $4)`, alice, bobs, bob, mine)

	if _, err := c.UpdateProfile(ctx, connect.NewRequest(&perspectivv1.UpdateProfileRequest{
		Bio: "bio", Avatar: testPNG(t, 8, 8), AvatarContentType: "image/png",
	})); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	_, err := c.DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{}))
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = c.DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{Password: "wrong"}))
	wantCode(t, err, connect.CodePermissionDenied)
	var deletedAt *time.Time
	e.scan(`SELECT deleted_at FROM users WHERE id = $1`, []any{alice}, &deletedAt)
	if deletedAt != nil {
		t.Fatal("a wrong password deleted the account")
	}

	if _, err := c.DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{Password: goodPassword})); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}

	var email, username, bio string
	var avatar *string
	e.scan(`SELECT deleted_at, email, username, bio, avatar_path FROM users WHERE id = $1`, []any{alice},
		&deletedAt, &email, &username, &bio, &avatar)
	if deletedAt == nil || email == "alice@example.test" || username == "alice" || bio != "" || avatar != nil {
		t.Fatalf("user row after delete: deleted_at=%v email=%q username=%q bio=%q avatar=%v", deletedAt, email, username, bio, avatar)
	}
	if err := user.ValidateUsername(username); err == nil {
		t.Fatalf("anonymized username %q is choosable", username)
	}
	if files := avatarFiles(t, e.avatarDir); len(files) != 0 {
		t.Fatalf("avatar files after delete = %v", files)
	}

	var live int
	e.scan(`SELECT count(*) FROM perspectives WHERE author_id = $1 AND deleted_at IS NULL`, []any{alice}, &live)
	if live != 0 {
		t.Fatalf("%d of the account's perspectives are still live", live)
	}
	const perspectiveDeleted = `SELECT deleted_at FROM perspectives WHERE id = $1`
	if e.timeCol(perspectiveDeleted, mine) == nil || e.timeCol(perspectiveDeleted, draft) == nil {
		t.Fatal("the account's perspectives were not soft-deleted")
	}
	if e.timeCol(perspectiveDeleted, bobs) != nil || e.timeCol(perspectiveDeleted, bobsElsewhere) != nil {
		t.Fatal("another user's perspective was deleted")
	}
	const arcDirty = `SELECT lore_dirty_at FROM arcs WHERE id = $1`
	if e.timeCol(arcDirty, arc) == nil || e.timeCol(arcDirty, otherArc) != nil {
		t.Fatal("lore_dirty_at: want only the account's arc marked")
	}
	// The account's saves go with it. Bob's save of its story stays, as any
	// save of a deleted story does: his Saved list leaves the story out.
	wantSaves(t, e, alice, 0)
	wantSaves(t, e, bob, 1)
	if e.timeCol(`SELECT revoked_at FROM sessions WHERE id = $1`, session) == nil ||
		e.timeCol(`SELECT revoked_at FROM refresh_tokens WHERE session_id = $1`, session) == nil {
		t.Fatal("sessions or refresh tokens left live")
	}

	// The freed username and email can be taken again.
	e.user("alice")

	// The deleted caller's still-unexpired token gets nothing.
	_, err = c.UpdateProfile(ctx, connect.NewRequest(&perspectivv1.UpdateProfileRequest{Bio: "back"}))
	wantCode(t, err, connect.CodeUnauthenticated)
	_, err = c.DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{Password: goodPassword}))
	wantCode(t, err, connect.CodeUnauthenticated)
	_, err = c.GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{}))
	wantCode(t, err, connect.CodeUnauthenticated)

	// Bob's blocked list shows the placeholder, never the old name.
	own, err := e.client(bob).GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{}))
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if b := own.Msg.GetAccount().GetBlocked(); len(b) != 1 || b[0].GetUsername() != user.DeletedUsername {
		t.Fatalf("blocked list after delete = %v", b)
	}
}

// wantSaves fails unless user has saved exactly want stories.
func wantSaves(t *testing.T, e *env, user uuid.UUID, want int) {
	t.Helper()
	var n int
	e.scan(`SELECT count(*) FROM saved_perspectives WHERE user_id = $1`, []any{user}, &n)
	if n != want {
		t.Fatalf("saved stories of %s = %d, want %d", user, n, want)
	}
}

// Wrong passwords on DeleteAccount count toward the same Postgres lockout as
// login; once locked, even the right password is refused.
func TestDeleteAccountLockout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	c := e.client(alice)
	ctx := context.Background()
	for i := range user.LockoutFailures {
		_, err := c.DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{Password: "wrong"}))
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	var failures int
	e.scan(`SELECT count(*) FROM login_attempts WHERE user_id = $1 AND NOT succeeded`, []any{alice}, &failures)
	if failures != user.LockoutFailures {
		t.Fatalf("failed attempts recorded = %d", failures)
	}
	_, err := c.DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{Password: goodPassword}))
	wantCode(t, err, connect.CodeResourceExhausted)

	// Failures older than the window no longer count.
	e.exec(`UPDATE login_attempts SET attempted_at = now() - interval '16 minutes' WHERE user_id = $1`, alice)
	if _, err := c.DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{Password: goodPassword})); err != nil {
		t.Fatalf("DeleteAccount after the window: %v", err)
	}
}
