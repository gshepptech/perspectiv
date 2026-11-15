//go:build integration

package vote_test

import (
	"context"
	"encoding/json"
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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore"
	"perspectiv/internal/testdb"
	"perspectiv/internal/vote"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// bearerAuthn treats "Bearer <uuid>" as a signed-in caller with that id; the
// real sessions belong to auth. These tests are about what vote does for a
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

// env is one test database with a place and an arc, the vote service over the
// real lore service and job enqueuer, served behind the production
// interceptor chain and router.
type env struct {
	t     *testing.T
	pool  *pgxpool.Pool
	lore  *lore.Service
	srv   *httptest.Server
	place uuid.UUID
	arc   uuid.UUID
	users int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testdb.New(t)
	log := zerolog.New(io.Discard)
	m := metrics.New(prometheus.NewRegistry())
	river, err := jobs.NewClient(pool, log, m, nil, nil)
	if err != nil {
		t.Fatalf("river client: %v", err)
	}
	enq := jobs.NewEnqueuer(river)
	loreSvc := lore.NewService(pool, enq, m)
	h := vote.NewHandler(vote.NewService(pool, loreSvc, enq))
	ics := connect.WithInterceptors(api.Interceptors(log, bearerAuthn{}, dbGate{pool}, allowAll{})...)
	mount := func(opts ...connect.HandlerOption) (string, http.Handler) {
		return h.Mount(append(opts, ics)...)
	}
	srv := httptest.NewServer(api.NewRouter(api.RouterConfig{Log: log, Handlers: []api.Mount{mount}}))
	t.Cleanup(srv.Close)
	e := &env{t: t, pool: pool, lore: loreSvc, srv: srv}
	state := e.id(`INSERT INTO places (kind, name, timezone, source)
		VALUES ('PLACE_KIND_STATE', 'Ohio', 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`)
	e.place = e.id(`INSERT INTO places (kind, name, parent_id, timezone, source)
		VALUES ('PLACE_KIND_STORE', 'Joe''s Diner', $1, 'America/New_York', 'PLACE_SOURCE_OVERTURE') RETURNING id`, state)
	e.arc = e.newArc()
	return e
}

func (e *env) newArc() uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-15', 'The night the fryer caught fire', $2)
		ON CONFLICT (place_id, local_date, hour) DO UPDATE SET title = EXCLUDED.title
		RETURNING id`, e.place, e.user())
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

func (e *env) text(query string, args ...any) string {
	e.t.Helper()
	var s *string
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&s); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	if s == nil {
		return ""
	}
	return *s
}

// user creates a verified user.
func (e *env) user() uuid.UUID {
	e.t.Helper()
	e.users++
	name := fmt.Sprintf("writer%d", e.users)
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

// published inserts a published PENDING perspective by author in the env's
// arc with chars characters.
func (e *env) published(author uuid.UUID, chars int) uuid.UUID {
	e.t.Helper()
	p := e.id(`INSERT INTO perspectives (arc_id, author_id, place_id, local_date, genre, status, lore_status, published_at)
		VALUES ($1, $2, $3, '2026-10-15', 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())
		RETURNING id`, e.arc, author, e.place)
	for i := 1; i <= chars; i++ {
		e.exec(`INSERT INTO characters (perspective_id, label, position) VALUES ($1, $2, $3)`, p, fmt.Sprintf("person %d", i), i)
	}
	return p
}

// writer is one author and their perspective in the env's arc.
type writer struct {
	user, perspective uuid.UUID
}

// star builds a lore through the lore service: a hub whose characters are
// claimed by each spoke and accepted by the hub. It returns the hub first.
func (e *env) star(size int) []writer {
	e.t.Helper()
	ctx := context.Background()
	hub := writer{user: e.user()}
	hub.perspective = e.published(hub.user, size-1)
	out := []writer{hub}
	for i := 1; i < size; i++ {
		w := writer{user: e.user()}
		w.perspective = e.published(w.user, 1)
		char := e.id(`SELECT id FROM characters WHERE perspective_id = $1 AND position = $2`, hub.perspective, i)
		claim, err := e.lore.ClaimCharacter(ctx, w.user, char)
		if err != nil {
			e.t.Fatalf("ClaimCharacter: %v", err)
		}
		if _, err := e.lore.ResolveClaim(ctx, hub.user, claim, true); err != nil {
			e.t.Fatalf("ResolveClaim: %v", err)
		}
		out = append(out, w)
	}
	return out
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

// client calls VoteService as the given user; uuid.Nil is signed out.
func (e *env) client(as uuid.UUID) perspectivv1connect.VoteServiceClient {
	var opts []connect.ClientOption
	if as != uuid.Nil {
		opts = append(opts, connect.WithInterceptors(bearer(as)))
	}
	return perspectivv1connect.NewVoteServiceClient(e.srv.Client(), e.srv.URL+"/api", opts...)
}

func (e *env) vote(as uuid.UUID, target string, on bool) (*perspectivv1.VoteResponse, error) {
	resp, err := e.client(as).Vote(context.Background(), connect.NewRequest(&perspectivv1.VoteRequest{PerspectiveId: target, On: on}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (e *env) mustVote(as, target uuid.UUID, on bool) *perspectivv1.VoteResponse {
	e.t.Helper()
	resp, err := e.vote(as, target.String(), on)
	if err != nil {
		e.t.Fatalf("Vote(%v): %v", on, err)
	}
	return resp
}

func (e *env) voteRemove(as uuid.UUID, target string) (*perspectivv1.VoteRemoveResponse, error) {
	resp, err := e.client(as).VoteRemove(context.Background(), connect.NewRequest(&perspectivv1.VoteRemoveRequest{PerspectiveId: target}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (e *env) mustVoteRemove(as, target uuid.UUID) *perspectivv1.VoteRemoveResponse {
	e.t.Helper()
	resp, err := e.voteRemove(as, target.String())
	if err != nil {
		e.t.Fatalf("VoteRemove: %v", err)
	}
	return resp
}

func (e *env) loreStatus(p uuid.UUID) string {
	e.t.Helper()
	return e.text(`SELECT lore_status FROM perspectives WHERE id = $1`, p)
}

// voteOutEvents returns the VOTE_OUT_OPENED events enqueued for notify.
func (e *env) voteOutEvents() []jobs.NotifyArgs {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT args FROM river_job WHERE kind = 'notify' ORDER BY id`)
	if err != nil {
		e.t.Fatalf("read notify jobs: %v", err)
	}
	defer rows.Close()
	var out []jobs.NotifyArgs
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			e.t.Fatalf("scan notify job: %v", err)
		}
		var a jobs.NotifyArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			e.t.Fatalf("decode notify job %s: %v", raw, err)
		}
		if a.Kind == perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED {
			out = append(out, a)
		}
	}
	if err := rows.Err(); err != nil {
		e.t.Fatalf("read notify jobs: %v", err)
	}
	return out
}

