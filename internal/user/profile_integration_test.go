//go:build integration

package user_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/user"
)

// FR-003, OT-004: another user's profile carries no email and no account; the
// caller's own profile carries its Account.
func TestGetProfile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	bob := e.user("Bob_Two")
	e.exec(`UPDATE users SET bio = 'I sell bagels' WHERE id = $1`, bob)
	ctx := context.Background()

	tests := []struct {
		name        string
		as          uuid.UUID
		req         *perspectivv1.GetProfileRequest
		wantCode    connect.Code
		wantUser    uuid.UUID
		wantName    string
		wantAccount bool
	}{
		{name: "other by id", as: alice, req: &perspectivv1.GetProfileRequest{UserId: bob.String()}, wantUser: bob, wantName: "Bob_Two"},
		{name: "other by username any case", as: alice, req: &perspectivv1.GetProfileRequest{Username: "bob_two"}, wantUser: bob, wantName: "Bob_Two"},
		{name: "anonymous reads a profile", req: &perspectivv1.GetProfileRequest{UserId: bob.String()}, wantUser: bob, wantName: "Bob_Two"},
		{name: "own by empty request", as: alice, req: &perspectivv1.GetProfileRequest{}, wantUser: alice, wantName: "alice", wantAccount: true},
		{name: "own by id", as: alice, req: &perspectivv1.GetProfileRequest{UserId: alice.String()}, wantUser: alice, wantName: "alice", wantAccount: true},
		{name: "own by username", as: alice, req: &perspectivv1.GetProfileRequest{Username: "ALICE"}, wantUser: alice, wantName: "alice", wantAccount: true},
		{name: "anonymous own profile", req: &perspectivv1.GetProfileRequest{}, wantCode: connect.CodeUnauthenticated},
		{name: "both identifiers", as: alice, req: &perspectivv1.GetProfileRequest{UserId: bob.String(), Username: "Bob_Two"}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed id", as: alice, req: &perspectivv1.GetProfileRequest{UserId: "not-a-uuid"}, wantCode: connect.CodeInvalidArgument},
		{name: "unknown id", as: alice, req: &perspectivv1.GetProfileRequest{UserId: uuid.NewString()}, wantCode: connect.CodeNotFound},
		{name: "unknown username", as: alice, req: &perspectivv1.GetProfileRequest{Username: "nobody_here"}, wantCode: connect.CodeNotFound},
		{name: "username breaking the rule", as: alice, req: &perspectivv1.GetProfileRequest{Username: "a b"}, wantCode: connect.CodeNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := e.client(tc.as).GetProfile(ctx, connect.NewRequest(tc.req))
			if tc.wantCode != 0 {
				wantCode(t, err, tc.wantCode)
				return
			}
			if err != nil {
				t.Fatalf("GetProfile: %v", err)
			}
			checkProfileResponse(t, res.Msg, tc.wantUser, tc.wantName, tc.wantAccount)
		})
	}
}

// checkProfileResponse asserts the profile names wantUser, and that the
// response carries an Account (alice's) only when wantAccount, and no email
// anywhere otherwise.
func checkProfileResponse(t *testing.T, msg *perspectivv1.GetProfileResponse, wantUser uuid.UUID, wantName string, wantAccount bool) {
	t.Helper()
	p := msg.GetProfile()
	if p.GetUserId() != wantUser.String() || p.GetUsername() != wantName {
		t.Fatalf("profile = %v, want %s %s", p, wantUser, wantName)
	}
	if p.GetJoinedAt() == nil {
		t.Fatal("joined_at unset")
	}
	raw, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !wantAccount {
		if msg.GetAccount() != nil {
			t.Fatal("another user's profile carries an Account")
		}
		if bytes.Contains(raw, []byte("@example.test")) {
			t.Fatal("response for another user contains an email")
		}
		return
	}
	acct := msg.GetAccount()
	if acct.GetEmail() != "alice@example.test" || !acct.GetEmailVerified() || acct.GetRole() != perspectivv1.Role_ROLE_USER {
		t.Fatalf("account = %v", acct)
	}
	if acct.GetUsernameChangedAt() != nil || acct.GetNextUsernameChangeAt() != nil {
		t.Fatalf("fresh account reports a username change: %v", acct)
	}
}

