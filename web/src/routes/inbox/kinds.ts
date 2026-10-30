import { NotificationKind } from "@/gen/perspectiv/v1/enums_pb";

/**
 * The notification kinds in enum order, each with the label the settings
 * page puts on its two toggles. UNSPECIFIED is never a real notification and
 * is not listed.
 */
export const notificationKinds: readonly { kind: NotificationKind; label: string }[] = [
  { kind: NotificationKind.CLAIM_RECEIVED, label: "Someone claims a character in your perspective" },
  { kind: NotificationKind.CLAIM_ACCEPTED, label: "Your claim is accepted" },
  { kind: NotificationKind.NEW_PERSPECTIVE_IN_ARC, label: "A new perspective in your arc" },
  { kind: NotificationKind.VOTE_OUT_OPENED, label: "A vote to remove opens" },
  { kind: NotificationKind.CANON_CHANGE, label: "The canon of your arc changes" },
  { kind: NotificationKind.REPORT_RESOLVED, label: "A report you made is resolved" },
  { kind: NotificationKind.INVITE_ANSWERED, label: "Someone you invited writes their side" },
  { kind: NotificationKind.NEW_STORY_AT_FOLLOWED_PLACE, label: "A new story at a place you follow" },
  { kind: NotificationKind.NEW_STORY_BY_FOLLOWED_WRITER, label: "A new story by a writer you follow" },
];
