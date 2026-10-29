//go:build integration

package auth_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/auth"
	"perspectiv/internal/jobs"
)

// id runs an INSERT ... RETURNING id.
func (e *env) id(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.scan(query, args, &id)
	return id
}

// count runs a SELECT count(*).
func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	e.scan(query, args, &n)
	return n
}

func (e *env) authAs(access string) perspectivv1connect.AuthServiceClient {
	return perspectivv1connect.NewAuthServiceClient(e.srv.Client(), e.srv.URL+"/api", clientOpts(access)...)
}

func (e *env) signUpWith(username, code string) error {
	_, err := e.auth().SignUp(context.Background(), connect.NewRequest(&perspectivv1.SignUpRequest{
		Email: emailOf(username), Password: goodPassword, Username: username, AgeConfirmed: true, AccessCode: code,
	}))
	return err
}

// betaEnvs returns an open server, used to make the admin and a member, and
// an invite-only server on the same database, with the admin's access token.
func betaEnvs(t *testing.T) (open, invite *env, adminAccess string, member uuid.UUID) {
	t.Helper()
	open = newEnv(t)
	adminID := open.signUp("boss")
	open.exec(`UPDATE users SET role = 'ROLE_ADMIN' WHERE id = $1`, adminID)
	member = open.signUp("member")
	invite = newEnvMode(t, open.pool, auth.SignUpInvite)
	adminAccess, _ = invite.mustLogin("boss")
	return open, invite, adminAccess, member
}

// signUpCase is one sign-up attempt and what it should do: be refused with
// want, or succeed and record joined.
type signUpCase struct {
	name     string
	username string
	code     string
	want     connect.Code
	joined   string
}

// checkSignUps runs the attempts in order, since each may use up a code.
func checkSignUps(t *testing.T, e *env, cases []signUpCase) {
	t.Helper()
	for _, tc := range cases {
		err := e.signUpWith(tc.username, tc.code)
		if tc.want != 0 {
			wantCode(t, err, tc.want)
			continue
		}
		if err != nil {
			t.Fatalf("%s: SignUp: %v", tc.name, err)
		}
		var joined *string
		e.scan(`SELECT joined_with FROM users WHERE username = $1`, []any{tc.username}, &joined)
		if joined == nil || *joined != tc.joined {
			t.Fatalf("%s: joined_with = %v, want %q", tc.name, joined, tc.joined)
		}
	}
}

