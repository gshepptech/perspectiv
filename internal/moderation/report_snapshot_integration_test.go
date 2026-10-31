//go:build integration

package moderation_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// A report keeps what its target said when it was filed, so an author who
// edits a reported post to something harmless before review cannot hide
// what was reported. Each target kind keeps its own words.
func TestReportKeepsWhatWasReported(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("writer")
	admin, member := e.adminUser("ana"), e.user("member")
	e.exec(`INSERT INTO perspective_parts (perspective_id, position, body) VALUES ($1, 1, 'First part.'), ($1, 2, 'Second part.')`, w.perspective)
	e.exec(`INSERT INTO characters (perspective_id, label, position) VALUES ($1, 'the cook', 1), ($1, 'Jo Smith at the till', 2)`, w.perspective)
	e.exec(`UPDATE users SET bio = 'call me' WHERE id = $1`, w.author)

	kinds := []struct {
		kind   perspectivv1.ReportTargetKind
		target uuid.UUID
		want   string
	}{
		{perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_POST, w.post, "The cashier is Jo Smith"},
		{perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_LORE_NAME, w.lore, "Deli regulars"},
		{perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective, "First part.\n\nSecond part.\n\nCharacters: the cook; Jo Smith at the till"},
		{perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE, w.store, "Corner Deli"},
		{perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, w.author, "writer\n\ncall me"},
	}
	ids := map[string]perspectivv1.ReportTargetKind{}
	for _, k := range kinds {
		ids[e.report(member, k.kind, k.target).String()] = k.kind
	}

	// The author then rewrites everything they can.
	e.exec(`UPDATE lore_posts SET body = 'ok', edited_at = now() WHERE id = $1`, w.post)
	e.exec(`UPDATE perspective_parts SET body = 'Nothing to see.' WHERE perspective_id = $1`, w.perspective)
	e.exec(`UPDATE users SET bio = '' WHERE id = $1`, w.author)

	res, err := e.admin(admin).ListReports(context.Background(), connect.NewRequest(&perspectivv1.ListReportsRequest{}))
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	got := map[perspectivv1.ReportTargetKind]string{}
	for _, r := range res.Msg.GetReports() {
		if _, ok := ids[r.GetId()]; ok {
			got[r.GetTargetKind()] = r.GetReportedText()
		}
	}
	for _, k := range kinds {
		if got[k.kind] != k.want {
			t.Errorf("%v reported text = %q, want %q", k.kind, got[k.kind], k.want)
		}
	}
}
