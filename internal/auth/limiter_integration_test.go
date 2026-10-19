//go:build integration

package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"perspectiv/internal/api"
	"perspectiv/internal/auth"
	"perspectiv/internal/testdb"
)

// FR-039, NFR-002, GI-006: each action allows its documented number of hits
// per subject in a window, counted in Postgres, user and IP separately.
func TestLimiterThresholds(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	lim := auth.NewLimiter(pool)
	ctx := context.Background()
	tests := []struct {
		action  string
		perUser int
		perIP   int
	}{
		{action: api.LimitSignup, perUser: 10, perIP: 10},
		{action: api.LimitLogin, perUser: 30, perIP: 30},
		{action: api.LimitPublish, perUser: 10, perIP: 30},
		{action: api.LimitClaim, perUser: 30, perIP: 90},
		{action: api.LimitVote, perUser: 60, perIP: 180},
		{action: api.LimitReact, perUser: 120, perIP: 360},
		{action: api.LimitReport, perUser: 10, perIP: 30},
		{action: auth.LimitPasswordReset, perUser: 3, perIP: 10},
	}
	for _, tc := range tests {
		t.Run(tc.action, func(t *testing.T) {
			t.Parallel()
			for _, sub := range []struct {
				subject string
				limit   int
			}{
				{subject: "user:" + uuid.NewString(), limit: tc.perUser},
				{subject: "ip:203.0.113." + uuid.NewString()[:3], limit: tc.perIP},
			} {
				for i := range sub.limit {
					ok, err := lim.Allow(ctx, tc.action, sub.subject)
					if err != nil || !ok {
						t.Fatalf("%s hit %d of %d: ok=%v err=%v", sub.subject, i+1, sub.limit, ok, err)
					}
				}
				ok, err := lim.Allow(ctx, tc.action, sub.subject)
				if err != nil || ok {
					t.Fatalf("%s hit %d: ok=%v err=%v, want refused", sub.subject, sub.limit+1, ok, err)
				}
			}
		})
	}
}

// The count survives a new limiter (a restart) and resets in the next window,
// where the stale window's row is deleted.
func TestLimiterPersistsAndRollsOver(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	ctx := context.Background()
	const subject = "user:00000000-0000-0000-0000-000000000001"
	for range 10 {
		if ok, err := auth.NewLimiter(pool).Allow(ctx, api.LimitPublish, subject); err != nil || !ok {
			t.Fatalf("Allow: ok=%v err=%v", ok, err)
		}
	}
	if ok, err := auth.NewLimiter(pool).Allow(ctx, api.LimitPublish, subject); err != nil || ok {
		t.Fatalf("11th publish in an hour after a restart: ok=%v err=%v, want refused", ok, err)
	}
	// Another subject and another action are counted separately.
	if ok, err := auth.NewLimiter(pool).Allow(ctx, api.LimitPublish, "user:00000000-0000-0000-0000-000000000002"); err != nil || !ok {
		t.Fatalf("other subject: ok=%v err=%v", ok, err)
	}
	if ok, err := auth.NewLimiter(pool).Allow(ctx, api.LimitReport, subject); err != nil || !ok {
		t.Fatalf("other action: ok=%v err=%v", ok, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE rate_limit_counters SET window_start = window_start - interval '1 hour'
		WHERE action = 'publish' AND subject = $1`, subject); err != nil {
		t.Fatalf("age window: %v", err)
	}
	if ok, err := auth.NewLimiter(pool).Allow(ctx, api.LimitPublish, subject); err != nil || !ok {
		t.Fatalf("first publish in the next window: ok=%v err=%v", ok, err)
	}
	var rows int
	var oldest time.Time
	if err := pool.QueryRow(ctx, `SELECT count(*), min(window_start) FROM rate_limit_counters
		WHERE action = 'publish' AND subject = $1`, subject).Scan(&rows, &oldest); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 1 || time.Since(oldest) > time.Hour {
		t.Fatalf("rows = %d oldest = %v; want only the current window kept", rows, oldest)
	}
}

func TestLimiterRefusesUnknownInput(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	lim := auth.NewLimiter(pool)
	tests := []struct {
		name, action, subject string
	}{
		{name: "unknown action", action: "dance", subject: "ip:203.0.113.1"},
		{name: "unknown subject kind", action: api.LimitVote, subject: "org:1"},
		{name: "empty subject", action: api.LimitVote, subject: "user:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if ok, err := lim.Allow(context.Background(), tc.action, tc.subject); err == nil || ok {
				t.Fatalf("Allow(%q, %q) = %v, %v; want an error", tc.action, tc.subject, ok, err)
			}
		})
	}
	pool.Close()
	if ok, err := lim.Allow(context.Background(), api.LimitVote, "ip:203.0.113.1"); err == nil || ok {
		t.Fatalf("Allow on a closed pool = %v, %v; want an error", ok, err)
	}
}

// The gate reads verification and suspension from Postgres; an unknown or
// deleted user is unverified, and suspension wins over verification.
func TestGate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	id := e.signUp("Writer_One")
	check := func(want api.Standing) {
		t.Helper()
		got, err := e.svc.WriteStanding(ctx, id)
		if err != nil || got != want {
			t.Fatalf("WriteStanding = %v, %v; want %v", got, err, want)
		}
	}
	check(api.StandingUnverified)
	e.exec(`UPDATE users SET suspended_at = now() WHERE id = $1`, id)
	check(api.StandingSuspended)
	e.exec(`UPDATE users SET email_verified_at = now(), suspended_at = NULL WHERE id = $1`, id)
	check(api.StandingWriter)
	e.exec(`UPDATE users SET suspended_at = now() WHERE id = $1`, id)
	check(api.StandingSuspended)
	e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, id)
	check(api.StandingUnverified)
	if got, err := e.svc.WriteStanding(ctx, uuid.New()); err != nil || got != api.StandingUnverified {
		t.Fatalf("unknown user: %v, %v", got, err)
	}
	e.pool.Close()
	if _, err := e.svc.WriteStanding(ctx, id); err == nil {
		t.Fatal("WriteStanding on a closed pool did not fail")
	}
}
