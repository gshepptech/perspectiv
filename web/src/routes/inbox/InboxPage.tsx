import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import type { Notification } from "@/gen/perspectiv/v1/notify_pb";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";
import { useDocumentTitle } from "@/lib/title";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";

import { describeNotification } from "./describe";

const when = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function Row({ n, onOpen }: { n: Notification; onOpen: (n: Notification) => void }) {
  const { text, to, detail } = describeNotification(n);
  const unread = !n.readAt;
  const at = n.createdAt ? timestampDate(n.createdAt) : null;
  const body = (
    <>
      <span className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-1">
        {unread && <span className="rounded-sm bg-accent px-1.5 text-sm font-medium text-accent-ink">New</span>}
        <span
          className={cn(
            "min-w-0 break-words text-ink",
            to && "underline-offset-4 group-hover:underline group-focus-visible:underline",
            unread && "font-medium",
          )}
        >
          {text}
        </span>
      </span>
      {detail && <span className="mt-1 block text-sm break-words text-ink-muted">{detail}</span>}
      {at && (
        <time dateTime={at.toISOString()} className="mt-1 block text-sm text-ink-muted">
          {when.format(at)}
        </time>
      )}
    </>
  );
  const row = cn("block min-h-11 min-w-0 border-l-2 py-3 pl-3", unread ? "border-accent" : "border-transparent");
  return (
    <li className="min-w-0">
      {to ? (
        // The whole row is the link. Following it any way (a click, Enter,
        // a middle click into a new tab) marks the notification read.
        <Link to={to} onClick={() => onOpen(n)} onAuxClick={() => onOpen(n)} className={cn(row, "group")}>
          {body}
        </Link>
      ) : (
        <div className={row}>{body}</div>
      )}
    </li>
  );
}

/**
 * The caller's inbox, newest first, a page at a time with the server's
 * cursor. Opening an unread notification marks it read; "Mark all read" marks
 * every one. After either, the loaded pages are fetched again so the list and
 * the unread count come from the server, not from a local guess.
 */
export function Component() {
  useDocumentTitle("Inbox");
  const queryClient = useQueryClient();
  const [unreadOnly, setUnreadOnly] = useState(false);
  const inbox = useInfiniteQuery({
    queryKey: ["notify", "listInbox", { unreadOnly }],
    queryFn: ({ pageParam }) => clients.notify.listInbox({ pageToken: pageParam, unreadOnly }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const markRead = useMutation({
    mutationFn: (ids: string[] | "all") =>
      clients.notify.markRead(ids === "all" ? { all: true } : { notificationIds: ids }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["notify", "listInbox"] }),
  });

  const notifications = inbox.data?.pages.flatMap((p) => p.notifications) ?? [];
  const more = useShowMore(notifications.length, inbox.fetchNextPage);
  const unreadCount = inbox.data?.pages[0]?.unreadCount ?? 0;
  const open = (n: Notification) => {
    if (!n.readAt) markRead.mutate([n.id]);
  };

  return (
    <div className="mx-auto w-full max-w-2xl min-w-0">
      <div className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
        <h1 className="text-2xl sm:text-3xl">Inbox</h1>
        {inbox.isSuccess && <p className="text-sm text-ink-muted">{unreadCount} unread</p>}
      </div>
      <div className="mt-4 flex min-w-0 flex-wrap items-center justify-between gap-2">
        <div role="group" aria-label="Show" className="flex gap-1">
          {[
            { label: "All", value: false },
            { label: "Unread", value: true },
          ].map((f) => (
            <Button
              key={f.label}
              size="sm"
              variant="ghost"
              aria-pressed={unreadOnly === f.value}
              onClick={() => setUnreadOnly(f.value)}
              className={cn("min-h-11 rounded-none border-b-2", unreadOnly === f.value ? "border-accent" : "border-transparent")}
            >
              {f.label}
            </Button>
          ))}
        </div>
        <Button
          size="sm"
          variant="secondary"
          className="min-h-11"
          disabled={!inbox.isSuccess || unreadCount === 0}
          pending={markRead.isPending}
          onClick={() => markRead.mutate("all")}
        >
          Mark all read
        </Button>
      </div>
      <div className="mt-4">
        <FormAlert message={markRead.isError ? errorMessage(markRead.error) : null} />
      </div>
      {inbox.isPending && (
        <p role="status" className="mt-6 text-ink-muted">
          Loading your inbox…
        </p>
      )}
      {inbox.isError && (
        <div className="mt-6">
          <FormAlert message={errorMessage(inbox.error)} />
        </div>
      )}
      {inbox.isSuccess && notifications.length === 0 && (
        <p className="mt-6 text-ink-muted">
          {unreadOnly
            ? "No unread notifications."
            : "Nothing here yet. When someone claims your characters or your day changes, it shows up here."}
        </p>
      )}
      {notifications.length > 0 && (
        <ul ref={more.list} aria-label="Notifications" className="mt-2 divide-y divide-rule border-y border-rule">
          {notifications.map((n) => (
            <Row key={n.id} n={n} onOpen={open} />
          ))}
        </ul>
      )}
      {inbox.hasNextPage && (
        <Button
          variant="secondary"
          className="mt-4"
          pending={inbox.isFetchingNextPage}
          onClick={more.showMore}
        >
          Show older
        </Button>
      )}
    </div>
  );
}
