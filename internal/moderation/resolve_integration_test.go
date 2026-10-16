//go:build integration

package moderation_test

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/jobs"
)

// auditRow is one moderation_audit row.
type auditRow struct {
	actor      *uuid.UUID
	action     string
	targetKind string
	target     uuid.UUID
	report     *uuid.UUID
	reason     string
}

func (e *env) audits(target uuid.UUID) []auditRow {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT actor_id, action, target_kind, target_id, report_id, reason
		FROM moderation_audit WHERE target_id = $1 ORDER BY id`, target)
	if err != nil {
		e.t.Fatalf("audits: %v", err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var a auditRow
		if err := rows.Scan(&a.actor, &a.action, &a.targetKind, &a.target, &a.report, &a.reason); err != nil {
			e.t.Fatalf("scan audit: %v", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		e.t.Fatalf("audits: %v", err)
	}
	return out
}

// notifyJobs returns the queued notify job args naming a report.
func (e *env) notifyJobs(report uuid.UUID) []jobs.NotifyArgs {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(),
		`SELECT args FROM river_job WHERE kind = 'notify' AND args->>'ReportID' = $1 ORDER BY id`, report.String())
	if err != nil {
		e.t.Fatalf("notify jobs: %v", err)
	}
	defer rows.Close()
	var out []jobs.NotifyArgs
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			e.t.Fatalf("scan job: %v", err)
		}
		var a jobs.NotifyArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			e.t.Fatalf("decode %s: %v", raw, err)
		}
		out = append(out, a)
	}
	return out
}

// AC-036, OT-038 (moderation part): only an ADMIN may open the queue or
// resolve. A USER token is refused by the interceptor; a token that still
// claims ADMIN for an account whose stored role is USER, or whose account is
// suspended, is refused by the service. Every refusal is PERMISSION_DENIED
// with an authz_denied line, and nothing changes.
func TestAdminOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("author_a")
	reporter := e.user("reporter_r")
	report := e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective)
	plain := e.user("plain_p")
	demoted := e.user("demoted_d")
	suspendedAdmin := e.adminUser("suspended_admin")
	e.exec(`UPDATE users SET suspended_at = now() WHERE id = $1`, suspendedAdmin)
	missing := uuid.New()

	cases := []struct {
		name   string
		caller uuid.UUID
		client func(uuid.UUID) perspectivv1connect.ModerationServiceClient
		reason string
	}{
		{"user token", plain, e.client, "role"},
		{"admin token, stored role user", demoted, e.admin, "role"},
		{"admin token, suspended admin", suspendedAdmin, e.admin, "suspended"},
		{"admin token, no account", missing, e.admin, "account_missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := tc.client(tc.caller)
			ctx := context.Background()
			_, err := c.Resolve(ctx, connect.NewRequest(&perspectivv1.ResolveRequest{
				ReportId: report.String(), Action: perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PERSPECTIVE, Reason: "names a person",
			}))
			wantCode(t, err, connect.CodePermissionDenied)
			_, err = c.ListReports(ctx, connect.NewRequest(&perspectivv1.ListReportsRequest{}))
			wantCode(t, err, connect.CodePermissionDenied)

			for _, method := range []string{
				perspectivv1connect.ModerationServiceResolveProcedure,
				perspectivv1connect.ModerationServiceListReportsProcedure,
			} {
				lines := e.logs.denials(tc.caller, method)
				if len(lines) == 0 {
					t.Fatalf("no authz_denied line for %s", method)
				}
				if lines[0]["required"] != "admin" || lines[0]["reason"] != tc.reason {
					t.Fatalf("denial line = %v, want required admin, reason %s", lines[0], tc.reason)
				}
			}
		})
	}
	t.Cleanup(func() {
		var status string
		var hidden *time.Time
		e.scan(`SELECT status FROM reports WHERE id = $1`, []any{report}, &status)
		e.scan(`SELECT hidden_at FROM perspectives WHERE id = $1`, []any{w.perspective}, &hidden)
		if status != "REPORT_STATUS_OPEN" || hidden != nil || len(e.audits(w.perspective)) != 0 {
			t.Errorf("denied resolve changed state: status %s hidden %v", status, hidden)
		}
	})
}

// AC-037, OT-039, CT-013: each action resolves the report, applies to its
// target and writes one audit row with actor, target, report and reason, and
// queues a REPORT_RESOLVED notification that names no actor.
func TestResolveActions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		kind   perspectivv1.ReportTargetKind
		action perspectivv1.ModerationAction
		target func(world) uuid.UUID
		// applied checks the target after the resolution.
		applied func(t *testing.T, e *env, w world)
	}{
		{
			name: "hide perspective", kind: perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE,
			action: perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PERSPECTIVE,
			target: func(w world) uuid.UUID { return w.perspective },
			applied: func(t *testing.T, e *env, w world) {
				var hidden, dirty *time.Time
				e.scan(`SELECT hidden_at FROM perspectives WHERE id = $1`, []any{w.perspective}, &hidden)
				e.scan(`SELECT lore_dirty_at FROM arcs WHERE id = $1`, []any{w.arc}, &dirty)
				if hidden == nil || dirty == nil {
					t.Fatalf("hidden_at %v lore_dirty_at %v, want both set", hidden, dirty)
				}
			},
		},
		{
			name: "hide place", kind: perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE,
			action: perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PLACE,
			target: func(w world) uuid.UUID { return w.store },
			applied: func(t *testing.T, e *env, w world) {
				var hidden *time.Time
				e.scan(`SELECT hidden_at FROM places WHERE id = $1`, []any{w.store}, &hidden)
				if hidden == nil {
					t.Fatal("place hidden_at not set")
				}
			},
		},
		{
			name: "suspend user", kind: perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER,
			action: perspectivv1.ModerationAction_MODERATION_ACTION_SUSPEND_USER,
			target: func(w world) uuid.UUID { return w.author },
			applied: func(t *testing.T, e *env, w world) {
				var suspended, hidden *time.Time
				e.scan(`SELECT suspended_at FROM users WHERE id = $1`, []any{w.author}, &suspended)
				e.scan(`SELECT hidden_at FROM perspectives WHERE id = $1`, []any{w.perspective}, &hidden)
				if suspended == nil {
					t.Fatal("suspended_at not set")
				}
				// Suspension does not remove what the account published.
				if hidden != nil {
					t.Fatal("suspension hid the account's perspective")
				}
			},
		},
		{
			name: "dismiss", kind: perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE,
			action: perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS,
			target: func(w world) uuid.UUID { return w.perspective },
			applied: func(t *testing.T, e *env, w world) {
				var hidden, dirty *time.Time
				e.scan(`SELECT hidden_at FROM perspectives WHERE id = $1`, []any{w.perspective}, &hidden)
				e.scan(`SELECT lore_dirty_at FROM arcs WHERE id = $1`, []any{w.arc}, &dirty)
				if hidden != nil || dirty != nil {
					t.Fatalf("dismiss changed the target: hidden %v dirty %v", hidden, dirty)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			w := e.world("author_a")
			reporter := e.user("reporter_r")
			admin := e.adminUser("admin_a")
			target := tc.target(w)
			report := e.report(reporter, tc.kind, target)

			res, err := e.admin(admin).Resolve(context.Background(), connect.NewRequest(&perspectivv1.ResolveRequest{
				ReportId: report.String(), Action: tc.action, Reason: "  names the cashier  ",
			}))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			tc.applied(t, e, w)
			checkResolved(t, e, res.Msg.GetReport(), resolution{
				admin: admin, report: report, action: tc.action, kind: tc.kind, target: target,
			})
		})
	}
}

// resolution is what one Resolve call should have recorded.
type resolution struct {
	admin, report, target uuid.UUID
	action                perspectivv1.ModerationAction
	kind                  perspectivv1.ReportTargetKind
}

// checkResolved asserts the returned report, the single audit row and the
// queued REPORT_RESOLVED notification of one resolution.
func checkResolved(t *testing.T, e *env, r *perspectivv1.Report, want resolution) {
	t.Helper()
	if r.GetStatus() != perspectivv1.ReportStatus_REPORT_STATUS_RESOLVED || r.GetResolution() != want.action ||
		r.GetResolvedBy().GetUserId() != want.admin.String() || r.GetResolvedBy().GetUsername() != "admin_a" ||
		r.GetResolutionReason() != "names the cashier" || r.GetResolvedAt() == nil {
		t.Fatalf("resolved report = %v", r)
	}
	audits := e.audits(want.target)
	if len(audits) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audits))
	}
	a := audits[0]
	if a.actor == nil || *a.actor != want.admin || a.action != want.action.String() || a.targetKind != want.kind.String() ||
		a.target != want.target || a.report == nil || *a.report != want.report || a.reason != "names the cashier" {
		t.Fatalf("audit row = %+v", a)
	}
	got := e.notifyJobs(want.report)
	job := jobs.NotifyArgs{Kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_REPORT_RESOLVED, ReportID: want.report}
	if len(got) != 1 || got[0] != job {
		t.Fatalf("notify jobs = %+v, want [%+v]", got, job)
	}
}

// Resolve refusals change nothing and write no audit row.
func TestResolveRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("author_a")
	reporter := e.user("reporter_r")
	admin := e.adminUser("admin_a")
	perspReport := e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective)
	adminReport := e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, admin)
	resolved := e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE, w.store)
	ctx := context.Background()
	if _, err := e.admin(admin).Resolve(ctx, connect.NewRequest(&perspectivv1.ResolveRequest{
		ReportId: resolved.String(), Action: perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS, Reason: "fine",
	})); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	auditsBefore := e.count(`SELECT count(*) FROM moderation_audit`)

	hide := perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PERSPECTIVE
	cases := []struct {
		name   string
		report string
		action perspectivv1.ModerationAction
		reason string
		code   connect.Code
	}{
		{"bad report id", "nope", hide, "r", connect.CodeInvalidArgument},
		{"unspecified action", perspReport.String(), perspectivv1.ModerationAction_MODERATION_ACTION_UNSPECIFIED, "r", connect.CodeInvalidArgument},
		{"unknown action", perspReport.String(), perspectivv1.ModerationAction(99), "r", connect.CodeInvalidArgument},
		{"blank reason", perspReport.String(), hide, "   ", connect.CodeInvalidArgument},
		{"action on wrong kind", perspReport.String(), perspectivv1.ModerationAction_MODERATION_ACTION_SUSPEND_USER, "r", connect.CodeInvalidArgument},
		{"missing report", uuid.NewString(), hide, "r", connect.CodeNotFound},
		{"already resolved", resolved.String(), perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PLACE, "r", connect.CodeFailedPrecondition},
		{"suspend self", adminReport.String(), perspectivv1.ModerationAction_MODERATION_ACTION_SUSPEND_USER, "r", connect.CodeFailedPrecondition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := e.admin(admin).Resolve(ctx, connect.NewRequest(&perspectivv1.ResolveRequest{
				ReportId: tc.report, Action: tc.action, Reason: tc.reason,
			}))
			wantCode(t, err, tc.code)
		})
	}
	t.Cleanup(func() {
		if n := e.count(`SELECT count(*) FROM moderation_audit`); n != auditsBefore {
			t.Errorf("refused resolutions wrote audit rows: %d, want %d", n, auditsBefore)
		}
		var hidden, suspended *time.Time
		e.scan(`SELECT hidden_at FROM perspectives WHERE id = $1`, []any{w.perspective}, &hidden)
		e.scan(`SELECT suspended_at FROM users WHERE id = $1`, []any{admin}, &suspended)
		if hidden != nil || suspended != nil {
			t.Errorf("refused resolutions changed targets: hidden %v suspended %v", hidden, suspended)
		}
		if n := e.count(`SELECT count(*) FROM reports WHERE status = 'REPORT_STATUS_OPEN'`); n != 2 {
			t.Errorf("open reports = %d, want 2", n)
		}
	})
}

// Two admins resolving one report at once: exactly one wins and one audit
// row is written.
func TestResolveConcurrentOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("author_a")
	report := e.report(e.user("reporter_r"), perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective)
	admins := []uuid.UUID{e.adminUser("admin_a"), e.adminUser("admin_b")}
	errs := make(chan error, len(admins))
	for _, a := range admins {
		go func() {
			_, err := e.admin(a).Resolve(context.Background(), connect.NewRequest(&perspectivv1.ResolveRequest{
				ReportId: report.String(), Action: perspectivv1.ModerationAction_MODERATION_ACTION_HIDE_PERSPECTIVE, Reason: "r",
			}))
			errs <- err
		}()
	}
	var ok, precondition int
	for range admins {
		switch err := <-errs; {
		case err == nil:
			ok++
		case connect.CodeOf(err) == connect.CodeFailedPrecondition:
			precondition++
		default:
			t.Fatalf("Resolve: %v", err)
		}
	}
	if ok != 1 || precondition != 1 {
		t.Fatalf("ok %d, failed precondition %d; want 1 and 1", ok, precondition)
	}
	if n := len(e.audits(w.perspective)); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
}

// listAll pages through the queue with page size size and returns every
// report, failing if a page is oversized or pagination does not end.
func listAll(t *testing.T, c perspectivv1connect.ModerationServiceClient, status perspectivv1.ReportStatus, size int32) []*perspectivv1.Report {
	t.Helper()
	var out []*perspectivv1.Report
	token := ""
	for range 10 {
		res, err := c.ListReports(context.Background(), connect.NewRequest(&perspectivv1.ListReportsRequest{
			Status: status, PageToken: token, PageSize: size,
		}))
		if err != nil {
			t.Fatalf("ListReports: %v", err)
		}
		if len(res.Msg.GetReports()) > int(size) {
			t.Fatalf("page of %d, size %d", len(res.Msg.GetReports()), size)
		}
		out = append(out, res.Msg.GetReports()...)
		if token = res.Msg.GetNextPageToken(); token == "" {
			return out
		}
	}
	t.Fatal("pagination did not end")
	return nil
}

func reportIDs(rs []*perspectivv1.Report) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.GetId())
	}
	return out
}

// FR-035: the queue lists reports oldest first, filters by status, pages with
// a cursor and labels each target.
func TestListReports(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.world("author_a")
	reporter := e.user("reporter_r")
	admin := e.adminUser("admin_a")
	ctx := context.Background()
	rp := e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PERSPECTIVE, w.perspective)
	rl := e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_PLACE, w.store)
	ru := e.report(reporter, perspectivv1.ReportTargetKind_REPORT_TARGET_KIND_USER, w.author)
	c := e.admin(admin)
	if _, err := c.Resolve(ctx, connect.NewRequest(&perspectivv1.ResolveRequest{
		ReportId: rl.String(), Action: perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS, Reason: "a real shop",
	})); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	all := listAll(t, c, perspectivv1.ReportStatus_REPORT_STATUS_UNSPECIFIED, 1)
	if got, want := reportIDs(all), []string{rp.String(), rl.String(), ru.String()}; !slices.Equal(got, want) {
		t.Fatalf("all = %v, want %v", got, want)
	}
	open := listAll(t, c, perspectivv1.ReportStatus_REPORT_STATUS_OPEN, 2)
	if got, want := reportIDs(open), []string{rp.String(), ru.String()}; !slices.Equal(got, want) {
		t.Fatalf("open = %v, want %v", got, want)
	}
	resolved := listAll(t, c, perspectivv1.ReportStatus_REPORT_STATUS_RESOLVED, 20)
	if len(resolved) != 1 || resolved[0].GetId() != rl.String() {
		t.Fatalf("resolved = %v", reportIDs(resolved))
	}
	if r := resolved[0]; r.GetResolution() != perspectivv1.ModerationAction_MODERATION_ACTION_DISMISS ||
		r.GetResolvedBy().GetUsername() != "admin_a" || r.GetResolutionReason() != "a real shop" || r.GetResolvedAt() == nil {
		t.Fatalf("resolved report = %v", r)
	}
	// An open report has no resolution fields.
	if r := open[0]; r.GetResolvedBy() != nil || r.GetResolvedAt() != nil ||
		r.GetResolution() != perspectivv1.ModerationAction_MODERATION_ACTION_UNSPECIFIED {
		t.Fatalf("open report carries resolution: %v", r)
	}

	labels := map[string]string{}
	for _, r := range all {
		labels[r.GetId()] = r.GetTargetLabel()
		if r.GetReporter().GetUserId() != reporter.String() || r.GetReporter().GetUsername() != "reporter_r" ||
			r.GetReason() != perspectivv1.ReportReason_REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER ||
			r.GetDetails() != "names the cashier" || r.GetCreatedAt() == nil {
			t.Fatalf("report = %v", r)
		}
	}
	want := map[string]string{rp.String(): "Rain at the deli", rl.String(): "Corner Deli", ru.String(): "author_a"}
	if !maps.Equal(labels, want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}

	// A deleted reporter reads as [deleted].
	e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, reporter)
	if got := listAll(t, c, perspectivv1.ReportStatus_REPORT_STATUS_OPEN, 20)[0].GetReporter().GetUsername(); got != "[deleted]" {
		t.Fatalf("deleted reporter username = %q", got)
	}
}

// ListReports refuses a page token it did not mint and an unknown status.
func TestListReportsRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.admin(e.adminUser("admin_a"))
	cases := []struct {
		name string
		req  *perspectivv1.ListReportsRequest
	}{
		{"bad token", &perspectivv1.ListReportsRequest{PageToken: "%%%"}},
		{"unknown status", &perspectivv1.ListReportsRequest{Status: perspectivv1.ReportStatus(99)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := c.ListReports(context.Background(), connect.NewRequest(tc.req))
			wantCode(t, err, connect.CodeInvalidArgument)
		})
	}
}
