import { Code, ConnectError } from "@connectrpc/connect";
import { Link, useLoaderData, type LoaderFunctionArgs } from "react-router";

import { clients } from "@/lib/connect";
import { currentSession, refreshSession } from "@/lib/session";

import { AuthHeading, AuthPanel, linkClass } from "./AuthPanel";

type Result = { verified: boolean };

// Answers that mean "this token will never work": it is malformed, expired,
// already used or unknown. Anything else (the server is down) is an error the
// route's error page shows, so a reader is never told a good link is dead.
const deadToken = new Set([Code.InvalidArgument, Code.NotFound, Code.FailedPrecondition, Code.PermissionDenied]);

/**
 * Verifies the token from the emailed link in the loader, so it is sent once
 * per visit. When the reader is signed in, the session is then re-read from
 * the server rather than patched locally: the link may belong to a different
 * account than the one signed in.
 */
export async function loader({ request }: LoaderFunctionArgs): Promise<Result> {
  const token = new URL(request.url).searchParams.get("token");
  if (!token) return { verified: false };
  try {
    await clients.auth.verifyEmail({ token });
  } catch (err) {
    if (deadToken.has(ConnectError.from(err).code)) return { verified: false };
    throw err;
  }
  if (currentSession()) await refreshSession().catch(() => null);
  return { verified: true };
}

export function Component() {
  const { verified } = useLoaderData<Result>();
  if (verified) {
    return (
      <AuthPanel>
        <AuthHeading>Your email is verified</AuthHeading>
        <p className="mt-4">You can now publish perspectives, claim characters, vote and react.</p>
        <p className="mt-6">
          <Link to={currentSession() ? "/" : "/login"} className={linkClass}>
            {currentSession() ? "Start reading" : "Sign in"}
          </Link>
        </p>
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
        <Link to="/login" className={linkClass}>
          Sign in
        </Link>
      </p>
    </AuthPanel>
  );
}
