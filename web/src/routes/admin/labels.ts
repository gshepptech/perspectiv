import { AuditAction, AuditTargetKind, ModerationAction, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";

import type { Resolution } from "./ResolveDialog";

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

/** The status line a page shows once a report is resolved. */
export function resolutionMessage(r: Resolution): string {
  return r.suspended
    ? `${r.suspended} is suspended. The report on their perspective stays open.`
    : `Report resolved: ${actionLabel(r.action).toLowerCase()}.`;
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

/** What an audit entry did, as the verb phrase after the actor's name. */
export function auditActionLabel(action: AuditAction): string {
  switch (action) {
    case AuditAction.HIDE_PERSPECTIVE:
      return "hid a perspective";
    case AuditAction.HIDE_PLACE:
      return "hid a place";
    case AuditAction.SUSPEND_USER:
      return "suspended a user";
    case AuditAction.LIFT_SUSPENSION:
      return "lifted a suspension";
    case AuditAction.RESTORE:
      return "restored hidden content";
    case AuditAction.DISMISS:
      return "dismissed a report";
    case AuditAction.HIDE_LORE_POST:
      return "hid a thread post";
    case AuditAction.CLEAR_LORE_NAME:
      return "cleared a lore's name";
    case AuditAction.ADMIN_GRANT:
      return "made a user an admin";
    case AuditAction.ACCESS_CODE_CREATE:
      return "created an access code";
    case AuditAction.ACCESS_CODE_REVOKE:
      return "revoked an access code";
    case AuditAction.WAITLIST_INVITE:
      return "invited someone from the waitlist";
    case AuditAction.USER_LOOKUP:
      return "looked up a user";
    case AuditAction.UNSPECIFIED:
      return "did something unrecorded";
  }
}

/** The audit log's filter choices, in the order the Select lists them. */
export const auditActions: AuditAction[] = [
  AuditAction.HIDE_PERSPECTIVE,
  AuditAction.HIDE_PLACE,
  AuditAction.HIDE_LORE_POST,
  AuditAction.CLEAR_LORE_NAME,
  AuditAction.SUSPEND_USER,
  AuditAction.LIFT_SUSPENSION,
  AuditAction.RESTORE,
  AuditAction.DISMISS,
  AuditAction.ADMIN_GRANT,
  AuditAction.ACCESS_CODE_CREATE,
  AuditAction.ACCESS_CODE_REVOKE,
  AuditAction.WAITLIST_INVITE,
  AuditAction.USER_LOOKUP,
];

/** Where an admin opens an audit entry's target, when it has a page. */
export function auditTargetHref(kind: AuditTargetKind, id: string): string | null {
  switch (kind) {
    case AuditTargetKind.PERSPECTIVE:
      return `/perspectives/${encodeURIComponent(id)}`;
    case AuditTargetKind.PLACE:
      return `/places/${encodeURIComponent(id)}`;
    case AuditTargetKind.LORE_NAME:
      return `/lores/${encodeURIComponent(id)}`;
    case AuditTargetKind.USER:
      return `/admin/users?q=${encodeURIComponent(id)}`;
    case AuditTargetKind.LORE_POST:
    case AuditTargetKind.ACCESS_CODE:
    case AuditTargetKind.WAITLIST_ENTRY:
    case AuditTargetKind.UNSPECIFIED:
      return null;
  }
}
