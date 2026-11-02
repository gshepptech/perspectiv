import { useQuery } from "@tanstack/react-query";
import { PenLine } from "lucide-react";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import { PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import { clients } from "@/lib/connect";
import { useWaitingRequests } from "@/routes/claims/waiting";
import { useUnreadCount } from "@/routes/inbox/unread";

const linkClass = "inline-flex min-h-11 items-center font-medium text-accent underline underline-offset-4";

/**
 * Whether the member has published anything: one published story is
 * enough to know. Undefined while it loads or when it fails.
 */
function useHasPublished(): boolean | undefined {
  const published = useQuery({
    queryKey: ["perspective", "listMine", { status: PerspectiveStatus.PUBLISHED, pageSize: 1 }],
    queryFn: ({ signal }) => clients.perspective.listMine({ status: PerspectiveStatus.PUBLISHED, pageSize: 1 }, { signal }),
  });
  return published.isSuccess ? published.data.perspectives.length > 0 : undefined;
}

/**
 * The first thing a member who has not published yet is asked to do, and
 * how a day works, said briefly enough to read before they start.
 */
function FirstStory() {
  return (
    <div className="mt-5 grid min-w-0 gap-6">
      <div>
        <Button asChild>
          <Link to="/compose">
            <PenLine aria-hidden size={18} />
            Write about a place you were at
          </Link>
        </Button>
      </div>
      <section aria-labelledby="welcome-how" className="max-w-measure min-w-0 rounded-md border border-rule bg-surface p-4">
        <h2 id="welcome-how" className="text-lg">
          How a day works
        </h2>
        <ol className="mt-2 grid list-decimal gap-1.5 pl-5 text-ink">
          <li>Every story is pinned to a place and a day, or one hour of it.</li>
          <li>Everyone who was there writes their own side.</li>
          <li>
            If someone else&apos;s story describes you, claim that character; your two stories link, and linked stories make a
            lore.
          </li>
          <li>The lore the day&apos;s writers favour most becomes its canon.</li>
        </ol>
      </section>
    </div>
  );
}

/**
 * The top of the front door for a member: what is waiting on them, each
 * with the page that deals with it. A count still loading, or one that
 * failed to load, is left out rather than shown as zero. A member who has
 * published is welcomed back, and told when nothing is waiting; one who has
 * not is welcomed, then asked to write and shown how a day works, with no
 * "Nothing is waiting" line, since nothing could be waiting yet.
 */
export function WelcomeBack({ username }: { username: string }) {
  const unread = useUnreadCount(true);
  const waiting = useWaitingRequests(true);
  const hasPublished = useHasPublished();
  const unreadCount = unread.data ?? 0;
  const waitingCount = waiting.data?.count ?? 0;
  const settled = unread.isSuccess && waiting.isSuccess;
  const nothingWaiting = settled && hasPublished === true && unreadCount === 0 && waitingCount === 0;
  return (
    <div className="min-w-0">
      <h1 id="landing-title" className="text-2xl wrap-anywhere sm:text-3xl">
        {hasPublished ? "Welcome back" : "Welcome"}, {username}
      </h1>
      {(nothingWaiting || unreadCount > 0 || waitingCount > 0) && (
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
          {nothingWaiting && <li className="text-ink-muted">Nothing is waiting on you.</li>}
        </ul>
      )}
      {hasPublished === false && <FirstStory />}
    </div>
  );
}