// While sign-up is invite-only, an account needs a live beta code or a
// member's invite link code; a refused sign-up creates nothing and says the
// same whether or not the email is taken.
func TestInviteOnlySignUp(t *testing.T) {
	t.Parallel()
	open, e, adminAccess, member := betaEnvs(t)
	ctx := context.Background()
	admin := e.authAs(adminAccess)

	info, err := e.auth().GetSignUpInfo(ctx, connect.NewRequest(&perspectivv1.GetSignUpInfoRequest{}))
	if err != nil || !info.Msg.GetInviteOnly() {
		t.Fatalf("GetSignUpInfo = %v, %v; want invite-only", info, err)
	}
	if res, err := open.auth().GetSignUpInfo(ctx, connect.NewRequest(&perspectivv1.GetSignUpInfoRequest{})); err != nil || res.Msg.GetInviteOnly() {
		t.Fatalf("open GetSignUpInfo = %v, %v", res, err)
	}

	two, err := admin.CreateAccessCode(ctx, connect.NewRequest(&perspectivv1.CreateAccessCodeRequest{Code: " UCF-Launch ", Note: "launch night", MaxUses: 2}))
	if err != nil || two.Msg.GetCode().GetCode() != "ucf-launch" || two.Msg.GetCode().GetMaxUses() != 2 {
		t.Fatalf("CreateAccessCode = %v, %v", two, err)
	}
	gen, err := admin.CreateAccessCode(ctx, connect.NewRequest(&perspectivv1.CreateAccessCodeRequest{MaxUses: 5, ExpiresInDays: 7}))
	if err != nil || !strings.HasPrefix(gen.Msg.GetCode().GetCode(), "beta-") || gen.Msg.GetCode().GetExpiresAt() == nil {
		t.Fatalf("generated code = %v, %v", gen, err)
	}

	// A member's invite link admits too.
	place := open.id(`INSERT INTO places (kind, name, timezone, source) VALUES ('PLACE_KIND_STATE', 'Beta State', 'America/New_York', 'PLACE_SOURCE_USER') RETURNING id`)
	arcID := open.id(`INSERT INTO arcs (place_id, local_date, title, created_by) VALUES ($1, '2026-10-01', 'Beta night', $2) RETURNING id`, place, member)
	p := open.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now()) RETURNING id`, arcID, member)
	open.exec(`INSERT INTO invites (code, perspective_id, inviter_id) VALUES ('abcde12345', $1, $2)`, p, member)

	tests := []signUpCase{
		{name: "no code", username: "nocode", code: "", want: connect.CodePermissionDenied},
		{name: "unknown code", username: "badcode", code: "nope-nope", want: connect.CodePermissionDenied},
		{name: "beta code, typed loosely", username: "first", code: "UCF-LAUNCH ", joined: "code:ucf-launch"},
		{name: "beta code, second and last use", username: "second", code: "ucf-launch", joined: "code:ucf-launch"},
		{name: "beta code used up", username: "third", code: "ucf-launch", want: connect.CodePermissionDenied},
		{name: "member invite code", username: "invited", code: "ABCDE12345", joined: "invite:abcde12345"},
		{name: "taken email with no code says the same", username: "member", code: "", want: connect.CodePermissionDenied},
	}
	checkSignUps(t, e, tests)
	if n := e.count(`SELECT count(*) FROM users WHERE username IN ('nocode', 'badcode', 'third')`); n != 0 {
		t.Fatalf("refused sign-ups created %d accounts", n)
	}

	// A revoked or expired code admits nobody more.
	if _, err := admin.RevokeAccessCode(ctx, connect.NewRequest(&perspectivv1.RevokeAccessCodeRequest{Code: gen.Msg.GetCode().GetCode()})); err != nil {
		t.Fatalf("RevokeAccessCode: %v", err)
	}
	wantCode(t, e.signUpWith("revoked", gen.Msg.GetCode().GetCode()), connect.CodePermissionDenied)
	e.exec(`INSERT INTO access_codes (code, max_uses, expires_at) VALUES ('old-code', 5, now() - interval '1 day')`)
	wantCode(t, e.signUpWith("expired", "old-code"), connect.CodePermissionDenied)
	// A suspended member's invites stop admitting.
	e.exec(`UPDATE users SET suspended_at = now() WHERE id = $1`, member)
	wantCode(t, e.signUpWith("late", "abcde12345"), connect.CodePermissionDenied)

	list, err := admin.ListAccessCodes(ctx, connect.NewRequest(&perspectivv1.ListAccessCodesRequest{PageSize: 1}))
	if err != nil || len(list.Msg.GetCodes()) != 1 || list.Msg.GetNextPageToken() == "" {
		t.Fatalf("ListAccessCodes page 1 = %v, %v", list, err)
	}
	// Open sign-up ignores a code and uses none.
	if err := open.signUpWith("openuser", "ucf-launch"); err != nil {
		t.Fatalf("open SignUp: %v", err)
	}
}

// The waitlist takes any address once and answers the same either way; an
// admin's invite issues a single-use code, emails a sign-up link with it,
// and that code admits exactly one account.
func TestWaitlistInvite(t *testing.T) {
	t.Parallel()
	_, e, adminAccess, _ := betaEnvs(t)
	ctx := context.Background()
	admin := e.authAs(adminAccess)
	join := func(email, note string) error {
		_, err := e.auth().JoinWaitlist(ctx, connect.NewRequest(&perspectivv1.JoinWaitlistRequest{Email: email, Note: note}))
		return err
	}
	if err := join("Pat@Example.test", "  UCF   games "); err != nil {
		t.Fatalf("JoinWaitlist: %v", err)
	}
	if err := join("pat@example.test", "again"); err != nil {
		t.Fatalf("JoinWaitlist repeat: %v", err)
	}
	wantCode(t, join("not an email", ""), connect.CodeInvalidArgument)
	wantCode(t, join("ok@example.test", strings.Repeat("x", 301)), connect.CodeInvalidArgument)

	wl, err := admin.ListWaitlist(ctx, connect.NewRequest(&perspectivv1.ListWaitlistRequest{WaitingOnly: true}))
	if err != nil || len(wl.Msg.GetEntries()) != 1 || wl.Msg.GetEntries()[0].GetNote() != "UCF games" ||
		wl.Msg.GetEntries()[0].GetEmailHint() != "Pa•••@Example.test" {
		t.Fatalf("ListWaitlist = %v, %v", wl, err)
	}
	entry := wl.Msg.GetEntries()[0]
	inv, err := admin.InviteFromWaitlist(ctx, connect.NewRequest(&perspectivv1.InviteFromWaitlistRequest{Id: entry.GetId()}))
	if err != nil || inv.Msg.GetEntry().GetInvitedAt() == nil || !strings.HasPrefix(inv.Msg.GetEntry().GetCode(), "wl-") {
		t.Fatalf("InviteFromWaitlist = %v, %v", inv, err)
	}
	// Inviting again resends the same code.
	again, err := admin.InviteFromWaitlist(ctx, connect.NewRequest(&perspectivv1.InviteFromWaitlistRequest{Id: entry.GetId()}))
	if err != nil || again.Msg.GetEntry().GetCode() != inv.Msg.GetEntry().GetCode() {
		t.Fatalf("second invite = %v, %v", again, err)
	}
	mails := e.emailJobs("Pat@Example.test")
	if len(mails) != 2 || mails[0].Template != jobs.TemplateBetaInvite {
		t.Fatalf("invite emails = %+v", mails)
	}
	u, err := url.Parse(mails[0].Data["link"])
	if err != nil || u.Path != "/signup" || u.Query().Get("code") != inv.Msg.GetEntry().GetCode() {
		t.Fatalf("invite link = %q", mails[0].Data["link"])
	}
	if res, _ := admin.ListWaitlist(ctx, connect.NewRequest(&perspectivv1.ListWaitlistRequest{WaitingOnly: true})); len(res.Msg.GetEntries()) != 0 {
		t.Fatalf("still waiting = %v", res.Msg.GetEntries())
	}

	code := inv.Msg.GetEntry().GetCode()
	if err := e.signUpWith("pat", code); err != nil {
		t.Fatalf("SignUp with waitlist code: %v", err)
	}
	wantCode(t, e.signUpWith("pat2", code), connect.CodePermissionDenied)
}

// The beta admin calls are for admins only, checked against the stored role.
func TestBetaAdminOnly(t *testing.T) {
	t.Parallel()
	_, e, adminAccess, _ := betaEnvs(t)
	ctx := context.Background()
	memberAccess, _ := e.mustLogin("member")
	calls := map[string]func(c perspectivv1connect.AuthServiceClient) error{
		"CreateAccessCode": func(c perspectivv1connect.AuthServiceClient) error {
			_, err := c.CreateAccessCode(ctx, connect.NewRequest(&perspectivv1.CreateAccessCodeRequest{MaxUses: 1}))
			return err
		},
		"ListAccessCodes": func(c perspectivv1connect.AuthServiceClient) error {
			_, err := c.ListAccessCodes(ctx, connect.NewRequest(&perspectivv1.ListAccessCodesRequest{}))
			return err
		},
		"ListWaitlist": func(c perspectivv1connect.AuthServiceClient) error {
			_, err := c.ListWaitlist(ctx, connect.NewRequest(&perspectivv1.ListWaitlistRequest{}))
			return err
		},
		"RevokeAccessCode": func(c perspectivv1connect.AuthServiceClient) error {
			_, err := c.RevokeAccessCode(ctx, connect.NewRequest(&perspectivv1.RevokeAccessCodeRequest{Code: "none-such"}))
			return err
		},
		"InviteFromWaitlist": func(c perspectivv1connect.AuthServiceClient) error {
			_, err := c.InviteFromWaitlist(ctx, connect.NewRequest(&perspectivv1.InviteFromWaitlistRequest{Id: uuid.NewString()}))
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wantCode(t, call(e.authAs(memberAccess)), connect.CodePermissionDenied)
			wantCode(t, call(e.auth()), connect.CodeUnauthenticated)
		})
	}
	// An admin token minted before a demotion no longer works.
	e.exec(`UPDATE users SET role = 'ROLE_USER' WHERE username = 'boss'`)
	_, err := e.authAs(adminAccess).ListWaitlist(ctx, connect.NewRequest(&perspectivv1.ListWaitlistRequest{}))
	wantCode(t, err, connect.CodePermissionDenied)
}

// A filled honeypot on sign-up or the waitlist is answered as a success and
// stores nothing.
func TestHoneypot(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	res, err := e.auth().SignUp(ctx, connect.NewRequest(&perspectivv1.SignUpRequest{
		Email: emailOf("botty"), Password: goodPassword, Username: "botty", AgeConfirmed: true, Website: "http://spam.example",
	}))
	if err != nil || res.Msg.GetUserId() == "" {
		t.Fatalf("honeypot SignUp = %v, %v; want a normal-looking success", res, err)
	}
	if _, err := e.auth().JoinWaitlist(ctx, connect.NewRequest(&perspectivv1.JoinWaitlistRequest{Email: "bot@example.test", Website: "x"})); err != nil {
		t.Fatalf("honeypot JoinWaitlist: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE username = 'botty'`); n != 0 {
		t.Fatalf("honeypot made %d accounts", n)
	}
	if n := e.count(`SELECT count(*) FROM waitlist`); n != 0 {
		t.Fatalf("honeypot listed %d addresses", n)
	}
	if mails := e.emailJobs(emailOf("botty")); len(mails) != 0 {
		t.Fatalf("honeypot sent %d emails", len(mails))
	}
}
