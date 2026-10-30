import { Code, ConnectError } from "@connectrpc/connect";

/**
 * The sentence an account page shows for a failed request. Refusals the
 * server words for the user (bad input, a taken name, a rule not yet met) are
 * shown in the server's words, as a sentence; transport and policy failures
 * get a fixed sentence. A page passes overrides for codes whose meaning it knows better,
 * such as Unauthenticated on the login form.
 */
export function errorMessage(err: unknown, overrides: Partial<Record<Code, string>> = {}): string {
  const e = ConnectError.from(err);
  const override = overrides[e.code];
  if (override) return override;
  switch (e.code) {
    case Code.InvalidArgument:
    case Code.AlreadyExists:
    case Code.FailedPrecondition:
    case Code.NotFound:
      return asSentence(e.rawMessage) || "That did not work. Check the form and try again.";
    case Code.ResourceExhausted:
      return "Too many attempts. Wait a few minutes and try again.";
    case Code.Unavailable:
      return "Perspectiv is unreachable. Check your connection and try again.";
    case Code.Unauthenticated:
      return "Your session has ended. Sign in again.";
    case Code.PermissionDenied:
      return "You are not allowed to do that.";
    default:
      return "Something went wrong. Try again.";
  }
}

/**
 * A server message as a sentence: the server writes Go-style errors
 * ("password must be at least 10 characters"), so the first letter is
 * capitalised and a closing full stop added when it has none.
 */
export function asSentence(message: string): string {
  const text = message.trim();
  if (!text) return "";
  const capitalised = text.charAt(0).toUpperCase() + text.slice(1);
  return /[.!?…]$/.test(capitalised) ? capitalised : `${capitalised}.`;
}
