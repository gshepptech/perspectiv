//go:build integration

package moderation_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// FR-034, CT-013: a verified user reports a perspective, a place or a user
// with the real-name reason; the report is stored OPEN with what was given.
func TestReportStoresOpenReport(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("author_a")
	reporter := e.user("reporter_r")
	cases := []struct {
		name   string
		kind   perspectivv1.ReportTargetKind
		target uuid.UUID
	}{
		{"perspective", perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective},
		{"place", perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE, w.store},
		{"user", perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, w.author},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := e.client(reporter).Report(context.Background(), connect.NewRequest(&perspectivv1.ReportRequest{
				TargetKind: tc.kind,
				TargetId:   tc.target.String(),
				Reason:     perspectivv1.ReportReason_REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER,
				Details:    "  names the cashier  ",
			}))
			if err != nil {
				t.Fatalf("Report: %v", err)
			}
			var gotReporter, gotTarget uuid.UUID
			var kind, reason, details, status string
			e.scan(`SELECT reporter_id, target_kind, target_id, reason, details, status FROM reports WHERE id = $1`,
				[]any{res.Msg.GetReportId()}, &gotReporter, &kind, &gotTarget, &reason, &details, &status)
			if gotReporter != reporter || kind != tc.kind.String() || gotTarget != tc.target ||
				reason != "REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER" || details != "names the cashier" ||
				status != "REPORT_STATUS_OPEN" {
				t.Fatalf("row = %v %s %v %s %q %s", gotReporter, kind, gotTarget, reason, details, status)
			}
		})
	}
}

// Decided in service.go: a reporter's second report on a target they already
// have open returns the first report's id and writes no row, including when
// the two arrive at once.
func TestReportDuplicateReturnsOpenReport(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("author_a")
	reporter := e.user("reporter_r")
	other := e.user("reporter_o")
	kind := perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE

	ids := make([]uuid.UUID, 4)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() { ids[i] = e.report(reporter, kind, w.perspective) })
	}
	wg.Wait()
	for _, id := range ids[1:] {
		if id != ids[0] {
			t.Fatalf("concurrent duplicate reports got ids %v", ids)
		}
	}
	if again := e.report(reporter, kind, w.perspective); again != ids[0] {
		t.Fatalf("repeat report id = %v, want %v", again, ids[0])
	}
	if theirs := e.report(other, kind, w.perspective); theirs == ids[0] {
		t.Fatal("another reporter's report reused the first reporter's id")
	}
	if n := e.count(`SELECT count(*) FROM reports WHERE target_id = $1`, w.perspective); n != 2 {
		t.Fatalf("reports on target = %d, want 2", n)
	}

	// Once resolved, a new report on the same target opens a new row.
	admin := e.adminUser("admin_a")
	if _, err := e.admin(admin).Resolve(context.Background(), connect.NewRequest(&perspectivv1.ResolveRequest{
		ReportId: ids[0].String(), Action: perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS, Reason: "fine",
	})); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if fresh := e.report(reporter, kind, w.perspective); fresh == ids[0] {
		t.Fatal("report after resolution reused the resolved report")
	}
}

