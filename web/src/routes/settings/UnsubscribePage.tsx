import { Code } from "@connectrpc/connect";
import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { Link, useSearchParams } from "react-router";

import { Button } from "@/components/ui/Button";
import { clients } from "@/lib/connect";
import { AuthHeading, AuthPanel, FormAlert, linkClass } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";

const settingsLink = (
  <Link to="/settings" className={linkClass}>
    Settings
  </Link>
);

function Done() {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <AuthPanel>
      <AuthHeading ref={heading}>You will get no more notification emails</AuthHeading>
      <p className="mt-4">
        Your inbox on Perspectiv still collects them. Emails about your account, such as password resets, still
        arrive. Turn some back on in {settingsLink}.
      </p>
    </AuthPanel>
  );
}

/**
 * The page an email's unsubscribe link opens. It asks before acting: link
 * scanners open every link in an email, and opening the page must change
 * nothing. Mail clients use the one-click endpoint instead.
 */
export function Component() {
  const [params] = useSearchParams();
  const userId = params.get("u") ?? "";
  const token = params.get("t") ?? "";
  const stop = useMutation({ mutationFn: () => clients.notify.unsubscribe({ userId, token }) });

  if (!userId || !token) {
    return (
      <AuthPanel>
        <AuthHeading>This link no longer works</AuthHeading>
        <p className="mt-4">The unsubscribe link is incomplete. Sign in and turn emails off in {settingsLink}.</p>
      </AuthPanel>
    );
  }
  if (stop.isSuccess) return <Done />;
  return (
    <AuthPanel>
      <AuthHeading>Stop notification emails?</AuthHeading>
      <p className="mt-4">
        Perspectiv stops emailing you about claims, votes, new stories and the rest. You still see them in your inbox
        when you sign in.
      </p>
      <div className="mt-6 grid gap-4">
        <FormAlert
          message={
            stop.error
              ? errorMessage(stop.error, {
                  [Code.InvalidArgument]: "This unsubscribe link is not valid. Sign in and turn emails off in Settings.",
                })
              : null
          }
        />
        <Button pending={stop.isPending} onClick={() => stop.mutate()}>
          Stop emails
        </Button>
      </div>
    </AuthPanel>
  );
}

