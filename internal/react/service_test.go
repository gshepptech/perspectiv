//go:build integration

package react_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/react"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// bearerAuthn treats "Bearer <uuid>" as a signed-in caller with that id; the
// real sessions belong to auth. These tests are about what react does for a
// given caller behind the production interceptor chain.
type bearerAuthn struct{}

func (bearerAuthn) Authenticate(_ context.Context, h http.Header) (apiutil.Caller, bool, error) {
	v := h.Get("Authorization")
	if v == "" {
		return apiutil.Caller{}, false, nil
	}
	id, err := uuid.Parse(strings.TrimPrefix(v, "Bearer "))
	if err != nil {
		return apiutil.Caller{}, false, apiutil.Unauthenticated("bad token")
	}
	return apiutil.Caller{UserID: id, Role: perspectivv1.Role_ROLE_USER, SessionID: uuid.New()}, true, nil
}

// dbGate reads the verified flag from the users table, as auth's gate does.
type dbGate struct{ pool *pgxpool.Pool }

func (g dbGate) WriteStanding(ctx context.Context, id uuid.UUID) (api.Standing, error) {
	var verified, suspended bool
	err := g.pool.QueryRow(ctx, `SELECT email_verified_at IS NOT NULL, suspended_at IS NOT NULL FROM users WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&verified, &suspended)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return api.StandingUnverified, nil
	case err != nil:
		return api.StandingUnverified, err
	case suspended:
		return api.StandingSuspended, nil
	case !verified:
		return api.StandingUnverified, nil
	}
	return api.StandingWriter, nil
}

type allowAll struct{}

func (allowAll) Allow(context.Context, string, string) (bool, error) { return true, nil }

type env struct {
	t     *testing.T
	pool  *pgxpool.Pool
	srv   *httptest.Server
	place uuid.UUID
	arc   uuid.UUID
	users int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testdb.New(t)
	log := zerolog.New(io.Discard)
	h := react.NewHandler(react.NewService(pool))
	ics := connect.WithInterceptors(api.Interceptors(log, bearerAuthn{}, dbGate{pool}, allowAll{})...)
	mount := func(opts ...connect.HandlerOption) (string, http.Handler) {
		return h.Mount(append(opts, ics)...)
	}
	srv := httptest.NewServer(api.NewRouter(api.RouterConfig{Log: log, Handlers: []api.Mount{mount}}))
	t.Cleanup(srv.Close)
	e := &env{t: t, pool: pool, srv: srv}
	state := e.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', 'Ohio', 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`)
	e.place = e.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_STORE', 'Joe''s Diner', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, state)
	e.arc = e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-15', 'The night the fryer caught fire', $2) RETURNING id`, e.place, e.user())
	return e
}

func (e *env) id(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&id); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return id
}

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), query, args...); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}

// user creates a verified user.
func (e *env) user() uuid.UUID {
	e.t.Helper()
	e.users++
	name := fmt.Sprintf("reader%d", e.users)
	return e.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at, email_verified_at)
		VALUES ($1, 'x', $2, now(), now()) RETURNING id`, name+"@example.test", name)
}

// unverified creates a user who has not verified their email.
func (e *env) unverified() uuid.UUID {
	e.t.Helper()
	e.users++
	name := fmt.Sprintf("newbie%d", e.users)
	return e.id(`INSERT INTO users (email, password_hash, username, age_confirmed_at)
		VALUES ($1, 'x', $2, now()) RETURNING id`, name+"@example.test", name)
}

// published inserts a published perspective by author in the env's arc.
func (e *env) published(author uuid.UUID) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO perspectives (arc_id, author_id, place_id, local_date, genre, status, lore_status, published_at)
		VALUES ($1, $2, $3, '2026-10-15', 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())
		RETURNING id`, e.arc, author, e.place)
}