// Decided in service.go: a suspended account keeps its username and is
// flagged; a deleted one reads as the placeholder with every other field blank.
func TestGetProfileSuspendedAndDeleted(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	sus := e.user("suspended_sam")
	e.exec(`UPDATE users SET suspended_at = now(), bio = 'still here' WHERE id = $1`, sus)
	gone := e.user("gone_gina")
	ctx := context.Background()

	if _, err := e.client(gone).DeleteAccount(ctx, connect.NewRequest(&perspectivv1.DeleteAccountRequest{Password: goodPassword})); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}

	res, err := e.client(alice).GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{UserId: sus.String()}))
	if err != nil {
		t.Fatalf("GetProfile suspended: %v", err)
	}
	if p := res.Msg.GetProfile(); !p.GetSuspended() || p.GetUsername() != "suspended_sam" || p.GetBio() != "still here" || p.GetDeleted() {
		t.Fatalf("suspended profile = %v", p)
	}

	res, err = e.client(alice).GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{UserId: gone.String()}))
	if err != nil {
		t.Fatalf("GetProfile deleted: %v", err)
	}
	p := res.Msg.GetProfile()
	if !p.GetDeleted() || p.GetUsername() != user.DeletedUsername || p.GetBio() != "" || p.GetAvatarUrl() != "" || p.GetJoinedAt() != nil {
		t.Fatalf("deleted profile = %v", p)
	}
	_, err = e.client(alice).GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{Username: "gone_gina"}))
	wantCode(t, err, connect.CodeNotFound)
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(1, 1, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png: %v", err)
	}
	return buf.Bytes()
}

func avatarFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read avatar dir: %v", err)
	}
	var names []string
	for _, en := range entries {
		if !strings.HasPrefix(en.Name(), ".") {
			names = append(names, en.Name())
		}
	}
	return names
}

// FR-001, FR-052: bio and a small avatar on local disk, served back over HTTP.
func TestUpdateProfile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	c := e.client(alice)
	ctx := context.Background()

	res, err := c.UpdateProfile(ctx, connect.NewRequest(&perspectivv1.UpdateProfileRequest{
		Bio: "  Night shift at the deli.  ", Avatar: testPNG(t, 32, 32), AvatarContentType: "image/png",
	}))
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	p := res.Msg.GetProfile()
	if p.GetBio() != "Night shift at the deli." {
		t.Fatalf("bio = %q", p.GetBio())
	}
	first := p.GetAvatarUrl()
	if !strings.HasPrefix(first, user.AvatarURLPrefix) {
		t.Fatalf("avatar_url = %q", first)
	}
	files := avatarFiles(t, e.avatarDir)
	if len(files) != 1 {
		t.Fatalf("avatar files = %v, want one", files)
	}

	// The URL is served by the avatar mount with an image content type.
	status, ctype, body := e.get(first)
	if status != http.StatusOK || ctype != "image/png" {
		t.Fatalf("GET avatar: %d %q", status, ctype)
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(body)); err != nil || cfg.Width != 32 {
		t.Fatalf("served avatar: %v %v", cfg, err)
	}

	// A bio-only update keeps the avatar.
	res, err = c.UpdateProfile(ctx, connect.NewRequest(&perspectivv1.UpdateProfileRequest{Bio: "Mornings now."}))
	if err != nil {
		t.Fatalf("UpdateProfile bio: %v", err)
	}
	if res.Msg.GetProfile().GetAvatarUrl() != first || res.Msg.GetProfile().GetBio() != "Mornings now." {
		t.Fatalf("profile = %v", res.Msg.GetProfile())
	}

	// A new avatar replaces the old file.
	res, err = c.UpdateProfile(ctx, connect.NewRequest(&perspectivv1.UpdateProfileRequest{
		Bio: "Mornings now.", Avatar: testPNG(t, 16, 16), AvatarContentType: "image/png",
	}))
	if err != nil {
		t.Fatalf("UpdateProfile new avatar: %v", err)
	}
	second := res.Msg.GetProfile().GetAvatarUrl()
	if second == first || second == "" {
		t.Fatalf("avatar_url = %q after replace (was %q)", second, first)
	}
	if files := avatarFiles(t, e.avatarDir); len(files) != 1 || !strings.HasSuffix(second, files[0]) {
		t.Fatalf("avatar files after replace = %v, url %q", files, second)
	}
	if status, _, _ := e.get(first); status != http.StatusNotFound {
		t.Fatalf("old avatar still served: %d", status)
	}

	// clear_avatar removes it.
	res, err = c.UpdateProfile(ctx, connect.NewRequest(&perspectivv1.UpdateProfileRequest{Bio: "Mornings now.", ClearAvatar: true}))
	if err != nil {
		t.Fatalf("UpdateProfile clear: %v", err)
	}
	if res.Msg.GetProfile().GetAvatarUrl() != "" {
		t.Fatalf("avatar_url = %q after clear", res.Msg.GetProfile().GetAvatarUrl())
	}
	if files := avatarFiles(t, e.avatarDir); len(files) != 0 {
		t.Fatalf("avatar files after clear = %v", files)
	}
}

