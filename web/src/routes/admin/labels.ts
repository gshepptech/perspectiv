import { ModerationAction, ReportReason, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";

export function targetKindLabel(kind: ReportTargetKind): string {
  switch (kind) {
    case ReportTargetKind.PERSPECTIVE:
      return "Perspective";
    case ReportTargetKind.PLACE:
      return "Place";
    case ReportTargetKind.USER:
      return "User";
    case ReportTargetKind.LORE_POST:
      return "Thread post";
    case ReportTargetKind.LORE_NAME:
      return "Lore name";
    case ReportTargetKind.UNSPECIFIED:
      return "Unknown target";
  }
}

export function reasonLabel(reason: ReportReason): string {
  switch (reason) {
    case ReportReason.REAL_NAME_OR_HANDLE_OF_NON_USER:
      return "Names a real person who is not a user";
    case ReportReason.HARASSMENT:
      return "Harassment";
    case ReportReason.HATEFUL_CONTENT:
      return "Hateful content";
    case ReportReason.SEXUAL_CONTENT:
      return "Sexual content";
    case ReportReason.SPAM:
      return "Spam";
    case ReportReason.OTHER:
      return "Other";
    case ReportReason.UNSPECIFIED:
      return "No reason given";
  }
}

export function actionLabel(action: ModerationAction): string {
  switch (action) {
    case ModerationAction.HIDE_PERSPECTIVE:
      return "Hide perspective";
    case ModerationAction.HIDE_PLACE:
      return "Hide place";
    case ModerationAction.SUSPEND_USER:
      return "Suspend user";
    case ModerationAction.HIDE_LORE_POST:
      return "Hide post";
    case ModerationAction.CLEAR_LORE_NAME:
      return "Clear the name";
    case ModerationAction.DISMISS:
      return "Dismiss";
    case ModerationAction.UNSPECIFIED:
      return "No action";
  }
}

/**
 * The actions the server applies to a report: the one that acts on the
 * reported target's own kind, and dismissal.
 */
export function actionsFor(kind: ReportTargetKind): ModerationAction[] {
  switch (kind) {
    case ReportTargetKind.PERSPECTIVE:
      return [ModerationAction.HIDE_PERSPECTIVE, ModerationAction.DISMISS];
    case ReportTargetKind.PLACE:
      return [ModerationAction.HIDE_PLACE, ModerationAction.DISMISS];
    case ReportTargetKind.USER:
      return [ModerationAction.SUSPEND_USER, ModerationAction.DISMISS];
    case ReportTargetKind.LORE_POST:
      return [ModerationAction.HIDE_LORE_POST, ModerationAction.DISMISS];
    case ReportTargetKind.LORE_NAME:
      return [ModerationAction.CLEAR_LORE_NAME, ModerationAction.DISMISS];
    case ReportTargetKind.UNSPECIFIED:
      return [ModerationAction.DISMISS];
  }
}

/**
 * The resolve dialog's choice that suspends the author of a reported
 * perspective. The server suspends only the target of a user report, so this
 * files a report on the author and resolves that one with SUSPEND_USER; the
 * audit row then names the user, and the perspective report stays open for
 * its own decision.
 */
export const SUSPEND_AUTHOR = "suspend-author";

export type ResolveChoice = { value: string; label: string };

/** The choices the resolve dialog offers for a report, in display order. */
export function choicesFor(kind: ReportTargetKind): ResolveChoice[] {
  const choices = actionsFor(kind).map((a) => ({ value: String(a), label: actionLabel(a) }));
  if (kind === ReportTargetKind.PERSPECTIVE) {
    choices.splice(1, 0, { value: SUSPEND_AUTHOR, label: "Suspend the author" });
  }
  return choices;
}

/** Where an admin reads the reported target, when it has a page. */
export function targetHref(kind: ReportTargetKind, id: string): { href: string; text: string } | null {
  switch (kind) {
    case ReportTargetKind.PERSPECTIVE:
      return { href: `/perspectives/${encodeURIComponent(id)}`, text: "Open the perspective" };
    case ReportTargetKind.PLACE:
      return { href: `/places/${encodeURIComponent(id)}`, text: "Open the place" };
    case ReportTargetKind.LORE_NAME:
      return { href: `/lores/${encodeURIComponent(id)}`, text: "Open the lore" };
    // The queue shows a post's text; a post has no page of its own.
    case ReportTargetKind.LORE_POST:
    case ReportTargetKind.USER:
    case ReportTargetKind.UNSPECIFIED:
      return null;
  }
}
