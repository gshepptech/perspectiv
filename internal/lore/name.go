package lore

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/lore/dbq"
)

// maxNameLen bounds a lore name, as lores_name_check does.
const maxNameLen = 60

// nameLockAfterClear is how long a lore cannot be renamed after a moderator
// clears its name, so a removed name cannot be put straight back.
const nameLockAfterClear = 7 * 24 * time.Hour

// NameLore sets a lore's name. Only a member of the lore may name it, and
// the latest name wins. Runs of whitespace collapse to one space.
func (s *Service) NameLore(ctx context.Context, caller, loreID uuid.UUID, name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		return "", apiutil.InvalidArg("give the lore a name")
	case n > maxNameLen:
		return "", apiutil.InvalidArg(fmt.Sprintf("keep a name to %d characters", maxNameLen))
	}
	q := dbq.New(s.pool)
	if _, err := readableLore(ctx, q, loreID); err != nil {
		return "", err
	}
	member, err := q.IsLoreMember(ctx, dbq.IsLoreMemberParams{LoreID: loreID, UserID: caller})
	if err != nil {
		return "", apiutil.Internal(fmt.Errorf("check lore membership: %w", err))
	}
	if !member {
		return "", apiutil.PermissionDenied("only the people in this lore can name it")
	}
	cleared, err := q.LoreNameClearedAt(ctx, loreID)
	if err != nil {
		return "", apiutil.Internal(fmt.Errorf("read name clear: %w", err))
	}
	if cleared.Valid && time.Since(cleared.Time) < nameLockAfterClear {
		until := cleared.Time.Add(nameLockAfterClear).Format("January 2")
		return "", apiutil.FailedPrecondition("a moderator cleared this lore's name; it can be named again after " + until)
	}
	if err := q.SetLoreName(ctx, dbq.SetLoreNameParams{ID: loreID, Name: name, NamedBy: caller}); err != nil {
		return "", apiutil.Internal(fmt.Errorf("name lore: %w", err))
	}
	return name, nil
}
