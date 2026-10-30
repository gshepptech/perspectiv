import { Link } from "react-router";

import { useWaitingRequests } from "@/routes/claims/waiting";
import { useUnreadCount } from "@/routes/inbox/unread";

const linkClass = "inline-flex min-h-11 items-center font-medium text-accent underline underline-offset-4";

/**
 * The top of the front door for a returning member: what is waiting on
 * them, each with the page that deals with it. A count still loading, or one
 * that failed to load, is left out rather than shown as zero.
 */
export function WelcomeBack({ username }: { username: string }) {
  const unread = useUnreadCount(true);
  const waiting = useWaitingRequests(true);
  const unreadCount = unread.data ?? 0;
  const waitingCount = waiting.data?.count ?? 0;
  const settled = unread.isSuccess && waiting.isSuccess;
  return (
    <div className="min-w-0">
      <h1 id="landing-title" className="text-2xl wrap-anywhere sm:text-3xl">
        Welcome back, {username}
      </h1>
      {(settled || unreadCount > 0 || waitingCount > 0) && (
        <ul aria-label="Waiting on you" className="mt-3 grid gap-x-6 sm:flex sm:flex-wrap">
          {unreadCount > 0 && (
            <li className="flex flex-wrap items-center gap-x-2">
              <span>
                <strong className="font-bold">{unreadCount}</strong> unread {unreadCount === 1 ? "notification" : "notifications"}
              </span>
              <span aria-hidden className="text-ink-muted">
                ·
              </span>
              <Link to="/inbox" className={linkClass}>
                Open your inbox
              </Link>
            </li>
          )}
          {waitingCount > 0 && (
            <li className="flex flex-wrap items-center gap-x-2">
              <span>
                <strong className="font-bold">
                  {waitingCount}
                  {waiting.data?.more ? "+" : ""}
                </strong>{" "}
                {waitingCount === 1 && !waiting.data?.more ? "request" : "requests"} waiting on you
              </span>
              <span aria-hidden className="text-ink-muted">
                ·
              </span>
              <Link to="/claims" className={linkClass}>
                Review requests
              </Link>
            </li>
          )}
          {settled && unreadCount === 0 && waitingCount === 0 && <li className="text-ink-muted">Nothing is waiting on you.</li>}
        </ul>
      )}
    </div>
  );
}
