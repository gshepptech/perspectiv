package arc

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/arc/dbq"
)

// ListOpenCharacters lists the characters in the arc's readable stories that
// nobody holds yet, for the arc page's "Is this you?" list. A missing arc,
// or one at or under a hidden place, is NotFound.
//
// The list query reads the arc's visibility itself and lists nothing for an
// arc at or under a hidden place, so the arc row is read only when the list
// is empty, to tell NotFound from an arc with no open characters. Both read
// through the pool, not in a snapshot: the arc row only decides NotFound and
// the list reads nothing from it, so a hide or a claim landing between them
// shows what a request a moment earlier or later would have.
func (s *Service) ListOpenCharacters(ctx context.Context, arcID uuid.UUID) ([]*perspectivv1.OpenCharacter, error) {
	q := dbq.New(s.pool)
	rows, err := q.ListOpenCharacters(ctx, arcID)
	if err != nil {
		return nil, apiutil.Internal(fmt.Errorf("list open characters: %w", err))
	}
	if len(rows) == 0 {
		if _, err := loadArc(ctx, q, arcID); err != nil {
			return nil, err
		}
	}
	out := make([]*perspectivv1.OpenCharacter, len(rows))
	for i, r := range rows {
		out[i] = &perspectivv1.OpenCharacter{
			CharacterId: r.ID.String(), Label: r.Label, PerspectiveId: r.PerspectiveID.String(),
			Author: &perspectivv1.UserRef{UserId: r.AuthorID.String(), Username: r.Username},
		}
	}
	return out, nil
}
