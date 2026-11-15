package notify

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/notify/dbq"
)

// listedCharacters is how many unclaimed characters a new-story notification
// names; the rest are counted ("and 2 more").
const listedCharacters = 3

// dataPerspectiveID is the email data key naming a new story, whose
// unclaimed characters the email worker reads as it sends.
const dataPerspectiveID = "perspective_id"

// cast is the unclaimed characters a notification names: up to
// listedCharacters labels, in the story's order, and how many more.
type cast struct {
	Labels []string
	More   int32
}

// unclaimedCast reads, for each story in ids that anyone may still read and
// claim from, the characters nobody has claimed yet: the rule
// ListOpenCharacters and the claim path apply. A story that is deleted,
// hidden, at or below a hidden place, voted out of its lore, by a deleted
// account, or has every character claimed is absent from the map.
func unclaimedCast(ctx context.Context, q *dbq.Queries, ids []uuid.UUID) (map[uuid.UUID]cast, error) {
	out := map[uuid.UUID]cast{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.UnclaimedCharacters(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("unclaimed characters: %w", err)
	}
	for _, r := range rows {
		c := out[r.PerspectiveID]
		if len(c.Labels) < listedCharacters {
			c.Labels = append(c.Labels, r.Label)
		} else {
			c.More++
		}
		out[r.PerspectiveID] = c
	}
	return out, nil
}

// withCast names, on each new-story notification in ns whose arc is still
// shown, the story's unclaimed characters as they stand now.
func withCast(ctx context.Context, q *dbq.Queries, ns []Notification) error {
	var ids []uuid.UUID
	for _, n := range ns {
		if id, ok := castSubject(n); ok {
			ids = append(ids, id)
		}
	}
	casts, err := unclaimedCast(ctx, q, ids)
	if err != nil {
		return err
	}
	for i, n := range ns {
		if id, ok := castSubject(n); ok {
			ns[i].Cast = casts[id]
		}
	}
	return nil
}

// castSubject is the story whose characters a notification names: the new
// story of a NEW_PERSPECTIVE_IN_ARC whose arc is not hidden.
func castSubject(n Notification) (uuid.UUID, bool) {
	if n.Kind != perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC ||
		n.Payload.ArcID == nil || n.Payload.PerspectiveID == nil {
		return uuid.Nil, false
	}
	return *n.Payload.PerspectiveID, true
}

// addCastSubject names, in a new story's email data, the story whose
// unclaimed characters the email lists. The labels themselves are read when
// the email is sent (see emailWorker.castFor), never copied into the job: a
// story hidden, deleted or voted out of its lore while the job waits or
// retries names nobody.
func addCastSubject(ev event, data map[string]string) {
	if ev.kind == perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC && ev.payload.PerspectiveID != nil {
		data[dataPerspectiveID] = ev.payload.PerspectiveID.String()
	}
}

// castPhrase lists the labels in quotes, joined as a sentence would, then
// "and N more": “a”, “b” and “c”; or “a”, “b”, “c” and 2 more. It is ""
// when there are no labels. Each label is put on one line with hidden
// characters dropped (apiutil.PlainLine): a label stored before saving
// enforced that cannot fake a line in the email. A label with nothing
// left is not listed.
func castPhrase(c cast) string {
	quoted := make([]string, 0, len(c.Labels)+1)
	for _, l := range c.Labels {
		if l = apiutil.PlainLine(l); l != "" {
			quoted = append(quoted, "“"+l+"”")
		}
	}
	if c.More > 0 {
		quoted = append(quoted, fmt.Sprintf("%d more", c.More))
	}
	switch len(quoted) {
	case 0:
		return ""
	case 1:
		return quoted[0]
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	}
}
