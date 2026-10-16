package react

import (
	"fmt"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
)

// The database stores proto enum value names. These are the react domain's
// only conversions; an unknown stored name is an error, never a zero value,
// so schema and proto drift surfaces as Internal.

// emojis is the fixed set in enum order, the order responses list them in.
var emojis = []perspectivv1.Emoji{
	perspectivv1.Emoji_EMOJI_HEART,
	perspectivv1.Emoji_EMOJI_LAUGH,
	perspectivv1.Emoji_EMOJI_WOW,
	perspectivv1.Emoji_EMOJI_SAD,
	perspectivv1.Emoji_EMOJI_FIRE,
	perspectivv1.Emoji_EMOJI_CLAP,
}

var statusPublished = perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_PUBLISHED.String()

// emojiToDB returns the stored name of an emoji in the fixed set; anything
// else, UNSPECIFIED included, is InvalidArg.
func emojiToDB(e perspectivv1.Emoji) (string, error) {
	switch e {
	case perspectivv1.Emoji_EMOJI_HEART,
		perspectivv1.Emoji_EMOJI_LAUGH,
		perspectivv1.Emoji_EMOJI_WOW,
		perspectivv1.Emoji_EMOJI_SAD,
		perspectivv1.Emoji_EMOJI_FIRE,
		perspectivv1.Emoji_EMOJI_CLAP:
		return e.String(), nil
	case perspectivv1.Emoji_EMOJI_UNSPECIFIED:
		return "", apiutil.InvalidArg("emoji is required")
	}
	return "", apiutil.InvalidArg("emoji is not one of the six reacts")
}

func emojiFromDB(s string) (perspectivv1.Emoji, error) {
	v, ok := perspectivv1.Emoji_value[s]
	if !ok || v == 0 {
		return 0, fmt.Errorf("unknown emoji %q", s)
	}
	return perspectivv1.Emoji(v), nil
}
