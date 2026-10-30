//go:build integration

package moderation_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/moderation"
)

// resolve resolves a report through the API as admin.
func (e *env) resolve(admin, report uuid.UUID, action perspectivv1.ModerationAction) {
	e.t.Helper()
	if _, err := e.admin(admin).Resolve(context.Background(), connect.NewRequest(&perspectivv1.ResolveRequest{
		ReportId: report.String(), Action: action, Reason: "breaks the rules",
	})); err != nil {
		e.t.Fatalf("Resolve: %v", err)
	}
}

func (e *env) auditLog(admin uuid.UUID, req *perspectivv1.ListAuditLogRequest) ([]*perspectivv1.AuditEntry, string, error) {
	res, err := e.admin(admin).ListAuditLog(context.Background(), connect.NewRequest(req))
	if err != nil {
		return nil, "", err
	}
	return res.Msg.GetEntries(), res.Msg.GetNextPageToken(), nil
}

// auditIDsPaged walks the whole log in pages of size and returns the ids.
func (e *env) auditIDsPaged(admin uuid.UUID, size int32) []string {
	e.t.Helper()
	var ids []string
	token := ""
	for range 20 {
		entries, next, err := e.auditLog(admin, &perspectivv1.ListAuditLogRequest{PageSize: size, PageToken: token})
		if err != nil {
			e.t.Fatalf("ListAuditLog page: %v", err)
		}
		for _, en := range entries {
			ids = append(ids, en.GetId())
		}
		if token = next; token == "" {
			break
		}
	}
	return ids
}

func actions(entries []*perspectivv1.AuditEntry) []perspectivv1.AuditAction {
	out := make([]perspectivv1.AuditAction, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.GetAction())
	}
	return out
}

func sameActions(got, want []perspectivv1.AuditAction) bool { return slices.Equal(got, want) }

// The audit log lists every admin action newest first, with its actor and a
// label for its target, and filters by action, by actor and by the user an
// action was about: the account or what it wrote.
func TestAuditLog(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("writer")
	other := e.world("bystander")
	reporter := e.user("reporter")
	ana, ben := e.adminUser("ana"), e.adminUser("ben")

	e.resolve(ana, e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective),
		perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PERSPECTIVE)
	e.resolve(ben, e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, other.perspective),
		perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS)
	e.resolve(ana, e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, w.author),
		perspectivv1.ModerationAction_MODERATION_ACTION_SUSPEND_USER)
	if err := moderation.GrantAdmin(context.Background(), e.pool, "reporter"); err != nil {
		t.Fatalf("GrantAdmin: %v", err)
	}

	all, _, err := e.auditLog(ana, &perspectivv1.ListAuditLogRequest{})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	want := []perspectivv1.AuditAction{
		perspectivv1.AuditAction_AUDIT_ACTION_ADMIN_GRANT,
		perspectivv1.AuditAction_AUDIT_ACTION_SUSPEND_USER,
		perspectivv1.AuditAction_AUDIT_ACTION_DISMISS,
		perspectivv1.AuditAction_AUDIT_ACTION_HIDE_PERSPECTIVE,
	}
	if got := actions(all); !sameActions(got, want) {
		t.Fatalf("log = %v, want %v newest first", got, want)
	}
	if all[0].GetActor() != nil || all[0].GetTargetLabel() != "reporter" {
		t.Errorf("grant entry = %+v, want no actor and the username as label", all[0])
	}
	hide := all[3]
	if hide.GetActor().GetUsername() != "ana" || hide.GetTargetLabel() != "Rain at the deli" ||
		hide.GetTargetKind() != perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PERSPECTIVE || hide.GetReportId() == "" {
		t.Errorf("hide entry = %+v", hide)
	}

	tests := []struct {
		name string
		req  *perspectivv1.ListAuditLogRequest
		want []perspectivv1.AuditAction
	}{
		{name: "by action", req: &perspectivv1.ListAuditLogRequest{Action: perspectivv1.AuditAction_AUDIT_ACTION_DISMISS},
			want: []perspectivv1.AuditAction{perspectivv1.AuditAction_AUDIT_ACTION_DISMISS}},
		{name: "by actor", req: &perspectivv1.ListAuditLogRequest{ActorId: ben.String()},
			want: []perspectivv1.AuditAction{perspectivv1.AuditAction_AUDIT_ACTION_DISMISS}},
		{name: "about a user and what they wrote", req: &perspectivv1.ListAuditLogRequest{SubjectUserId: w.author.String()},
			want: []perspectivv1.AuditAction{perspectivv1.AuditAction_AUDIT_ACTION_SUSPEND_USER, perspectivv1.AuditAction_AUDIT_ACTION_HIDE_PERSPECTIVE}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := e.auditLog(ana, tc.req)
			if err != nil {
				t.Fatalf("ListAuditLog: %v", err)
			}
			if !sameActions(actions(got), tc.want) {
				t.Fatalf("log = %v, want %v", actions(got), tc.want)
			}
		})
	}

	// Pages do not overlap and end with an empty token.
	all, _, err = e.auditLog(ana, &perspectivv1.ListAuditLogRequest{})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	paged := e.auditIDsPaged(ana, 3)
	if len(paged) != len(all) || paged[0] != all[0].GetId() || paged[len(paged)-1] != all[len(all)-1].GetId() {
		t.Fatalf("paged ids %v, want the %d entries once each", paged, len(all))
	}
	_, _, err = e.auditLog(ana, &perspectivv1.ListAuditLogRequest{PageToken: "not-a-token"})
	wantCode(t, err, connect.CodeInvalidArgument)
}

