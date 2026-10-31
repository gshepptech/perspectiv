package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/notify/dbq"
)

// listedCharacters is how many unclaimed characters a new-story notification
// names; the rest are counted ("and 2 more").
const listedCharacters = 3

// Email data keys of a new-story notification: the listed labels as a JSON
// array and how many more there are.
const (
	dataCharacters     = "characters"
	dataMoreCharacters = "more_characters"
)

// cast is the unclaimed characters a notification names: up to
// listedCharacters labels, in the story's order, and how many more.
type cast struct {
	Labels []string
	More   int32
}

// unclaimedCast reads, for each story in ids that anyone may still read, the
// characters nobody has claimed yet. A story that is deleted, hidden or has
// every character claimed is absent from the map. The caller checks the
// arc's place, which is not read here.
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

// addCastData puts a new story's unclaimed characters into its email data,
// read from the database now; nothing is added when it has none.
func addCastData(ctx context.Context, q *dbq.Queries, ev event, data map[string]string) error {
	if ev.kind != perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC || ev.payload.PerspectiveID == nil {
		return nil
	}
	id := *ev.payload.PerspectiveID
	casts, err := unclaimedCast(ctx, q, []uuid.UUID{id})
	if err != nil {
		return err
	}
	c, ok := casts[id]
	if !ok {
		return nil
	}
	labels, err := json.Marshal(c.Labels)
	if err != nil {
		return err
	}
	data[dataCharacters] = string(labels)
	data[dataMoreCharacters] = strconv.Itoa(int(c.More))
	return nil
}

// castFromData reads back what addCastData wrote; a job without it, or with
// data it cannot read, names nobody.
func castFromData(data map[string]string) cast {
	var c cast
	if err := json.Unmarshal([]byte(data[dataCharacters]), &c.Labels); err != nil {
		return cast{}
	}
	more, err := strconv.ParseInt(data[dataMoreCharacters], 10, 32)
	if err == nil && more > 0 {
		c.More = int32(more)
	}
	return c
}

// castPhrase lists the labels in quotes, joined as a sentence would, then
// "and N more": “a”, “b” and “c”; or “a”, “b”, “c” and 2 more. It is ""
// when there are no labels.
func castPhrase(c cast) string {
	quoted := make([]string, len(c.Labels))
	for i, l := range c.Labels {
		quoted[i] = "“" + l + "”"
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
