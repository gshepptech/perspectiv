//go:build integration

package moderation_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// Hiding a top-level thread post through a report hides its branch: the
// replies under it keep their own rows untouched but can no longer be
// reported, and restoring the post makes them reportable again. Each step
// leaves its audit entry, and a reply can still be hidden on its own.
func TestHideAndRestoreThreadPostWithReplies(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("writer")
	admin, member := e.adminUser("ana"), e.user("member")
	reply := e.id(`INSERT INTO lore_posts (lore_id, author_id, body, reply_to_id, answered_id)
		VALUES ($1, $2, 'Her badge said so', $3, $3) RETURNING id`, w.lore, member, w.post)
	reportReply := func() error {
		_, err := e.client(member).Report(context.Background(), connect.NewRequest(&perspectivv1.ReportRequest{
			TargetKind: perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_POST, TargetId: reply.String(),
			Reason: perspectivv1.ReportReason_REPORT_REASON_OTHER, Details: "repeats the name",
		}))
		return err
	}
	hiddenAt := func(id uuid.UUID) bool {
		t.Helper()
		var h bool
		e.scan(`SELECT hidden_at IS NOT NULL FROM lore_posts WHERE id = $1`, []any{id}, &h)
		return h
	}

	e.resolve(admin, e.report(member, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_POST, w.post),
		perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_LORE_POST)
	if !hiddenAt(w.post) || hiddenAt(reply) {
		t.Fatalf("after hiding the post: post hidden %v, reply hidden %v; want the post only", hiddenAt(w.post), hiddenAt(reply))
	}
	wantCode(t, reportReply(), connect.CodeNotFound)

	if _, err := e.admin(admin).Restore(context.Background(), connect.NewRequest(&perspectivv1.RestoreRequest{
		TargetKind: perspectivv1.AuditTargetKind_AUDIT_TARGET_KIND_LORE_POST, TargetId: w.post.String(), Reason: "fair comment",
	})); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if hiddenAt(w.post) {
		t.Fatal("post still hidden after the restore")
	}
	if err := reportReply(); err != nil {
		t.Fatalf("report the reply after the restore: %v", err)
	}
	e.resolve(admin, e.report(admin, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_POST, reply),
		perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_LORE_POST)
	if hiddenAt(w.post) || !hiddenAt(reply) {
		t.Fatal("hiding the reply hid more than the reply")
	}

	for _, a := range []struct {
		action string
		target uuid.UUID
	}{{"AUDIT_ACTION_HIDE_LORE_POST", w.post}, {"AUDIT_ACTION_RESTORE", w.post}, {"AUDIT_ACTION_HIDE_LORE_POST", reply}} {
		if n := e.count(`SELECT count(*) FROM moderation_audit WHERE action = $1 AND target_kind = 'AUDIT_TARGET_KIND_LORE_POST' AND target_id = $2`,
			a.action, a.target); n != 1 {
			t.Errorf("%s entries on %s = %d, want 1", a.action, a.target, n)
		}
	}
}
