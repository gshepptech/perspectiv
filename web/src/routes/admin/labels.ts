import { ModerationAction, ReportReason, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";

export function targetKindLabel(kind: ReportTargetKind): string {
  switch (kind) {
    case ReportTargetKind.PERSPECTIVE:
      return "Perspective";
    case ReportTargetKind.PLACE:
      return "Place";
    case ReportTargetKind.USER:
      return "User";
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
    case ModerationAction.DISMISS:
      return "Dismiss";
    case ModerationAction.UNSPECIFIED:
      return "No action";
  }
}

/**
 * The actions an admin may take on a report: the one that acts on the
 * reported target's own kind, and dismissal. A report names one target, so a
 * perspective report hides that perspective rather than suspending its author;
 * suspending someone is done from a report on the user.
 */
export function actionsFor(kind: ReportTargetKind): ModerationAction[] {
  switch (kind) {
    case ReportTargetKind.PERSPECTIVE:
      return [ModerationAction.HIDE_PERSPECTIVE, ModerationAction.DISMISS];
    case ReportTargetKind.PLACE:
      return [ModerationAction.HIDE_PLACE, ModerationAction.DISMISS];
    case ReportTargetKind.USER:
      return [ModerationAction.SUSPEND_USER, ModerationAction.DISMISS];
    case ReportTargetKind.UNSPECIFIED:
      return [ModerationAction.DISMISS];
  }
}

/** Where an admin reads the reported target, when it has a page. */
export function targetHref(kind: ReportTargetKind, id: string): { href: string; text: string } | null {
  switch (kind) {
    case ReportTargetKind.PERSPECTIVE:
      return { href: `/perspectives/${encodeURIComponent(id)}`, text: "Open the perspective" };
    case ReportTargetKind.PLACE:
      return { href: `/places/${encodeURIComponent(id)}`, text: "Open the place" };
    case ReportTargetKind.USER:
    case ReportTargetKind.UNSPECIFIED:
      return null;
  }
}
