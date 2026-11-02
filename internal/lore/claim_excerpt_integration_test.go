//go:build integration

package lore_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// An incoming claim quotes the opening of the claimant's own story, so the
// author can judge it without leaving the Requests page, but only while
// anyone may read that story. The claimant sees their own claims the same
// way.
func TestListClaimsQuotesTheClaimantsStory(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("The power went out halfway through open mic. ", 8)
	tests := []struct {
		name  string
		parts []string
		// after runs once the claim is made.
		after func(w *world, claimant author)
		want  string
	}{
		{
			name:  "a short opening is quoted whole, its whitespace folded",
			parts: []string{"  I was the one\n\nin the yellow   raincoat.  ", "Part two is never quoted."},
			want:  "I was the one in the yellow raincoat.",
		},
		{
			name:  "a long opening is cut at a word and marked",
			parts: []string{long},
			want:  "The power went out halfway through open mic. The power went out halfway through open mic. The power went out halfway through open mic. The power went out halfway through open mic. The power went out halfway through open mic. The power went…",
		},
		{
			name:  "a story with no text quotes nothing",
			parts: nil,
			want:  "",
		},
		{
			name:  "a deleted story quotes nothing",
			parts: []string{"I was there."},
			after: func(w *world, c author) {
				w.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, c.perspective)
			},
		},
		{
			name:  "a hidden story quotes nothing",
			parts: []string{"I was there."},
			after: func(w *world, c author) {
				w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, c.perspective)
			},
		},
		{
			name:  "a story at a hidden place quotes nothing",
			parts: []string{"I was there."},
			after: func(w *world, _ author) {
				w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, w.place)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			alice, bob := w.author("alice"), w.author("bob")
			for i, body := range tc.parts {
				w.exec(`INSERT INTO perspective_parts (perspective_id, position, body) VALUES ($1, $2, $3)`, bob.perspective, i+1, body)
			}
			w.mustClaim(bob.user, alice.character)
			if tc.after != nil {
				tc.after(w, bob)
			}
			checkExcerpt(t, w, alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING, tc.want)
			checkExcerpt(t, w, bob.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_OUTGOING, tc.want)
		})
	}
}

// checkExcerpt lists caller's one claim in dir and checks the excerpt it
// quotes.
func checkExcerpt(t *testing.T, w *world, caller uuid.UUID, dir perspectivv1.ClaimDirection, want string) {
	t.Helper()
	resp, err := w.listClaims(caller, dir, perspectivv1.ClaimStatus_CLAIM_STATUS_UNSPECIFIED, "", 0)
	if err != nil {
		t.Fatalf("ListClaims %v: %v", dir, err)
	}
	if len(resp.GetClaims()) != 1 {
		t.Fatalf("ListClaims %v = %d claims, want 1", dir, len(resp.GetClaims()))
	}
	if got := resp.GetClaims()[0].GetClaimantExcerpt(); got != want {
		t.Errorf("%v excerpt = %q, want %q", dir, got, want)
	}
}

// Each claim says where and when the story it is about is set, so the
// Requests page names that story rather than linking a bare "Your
// perspective". A day's story has no hour.
func TestListClaimsNameTheStory(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	alice, bob := w.author("alice"), w.author("bob")
	w.mustClaim(bob.user, alice.character)
	tests := []struct {
		name   string
		caller uuid.UUID
		dir    perspectivv1.ClaimDirection
	}{
		{name: "on the author's characters", caller: alice.user, dir: perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING},
		{name: "in the claimant's own claims", caller: bob.user, dir: perspectivv1.ClaimDirection_CLAIM_DIRECTION_OUTGOING},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, err := w.listClaims(tc.caller, tc.dir, perspectivv1.ClaimStatus_CLAIM_STATUS_UNSPECIFIED, "", 0)
			if err != nil {
				t.Fatalf("ListClaims: %v", err)
			}
			if len(resp.GetClaims()) != 1 {
				t.Fatalf("ListClaims = %d claims, want 1", len(resp.GetClaims()))
			}
			c := resp.GetClaims()[0]
			if c.GetPlaceName() != "Ohio" || c.GetLocalDate() != "2026-10-01" || c.Hour != nil {
				t.Fatalf("claim at %q on %q hour %v, want Ohio on 2026-10-01 with no hour", c.GetPlaceName(), c.GetLocalDate(), c.Hour)
			}
		})
	}
}

// A place hidden by moderation is named nowhere, so a claim on a story
// there names neither the place nor the day, on either side of the claim.
func TestListClaimsDoNotNameAHiddenPlace(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	alice, bob := w.author("alice"), w.author("bob")
	w.mustClaim(bob.user, alice.character)
	w.exec(`UPDATE places SET hidden_at = now() WHERE id = $1`, w.place)
	for _, tc := range []struct {
		caller uuid.UUID
		dir    perspectivv1.ClaimDirection
	}{
		{alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING},
		{bob.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_OUTGOING},
	} {
		resp, err := w.listClaims(tc.caller, tc.dir, perspectivv1.ClaimStatus_CLAIM_STATUS_UNSPECIFIED, "", 0)
		if err != nil {
			t.Fatalf("ListClaims %v: %v", tc.dir, err)
		}
		if len(resp.GetClaims()) != 1 {
			t.Fatalf("ListClaims %v = %d claims, want 1", tc.dir, len(resp.GetClaims()))
		}
		if c := resp.GetClaims()[0]; c.GetPlaceName() != "" || c.GetLocalDate() != "" || c.Hour != nil {
			t.Fatalf("%v: claim names %q on %q hour %v under a hidden place", tc.dir, c.GetPlaceName(), c.GetLocalDate(), c.Hour)
		}
	}
}
