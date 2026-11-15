import { NotificationKind } from "@/gen/perspectiv/v1/enums_pb";
import type { Notification } from "@/gen/perspectiv/v1/notify_pb";

/**
 * What an inbox row says and where it leads; `to` is null when there is no
 * page to open. `detail`, when set, is a second line under the sentence.
 */
export type Described = { text: string; to: string | null; detail?: string };

/**
 * The new story's characters nobody has claimed, as a sentence: up to three
 * labels in quotes, then "and N more" ("“a”, “b”, “c” and 2 more"). Empty
 * when the server names none. The email words it the same way.
 */
function castLine(n: Pick<Notification, "unclaimedCharacters" | "moreUnclaimedCharacters">): string {
  const items = n.unclaimedCharacters.map((label) => `“${label}”`);
  if (items.length === 0) return "";
  if (n.moreUnclaimedCharacters > 0) items.push(`${n.moreUnclaimedCharacters} more`);
  const last = items.pop() ?? "";
  return `Who might be in it: ${items.length > 0 ? `${items.join(", ")} and ${last}` : last}.`;
}

/**
 * The sentence and destination for one notification. Other users appear only
 * by username (the API carries nothing else). A claim on your character opens
 * the claims page where you accept or reject it; an event about one
 * perspective (a new one, a vote to remove one) opens that perspective; an
 * event about the arc as a whole opens the arc; a reply to your thread post
 * opens the lore page at that reply. A resolved report has no page
 * a non-admin can open, so it has no link.
 */
export function describeNotification(n: Notification): Described {
  const who = n.actor?.username || "Someone";
  const arc = n.arcTitle || "a day";
  const arcPage = n.arcId ? `/arcs/${n.arcId}` : null;
  const perspectivePage = n.perspectiveId ? `/perspectives/${n.perspectiveId}` : arcPage;
  switch (n.kind) {
    case NotificationKind.CLAIM_RECEIVED:
      return { text: `${who} claimed a character in your story in ${arc}.`, to: "/claims" };
    case NotificationKind.CLAIM_ACCEPTED:
      return { text: `${who} accepted your claim in ${arc}.`, to: arcPage };
    case NotificationKind.NEW_PERSPECTIVE_IN_ARC: {
      const detail = castLine(n);
      return { text: `${who} published a new story in ${arc}.`, to: perspectivePage, ...(detail && { detail }) };
    }
    case NotificationKind.VOTE_OUT_OPENED:
      return { text: `A vote to remove a story opened in ${arc}.`, to: perspectivePage };
    case NotificationKind.CANON_CHANGE:
      return { text: `The canon of ${arc} changed.`, to: arcPage };
    case NotificationKind.INVITE_ANSWERED:
      return { text: `${who} followed your invite and wrote their side of ${arc}.`, to: perspectivePage };
    case NotificationKind.NEW_STORY_AT_FOLLOWED_PLACE:
      return { text: `${who} wrote a new story in ${arc}, at a place you follow.`, to: perspectivePage };
    case NotificationKind.NEW_STORY_BY_FOLLOWED_WRITER:
      return { text: `${who} wrote a new story in ${arc}.`, to: perspectivePage };
    case NotificationKind.THREAD_REPLY: {
      // The lore page scrolls to and focuses the reply its anchor names.
      const lore = n.loreId ? `/lores/${n.loreId}${n.postId ? `#post-${n.postId}` : ""}` : arcPage;
      return { text: `${who} replied to your post in the thread of ${arc}.`, to: lore };
    }
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
