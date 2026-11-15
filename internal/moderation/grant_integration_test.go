//go:build integration

package moderation_test

import (
	"context"
	"errors"
	"testing"

	"perspectiv/internal/moderation"
)

// GrantAdmin sets ROLE_ADMIN on the
// account a username names, matched without regard to case, and writes one
// audit row with no actor. Granting again changes nothing.
func TestGrantAdmin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.user("Founder_1")
	other := e.user("bystander")
	ctx := context.Background()

	if err := moderation.GrantAdmin(ctx, e.pool, "  founder_1 "); err != nil {
		t.Fatalf("GrantAdmin: %v", err)
	}
	var role, otherRole string
	e.scan(`SELECT role FROM users WHERE id = $1`, []any{id}, &role)
	e.scan(`SELECT role FROM users WHERE id = $1`, []any{other}, &otherRole)
	if role != "ROLE_ADMIN" || otherRole != "ROLE_USER" {
		t.Fatalf("roles = %s, %s; want ROLE_ADMIN, ROLE_USER", role, otherRole)
	}
	audits := e.audits(id)
	if len(audits) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audits))
	}
	a := audits[0]
	if a.actor != nil || a.action != "AUDIT_ACTION_ADMIN_GRANT" || a.targetKind != "AUDIT_TARGET_KIND_USER" ||
		a.report != nil || a.reason == "" {
		t.Fatalf("audit row = %+v", a)
	}

	// Decided in grant.go: a second grant is a no-op with no audit row.
	if err := moderation.GrantAdmin(ctx, e.pool, "FOUNDER_1"); err != nil {
		t.Fatalf("GrantAdmin again: %v", err)
	}
	if n := len(e.audits(id)); n != 1 {
		t.Fatalf("audit rows after second grant = %d, want 1", n)
	}
}

// An unknown username, a deleted account's old username or a
// blank one is an error and changes nothing.
func TestGrantAdminUnknownUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	gone := e.user("gone_user")
	e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, gone)
	ctx := context.Background()
	cases := []struct {
		name     string
		username string
		unknown  bool
	}{
		{"unknown", "nobody_here", true},
		{"deleted account", "gone_user", true},
		{"blank", "   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := moderation.GrantAdmin(ctx, e.pool, tc.username)
			if err == nil {
				t.Fatal("GrantAdmin succeeded")
			}
			if got := errors.Is(err, moderation.ErrUnknownUser); got != tc.unknown {
				t.Fatalf("errors.Is(ErrUnknownUser) = %v for %v", got, err)
			}
		})
	}
	t.Cleanup(func() {
		if n := e.count(`SELECT count(*) FROM users WHERE role = 'ROLE_ADMIN'`); n != 0 {
			t.Errorf("admins = %d, want 0", n)
		}
		if n := e.count(`SELECT count(*) FROM moderation_audit`); n != 0 {
			t.Errorf("audit rows = %d, want 0", n)
		}
	})
}

// A freshly granted admin can open the queue at once, because the
// service reads the stored role.
func TestGrantedAdminCanOpenQueue(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.user("first_admin")
	if err := moderation.GrantAdmin(context.Background(), e.pool, "first_admin"); err != nil {
		t.Fatalf("GrantAdmin: %v", err)
	}
	if _, _, err := e.svc.ListReports(context.Background(), id, moderation.ReportFilter{}, pageOf(20)); err != nil {
		t.Fatalf("ListReports after grant: %v", err)
	}
}