// Looking an account up is in the whole log but not in the account's list of
// actions on it, which a dossier opened often would otherwise bury.
func TestDossierActionsLeaveOutLookups(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("writer")
	ana := e.adminUser("ana")
	e.resolve(ana, e.report(e.user("reporter"), perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, w.author),
		perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS)
	if _, err := e.admin(ana).LookupUser(context.Background(), connect.NewRequest(&perspectivv1.LookupUserRequest{Query: "writer"})); err != nil {
		t.Fatalf("LookupUser: %v", err)
	}
	about, _, err := e.auditLog(ana, &perspectivv1.ListAuditLogRequest{SubjectUserId: w.author.String()})
	if err != nil || !sameActions(actions(about), []perspectivv1.AuditAction{perspectivv1.AuditAction_AUDIT_ACTION_DISMISS}) {
		t.Fatalf("actions about the author = %v, %v; want only the dismissal", actions(about), err)
	}
	lookups, _, err := e.auditLog(ana, &perspectivv1.ListAuditLogRequest{Action: perspectivv1.AuditAction_AUDIT_ACTION_USER_LOOKUP})
	if err != nil || len(lookups) != 1 {
		t.Fatalf("lookups in the log = %d, %v; want 1", len(lookups), err)
	}
}

func TestAuditLogIsAdminOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	member := e.user("member")
	_, err := e.client(member).ListAuditLog(context.Background(), connect.NewRequest(&perspectivv1.ListAuditLogRequest{}))
	wantCode(t, err, connect.CodePermissionDenied)
	// A token that still claims ADMIN after the stored role changed is refused.
	_, _, err = e.auditLog(member, &perspectivv1.ListAuditLogRequest{})
	wantCode(t, err, connect.CodePermissionDenied)
	if len(e.logs.denials(member, "/perspectiv.v1.ModerationService/ListAuditLog")) == 0 {
		t.Fatal("a refused audit log read wrote no authz_denied line")
	}
}

func TestLookupUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("Writer_One")
	reporter := e.user("reporter")
	admin := e.adminUser("ana")
	e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective)
	e.resolve(admin, e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_POST, w.post),
		perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_LORE_POST)
	e.exec(`INSERT INTO login_attempts (user_id, succeeded) VALUES ($1, false), ($1, false)`, w.author)
	gone := e.user("gone")
	e.exec(`UPDATE users SET deleted_at = now(), email = id::text || '@deleted.invalid' WHERE id = $1`, gone)
	lookup := func(query string) (*perspectivv1.AdminUserView, error) {
		res, err := e.admin(admin).LookupUser(context.Background(), connect.NewRequest(&perspectivv1.LookupUserRequest{Query: query}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetUser(), nil
	}

	tests := []struct {
		name, query string
		want        uuid.UUID
	}{
		{name: "username in any case", query: "  writer_ONE ", want: w.author},
		{name: "email", query: "Writer_One@example.test", want: w.author},
		{name: "id", query: w.author.String(), want: w.author},
		{name: "a deleted account by id", query: gone.String(), want: gone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, err := lookup(tc.query)
			if err != nil {
				t.Fatalf("LookupUser(%q): %v", tc.query, err)
			}
			if u.GetUserId() != tc.want.String() {
				t.Fatalf("found %s, want %s", u.GetUserId(), tc.want)
			}
		})
	}

	u, err := lookup("writer_one")
	if err != nil {
		t.Fatalf("LookupUser: %v", err)
	}
	if u.GetEmailHint() != "Wr•••@example.test" || !u.GetEmailVerified() || u.GetRole() != perspectivv1.Role_ROLE_USER ||
		u.GetStories() != 1 || u.GetFailedSignIns_24H() != 2 || u.GetReportsAgainst() != 2 || u.GetOpenReportsAgainst() != 1 ||
		u.GetReportsFiled() != 0 || u.GetLastSignInAt() != nil || u.GetDeleted() {
		t.Fatalf("view = %+v", u)
	}
	g, err := lookup(gone.String())
	if err != nil {
		t.Fatalf("LookupUser deleted: %v", err)
	}
	if !g.GetDeleted() || g.GetEmailHint() != "" {
		t.Fatalf("deleted view = %+v, want deleted and no hint", g)
	}

	for _, q := range []string{"nobody", "nobody@example.test", uuid.NewString(), "gone"} {
		_, err := lookup(q)
		wantCode(t, err, connect.CodeNotFound)
	}
	_, err = lookup("  ")
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = lookup("not an @ address")
	wantCode(t, err, connect.CodeInvalidArgument)

	// Each match is audited with how it was found, never the address.
	var n, byEmail int
	e.scan(`SELECT count(*), count(*) FILTER (WHERE reason = 'looked up by email') FROM moderation_audit
	        WHERE action = 'AUDIT_ACTION_USER_LOOKUP' AND actor_id = $1`, []any{admin}, &n, &byEmail)
	if n != 6 || byEmail != 1 {
		t.Fatalf("lookup audit rows = %d (%d by email), want 6 (1 by email)", n, byEmail)
	}
	if e.count(`SELECT count(*) FROM moderation_audit WHERE reason ILIKE '%example.test%'`) != 0 {
		t.Fatal("an audit row carries a searched address")
	}

	_, err = e.client(reporter).LookupUser(context.Background(), connect.NewRequest(&perspectivv1.LookupUserRequest{Query: "writer_one"}))
	wantCode(t, err, connect.CodePermissionDenied)
}

func TestListReportsFilters(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("writer")
	other := e.world("bystander")
	alice, bob := e.user("alice"), e.user("bob")
	admin := e.adminUser("ana")
	onStory := e.report(alice, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective)
	onPost := e.report(bob, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_POST, w.post)
	onAccount := e.report(bob, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, w.author)
	unrelated := e.report(alice, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, other.perspective)

	ids := func(req *perspectivv1.ListReportsRequest) []string {
		t.Helper()
		res, err := e.admin(admin).ListReports(context.Background(), connect.NewRequest(req))
		if err != nil {
			t.Fatalf("ListReports: %v", err)
		}
		var out []string
		for _, r := range res.Msg.GetReports() {
			out = append(out, r.GetId())
		}
		return out
	}
	tests := []struct {
		name string
		req  *perspectivv1.ListReportsRequest
		want []uuid.UUID
	}{
		{name: "against a user and what they wrote", req: &perspectivv1.ListReportsRequest{SubjectUserId: w.author.String()},
			want: []uuid.UUID{onStory, onPost, onAccount}},
		{name: "filed by a user", req: &perspectivv1.ListReportsRequest{ReporterId: alice.String()},
			want: []uuid.UUID{onStory, unrelated}},
		{name: "both", req: &perspectivv1.ListReportsRequest{SubjectUserId: w.author.String(), ReporterId: bob.String()},
			want: []uuid.UUID{onPost, onAccount}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(tc.req)
			if len(got) != len(tc.want) {
				t.Fatalf("reports = %v, want %v", got, tc.want)
			}
			for i, id := range tc.want {
				if got[i] != id.String() {
					t.Fatalf("reports = %v, want %v", got, tc.want)
				}
			}
		})
	}
	_, err := e.admin(admin).ListReports(context.Background(), connect.NewRequest(&perspectivv1.ListReportsRequest{SubjectUserId: "nope"}))
	wantCode(t, err, connect.CodeInvalidArgument)
}