func TestUpdateProfileRejects(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	ctx := context.Background()

	tests := []struct {
		name string
		as   uuid.UUID
		req  *perspectivv1.UpdateProfileRequest
		code connect.Code
	}{
		{name: "anonymous", req: &perspectivv1.UpdateProfileRequest{Bio: "hi"}, code: connect.CodeUnauthenticated},
		{name: "bio too long", as: alice, req: &perspectivv1.UpdateProfileRequest{Bio: strings.Repeat("é", user.MaxBioRunes+1)}, code: connect.CodeInvalidArgument},
		{name: "avatar not an image", as: alice, req: &perspectivv1.UpdateProfileRequest{Avatar: []byte("hello"), AvatarContentType: "image/png"}, code: connect.CodeInvalidArgument},
		{name: "avatar too large", as: alice, req: &perspectivv1.UpdateProfileRequest{Avatar: testPNG(t, user.MaxAvatarSide+1, 2), AvatarContentType: "image/png"}, code: connect.CodeInvalidArgument},
		{name: "avatar type mismatch", as: alice, req: &perspectivv1.UpdateProfileRequest{Avatar: testPNG(t, 4, 4), AvatarContentType: "image/jpeg"}, code: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := e.client(tc.as).UpdateProfile(ctx, connect.NewRequest(tc.req))
			wantCode(t, err, tc.code)
		})
	}
	var bio string
	e.scan(`SELECT bio FROM users WHERE id = $1`, []any{alice}, &bio)
	if bio != "" {
		t.Fatalf("a rejected update wrote bio %q", bio)
	}
	if files := avatarFiles(t, e.avatarDir); len(files) != 0 {
		t.Fatalf("a rejected update left avatar files %v", files)
	}
}

func TestAvatarMountRefusesOtherPaths(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.avatarDir, "notes.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"notes.txt", "..%2Fetc%2Fpasswd", "missing.png", ""} {
		if status, _, _ := e.get(user.AvatarURLPrefix + p); status != http.StatusNotFound {
			t.Fatalf("GET %q = %d, want 404", p, status)
		}
	}
	if status, _, _ := e.do(http.MethodPost, user.AvatarURLPrefix+"x.png"); status != http.StatusMethodNotAllowed {
		t.Fatalf("POST avatar = %d, want 405", status)
	}
}

