import { Code, ConnectError } from "@connectrpc/connect";
import { Link, useLoaderData, type LoaderFunctionArgs } from "react-router";

import { clients } from "@/lib/connect";
import { currentSession, refreshSession } from "@/lib/session";
import { useDocumentTitle } from "@/lib/title";

import { AuthHeading, AuthPanel, linkClass } from "./AuthPanel";

type Result = "changed" | "taken" | "dead";

// Answers that mean "this link will never work". Anything else (the server
// is down) is an error the route's error page shows.
const deadToken = new Set([Code.InvalidArgument, Code.NotFound, Code.FailedPrecondition, Code.PermissionDenied]);

/**
 * Confirms the email change from the link sent to the new address, in the
 * loader so it is sent once per visit. A signed-in session is re-read from
 * the server afterwards rather than patched locally.
 */
export async function loader({ request }: LoaderFunctionArgs): Promise<Result> {
  const token = new URL(request.url).searchParams.get("token");
  if (!token) return "dead";
  try {
    await clients.auth.confirmEmailChange({ token });
  } catch (err) {
    const code = ConnectError.from(err).code;
    if (code === Code.AlreadyExists) return "taken";
    if (deadToken.has(code)) return "dead";
    throw err;
  }
  if (currentSession()) await refreshSession().catch(() => null);
  return "changed";
}

export function Component() {
  useDocumentTitle("Confirm your new email");
  const result = useLoaderData<Result>();
  const signedIn = currentSession() !== null;
  if (result === "changed") {
    return (
      <AuthPanel>
        <AuthHeading>Your email is changed</AuthHeading>
        <p className="mt-4">Sign in with your new address from now on.</p>
        <p className="mt-6">
          <Link to={signedIn ? "/settings" : "/login"} className={linkClass}>
            {signedIn ? "Back to settings" : "Sign in"}
          </Link>
        </p>
      </AuthPanel>
    );
  }
  return (
    <AuthPanel>
      <AuthHeading>This link no longer works</AuthHeading>
      {result === "taken" ? (
        <p className="mt-4">Another account started using that address before you confirmed it, so your email was not changed.</p>
      ) : (
        // A used link is dead too, and it may have worked the first time, so
        // this says nothing about whether the email changed.
        <p className="mt-4">
          The link is incomplete, has expired, was already used, or a newer change replaced it. Settings shows the email
          your account uses now.
        </p>
      )}
      <p className="mt-6">
        <Link to={signedIn ? "/settings" : "/login?next=%2Fsettings"} className={linkClass}>
          {signedIn ? "Open settings" : "Sign in"}
        </Link>
      </p>
    </AuthPanel>
  );
}