// WriteAudit refuses an entry that names its target twice or not at all, or
// carries an unrecordable action, and writes nothing.
func TestWriteAuditRefusesMalformedEntries(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	target := e.user("target")
	tests := []struct {
		name  string
		entry moderation.AuditEntry
	}{
		{name: "no target", entry: moderation.AuditEntry{Action: perspectivv1.AuditAction_AUDIT_ACTION_USER_LOOKUP,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER}},
		{name: "two targets", entry: moderation.AuditEntry{Action: perspectivv1.AuditAction_AUDIT_ACTION_USER_LOOKUP,
			TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER, TargetID: &target, TargetRef: "beta-x"}},
		{name: "no action", entry: moderation.AuditEntry{TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER, TargetID: &target}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := pgx.BeginFunc(context.Background(), e.pool, func(tx pgx.Tx) error {
				return moderation.WriteAudit(context.Background(), tx, tc.entry)
			})
			if err == nil {
				t.Fatal("WriteAudit = nil, want a refusal")
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM moderation_audit`); n != 0 {
		t.Fatalf("audit rows = %d, want 0", n)
	}
}

// An admin lifts a suspension with a reason: the account can report again,
// the dossier shows it unsuspended, and the audit log names who and why.
func TestLiftSuspension(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("writer")
	member := e.user("member")
	admin := e.adminUser("ana")
	e.resolve(admin, e.report(member, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, w.author),
		perspectivv1.ModerationAction_MODERATION_ACTION_SUSPEND_USER)
	lift := func(as uuid.UUID, target, reason string) (*perspectivv1.AdminUserView, error) {
		res, err := e.admin(as).LiftSuspension(context.Background(), connect.NewRequest(&perspectivv1.LiftSuspensionRequest{
			UserId: target, Reason: reason,
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetUser(), nil
	}

	tests := []struct {
		name, target, reason string
		as                   uuid.UUID
		want                 connect.Code
	}{
		{name: "no reason", as: admin, target: w.author.String(), reason: "  ", want: connect.CodeInvalidArgument},
		{name: "not an id", as: admin, target: "writer", reason: "appeal upheld", want: connect.CodeInvalidArgument},
		{name: "no such account", as: admin, target: uuid.NewString(), reason: "appeal upheld", want: connect.CodeNotFound},
		{name: "not suspended", as: admin, target: member.String(), reason: "appeal upheld", want: connect.CodeFailedPrecondition},
		{name: "not an admin", as: member, target: w.author.String(), reason: "appeal upheld", want: connect.CodePermissionDenied},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := lift(tc.as, tc.target, tc.reason)
			wantCode(t, err, tc.want)
		})
	}

	u, err := lift(admin, w.author.String(), "  appeal upheld  ")
	if err != nil {
		t.Fatalf("LiftSuspension: %v", err)
	}
	if u.GetSuspendedAt() != nil || u.GetUserId() != w.author.String() {
		t.Fatalf("view after lifting = %+v", u)
	}
	// Standing restored: the account can file a report again.
	e.report(w.author, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, e.world("other").perspective)
	entries, _, err := e.auditLog(admin, &perspectivv1.ListAuditLogRequest{SubjectUserId: w.author.String()})
	if err != nil || len(entries) == 0 {
		t.Fatalf("audit entries = %v, %v", entries, err)
	}
	last := entries[0]
	if last.GetAction() != perspectivv1.AuditAction_AUDIT_ACTION_LIFT_SUSPENSION || last.GetReason() != "appeal upheld" ||
		last.GetActor().GetUsername() != "ana" {
		t.Fatalf("newest entry = %+v", last)
	}
	// Lifting twice is refused rather than audited twice.
	_, err = lift(admin, w.author.String(), "again")
	wantCode(t, err, connect.CodeFailedPrecondition)
}

// An admin restores hidden content with a reason: it is readable again, the
// arc is queued for a lore recount, and the audit log names who and why.
func TestRestore(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("writer")
	member := e.user("member")
	admin := e.adminUser("ana")
	for _, r := range []struct {
		kind   perspectivv1.ReportTargetKind
		target uuid.UUID
		action perspectivv1.ModerationAction
	}{
		{perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective, perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PERSPECTIVE},
		{perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE, w.store, perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PLACE},
		{perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_POST, w.post, perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_LORE_POST},
	} {
		e.resolve(admin, e.report(member, r.kind, r.target), r.action)
	}
	e.exec(`UPDATE arcs SET lore_dirty_at = NULL WHERE id = $1`, w.arc)
	restore := func(as uuid.UUID, kind perspectivv1.AuditTargetKind, target uuid.UUID, reason string) error {
		_, err := e.admin(as).Restore(context.Background(), connect.NewRequest(&perspectivv1.RestoreRequest{
			TargetKind: kind, TargetId: target.String(), Reason: reason,
		}))
		return err
	}
	hidden := func(table string, id uuid.UUID) bool {
		t.Helper()
		var h bool
		e.scan(`SELECT hidden_at IS NOT NULL FROM `+table+` WHERE id = $1`, []any{id}, &h)
		return h
	}

	refusals := []struct {
		name   string
		as     uuid.UUID
		kind   perspectivv1.AuditTargetKind
		target uuid.UUID
		reason string
		want   connect.Code
	}{
		{name: "no reason", as: admin, kind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PLACE, target: w.store, want: connect.CodeInvalidArgument},
		{name: "a kind that cannot be restored", as: admin, kind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_USER, target: w.author, reason: "x", want: connect.CodeInvalidArgument},
		{name: "not hidden", as: admin, kind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PERSPECTIVE, target: e.world("other").perspective, reason: "x", want: connect.CodeFailedPrecondition},
		{name: "not an admin", as: member, kind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PLACE, target: w.store, reason: "x", want: connect.CodePermissionDenied},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, restore(tc.as, tc.kind, tc.target, tc.reason), tc.want)
		})
	}

	restores := []struct {
		kind   perspectivv1.AuditTargetKind
		table  string
		target uuid.UUID
	}{
		{perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PERSPECTIVE, "perspectives", w.perspective},
		{perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_PLACE, "places", w.store},
		{perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_LORE_POST, "lore_posts", w.post},
	}
	for _, r := range restores {
		if !hidden(r.table, r.target) {
			t.Fatalf("%s was not hidden before the restore", r.table)
		}
		if err := restore(admin, r.kind, r.target, "hidden by mistake"); err != nil {
			t.Fatalf("Restore %v: %v", r.kind, err)
		}
		if hidden(r.table, r.target) {
			t.Fatalf("%s still hidden after the restore", r.table)
		}
		wantCode(t, restore(admin, r.kind, r.target, "again"), connect.CodeFailedPrecondition)
	}
	var dirty bool
	e.scan(`SELECT lore_dirty_at IS NOT NULL FROM arcs WHERE id = $1`, []any{w.arc}, &dirty)
	if !dirty {
		t.Fatal("restoring a perspective did not queue its arc for a lore recount")
	}
	if n := e.count(`SELECT count(*) FROM moderation_audit WHERE action = 'AUDIT_ACTION_RESTORE' AND reason = 'hidden by mistake' AND actor_id = $1`, admin); n != 3 {
		t.Fatalf("restore audit entries = %d, want 3", n)
	}
}
