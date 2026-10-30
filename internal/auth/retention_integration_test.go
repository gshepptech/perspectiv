//go:build integration

package auth_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"perspectiv/internal/auth"
	"perspectiv/internal/auth/dbq"
)

// The retention pass deletes each kind of security record only once it is
// older than its retention, and leaves a live session's tokens alone.
func TestRetentionPurge(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	_, liveCookie := e.mustLogin("Writer_One")
	now := time.Now()
	old := func(d time.Duration) time.Time { return now.Add(-d - time.Hour) }
	fresh := func(d time.Duration) time.Time { return now.Add(-d + time.Hour) }

	// A dead session past retention, and one inside it.
	e.exec(`INSERT INTO sessions (user_id, expires_at, revoked_at) VALUES ($1, $2, $2), ($1, $3, $3)`,
		id, old(auth.SessionRetention), fresh(auth.SessionRetention))
	e.exec(`INSERT INTO refresh_tokens (session_id, token_hash, expires_at)
		SELECT id, uuid_send(gen_random_uuid()), expires_at FROM sessions WHERE revoked_at IS NOT NULL`)
	e.exec(`INSERT INTO email_tokens (user_id, purpose, token_hash, expires_at, used_at) VALUES
		($1, 'password_reset', uuid_send(gen_random_uuid()), $2, NULL),
		($1, 'password_reset', uuid_send(gen_random_uuid()), now() + interval '1 hour', $2),
		($1, 'password_reset', uuid_send(gen_random_uuid()), $3, NULL)`,
		id, old(auth.EmailTokenRetention), fresh(auth.EmailTokenRetention))
	e.exec(`INSERT INTO login_attempts (user_id, succeeded, attempted_at) VALUES ($1, false, $2), ($1, false, $3)`,
		id, old(auth.LoginAttemptRetention), fresh(auth.LoginAttemptRetention))
	e.exec(`INSERT INTO rate_limit_counters (action, subject, window_start, count) VALUES
		('login', 'ip:192.0.2.1', $1, 1), ('login', 'ip:192.0.2.1', $2, 1)`,
		old(auth.RateWindowRetention), fresh(auth.RateWindowRetention))

	before := map[string]int{}
	tables := []string{"sessions", "refresh_tokens", "email_tokens", "login_attempts", "rate_limit_counters"}
	for _, tb := range tables {
		before[tb] = e.count(`SELECT count(*) FROM ` + tb)
	}
	if err := auth.Purge(context.Background(), dbq.New(e.pool), now, zerolog.New(io.Discard)); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	// One old row each, plus the old session's refresh token, and both the
	// expired and the long-spent email link.
	want := map[string]int{"sessions": 1, "refresh_tokens": 1, "email_tokens": 2, "login_attempts": 1, "rate_limit_counters": 1}
	for _, tb := range tables {
		if gone := before[tb] - e.count(`SELECT count(*) FROM `+tb); gone != want[tb] {
			t.Errorf("%s: deleted %d, want %d", tb, gone, want[tb])
		}
	}
	// The signed-in session still refreshes.
	if _, _, err := e.refresh(liveCookie, ""); err != nil {
		t.Fatalf("refresh after purge: %v", err)
	}
	// A second pass finds nothing more.
	if err := auth.Purge(context.Background(), dbq.New(e.pool), now, zerolog.New(io.Discard)); err != nil {
		t.Fatalf("second Purge: %v", err)
	}
}
