import { Code, ConnectError } from "@connectrpc/connect";

// Codes whose server message is written for the person who acted ("verify
// your email first", "not enough members to vote"), so it is shown as sent.
const userFacing = new Set([
  Code.InvalidArgument,
  Code.NotFound,
  Code.AlreadyExists,
  Code.PermissionDenied,
  Code.FailedPrecondition,
  Code.ResourceExhausted,
]);

const fallback: Partial<Record<Code, string>> = {
  [Code.InvalidArgument]: "That request was not valid.",
  [Code.NotFound]: "It may have been removed or hidden.",
  [Code.AlreadyExists]: "That has already been done.",
  [Code.PermissionDenied]: "You cannot do that here.",
  [Code.FailedPrecondition]: "That cannot be done right now.",
  [Code.ResourceExhausted]: "Too many tries. Wait a little and try again.",
  [Code.Unauthenticated]: "Sign in again to do that.",
};

/**
 * The message an action shows when the server refuses it. A refusal on the
 * request's merits shows the server's own message; anything else (a crash, the
 * server unreachable) gets a generic line, never a stack or internal text.
 */
export function actionError(err: unknown): string {
  const e = ConnectError.from(err);
  if (userFacing.has(e.code)) return e.rawMessage || fallback[e.code] || "That cannot be done.";
  return fallback[e.code] ?? "Something went wrong. Try again.";
}
