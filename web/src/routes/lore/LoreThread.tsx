import { useInfiniteQuery, useMutation } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState } from "react";
import { useLocation } from "react-router";

import type { ArcSpan } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { LorePostSort } from "@/gen/perspectiv/v1/enums_pb";
import type { LorePost } from "@/gen/perspectiv/v1/lore_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";
import { useShowMore } from "@/lib/showMore";

import { PostForm } from "./PostForm";
import { ThreadBranch } from "./ThreadBranch";
import { threadQuery, useInvalidateThread } from "./threadQueries";
import { useFocusPost, useWritten, withWritten } from "./written";

const sorts = { top: LorePostSort.TOP, new: LorePostSort.NEW } as const;
type SortName = keyof typeof sorts;

/** What the hidden status says once the reader picks an order. */
const sortSaid: Record<SortName, string> = { top: "Most upvoted first.", new: "Newest first." };

/**
 * The lore's thread: the people in it talking about the day. Anyone can
 * read it, best first or newest first; someone with a perspective in the
 * arc can post, reply and upvote. The server enforces who may; hiding the
 * controls is usability only.
 */
export function LoreThread({ loreId, canPost, span = "day" }: { loreId: string; canPost: boolean; span?: ArcSpan }) {
  const headingId = useId();
  const heading = useRef<HTMLHeadingElement>(null);
  const section = useRef<HTMLElement>(null);
  const [sort, setSort] = useState<SortName>("top");
  // Empty until the reader picks an order, so arriving announces nothing.
  const [sortStatus, setSortStatus] = useState("");
  // A new post shows at once and takes focus, as a new reply does.
  const written = useWritten();
  const focusPost = useFocusPost(section);
  const invalidate = useInvalidateThread();
  const add = useMutation({
    mutationFn: (body: string) => clients.lore.addLorePost({ loreId, body }),
    onSuccess: async (res) => {
      if (res.post) {
        written.add(res.post);
        focusPost(res.post.id);
      }
      await invalidate();
    },
  });
  // A deleted post can take the focused control with it; the heading keeps
  // the reader's place.
  const onGone = (id: string) => {
    written.gone(id);
    heading.current?.focus();
  };
  // "Read its thread" at the end of the lore's book links here as #thread:
  // focusing the heading brings the thread into view and starts a screen
  // reader there.
  const { hash } = useLocation();
  useEffect(() => {
    if (hash === "#thread") heading.current?.focus();
  }, [hash]);

  return (
    <section ref={section} id="thread" aria-labelledby={headingId} className="mt-10 border-t border-rule pt-8">
      <h2 id={headingId} ref={heading} tabIndex={-1} className="text-xl">
        Thread
      </h2>
      <p role="status" className="sr-only">
        {sortStatus}
      </p>
      <Tabs
        value={sort}
        onValueChange={(v) => {
          setSort(v as SortName);
          setSortStatus(sortSaid[v as SortName]);
        }}
        className="mt-3"
      >
        <TabsList aria-label="Sort posts">
          <TabsTrigger value="top">Top</TabsTrigger>
          <TabsTrigger value="new">New</TabsTrigger>
        </TabsList>
        {(Object.keys(sorts) as SortName[]).map((name) => (
          <TabsContent key={name} value={name}>
            <ThreadList
              loreId={loreId}
              sort={sorts[name]}
              canPost={canPost}
              written={written.written}
              onGone={onGone}
              onEdited={written.edited}
            />
          </TabsContent>
        ))}
      </Tabs>
      {canPost ? (
        <div className="mt-6">
          <PostForm
            label="Add to the thread"
            submitLabel="Post"
            pendingLabel="Posting…"
            send={(body, done) => add.mutate(body, { onSuccess: done })}
            pending={add.isPending}
            error={add.isError ? add.error : null}
          />
        </div>
      ) : (
        <p className="mt-6 text-sm text-ink-muted">Anyone can read this thread. Only the people who wrote about this {span} can post in it.</p>
      )}
    </section>
  );
}

type ListProps = {
  loreId: string;
  sort: LorePostSort;
  canPost: boolean;
  /** Posts the viewer wrote here, shown at the end until a read brings them. */
  written: LorePost[];
  onGone: (id: string) => void;
  onEdited: (post: LorePost) => void;
};

function ThreadList({ loreId, sort, canPost, written, onGone, onEdited }: ListProps) {
  const session = useSession();
  const posts = useInfiniteQuery(threadQuery(loreId, sort));
  const loaded = posts.data?.pages.flatMap((p) => p.posts) ?? [];
  const items = posts.isSuccess ? withWritten(loaded, written) : loaded;
  // A new page lands after the loaded posts, ahead of any written ones.
  const more = useShowMore<HTMLOListElement>(loaded.length, posts.fetchNextPage);
  return (
    <>
      {posts.isError && (
        <p role="alert" className="text-sm text-error">
          The thread could not be loaded. Try again in a moment.
        </p>
      )}
      {posts.isSuccess && items.length === 0 && (
        <p className="text-ink-muted">{canPost ? "Nobody has said anything yet. Start the conversation." : "Nobody has posted yet."}</p>
      )}
      {items.length > 0 && (
        <ol ref={more.list} aria-label="Posts" className="grid gap-6">
          {items.map((p) => (
            <li key={p.id} className="min-w-0">
              <ThreadBranch top={p} loreId={loreId} canPost={canPost} viewerId={session?.userId} onGone={onGone} onEdited={onEdited} />
            </li>
          ))}
        </ol>
      )}
      {posts.hasNextPage && (
        <Button variant="ghost" size="sm" className="mt-3 min-h-11" pending={posts.isFetchingNextPage} onClick={more.showMore}>
          More posts
        </Button>
      )}
    </>
  );
}
