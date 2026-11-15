import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useLocation } from "react-router";

import { LorePostSort } from "@/gen/perspectiv/v1/enums_pb";
import type { ListLorePostsResponse } from "@/gen/perspectiv/v1/lore_pb";

import { postAnchor, threadQuery } from "./threadQueries";

/** The thread's heading, where `#thread` lands and where a missed post falls back to. */
export const THREAD_HEADING_ID = "thread-heading";

/** How many pages of the Top order (20 posts each) are read looking for a post. */
const MAX_THREAD_PAGES = 10;

/** How long a post reached by its anchor keeps its outline. */
const HIGHLIGHT_MS = 2000;

/** How long, in 50ms steps, a post found in the data may take to reach the page. */
const RENDER_TRIES = 20;

type Target = { kind: "thread" } | { kind: "post"; id: string };

/** What the address's hash asks the lore page to land on, if anything. */
export function parseTarget(hash: string): Target | null {
  if (hash === "#thread") return { kind: "thread" };
  // Post ids are UUIDs, so the hash needs no decoding.
  const id = /^#post-(.+)$/.exec(hash)?.[1];
  return id ? { kind: "post", id } : null;
}

/** Whether the loaded pages hold the post, as a top-level post or one of the replies that came with one. */
function holds(pages: readonly ListLorePostsResponse[], id: string): boolean {
  return pages.some((page) => page.posts.some((p) => p.id === id || p.replies.some((r) => r.id === id)));
}

/**
 * Lands the lore page on the post or the thread its address names
 * (`#post-<id>`, `#thread`), once `ready` says everything above the thread
 * has rendered, so the target stays where it was scrolled to.
 *
 * A post not on the thread's first page is looked for in the Top order a
 * page at a time, up to MAX_THREAD_PAGES; the pages it reads are the thread
 * list's own, so they show there too. The post found is scrolled to,
 * focused and outlined for two seconds. A post that cannot be found (gone,
 * among the later replies of a long branch, or past the last page read)
 * lands on the thread's heading instead, and `missed` is true until the
 * address changes.
 */
export function useThreadTarget(loreId: string, ready: boolean): { missed: boolean } {
  const { hash, key: entry } = useLocation();
  // An address typed or edited by hand comes without history state, and
  // every such entry shares the key "default"; the hash tells them apart.
  const key = `${entry}${hash}`;
  const target = parseTarget(hash);
  const kind = target?.kind ?? null;
  const postId = target?.kind === "post" ? target.id : "";
  // The thread list's Top query: the same cache, so no extra request.
  const thread = useInfiniteQuery({ ...threadQuery(loreId, LorePostSort.TOP), enabled: kind !== null && loreId !== "" });
  // The navigation already landed, so a later re-render never scrolls again.
  const handled = useRef("");
  const [missed, setMissed] = useState(false);
  const timers = useRef<number[]>([]);
  useEffect(() => {
    const pending = timers.current;
    return () => pending.forEach((t) => window.clearTimeout(t));
  }, []);

  const pages = thread.data?.pages;
  const settled = !thread.isPending && !thread.isFetchingNextPage;
  useEffect(() => {
    if (kind === null || !ready || !settled || handled.current === key) return;
    const later = (fn: () => void, ms: number) => timers.current.push(window.setTimeout(fn, ms));
    // Landing waits a task: a hash edited in the address bar is followed by
    // the browser's own jump to the fragment, which drops focus when the
    // element it names (the thread's section) cannot take it.
    const toHeading = (miss: boolean) => {
      setMissed(miss);
      later(() => {
        const heading = document.getElementById(THREAD_HEADING_ID);
        heading?.scrollIntoView?.({ block: "start" });
        heading?.focus({ preventScroll: true });
      }, 0);
    };
    if (kind === "thread") {
      handled.current = key;
      toHeading(false);
      return;
    }
    const found = !!pages && holds(pages, postId);
    if (!found && !thread.isError && thread.hasNextPage && (pages?.length ?? 0) < MAX_THREAD_PAGES) {
      void thread.fetchNextPage();
      return;
    }
    handled.current = key;
    if (!found) {
      toHeading(true);
      return;
    }
    setMissed(false);
    // The thread list renders the page that held the post in the same pass
    // or the next; give it a moment before falling back.
    const land = (tries: number) => {
      const post = document.getElementById(postAnchor(postId));
      if (!post) {
        if (tries > 0) later(() => land(tries - 1), 50);
        else toHeading(true);
        return;
      }
      post.scrollIntoView?.({ block: "center" });
      post.focus({ preventScroll: true });
      post.setAttribute("data-highlight", "");
      later(() => post.removeAttribute("data-highlight"), HIGHLIGHT_MS);
    };
    later(() => land(RENDER_TRIES), 0);
  }, [kind, postId, ready, settled, key, pages, thread]);

  return { missed };
}
