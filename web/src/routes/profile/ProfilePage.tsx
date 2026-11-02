import { Code, ConnectError } from "@connectrpc/connect";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Crown, Users } from "lucide-react";
import type { RefObject } from "react";
import { Link, useParams, useSearchParams } from "react-router";

import { StoryLinkRow } from "@/components/reader/StoryLinkRow";
import { Button } from "@/components/ui/Button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import type { Profile } from "@/gen/perspectiv/v1/user_pb";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";
import { useDocumentTitle } from "@/lib/title";

import { FollowWriter } from "./FollowWriter";
import { ThreadPosts } from "./ThreadPosts";
import { TrackRecord } from "./TrackRecord";

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
          <p className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-sm">
            {profile.canonCount > 0 && (
              <span className="inline-flex items-center gap-1 font-medium text-accent">
                <Crown aria-hidden size={16} />
                In {profile.canonCount} {profile.canonCount === 1 ? "canon" : "canons"}
              </span>
            )}
            <span className="inline-flex items-center gap-1 text-ink-muted">
              <Users aria-hidden size={16} />
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

/**
 * The user's published perspectives, newest first, tales included. The
 * server leaves out drafts and anything deleted or hidden.
 */
function Stories({ userId, username }: { userId: string; username: string }) {
  const list = useInfiniteQuery({
    queryKey: ["perspective", "listByAuthor", { userId }],
    queryFn: ({ pageParam, signal }) => clients.perspective.listByAuthor({ userId, pageToken: pageParam, pageSize: PAGE_SIZE }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const count = list.data?.pages.reduce((n, page) => n + page.perspectives.length, 0) ?? 0;
  const more = useShowMore(count, list.fetchNextPage);
  return (
    <>
      {list.isPending && (
        <p role="status" className="text-ink-muted">
          Loading…
        </p>
      )}
      {list.isError && (
        <p role="alert" className="text-sm text-error">
          {username}&apos;s stories could not be loaded. Try again in a moment.
        </p>
      )}
      {list.isSuccess && <WritingList pages={list.data.pages} username={username} listRef={more.list} />}
      {list.hasNextPage && (
        <Button variant="secondary" className="mt-3" pending={list.isFetchingNextPage} onClick={more.showMore}>
          Show more
        </Button>
      )}
    </>
  );
}

/** The profile's tabs, by their value in the URL's `tab`; Stories has none. */
const STORIES = "stories";
const POSTS = "posts";

/**
 * The writer's work in two tabs, Stories and Thread posts. The current tab
 * lives in the URL (`?tab=posts`), replaced rather than pushed, and a panel
 * loads its list only once it is shown.
 */
function Writing({ userId, username }: { userId: string; username: string }) {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") === POSTS ? POSTS : STORIES;
  function select(value: string) {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === POSTS) next.set("tab", POSTS);
        else next.delete("tab");
        return next;
      },
      { replace: true },
    );
  }
  return (
    <section aria-labelledby="profile-writing" className="mt-8">
      <h2 id="profile-writing" className="sr-only">
        {username}&apos;s writing
      </h2>
      <Tabs value={tab} onValueChange={select}>
        <TabsList aria-labelledby="profile-writing">
          <TabsTrigger value={STORIES}>Stories</TabsTrigger>
          <TabsTrigger value={POSTS}>Thread posts</TabsTrigger>
        </TabsList>
        <TabsContent value={STORIES}>
          <Stories userId={userId} username={username} />
        </TabsContent>
        <TabsContent value={POSTS}>
          <ThreadPosts userId={userId} username={username} />
        </TabsContent>
      </Tabs>
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
  if (items.length === 0) return <p className="text-ink-muted">{username} has not published a story yet.</p>;
  return (
    // The tab row's hairline is the rule above the first row (Folio, Profile).
    <ul ref={listRef} aria-label="Stories" className="divide-y divide-rule border-b border-rule">
      {items.map((p) => (
        <StoryLinkRow key={p.id} p={p} />
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
      <TrackRecord profile={profile} />
      <Writing userId={profile.userId} username={profile.username} />
    </article>
  );
}