// AC-002 (user part), AC-005, FR-005: rule, case-insensitive uniqueness and
// the 30-day window.
func TestChangeUsername(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	e.user("Taken_Name")
	c := e.client(alice)
	ctx := context.Background()
	change := func(name string) (*perspectivv1.ChangeUsernameResponse, error) {
		res, err := c.ChangeUsername(ctx, connect.NewRequest(&perspectivv1.ChangeUsernameRequest{Username: name}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}

	for _, bad := range []string{"ab", strings.Repeat("x", 21), "has space", "dash-name", "émile_x"} {
		_, err := change(bad)
		wantCode(t, err, connect.CodeInvalidArgument)
	}
	_, err := change("taken_name")
	wantCode(t, err, connect.CodeAlreadyExists)
	_, err = change("alice")
	wantCode(t, err, connect.CodeInvalidArgument)

	before := time.Now()
	msg, err := change("Night_Owl")
	if err != nil {
		t.Fatalf("ChangeUsername: %v", err)
	}
	if msg.GetProfile().GetUsername() != "Night_Owl" {
		t.Fatalf("username = %q", msg.GetProfile().GetUsername())
	}
	next := msg.GetNextUsernameChangeAt().AsTime()
	if d := next.Sub(before); d < 30*24*time.Hour-time.Minute || d > 30*24*time.Hour+time.Minute {
		t.Fatalf("next change at %v, %v after the change", next, d)
	}

	// The old name is free for someone else, the new one is taken.
	var n int
	e.scan(`SELECT count(*) FROM users WHERE lower(username) = 'alice'`, nil, &n)
	if n != 0 {
		t.Fatal("old username still held")
	}

	// A second change inside the window.
	_, err = change("Second_Name")
	wantCode(t, err, connect.CodeFailedPrecondition)

	own, err := c.GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{}))
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if own.Msg.GetAccount().GetUsernameChangedAt() == nil || !own.Msg.GetAccount().GetNextUsernameChangeAt().AsTime().Equal(next) {
		t.Fatalf("account after change = %v", own.Msg.GetAccount())
	}

	// 29 days later still refused; 30 days later allowed.
	e.exec(`UPDATE users SET username_changed_at = now() - interval '29 days' WHERE id = $1`, alice)
	_, err = change("Second_Name")
	wantCode(t, err, connect.CodeFailedPrecondition)
	e.exec(`UPDATE users SET username_changed_at = now() - interval '30 days' WHERE id = $1`, alice)
	// A change of case only is a change and is allowed.
	msg, err = change("NIGHT_OWL")
	if err != nil {
		t.Fatalf("ChangeUsername after window: %v", err)
	}
	if msg.GetProfile().GetUsername() != "NIGHT_OWL" {
		t.Fatalf("username = %q", msg.GetProfile().GetUsername())
	}

	_, err = e.client(uuid.Nil).ChangeUsername(ctx, connect.NewRequest(&perspectivv1.ChangeUsernameRequest{Username: "anon_name"}))
	wantCode(t, err, connect.CodeUnauthenticated)
}

// Two concurrent changes inside one window: exactly one wins.
func TestChangeUsernameConcurrent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice := e.user("alice")
	c := e.client(alice)
	ctx := context.Background()
	errs := make(chan error, 2)
	for _, name := range []string{"first_name", "second_name"} {
		go func() {
			_, err := c.ChangeUsername(ctx, connect.NewRequest(&perspectivv1.ChangeUsernameRequest{Username: name}))
			errs <- err
		}()
	}
	var ok, refused int
	for range 2 {
		switch err := <-errs; {
		case err == nil:
			ok++
		case connect.CodeOf(err) == connect.CodeFailedPrecondition:
			refused++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || refused != 1 {
		t.Fatalf("ok=%d refused=%d, want 1 and 1", ok, refused)
	}
}

// A profile carries its badges: the canons and lores the user's readable
// perspectives belong to. A hidden perspective does not count.
func TestGetProfileLoreBadges(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	writer := e.user("badged")
	store := e.store()
	arc1, arc2, arc3 := e.dayArc(store, writer), e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-02', 'Second day', $2) RETURNING id`, store, writer), e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-03', 'Third day', $2) RETURNING id`, store, writer)
	member := func(arc uuid.UUID, standing string) uuid.UUID {
		p := e.published(arc, writer)
		l := e.id(`INSERT INTO lores (arc_id, status, member_count) VALUES ($1, $2, 2) RETURNING id`, arc, standing)
		e.exec(`INSERT INTO lore_members (lore_id, perspective_id) VALUES ($1, $2)`, l, p)
		return p
	}
	member(arc1, "LORE_STANDING_CANON")
	member(arc2, "LORE_STANDING_SUBLORE")
	hidden := member(arc3, "LORE_STANDING_CANON")
	e.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)
	// A canon at a hidden place is not readable, so it is not a badge.
	gone := e.hiddenStore()
	member(e.dayArc(gone, writer), "LORE_STANDING_CANON")

	res, err := e.client(uuid.Nil).GetProfile(ctx, connect.NewRequest(&perspectivv1.GetProfileRequest{Username: "badged"}))
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if p := res.Msg.GetProfile(); p.GetCanonCount() != 1 || p.GetLoreCount() != 2 {
		t.Fatalf("badges = %d canons, %d lores; want 1 and 2", p.GetCanonCount(), p.GetLoreCount())
	}
}