// expectDissolved checks a voted-out target's links are gone: its claims are
// rejected and it left the lore, which keeps the remaining members, and the
// arc is marked for the rank job.
func (e *env) expectDissolved(target writer, loreID uuid.UUID, remaining ...writer) {
	e.t.Helper()
	if n := e.count(`SELECT count(*) FROM claims WHERE claimant_perspective_id = $1 AND status <> 'CLAIM_STATUS_REJECTED'`, target.perspective); n != 0 {
		e.t.Fatalf("target still has %d live claims", n)
	}
	if n := e.count(`SELECT count(*) FROM lore_members WHERE perspective_id = $1`, target.perspective); n != 0 {
		e.t.Fatalf("target is still a lore member")
	}
	for _, w := range remaining {
		if s := e.loreStatus(w.perspective); s != "LORE_STATUS_IN_LORE" {
			e.t.Fatalf("remaining member lore_status = %s, want IN_LORE", s)
		}
	}
	if n := e.count(`SELECT count(*) FROM lore_members WHERE lore_id = $1`, loreID); n != len(remaining) {
		e.t.Fatalf("lore keeps %d members, want %d", n, len(remaining))
	}
	if n := e.count(`SELECT count(*) FROM arcs WHERE id = $1 AND lore_dirty_at IS NOT NULL`, e.arc); n != 1 {
		e.t.Fatalf("arc not marked lore-dirty after the vote-out")
	}
}

func codeOf(err error) connect.Code {
	if err == nil {
		return 0
	}
	return connect.CodeOf(err)
}