// draft inserts an unpublished draft by author.
func (e *env) draft(author uuid.UUID) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO perspectives (author_id) VALUES ($1) RETURNING id`, author)
}

type bearer uuid.UUID

func (b bearer) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+uuid.UUID(b).String())
		return next(ctx, req)
	}
}

func (b bearer) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (b bearer) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// react calls ReactService.React as the given user; uuid.Nil is signed out.
func (e *env) react(as, perspective uuid.UUID, emoji perspectivv1.Emoji, on bool) (*perspectivv1.ReactResponse, error) {
	var opts []connect.ClientOption
	if as != uuid.Nil {
		opts = append(opts, connect.WithInterceptors(bearer(as)))
	}
	c := perspectivv1connect.NewReactServiceClient(e.srv.Client(), e.srv.URL+"/api", opts...)
	resp, err := c.React(context.Background(), connect.NewRequest(&perspectivv1.ReactRequest{
		PerspectiveId: perspective.String(), Emoji: emoji, On: on,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (e *env) mustReact(as, perspective uuid.UUID, emoji perspectivv1.Emoji, on bool) *perspectivv1.ReactResponse {
	e.t.Helper()
	resp, err := e.react(as, perspective, emoji, on)
	if err != nil {
		e.t.Fatalf("React(%v, %v): %v", emoji, on, err)
	}
	return resp
}

const (
	heart = perspectivv1.Emoji_EMOJI_HEART
	fire  = perspectivv1.Emoji_EMOJI_FIRE
)

var allEmoji = []perspectivv1.Emoji{
	perspectivv1.Emoji_EMOJI_HEART, perspectivv1.Emoji_EMOJI_LAUGH, perspectivv1.Emoji_EMOJI_WOW,
	perspectivv1.Emoji_EMOJI_SAD, perspectivv1.Emoji_EMOJI_FIRE, perspectivv1.Emoji_EMOJI_CLAP,
}

// counts flattens a response to emoji -> (count, mine) and checks it lists
// every emoji of the fixed set once, in enum order.
func counts(t *testing.T, resp *perspectivv1.ReactResponse) map[perspectivv1.Emoji][2]int {
	t.Helper()
	got := resp.GetCounts()
	if len(got) != len(allEmoji) {
		t.Fatalf("got %d counts, want one per emoji (%d): %v", len(got), len(allEmoji), got)
	}
	out := make(map[perspectivv1.Emoji][2]int, len(got))
	for i, c := range got {
		if c.GetEmoji() != allEmoji[i] {
			t.Fatalf("counts[%d] = %v, want %v (enum order)", i, c.GetEmoji(), allEmoji[i])
		}
		mine := 0
		if c.GetMine() {
			mine = 1
		}
		out[c.GetEmoji()] = [2]int{int(c.GetCount()), mine}
	}
	return out
}

func codeOf(err error) connect.Code {
	if err == nil {
		return 0
	}
	return connect.CodeOf(err)
}

// A reader with no perspective in the arc can react, and counts come back
// per emoji with the caller's own reacts marked.
func TestReactByReader(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	target := e.published(e.user())
	reader, other := e.user(), e.user()

	e.mustReact(other, target, heart, true)
	got := counts(t, e.mustReact(reader, target, heart, true))
	if got[heart] != [2]int{2, 1} {
		t.Fatalf("heart = %v, want 2 reacts, mine", got[heart])
	}
	for _, em := range allEmoji[1:] {
		if got[em] != [2]int{0, 0} {
			t.Fatalf("%v = %v, want none", em, got[em])
		}
	}
	// The other user sees the same total without the reader's mark.
	got = counts(t, e.mustReact(other, target, fire, true))
	if got[heart] != [2]int{2, 1} || got[fire] != [2]int{1, 1} {
		t.Fatalf("other's view = %v", got)
	}
}

// A user holds up to one of each of the 6 emoji and never two of the same;
// clearing removes only that emoji.
func TestReactToggleSixDistinct(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	author := e.user()
	target := e.published(author)
	u := e.user()

	for _, em := range allEmoji {
		e.mustReact(u, target, em, true)
		e.mustReact(u, target, em, true) // a repeat is a no-op
	}
	got := counts(t, e.mustReact(u, target, heart, true))
	for _, em := range allEmoji {
		if got[em] != [2]int{1, 1} {
			t.Fatalf("%v = %v, want exactly one, mine", em, got[em])
		}
	}
	if n := e.count(`SELECT count(*) FROM reacts WHERE perspective_id = $1 AND user_id = $2`, target, u); n != 6 {
		t.Fatalf("react rows = %d, want 6", n)
	}

	got = counts(t, e.mustReact(u, target, heart, false))
	if got[heart] != [2]int{0, 0} || got[fire] != [2]int{1, 1} {
		t.Fatalf("after clearing heart: %v", got)
	}
	// Clearing what is not held changes nothing.
	got = counts(t, e.mustReact(u, target, heart, false))
	if got[heart] != [2]int{0, 0} {
		t.Fatalf("after clearing again: %v", got)
	}
	if n := e.count(`SELECT count(*) FROM reacts WHERE perspective_id = $1 AND user_id = $2`, target, u); n != 5 {
		t.Fatalf("react rows = %d, want 5", n)
	}

	// Any signed-in user may react, the author included.
	got = counts(t, e.mustReact(author, target, fire, true))
	if got[fire] != [2]int{2, 1} {
		t.Fatalf("author's fire = %v", got[fire])
	}
}

// A blocked user cannot add a react to the blocker's perspective but can
// clear one they already hold; anyone else is unaffected.
func TestReactBlocked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	author := e.user()
	target := e.published(author)
	pest, bystander := e.user(), e.user()

	e.mustReact(pest, target, heart, true)
	e.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, author, pest)

	if _, err := e.react(pest, target, fire, true); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("blocked react: code %v (%v), want PermissionDenied", codeOf(err), err)
	}
	got := counts(t, e.mustReact(pest, target, heart, false))
	if got[heart] != [2]int{0, 0} || got[fire] != [2]int{0, 0} {
		t.Fatalf("after blocked user cleared: %v", got)
	}
	e.mustReact(bystander, target, fire, true)

	// The block is one-way: the author can still react to the pest's work.
	e.mustReact(author, e.published(pest), heart, true)
}

func TestReactRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	author := e.user()
	target := e.published(author)
	hidden := e.published(e.user())
	e.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)
	deleted := e.published(e.user())
	e.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, deleted)
	draft := e.draft(author)
	u := e.user()
	newbie := e.unverified()

	tests := []struct {
		name   string
		as     uuid.UUID
		target string
		emoji  perspectivv1.Emoji
		want   connect.Code
	}{
		{"signed out", uuid.Nil, target.String(), heart, connect.CodeUnauthenticated},
		{"unverified email", newbie, target.String(), heart, connect.CodeFailedPrecondition},
		{"unspecified emoji", u, target.String(), perspectivv1.Emoji_EMOJI_UNSPECIFIED, connect.CodeInvalidArgument},
		{"emoji outside the enum", u, target.String(), perspectivv1.Emoji(7), connect.CodeInvalidArgument},
		{"missing perspective id", u, "", heart, connect.CodeInvalidArgument},
		{"malformed perspective id", u, "nope", heart, connect.CodeInvalidArgument},
		{"unknown perspective", u, uuid.New().String(), heart, connect.CodeNotFound},
		{"hidden perspective", u, hidden.String(), heart, connect.CodeNotFound},
		{"deleted perspective", u, deleted.String(), heart, connect.CodeNotFound},
		{"draft", u, draft.String(), heart, connect.CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var opts []connect.ClientOption
			if tt.as != uuid.Nil {
				opts = append(opts, connect.WithInterceptors(bearer(tt.as)))
			}
			c := perspectivv1connect.NewReactServiceClient(e.srv.Client(), e.srv.URL+"/api", opts...)
			_, err := c.React(context.Background(), connect.NewRequest(&perspectivv1.ReactRequest{
				PerspectiveId: tt.target, Emoji: tt.emoji, On: true,
			}))
			if codeOf(err) != tt.want {
				t.Fatalf("code %v (%v), want %v", codeOf(err), err, tt.want)
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM reacts`); n != 0 {
		t.Fatalf("refused reacts wrote %d rows", n)
	}
}