// Report refuses bad input and targets a reader cannot see, writing nothing.
func TestReportRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("author_a")
	reporter := e.user("reporter_r")
	hiddenPlace := e.id(`INSERT INTO places (kind, name, parent_id, timezone, source, hidden_at)
		VALUES ('PLACE_KIND_STORE', 'Gone Deli', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE', now()) RETURNING id`, w.state)
	hiddenPersp := e.id(`INSERT INTO perspectives (arc_id, author_id, genre, status, lore_status, published_at, hidden_at)
		VALUES ($1, $2, 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now(), now())
		RETURNING id`, w.arc, reporter)
	draft := e.id(`INSERT INTO perspectives (author_id) VALUES ($1) RETURNING id`, w.author)
	deleted := e.user("deleted_d")
	e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, deleted)

	persp := perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE
	place := perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE
	usr := perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER
	realName := perspectivv1.ReportReason_REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER
	cases := []struct {
		name    string
		kind    perspectivv1.ReportTargetKind
		target  string
		reason  perspectivv1.ReportReason
		details string
		code    connect.Code
	}{
		{"unspecified kind", perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_UNSPECIFIED, w.store.String(), realName, "", connect.CodeInvalidArgument},
		{"unknown kind", perspectivv1.ReportTargetKind(99), w.store.String(), realName, "", connect.CodeInvalidArgument},
		{"unspecified reason", place, w.store.String(), perspectivv1.ReportReason_REPORT_REASON_UNSPECIFIED, "", connect.CodeInvalidArgument},
		{"unknown reason", place, w.store.String(), perspectivv1.ReportReason(99), "", connect.CodeInvalidArgument},
		{"bad target id", place, "nope", realName, "", connect.CodeInvalidArgument},
		{"nil target id", place, uuid.Nil.String(), realName, "", connect.CodeInvalidArgument},
		{"details too long", place, w.store.String(), realName, strings.Repeat("é", 1001), connect.CodeInvalidArgument},
		{"self", usr, reporter.String(), realName, "", connect.CodeInvalidArgument},
		{"missing perspective", persp, uuid.NewString(), realName, "", connect.CodeNotFound},
		{"hidden perspective", persp, hiddenPersp.String(), realName, "", connect.CodeNotFound},
		{"draft perspective", persp, draft.String(), realName, "", connect.CodeNotFound},
		{"hidden place", place, hiddenPlace.String(), realName, "", connect.CodeNotFound},
		{"missing place", place, uuid.NewString(), realName, "", connect.CodeNotFound},
		{"deleted user", usr, deleted.String(), realName, "", connect.CodeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := e.client(reporter).Report(context.Background(), connect.NewRequest(&perspectivv1.ReportRequest{
				TargetKind: tc.kind, TargetId: tc.target, Reason: tc.reason, Details: tc.details,
			}))
			wantCode(t, err, tc.code)
		})
	}
	t.Cleanup(func() {
		if n := e.count(`SELECT count(*) FROM reports`); n != 0 {
			t.Errorf("refused reports wrote %d rows", n)
		}
	})
}

// CT-013, AC-001: Report needs a verified email (FAILED_PRECONDITION) and a
// signed-in caller, and counts against the report rate limit
// (RESOURCE_EXHAUSTED). None of the refusals writes a report.
func TestReportGates(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("author_a")
	unverified := e.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at)
		VALUES ('u@example.test', 'x', 'unverified_u', now()) RETURNING id`)
	verified := e.user("verified_v")
	req := func() *connect.Request[perspectivv1.ReportRequest] {
		return connect.NewRequest(&perspectivv1.ReportRequest{
			TargetKind: perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE,
			TargetId:   w.store.String(),
			Reason:     perspectivv1.ReportReason_REPORT_REASON_SPAM,
		})
	}
	ctx := context.Background()

	_, err := e.client(uuid.Nil).Report(ctx, req())
	wantCode(t, err, connect.CodeUnauthenticated)

	_, err = e.client(unverified).Report(ctx, req())
	wantCode(t, err, connect.CodeFailedPrecondition)

	e.lim.exhausted.Store(true)
	_, err = e.client(verified).Report(ctx, req())
	wantCode(t, err, connect.CodeResourceExhausted)

	if n := e.count(`SELECT count(*) FROM reports`); n != 0 {
		t.Fatalf("gated reports wrote %d rows", n)
	}

	e.lim.exhausted.Store(false)
	if _, err := e.client(verified).Report(ctx, req()); err != nil {
		t.Fatalf("Report after limit lifted: %v", err)
	}
}

// Decided in service.go: a suspended account cannot file a report, and an
// account deleted after its token was minted is signed out.
func TestReportBySuspendedOrDeletedAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("author_a")
	suspended := e.user("suspended_s")
	e.exec(`UPDATE users SET suspended_at = now() WHERE id = $1`, suspended)
	gone := e.user("gone_g")
	ctx := context.Background()
	req := connect.NewRequest(&perspectivv1.ReportRequest{
		TargetKind: perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE,
		TargetId:   w.store.String(),
		Reason:     perspectivv1.ReportReason_REPORT_REASON_SPAM,
	})
	_, err := e.client(suspended).Report(ctx, req)
	wantCode(t, err, connect.CodePermissionDenied)

	e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, gone)
	_, err = e.client(gone).Report(ctx, req)
	wantCode(t, err, connect.CodeUnauthenticated)

	if n := e.count(`SELECT count(*) FROM reports`); n != 0 {
		t.Fatalf("refused reports wrote %d rows", n)
	}
}
