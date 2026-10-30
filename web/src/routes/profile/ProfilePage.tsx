import { Code, ConnectError } from "@connectrpc/connect";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Crown } from "lucide-react";
import type { RefObject } from "react";
import { Link, useParams } from "react-router";

import { formatDayBeside, loreStatusLabel } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import type { Profile } from "@/gen/perspectiv/v1/user_pb";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";
import { useDocumentTitle } from "@/lib/title";

import { FollowWriter } from "./FollowWriter";

const joinedFormat = new Intl.DateTimeFormat("en-US", { month: "long", year: "numeric", timeZone: "UTC" });

function Missing() {
  return (
    <section className="mx-auto max-w-measure py-12" aria-labelledby="profile-missing">
      <h1 id="profile-missing" className="text-2xl sm:text-3xl">
        User not found
      </h1>
      <p className="mt-3 text-ink-muted">No one goes by that username.</p>
      <p className="mt-6">
        <Link to="/" className="text-accent underline underline-offset-4">
          Search for a place
        </Link>
      </p>
    </section>
  );
}

function Header({ profile }: { profile: Profile }) {
  return (
    <header className="flex min-w-0 flex-wrap items-center gap-4">
      {profile.avatarUrl && (
        <img
          src={profile.avatarUrl}
          alt={`${profile.username}'s avatar`}
          className="size-20 shrink-0 rounded-full border border-rule object-cover"
        />
      )}
      <div className="min-w-0">
        <h1 className="text-2xl wrap-anywhere sm:text-3xl">{profile.username}</h1>
        {profile.joinedAt && (
          <p className="mt-1 text-sm text-ink-muted">Joined {joinedFormat.format(timestampDate(profile.joinedAt))}</p>
        )}
        {(profile.canonCount > 0 || profile.loreCount > 0) && (
          <p className="mt-2 flex flex-wrap gap-2 text-sm">
            {profile.canonCount > 0 && (
              <span className="inline-flex items-center gap-1 rounded-full bg-accent/10 px-3 py-0.5 font-medium text-accent">
                <Crown aria-hidden size={14} />
                In {profile.canonCount} {profile.canonCount === 1 ? "canon" : "canons"}
              </span>
            )}
            <span className="inline-flex items-center rounded-full border border-rule px-3 py-0.5 text-ink-muted">
              In {profile.loreCount} {profile.loreCount === 1 ? "lore" : "lores"}
            </span>
          </p>
        )}
        {profile.suspended && <p className="mt-1 text-sm text-warning">This account is suspended.</p>}
      </div>
    </header>
  );
}

const PAGE_SIZE = 20;

/** How much of a story's first part a row quotes; the row clamps it to two lines. */
const OPENING_CHARS = 240;

/** The story's own opening: its first part, whitespace folded, cut at a word. */
function opening(p: Perspective): string {
  const first = [...p.parts].sort((a, b) => a.position - b.position)[0]?.body ?? "";
  const text = first.replace(/\s+/g, " ").trim();
  if (text.length <= OPENING_CHARS) return text;
  const cut = text.slice(0, OPENING_CHARS);
  return `${cut.slice(0, cut.lastIndexOf(" ") > 0 ? cut.lastIndexOf(" ") : OPENING_CHARS)}…`;
}

/**
 * One story on a profile: where and when first, then the day's title, then
 * the story's own opening, so stories whose days share a title still differ.
 */
function WritingRow({ p }: { p: Perspective }) {
  const title = p.arcTitle || "Untitled";
  const where = [p.place?.name, formatDayBeside(p.arcTitle, p.localDate, p.hour)].filter(Boolean).join(" · ");
  const standing = loreStatusLabel(p.loreStatus);
  const start = opening(p);
  return (
    <li className="py-3">
      <Link to={`/perspectives/${encodeURIComponent(p.id)}`} className="group flex min-h-11 min-w-0 flex-col justify-center gap-0.5">
        <span className="text-sm font-medium wrap-anywhere text-ink">{where}</span>
        <span className="font-serif text-lg wrap-anywhere text-accent underline-offset-4 group-hover:underline">{title}</span>
        {start && <span className="line-clamp-2 font-serif wrap-anywhere text-ink-muted">{start}</span>}
        {standing && <span className="text-sm text-ink-muted">{standing}</span>}
      </Link>
    </li>
  );
}

