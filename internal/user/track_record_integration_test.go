//go:build integration

package user_test

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// as returns a copy of the env whose helpers fail t, so a parallel subtest
// can seed its own rows.
func (e *env) as(t *testing.T) *env {
	t.Helper()
	c := *e
	c.t = t
	return &c
}

// arcOn inserts a day arc at place on day.
func (e *env) arcOn(place, by uuid.UUID, day string) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO arcs (place_id, local_date, title, created_by)
		VALUES ($1, $2, 'A real title', $3) RETURNING id`, place, day, by)
}

// hiddenStore returns a store under a state a moderator hid: the store
// itself is not hidden, but hiding a place hides its whole subtree.
func (e *env) hiddenStore() uuid.UUID {
	e.t.Helper()
	store := e.store()
	e.exec(`UPDATE places SET hidden_at = now() WHERE id = (SELECT parent_id FROM places WHERE id = $1)`, store)
	return store
}

// character adds a character to the story and returns it.
func (e *env) character(story uuid.UUID) uuid.UUID {
	e.t.Helper()
	return e.id(`INSERT INTO characters (perspective_id, label, position) VALUES ($1, 'the one with the umbrella', 1) RETURNING id`, story)
}

// claim records claimant's claim, made from their story, on the character.
func (e *env) claim(character, claimant, claimantStory uuid.UUID, status string) {
	e.t.Helper()
	e.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status) VALUES ($1, $2, $3, $4)`,
		character, claimant, claimantStory, status)
}

func (e *env) upvote(story, voter uuid.UUID) {
	e.t.Helper()
	e.exec(`INSERT INTO votes (perspective_id, user_id) VALUES ($1, $2)`, story, voter)
}

// tag is a username suffix unique to the writer: the random tail of their
// uuidv7 id, whose head is a timestamp that ids made together share.
func tag(w uuid.UUID) string { return w.String()[24:] }

const (
	accepted = "CLAIM_STATUS_ACCEPTED"
	pending  = "CLAIM_STATUS_PENDING"
	rejected = "CLAIM_STATUS_REJECTED"
)

type trackRecord struct{ stories, upvotes, people, places int32 }

