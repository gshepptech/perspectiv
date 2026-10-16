package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"perspectiv/internal/api"
	"perspectiv/internal/auth/dbq"
)

// LimitPasswordReset is the action the service counts reset emails against,
// per account, on top of the route limits the interceptor applies.
const LimitPasswordReset = "password_reset"

// Window lengths in seconds.
const (
	minute int32 = 60
	hour         = 60 * minute
)

// rateRule is the most hits one subject may make on an action in one fixed
// window.
type rateRule struct {
	perUser    int32
	perIP      int32
	windowSecs int32
}

// rateRules are the rate limits, per action. A signed-in caller counts
// against both its user and its IP limit; an anonymous caller only against
// the IP limit. The IP limits are higher where many people may share one
// address (a household or office behind NAT) and the user limit is the real
// guard; sign up and login have no user yet, so their two limits are equal.
//
//	signup          10 per hour      per IP
//	login           30 per 15 min    per IP (the account lockout is separate)
//	publish         10 per hour      per user, 30 per IP
//	claim           30 per hour      per user, 90 per IP
//	vote            60 per hour      per user, 180 per IP
//	react          120 per hour      per user, 360 per IP
//	report          10 per hour      per user, 30 per IP
//	password_reset   3 reset emails per hour per account
var rateRules = map[string]rateRule{
	api.LimitSignup:    {perUser: 10, perIP: 10, windowSecs: hour},
	api.LimitLogin:     {perUser: 30, perIP: 30, windowSecs: 15 * minute},
	api.LimitPublish:   {perUser: 10, perIP: 30, windowSecs: hour},
	api.LimitClaim:     {perUser: 30, perIP: 90, windowSecs: hour},
	api.LimitVote:      {perUser: 60, perIP: 180, windowSecs: hour},
	api.LimitReact:     {perUser: 120, perIP: 360, windowSecs: hour},
	api.LimitReport:    {perUser: 10, perIP: 30, windowSecs: hour},
	LimitPasswordReset: {perUser: 3, perIP: 10, windowSecs: hour},
}

// Limiter counts hits in fixed windows in the rate_limit_counters table.
// It holds no counts in memory, so limits survive a restart and are shared
// by every replica.
type Limiter struct {
	q *dbq.Queries
}

// NewLimiter returns a limiter over pool.
func NewLimiter(pool *pgxpool.Pool) *Limiter {
	return &Limiter{q: dbq.New(pool)}
}

// Allow counts one hit of action by subject ("user:<uuid>" or "ip:<addr>")
// and reports whether it is within the limit. The hit is counted even when
// it is refused, so hammering a limit keeps it shut until the window ends.
// An unknown action or subject is an error, which the caller treats as a
// refusal.
func (l *Limiter) Allow(ctx context.Context, action, subject string) (bool, error) {
	rule, ok := rateRules[action]
	if !ok {
		return false, fmt.Errorf("rate limit: unknown action %q", action)
	}
	kind, id, _ := strings.Cut(subject, ":")
	var limit int32
	switch {
	case id == "":
		return false, fmt.Errorf("rate limit: empty subject %q", subject)
	case kind == "user":
		limit = rule.perUser
	case kind == "ip":
		limit = rule.perIP
	default:
		return false, fmt.Errorf("rate limit: unknown subject kind %q", subject)
	}
	hit, err := l.q.HitRateLimit(ctx, dbq.HitRateLimitParams{
		Action: action, Subject: subject, WindowSeconds: rule.windowSecs,
	})
	if err != nil {
		return false, fmt.Errorf("rate limit %s %s: %w", action, subject, err)
	}
	// Earlier windows of this action and subject are spent; deleting them here
	// keeps the table to about one row per active subject without a sweeper.
	if err := l.q.DeleteStaleRateWindows(ctx, dbq.DeleteStaleRateWindowsParams{
		Action: action, Subject: subject, CurrentWindow: hit.WindowStart,
	}); err != nil {
		return false, fmt.Errorf("rate limit %s %s: prune: %w", action, subject, err)
	}
	return hit.Count <= limit, nil
}
