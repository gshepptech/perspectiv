import { NotificationKind } from "@/gen/perspectiv/v1/enums_pb";
import type { Notification } from "@/gen/perspectiv/v1/notify_pb";

/** What an inbox row says and where it leads; `to` is null when there is no page to open. */
export type Described = { text: string; to: string | null };

/**
 * The sentence and destination for one notification. Other users appear only
 * by username (the API carries nothing else). A claim on your character opens
 * the claims page where you accept or reject it; an event about one
 * perspective (a new one, a vote to remove one) opens that perspective; an
 * event about the arc as a whole opens the arc; a reply to your thread post
 * opens the lore page the thread is on. A resolved report has no page
 * a non-admin can open, so it has no link.
 */
export function describeNotification(n: Notification): Described {
  const who = n.actor?.username || "Someone";
  const arc = n.arcTitle || "an arc";
  const arcPage = n.arcId ? `/arcs/${n.arcId}` : null;
  const perspectivePage = n.perspectiveId ? `/perspectives/${n.perspectiveId}` : arcPage;
  switch (n.kind) {
    case NotificationKind.CLAIM_RECEIVED:
      return { text: `${who} claimed a character in your perspective in ${arc}.`, to: "/claims" };
    case NotificationKind.CLAIM_ACCEPTED:
      return { text: `${who} accepted your claim in ${arc}.`, to: arcPage };
    case NotificationKind.NEW_PERSPECTIVE_IN_ARC:
      return { text: `${who} published a new perspective in ${arc}.`, to: perspectivePage };
    case NotificationKind.VOTE_OUT_OPENED:
      return { text: `A vote to remove a perspective opened in ${arc}.`, to: perspectivePage };
    case NotificationKind.CANON_CHANGE:
      return { text: `The canon of ${arc} changed.`, to: arcPage };
    case NotificationKind.INVITE_ANSWERED:
      return { text: `${who} followed your invite and wrote their side of ${arc}.`, to: perspectivePage };
    case NotificationKind.NEW_STORY_AT_FOLLOWED_PLACE:
      return { text: `${who} wrote a new story in ${arc}, at a place you follow.`, to: perspectivePage };
    case NotificationKind.NEW_STORY_BY_FOLLOWED_WRITER:
      return { text: `${who} wrote a new story in ${arc}.`, to: perspectivePage };
    case NotificationKind.THREAD_REPLY:
      return { text: `${who} replied to your post in the thread of ${arc}.`, to: n.loreId ? `/lores/${n.loreId}` : arcPage };
    case NotificationKind.REPORT_RESOLVED:
      return { text: "A report you made was resolved. Thank you for keeping Perspectiv safe.", to: null };
    case NotificationKind.UNSPECIFIED:
      break;
    default: {
      // Fails to compile when a kind is added to the enum and not handled above.
      const unhandled: never = n.kind;
      void unhandled;
    }
  }
  // An unspecified kind, or one newer than this build: say something true and open the arc.
  return { text: `Something happened in ${arc}.`, to: arcPage };
}