// An up-vote is a toggle: voting twice leaves one row, clearing removes it,
// and the response carries the perspective's total.
func TestVoteToggle(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	author, voter, second := e.user(), e.user(), e.user()
	target := e.published(author, 0)
	e.published(voter, 0)
	e.published(second, 0)
	rows := func() int {
		return e.count(`SELECT count(*) FROM votes WHERE perspective_id = $1 AND user_id = $2`, target, voter)
	}

	if got := e.mustVote(voter, target, true); got.GetUpvotes() != 1 || !got.GetOn() {
		t.Fatalf("first vote = %v, want 1 up-vote, on", got)
	}
	if got := e.mustVote(voter, target, true); got.GetUpvotes() != 1 || !got.GetOn() {
		t.Fatalf("second vote = %v, want still 1 up-vote", got)
	}
	if rows() != 1 {
		t.Fatalf("vote rows = %d, want 1", rows())
	}
	if got := e.mustVote(second, target, true); got.GetUpvotes() != 2 {
		t.Fatalf("second voter: upvotes = %d, want 2", got.GetUpvotes())
	}
	if got := e.mustVote(voter, target, false); got.GetUpvotes() != 1 || got.GetOn() {
		t.Fatalf("clear = %v, want 1 up-vote, off", got)
	}
	if rows() != 0 {
		t.Fatalf("vote rows after clearing = %d, want 0", rows())
	}
	if got := e.mustVote(voter, target, false); got.GetUpvotes() != 1 {
		t.Fatalf("clearing an absent vote: upvotes = %d, want 1", got.GetUpvotes())
	}

	// A vote changes rank inputs, so the arc is marked for the rank job; the
	// vote itself never writes lore standing or canon.
	if n := e.count(`SELECT count(*) FROM arcs WHERE id = $1 AND lore_dirty_at IS NOT NULL`, e.arc); n != 1 {
		t.Fatalf("arc not marked lore-dirty after a vote")
	}
	if n := e.count(`SELECT count(*) FROM arcs WHERE canon_lore_id IS NOT NULL`); n != 0 {
		t.Fatalf("a vote wrote canon state")
	}
}

// Only participants vote; a reader with no perspective in the arc is refused.
func TestVoteRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	author, voter := e.user(), e.user()
	target := e.published(author, 0)
	e.published(voter, 0)

	reader := e.user()
	elsewhere := e.user() // published in a different arc at the same place
	other := e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, '2026-10-16', 'The morning after', $2) RETURNING id`, e.place, elsewhere)
	e.exec(`INSERT INTO perspectives (arc_id, author_id, place_id, local_date, genre, status, lore_status, published_at)
		VALUES ($1, $2, $3, '2026-10-16', 'GENRE_DRAMA', 'PERSPECTIVE_STATUS_PUBLISHED', 'LORE_STATUS_PENDING', now())`,
		other, elsewhere, e.place)
	drafter := e.user()
	e.exec(`INSERT INTO perspectives (author_id, place_id, local_date) VALUES ($1, $2, '2026-10-15')`, drafter, e.place)
	withdrawn := e.user()
	e.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, e.published(withdrawn, 0))
	hiddenAuthor := e.user()
	e.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, e.published(hiddenAuthor, 0))
	hidden := e.published(e.user(), 0)
	e.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)
	draft := e.id(`INSERT INTO perspectives (author_id) VALUES ($1) RETURNING id`, author)
	newbie := e.unverified()
	e.published(newbie, 0)

	tests := []struct {
		name   string
		as     uuid.UUID
		target string
		want   connect.Code
	}{
		{"reader with no perspective in the arc", reader, target.String(), connect.CodePermissionDenied},
		{"author in another arc only", elsewhere, target.String(), connect.CodePermissionDenied},
		{"caller with only a draft", drafter, target.String(), connect.CodePermissionDenied},
		{"caller whose perspective is deleted", withdrawn, target.String(), connect.CodePermissionDenied},
		{"caller whose perspective is hidden", hiddenAuthor, target.String(), connect.CodePermissionDenied},
		{"own perspective", author, target.String(), connect.CodeInvalidArgument},
		{"signed out", uuid.Nil, target.String(), connect.CodeUnauthenticated},
		{"unverified email", newbie, target.String(), connect.CodeFailedPrecondition},
		{"missing perspective id", voter, "", connect.CodeInvalidArgument},
		{"malformed perspective id", voter, "nope", connect.CodeInvalidArgument},
		{"unknown perspective", voter, uuid.New().String(), connect.CodeNotFound},
		{"hidden target", voter, hidden.String(), connect.CodeNotFound},
		{"draft target", voter, draft.String(), connect.CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, on := range []bool{true, false} {
				if _, err := e.vote(tt.as, tt.target, on); codeOf(err) != tt.want {
					t.Fatalf("on=%v: code %v (%v), want %v", on, codeOf(err), err, tt.want)
				}
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM votes`); n != 0 {
		t.Fatalf("refused votes wrote %d rows", n)
	}
}