/**
 * The user's published perspectives, newest first, tales included. The
 * server leaves out drafts and anything deleted or hidden.
 */
function Writing({ userId, username }: { userId: string; username: string }) {
  const list = useInfiniteQuery({
    queryKey: ["perspective", "listByAuthor", { userId }],
    queryFn: ({ pageParam, signal }) => clients.perspective.listByAuthor({ userId, pageToken: pageParam, pageSize: PAGE_SIZE }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const count = list.data?.pages.reduce((n, page) => n + page.perspectives.length, 0) ?? 0;
  const more = useShowMore(count, list.fetchNextPage);
  return (
    <section aria-labelledby="profile-writing" className="mt-10">
      <h2 id="profile-writing" className="text-xl">
        Perspectives
      </h2>
      {list.isPending && (
        <p role="status" className="mt-3 text-ink-muted">
          Loading…
        </p>
      )}
      {list.isError && (
        <p role="alert" className="mt-3 text-sm text-error">
          {username}&apos;s perspectives could not be loaded. Try again in a moment.
        </p>
      )}
      {list.isSuccess && <WritingList pages={list.data.pages} username={username} listRef={more.list} />}
      {list.hasNextPage && (
        <Button variant="secondary" className="mt-3" pending={list.isFetchingNextPage} onClick={more.showMore}>
          Show more
        </Button>
      )}
    </section>
  );
}

function WritingList({
  pages,
  username,
  listRef,
}: {
  pages: readonly { perspectives: Perspective[] }[];
  username: string;
  listRef: RefObject<HTMLUListElement | null>;
}) {
  const items = pages.flatMap((page) => page.perspectives);
  if (items.length === 0) return <p className="mt-3 text-ink-muted">{username} has not published a perspective yet.</p>;
  return (
    <ul ref={listRef} className="mt-3 divide-y divide-rule border-y border-rule">
      {items.map((p) => (
        <WritingRow key={p.id} p={p} />
      ))}
    </ul>
  );
}

/**
 * A user's public profile, looked up by username. It shows only the public
 * Profile message; the Account (and its email) the server returns for the
 * caller's own profile is never rendered here.
 */
export function Component() {
  const { username = "" } = useParams();
  const query = useQuery({
    queryKey: ["user", "getProfile", { username }],
    queryFn: ({ signal }) => clients.user.getProfile({ username }, { signal }),
  });
  useDocumentTitle(query.isError ? "User not found" : query.data?.profile?.username);

  if (query.isPending) {
    return (
      <p role="status" className="mx-auto max-w-measure text-ink-muted">
        Loading…
      </p>
    );
  }
  if (query.isError) {
    if (ConnectError.from(query.error).code === Code.NotFound) return <Missing />;
    return (
      <p role="alert" className="mx-auto max-w-measure text-error">
        This profile could not be loaded. Try again in a moment.
      </p>
    );
  }
  const profile = query.data.profile;
  if (!profile) return <Missing />;
  if (profile.deleted) {
    return (
      <section className="mx-auto max-w-measure py-12" aria-labelledby="profile-gone">
        <h1 id="profile-gone" className="text-2xl sm:text-3xl">
          {username}
        </h1>
        <p className="mt-3 text-ink-muted">This account was deleted.</p>
      </section>
    );
  }
  return (
    <article className="mx-auto min-w-0 max-w-measure">
      <Header profile={profile} />
      <FollowWriter
        lookup={username}
        userId={profile.userId}
        username={profile.username}
        following={query.data.following}
        followers={query.data.followers}
      />
      {profile.bio && <p className="mt-6 whitespace-pre-line wrap-anywhere">{profile.bio}</p>}
      <Writing userId={profile.userId} username={profile.username} />
    </article>
  );
}
