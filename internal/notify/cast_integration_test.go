//go:build integration

package notify_test

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/jobs"
	"perspectiv/internal/notify"
)

// castLabels is the characters every case's new story declares, in order.
var castLabels = []string{
	"the woman with the foam parrot", "the clerk", "the man in the red hat",
	"the twins", "the drummer", "the dog walker",
}

// A new story's notification names the characters nobody has claimed: up to
// three in the story's order, then a count. The email reads them as each
// attempt sends it and the inbox each time it is listed, by the rule the
// claim path applies, so a claim, delete, hide, vote-out or hidden place
// made after delivery shows in both. Neither ever reads them from the job.
func TestNewStoryNamesUnclaimedCharacters(t *testing.T) {
	t.Parallel()
	listed := []string{"the woman with the foam parrot", "the man in the red hat", "the drummer"}
	tests := []struct {
		name string
		// claims maps a character's index to the status of a claim on it.
		claims    map[int]string
		after     func(s *story, chars []uuid.UUID)
		wantMail  []string
		mailMore  int32
		wantInbox []string
		inboxMore int32
	}{
		{
			name:      "three listed then a count, claimed characters left out",
			claims:    map[int]string{1: "CLAIM_STATUS_PENDING", 3: "CLAIM_STATUS_ACCEPTED"},
			wantMail:  listed,
			mailMore:  1,
			wantInbox: listed,
			inboxMore: 1,
		},
		{
			name:     "a rejected claim leaves the character unclaimed",
			claims:   map[int]string{0: "CLAIM_STATUS_REJECTED", 1: "CLAIM_STATUS_PENDING", 3: "CLAIM_STATUS_ACCEPTED"},
			wantMail: listed, mailMore: 1,
			wantInbox: listed, inboxMore: 1,
		},
		{
			name:   "a claim made after delivery drops out of the inbox and the email",
			claims: map[int]string{1: "CLAIM_STATUS_PENDING", 3: "CLAIM_STATUS_ACCEPTED"},
			after: func(s *story, chars []uuid.UUID) {
				s.exec(`INSERT INTO claims (character_id, claimant_user_id, claimant_perspective_id, status)
					VALUES ($1, $2, $3, 'CLAIM_STATUS_PENDING')`, chars[4], s.dan, s.pDan)
			},
			wantMail:  []string{"the woman with the foam parrot", "the man in the red hat", "the dog walker"},
			wantInbox: []string{"the woman with the foam parrot", "the man in the red hat", "the dog walker"},
		},
		{
			name: "a story deleted after delivery names nobody",
			after: func(s *story, _ []uuid.UUID) {
				s.exec(`UPDATE perspectives SET deleted_at = now() WHERE id = $1`, s.pAnn)
			},
		},
		{
			name: "a story hidden after delivery names nobody",
			after: func(s *story, _ []uuid.UUID) {
				s.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, s.pAnn)
			},
		},
		{
			name: "a story voted out of its lore names nobody",
			after: func(s *story, _ []uuid.UUID) {
				s.exec(`UPDATE perspectives SET lore_status = 'LORE_STATUS_REMOVED' WHERE id = $1`, s.pAnn)
			},
		},
		{
			name: "a story at a place hidden after delivery names nobody",
			after: func(s *story, _ []uuid.UUID) {
				s.exec(`UPDATE places SET hidden_at = now() WHERE id = (SELECT place_id FROM arcs WHERE id = $1)`, s.arc)
			},
		},
		{
			name: "a story by an account deleted after delivery names nobody",
			after: func(s *story, _ []uuid.UUID) {
				s.exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, s.ann)
			},
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
			if _, ok := mails[0].args.Data["characters"]; ok {
				t.Fatalf("email job carries labels %q; they are read as it sends", mails[0].args.Data["characters"])
			}
			checkMailCast(t, s, mails[0].args, tc.wantMail, tc.mailMore)

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

// checkMailCast sends the email job args again, as a retry would now, and
// checks the characters the email names.
func checkMailCast(t *testing.T, s *story, args jobs.EmailArgs, want []string, more int32) {
	t.Helper()
	rec := &recorder{}
	w := notify.NewEmailWorker(s.pool, rec, metrics.New(prometheus.NewRegistry()), zerolog.New(io.Discard), baseURL, unsub)
	if err := w.Work(context.Background(), emailJob(2, args)); err != nil {
		t.Fatalf("Work: %v", err)
	}
	text := rec.messages()[0].Text
	if len(want) == 0 {
		if strings.Contains(text, "Who might be in it") {
			t.Fatalf("email names characters, want none: %q", text)
		}
		return
	}
	quoted := make([]string, len(want))
	for i, l := range want {
		quoted[i] = "“" + l + "”"
	}
	if more > 0 {
		quoted = append(quoted, fmt.Sprintf("%d more", more))
	}
	phrase := strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	if !strings.Contains(text, "Who might be in it: "+phrase+".") {
		t.Fatalf("email %q does not name %s", text, phrase)
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