// A user the author blocked cannot up-vote the author's perspective but can
// clear a vote they already hold.
func TestVoteBlocked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	author, pest := e.user(), e.user()
	target := e.published(author, 0)
	mine := e.published(pest, 0)

	e.mustVote(pest, target, true)
	e.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, author, pest)
	if _, err := e.vote(pest, target.String(), true); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("blocked vote: code %v (%v), want PermissionDenied", codeOf(err), err)
	}
	if got := e.mustVote(pest, target, false); got.GetUpvotes() != 0 {
		t.Fatalf("clearing under a block: upvotes = %d, want 0", got.GetUpvotes())
	}
	if _, err := e.vote(pest, target.String(), true); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("blocked re-vote: code %v, want PermissionDenied", codeOf(err))
	}
	// The block is one-way.
	if got := e.mustVote(author, mine, true); got.GetUpvotes() != 1 {
		t.Fatalf("blocker's vote: upvotes = %d, want 1", got.GetUpvotes())
	}
}

// In a 4-member lore, 2 remove votes from the other 3 members set the target
// REMOVED and dissolve its links; the author's own vote and a vote from a
// member of a different lore in the same arc are rejected; the first vote
// opens the vote-out.
func TestVoteRemoveMajority(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	members := e.star(4)
	hub, target, third, fourth := members[0], members[1], members[2], members[3]
	other := e.star(2) // a different lore in the same arc
	outsider := e.user()
	e.published(outsider, 0)
	loreID := e.id(`SELECT lore_id FROM perspectives WHERE id = $1`, target.perspective)

	refused := []struct {
		name string
		as   uuid.UUID
		want connect.Code
	}{
		{"target author", target.user, connect.CodePermissionDenied},
		{"member of a different lore in the arc", other[0].user, connect.CodePermissionDenied},
		{"participant in no lore", outsider, connect.CodePermissionDenied},
		{"reader with no perspective", e.user(), connect.CodePermissionDenied},
		{"signed out", uuid.Nil, connect.CodeUnauthenticated},
	}
	for _, r := range refused {
		if _, err := e.voteRemove(r.as, target.perspective.String()); codeOf(err) != r.want {
			t.Fatalf("%s: code %v (%v), want %v", r.name, codeOf(err), err, r.want)
		}
	}
	if n := e.count(`SELECT count(*) FROM remove_votes`); n != 0 {
		t.Fatalf("refused remove votes wrote %d rows", n)
	}

	first := e.mustVoteRemove(hub.user, target.perspective)
	if first.GetRemoveVotes() != 1 || first.GetEligibleVoters() != 3 || first.GetRemoved() {
		t.Fatalf("first remove vote = %v, want 1 of 3, not removed", first)
	}
	events := e.voteOutEvents()
	if len(events) != 1 {
		t.Fatalf("vote-out events after the first vote = %d, want 1", len(events))
	}
	want := jobs.NotifyArgs{
		Kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_VOTE_OUT_OPENED, ActorID: hub.user,
		ArcID: e.arc, PerspectiveID: target.perspective, LoreID: loreID,
	}
	if events[0] != want {
		t.Fatalf("vote-out event = %+v, want %+v", events[0], want)
	}

	// Voting again is counted once and does not reopen the vote-out.
	again := e.mustVoteRemove(hub.user, target.perspective)
	if again.GetRemoveVotes() != 1 || again.GetRemoved() {
		t.Fatalf("repeat remove vote = %v, want still 1, not removed", again)
	}
	if e.loreStatus(target.perspective) != "LORE_STATUS_IN_LORE" {
		t.Fatalf("target left lore before a majority")
	}

	second := e.mustVoteRemove(third.user, target.perspective)
	if second.GetRemoveVotes() != 2 || second.GetEligibleVoters() != 3 || !second.GetRemoved() {
		t.Fatalf("second remove vote = %v, want 2 of 3, removed", second)
	}
	if n := len(e.voteOutEvents()); n != 1 {
		t.Fatalf("vote-out events = %d, want 1", n)
	}
	if s := e.loreStatus(target.perspective); s != "LORE_STATUS_REMOVED" {
		t.Fatalf("target lore_status = %s, want REMOVED", s)
	}
	e.expectDissolved(target, loreID, hub, third, fourth)
	// REMOVED is terminal: the target is out of lore and cannot be voted on
	// again, and it cannot rejoin through a new claim.
	if _, err := e.voteRemove(fourth.user, target.perspective.String()); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("vote on a REMOVED target: code %v (%v), want FailedPrecondition", codeOf(err), err)
	}
	char := e.id(`SELECT id FROM characters WHERE perspective_id = $1`, target.perspective)
	if _, err := e.lore.ClaimCharacter(context.Background(), fourth.user, char); err == nil {
		t.Fatalf("a claim on a REMOVED perspective was accepted")
	}
	// The removed perspective stays readable.
	if n := e.count(`SELECT count(*) FROM perspectives WHERE id = $1 AND deleted_at IS NULL AND hidden_at IS NULL`, target.perspective); n != 1 {
		t.Fatalf("the removed perspective is no longer readable")
	}
}

