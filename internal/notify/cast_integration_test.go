//go:build integration

package notify_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/jobs"
)

// castLabels is the characters every case's new story declares, in order.
var castLabels = []string{
	"the woman with the foam parrot", "the clerk", "the man in the red hat",
	"the twins", "the drummer", "the dog walker",
}

// A new story's notification names the characters nobody has claimed: up to
// three in the story's order, then a count. The email reads them as it is
// delivered; the inbox reads them each time it is listed, so a later claim,
// delete or hide shows there. Neither ever reads them from the job.
func TestNewStoryNamesUnclaimedCharacters(t *testing.T) {
	t.Parallel()
	listed := []string{"the woman with the foam parrot", "the man in the red hat", "the drummer"}
	tests := []struct {
		name string
		// claims maps a character's index to the status of a claim on it.
		claims    map[int]string
		after     func(s *story, chars []uuid.UUID)
		wantMail  []string
		mailMore  string
		wantInbox []string
		inboxMore int32
	}{
		{
			name:      "three listed then a count, claimed characters left out",
			claims:    map[int]string{1: "CLAIM_STATUS_PENDING", 3: "CLAIM_STATUS_ACCEPTED"},
			wantMail:  listed,
			mailMore:  "1",
			wantInbox: listed,
			inboxMore: 1,
		},
		{
			name:     "a rejected claim leaves the character unclaimed",
			claims:   map[int]string{0: "CLAIM_STATUS_REJECTED", 1: "CLAIM_STATUS_PENDING", 3: "CLAIM_STATUS_ACCEPTED"},
			wantMail: listed, mailMore: "1",
			wantInbox: listed, inboxMore: 1,
		},
		{
			name:   "a claim made after delivery drops out of the inbox",
			claims: map[int]string{1: "CLAIM_STATUS_PENDING", 3: "CLAIM_STATUS_ACCEPTED"},
			after: func(s *story, chars []uuid.UUID) {
				s.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status)
					VALUES ($1, $2, $3, 'CLAIM_STATUS_PENDING')`, chars[4], s.dan, s.pDan)
			},
			wantMail: listed, mailMore: "1",
			wantInbox: []string{"the woman with the foam parrot", "the man in the red hat", "the dog walker"},
		},
		{
			name: "a story deleted after delivery names nobody in the inbox",
			after: func(s *story, _ []uuid.UUID) {
				s.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, s.pAnn)
			},
			wantMail: castLabels[:3], mailMore: "3",
		},
		{
			name: "a story hidden after delivery names nobody in the inbox",
			after: func(s *story, _ []uuid.UUID) {
				s.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, s.pAnn)
			},
			wantMail: castLabels[:3], mailMore: "3",
		},
		{
			name: "a story whose characters are all claimed names nobody",
			claims: map[int]string{
				0: "CLAIM_STATUS_ACCEPTED", 1: "CLAIM_STATUS_PENDING", 2: "CLAIM_STATUS_ACCEPTED",
				3: "CLAIM_STATUS_PENDING", 4: "CLAIM_STATUS_ACCEPTED", 5: "CLAIM_STATUS_PENDING",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStory(t)
			chars := s.castOnAnn(tc.claims)

			s.notifyNow(jobs.NotifyArgs{Kind: kNewInArc, ActorID: s.ann, ArcID: s.arc, PerspectiveID: s.pAnn})
			if tc.after != nil {
				tc.after(s, chars)
			}

			mails := s.emailJobs("ben@example.test")
			if len(mails) != 1 {
				t.Fatalf("ben's email jobs = %d, want 1", len(mails))
			}
			checkMailCast(t, mails[0].args.Data, tc.wantMail, tc.mailMore)

			checkInboxCast(t, s, tc.wantInbox, tc.inboxMore)
		})
	}
}

// castOnAnn gives ann's story the castLabels characters, in order, and the
// claims listed (character index to status), and returns the characters.
// Each claim comes from a different writer of the day in turn, since a
// writer holds one claim per character.
func (s *story) castOnAnn(claims map[int]string) []uuid.UUID {
	s.t.Helper()
	chars := make([]uuid.UUID, len(castLabels))
	for i, label := range castLabels {
		chars[i] = s.id(`INSERT INTO characters (perspective_id, label, position) VALUES ($1, $2, $3) RETURNING id`,
			s.pAnn, label, i+1)
	}
	claimants := []uuid.UUID{s.pBen, s.pCat, s.pDan}
	for i, status := range claims {
		s.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status, resolved_at)
			SELECT $1, author_id, id, $3::text, CASE WHEN $3::text = 'CLAIM_STATUS_PENDING' THEN NULL ELSE now() END
			FROM perspectives WHERE id = $2`, chars[i], claimants[i%len(claimants)], status)
	}
	return chars
}

// checkInboxCast checks the characters ben's one notification names when
// his inbox is listed now.
func checkInboxCast(t *testing.T, s *story, want []string, more int32) {
	t.Helper()
	resp, err := s.client(s.ben).ListInbox(context.Background(), connect.NewRequest(&perspectivv1.ListInboxRequest{}))
	if err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	ns := resp.Msg.GetNotifications()
	if len(ns) != 1 {
		t.Fatalf("ben's inbox = %d notifications, want 1", len(ns))
	}
	if got := ns[0].GetUnclaimedCharacters(); !slices.Equal(got, want) {
		t.Errorf("inbox characters = %q, want %q", got, want)
	}
	if got := ns[0].GetMoreUnclaimedCharacters(); got != more {
		t.Errorf("inbox more = %d, want %d", got, more)
	}
}

// checkMailCast checks the characters a notification email job carries.
func checkMailCast(t *testing.T, data map[string]string, want []string, more string) {
	t.Helper()
	raw, ok := data["characters"]
	if len(want) == 0 {
		if ok {
			t.Fatalf("email names characters %s, want none", raw)
		}
		return
	}
	var got []string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("email characters %q: %v", raw, err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("email characters = %q, want %q", got, want)
	}
	if data["more_characters"] != more {
		t.Errorf("email more = %q, want %q", data["more_characters"], more)
	}
}

// Only the new-story notification names characters: a claim on the same
// story names nobody.
func TestOnlyNewStoryNamesCharacters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind perspectivv1.NotificationKind
	}{
		{name: "claim received", kind: kClaimReceived},
		{name: "a new story at a followed place", kind: perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStory(t)
			s.exec(`INSERT INTO characters (perspective_id, label, position) VALUES ($1, 'the drummer', 1)`, s.pAnn)
			payload := `{"actor_id":"` + s.ann.String() + `","arc_id":"` + s.arc.String() + `","perspective_id":"` + s.pAnn.String() + `"}`
			s.exec(`INSERT INTO notifications (user_id, kind, payload) VALUES ($1, $2, $3::jsonb)`, s.ben, tc.kind.String(), payload)
			resp, err := s.client(s.ben).ListInbox(context.Background(), connect.NewRequest(&perspectivv1.ListInboxRequest{}))
			if err != nil {
				t.Fatalf("ListInbox: %v", err)
			}
			if got := resp.Msg.GetNotifications()[0].GetUnclaimedCharacters(); len(got) != 0 {
				t.Fatalf("characters = %q, want none", got)
			}
		})
	}
}