// The track record counts only what a reader can open. Each case seeds one
// writer's world and reads their profile signed out.
func TestGetProfileTrackRecord(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tests := []struct {
		name  string
		build func(e *env, w uuid.UUID)
		want  trackRecord
	}{
		{name: "a new writer has nothing yet", build: func(*env, uuid.UUID) {}},
		{name: "a readable story counts with its place", want: trackRecord{stories: 1, places: 1}, build: func(e *env, w uuid.UUID) {
			e.published(e.dayArc(e.store(), w), w)
		}},
		{name: "a draft is not a story", build: func(e *env, w uuid.UUID) {
			e.draft(w)
		}},
		{name: "a deleted story and its upvotes do not count", build: func(e *env, w uuid.UUID) {
			s := e.published(e.dayArc(e.store(), w), w)
			e.upvote(s, e.user("v_"+tag(w)))
			e.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, s)
		}},
		{name: "a hidden story and its upvotes do not count", build: func(e *env, w uuid.UUID) {
			s := e.published(e.dayArc(e.store(), w), w)
			e.upvote(s, e.user("v_"+tag(w)))
			e.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, s)
		}},
		{name: "a story under a hidden place does not count", build: func(e *env, w uuid.UUID) {
			s := e.published(e.dayArc(e.hiddenStore(), w), w)
			e.upvote(s, e.user("v_"+tag(w)))
		}},
		{name: "two stories at one place are one place", want: trackRecord{stories: 2, places: 1}, build: func(e *env, w uuid.UUID) {
			place := e.store()
			e.published(e.arcOn(place, w, "2026-10-01"), w)
			e.published(e.arcOn(place, w, "2026-10-02"), w)
		}},
		{name: "upvotes count as a story page counts them, a deleted voter included", want: trackRecord{stories: 1, upvotes: 3, places: 1}, build: func(e *env, w uuid.UUID) {
			sfx := tag(w)
			s := e.published(e.dayArc(e.store(), w), w)
			other := e.published(e.dayArc(e.store(), w), w)
			e.upvote(s, e.user("v1_"+sfx))
			gone := e.user("v2_" + sfx)
			e.upvote(s, gone)
			e.upvote(other, e.user("v3_"+sfx))
			e.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, other)
			e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, gone)
			e.upvote(s, e.user("v4_"+sfx))
		}},
		{name: "people are found both ways and each counts once", want: trackRecord{stories: 2, people: 2, places: 2}, build: func(e *env, w uuid.UUID) {
			sfx := tag(w)
			a, b := e.user("pa_"+sfx), e.user("pb_"+sfx)
			day1, day2 := e.dayArc(e.store(), w), e.dayArc(e.store(), w)
			mine1, mine2 := e.published(day1, w), e.published(day2, w)
			// a claimed a character in the writer's story, on both days.
			e.claim(e.character(mine1), a, e.published(day1, a), accepted)
			e.claim(e.character(mine2), a, e.published(day2, a), accepted)
			// The writer claimed a character in b's story.
			e.claim(e.character(e.published(day1, b)), w, mine1, accepted)
		}},
		{name: "pending and rejected claims find no one", want: trackRecord{stories: 1, places: 1}, build: func(e *env, w uuid.UUID) {
			sfx := tag(w)
			a, b := e.user("pa_"+sfx), e.user("pb_"+sfx)
			day := e.dayArc(e.store(), w)
			mine := e.published(day, w)
			e.claim(e.character(mine), a, e.published(day, a), pending)
			e.claim(e.character(e.published(day, b)), w, mine, rejected)
		}},
		{name: "a person whose story is unreadable is not found", want: trackRecord{stories: 1, places: 1}, build: func(e *env, w uuid.UUID) {
			sfx := tag(w)
			a, b := e.user("pa_"+sfx), e.user("pb_"+sfx)
			day := e.dayArc(e.store(), w)
			mine := e.published(day, w)
			theirs := e.published(day, a)
			e.claim(e.character(mine), a, theirs, accepted)
			e.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, theirs)
			bs := e.published(day, b)
			e.claim(e.character(bs), w, mine, accepted)
			e.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, bs)
		}},
		{name: "a deleted account is not found", want: trackRecord{stories: 1, places: 1}, build: func(e *env, w uuid.UUID) {
			a := e.user("pa_" + tag(w))
			day := e.dayArc(e.store(), w)
			e.claim(e.character(e.published(day, w)), a, e.published(day, a), accepted)
			e.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, a)
		}},
		{name: "a claim on an unreadable story of the writer finds no one", build: func(e *env, w uuid.UUID) {
			sfx := tag(w)
			a, b := e.user("pa_"+sfx), e.user("pb_"+sfx)
			hidden := e.hiddenStore()
			day := e.dayArc(hidden, w)
			mine := e.published(day, w)
			e.claim(e.character(mine), a, e.published(day, a), accepted)
			e.claim(e.character(e.published(day, b)), w, mine, accepted)
		}},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			se := e.as(t)
			name := fmt.Sprintf("tracked_%d", i)
			w := se.user(name)
			tc.build(se, w)
			res, err := se.client(uuid.Nil).GetProfile(context.Background(), connect.NewRequest(&perspectivv1.GetProfileRequest{Username: name}))
			if err != nil {
				t.Fatalf("GetProfile: %v", err)
			}
			p := res.Msg.GetProfile()
			got := trackRecord{stories: p.GetStoryCount(), upvotes: p.GetUpvotesReceived(), people: p.GetPeopleFound(), places: p.GetPlacesWritten()}
			if got != tc.want {
				t.Fatalf("track record = %+v, want %+v", got, tc.want)
			}
		})
	}
}
