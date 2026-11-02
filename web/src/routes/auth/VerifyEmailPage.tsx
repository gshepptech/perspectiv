import { Code, ConnectError } from "@connectrpc/connect";
import { Link, useLoaderData, type LoaderFunctionArgs } from "react-router";

import { clients } from "@/lib/connect";
import { currentSession, refreshSession } from "@/lib/session";
import { useDocumentTitle } from "@/lib/title";

import { readAfterVerify } from "./afterVerify";
import { AuthHeading, AuthPanel, linkClass } from "./AuthPanel";

/** `next` is where the reader was headed when they signed up, if this browser kept it. */
type Result = { verified: boolean; next: string | null };

// Answers that mean "this token will never work": it is malformed, expired,
// already used or unknown. Anything else (the server is down) is an error the
// route's error page shows, so a reader is never told a good link is dead.
const deadToken = new Set([Code.InvalidArgument, Code.NotFound, Code.FailedPrecondition, Code.PermissionDenied]);

/**
 * Verifies the token from the emailed link in the loader, so it is sent once
 * per visit. When the reader is signed in, the session is then re-read from
 * the server rather than patched locally: the link may belong to a different
 * account than the one signed in. Where the reader was headed when they
 * signed up is read here too, and given up once the email is verified.
 */
export async function loader({ request }: LoaderFunctionArgs): Promise<Result> {
  const token = new URL(request.url).searchParams.get("token");
  if (!token) return { verified: false, next: readAfterVerify() };
  try {
    await clients.auth.verifyEmail({ token });
  } catch (err) {
    if (deadToken.has(ConnectError.from(err).code)) return { verified: false, next: readAfterVerify() };
    throw err;
  }
  if (currentSession()) await refreshSession().catch(() => null);
  return { verified: true, next: readAfterVerify({ take: true }) };
}

/** The page itself when signed in; otherwise sign-in first, which then goes there. */
function through(to: string): string {
  return currentSession() ? to : `/login?next=${encodeURIComponent(to)}`;
}

export function Component() {
  useDocumentTitle("Verify your email");
  const { verified, next } = useLoaderData<Result>();
  if (verified) {
    return (
      <AuthPanel>
        <AuthHeading>Your email is verified</AuthHeading>
        <p className="mt-4">You can now publish stories, claim characters, vote and react.</p>
        {next ? (
          <p className="mt-6">
            <Link to={through(next)} className={linkClass}>
              Continue where you were
            </Link>
          </p>
        ) : (
          // Verifying is when a new member may first publish, so the page
          // offers writing first, then reading.
          <p className="mt-4 flex flex-wrap gap-x-6">
            <Link to={through("/compose")} className={`inline-flex min-h-11 items-center ${linkClass}`}>
              {currentSession() ? "Write about a place you were at" : "Sign in to write about a place you were at"}
            </Link>
            <Link to={currentSession() ? "/" : "/login"} className={`inline-flex min-h-11 items-center ${linkClass}`}>
              {currentSession() ? "Start reading" : "Sign in"}
            </Link>
          </p>
        )}
      </AuthPanel>
    );
  }
  return (
    <AuthPanel>
      <AuthHeading>This link no longer works</AuthHeading>
      <p className="mt-4">
        The verification link is incomplete, has expired or was already used. Sign in to see whether your email is
        already verified.
      </p>
      <p className="mt-6">
        <Link to={next ? `/login?next=${encodeURIComponent(next)}` : "/login"} className={linkClass}>
          Sign in
        </Link>
      </p>
    </AuthPanel>
  );
}