// Remove votes open only in a lore of 4 or more members, on a target that is
// IN_LORE.
func TestVoteRemovePreconditions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	small := e.star(3)
	pending := e.published(e.user(), 0)
	solo := e.published(e.user(), 0)
	e.exec(`UPDATE perspectives SET lore_status = 'LORE_STATUS_SOLO' WHERE id = $1`, solo)
	hidden := e.published(e.user(), 0)
	e.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, hidden)
	draft := e.id(`INSERT INTO perspectives (author_id) VALUES ($1) RETURNING id`, small[0].user)
	newbie := e.unverified()

	tests := []struct {
		name   string
		as     uuid.UUID
		target string
		want   connect.Code
	}{
		{"lore of 3 members", small[0].user, small[1].perspective.String(), connect.CodeFailedPrecondition},
		{"PENDING target", small[0].user, pending.String(), connect.CodeFailedPrecondition},
		{"SOLO target", small[0].user, solo.String(), connect.CodeFailedPrecondition},
		{"hidden target", small[0].user, hidden.String(), connect.CodeNotFound},
		{"draft target", small[0].user, draft.String(), connect.CodeNotFound},
		{"unknown target", small[0].user, uuid.New().String(), connect.CodeNotFound},
		{"malformed id", small[0].user, "nope", connect.CodeInvalidArgument},
		{"unverified email", newbie, small[1].perspective.String(), connect.CodeFailedPrecondition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := e.voteRemove(tt.as, tt.target); codeOf(err) != tt.want {
				t.Fatalf("code %v (%v), want %v", codeOf(err), err, tt.want)
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM remove_votes`); n != 0 {
		t.Fatalf("refused remove votes wrote %d rows", n)
	}
}

// Eligible voters are the lore's current members excluding the author, and a
// remove vote from someone who has since left the lore stops counting.
func TestVoteRemoveCountsCurrentMembers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	members := e.star(5)
	hub, target, c, d, leaver := members[0], members[1], members[2], members[3], members[4]

	if got := e.mustVoteRemove(c.user, target.perspective); got.GetRemoveVotes() != 1 || got.GetEligibleVoters() != 4 {
		t.Fatalf("first vote = %v, want 1 of 4", got)
	}
	if got := e.mustVoteRemove(leaver.user, target.perspective); got.GetRemoveVotes() != 2 || got.GetRemoved() {
		t.Fatalf("second vote = %v, want 2 of 4, not removed (2 is not a majority of 4)", got)
	}

	// The leaver is voted out of the lore by the others.
	for _, w := range []writer{hub, c, d} {
		e.mustVoteRemove(w.user, leaver.perspective)
	}
	if s := e.loreStatus(leaver.perspective); s != "LORE_STATUS_REMOVED" {
		t.Fatalf("leaver lore_status = %s, want REMOVED", s)
	}

	got := e.mustVoteRemove(c.user, target.perspective)
	if got.GetRemoveVotes() != 1 || got.GetEligibleVoters() != 3 || got.GetRemoved() {
		t.Fatalf("after the leaver left = %v, want 1 of 3 (the leaver's vote no longer counts)", got)
	}
	if got := e.mustVoteRemove(d.user, target.perspective); !got.GetRemoved() || got.GetRemoveVotes() != 2 {
		t.Fatalf("majority of the current members = %v, want 2 of 3, removed", got)
	}
}

// A member the target's author blocked cannot vote the author out.
func TestVoteRemoveBlocked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	members := e.star(4)
	target, pest := members[1], members[2]
	e.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, target.user, pest.user)
	if _, err := e.voteRemove(pest.user, target.perspective.String()); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("blocked remove vote: code %v (%v), want PermissionDenied", codeOf(err), err)
	}
	if n := e.count(`SELECT count(*) FROM remove_votes`); n != 0 {
		t.Fatalf("blocked remove vote wrote %d rows", n)
	}
}
