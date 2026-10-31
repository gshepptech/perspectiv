import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Button } from "@/components/ui/Button";
import type { LorePost } from "@/gen/perspectiv/v1/lore_pb";
import { useShowMore } from "@/lib/showMore";

import { repliesQuery } from "./threadQueries";
import { ThreadPost } from "./ThreadPost";
import { useFocusPost, useWritten, withWritten } from "./written";

type Props = {
  top: LorePost;
  loreId: string;
  canPost: boolean;
  viewerId: string | undefined;
  /** The viewer deleted the top post or one of its replies. */
  onGone: (id: string) => void;
  /** The viewer edited the top post. */
  onEdited: (post: LorePost) => void;
};

/**
 * A top-level post and the replies under it, one step in with a left rule,
 * oldest first: the first three as they came with the post, the rest a page
 * at a time behind "Show N more replies". A reply the viewer writes here
 * shows at once, at the end, and takes focus.
 */
export function ThreadBranch({ top, loreId, canPost, viewerId, onGone, onEdited }: Props) {
  const client = useQueryClient();
  const written = useWritten();
  const [expanded, setExpanded] = useState(false);
  const options = repliesQuery(top.id, top.moreRepliesToken);
  const more = useInfiniteQuery({ ...options, enabled: expanded && top.moreRepliesToken !== "" });

  const loaded = dedupe([...top.replies, ...(more.data?.pages.flatMap((p) => p.replies) ?? [])]);
  const replies = withWritten(loaded, written.written);
  const remaining = top.moreRepliesToken === "" ? 0 : Math.max(0, top.replyCount - replies.length);

  const fetchMore = (): Promise<{ isError: boolean }> => {
    if (!expanded) {
      setExpanded(true);
      return client.fetchInfiniteQuery(options).then(
        () => ({ isError: false }),
        () => ({ isError: true }),
      );
    }
    return more.hasNextPage ? more.fetchNextPage() : more.refetch();
  };
  const show = useShowMore<HTMLOListElement>(loaded.length, fetchMore);
  const focusPost = useFocusPost(show.list);

  const replied = (reply: LorePost) => {
    written.add(reply);
    focusPost(reply.id);
  };

  return (
    <>
      <ThreadPost
        post={top}
        loreId={loreId}
        canPost={canPost}
        viewerId={viewerId}
        onReplied={replied}
        onEdited={onEdited}
        onGone={() => onGone(top.id)}
      />
      {replies.length > 0 && (
        <ol ref={show.list} aria-label={`Replies to ${top.author?.username ?? "a deleted post"}`} className="mt-3 grid gap-3 border-l-2 border-rule pl-4">
          {replies.map((r) => (
            <li key={r.id} className="min-w-0">
              <ThreadPost
                post={r}
                loreId={loreId}
                canPost={canPost}
                viewerId={viewerId}
                onReplied={replied}
                onEdited={written.edited}
                onGone={() => {
                  written.gone(r.id);
                  onGone(r.id);
                }}
              />
            </li>
          ))}
        </ol>
      )}
      {remaining > 0 && (
        <Button variant="ghost" size="sm" className="mt-1 ml-4 min-h-11" pending={more.isFetching} onClick={show.showMore}>
          Show {remaining} more {remaining === 1 ? "reply" : "replies"}
        </Button>
      )}
      {more.isError && (
        <p role="alert" className="mt-1 ml-4 text-sm text-error">
          The replies could not be loaded. Try again in a moment.
        </p>
      )}
    </>
  );
}

function dedupe(posts: LorePost[]): LorePost[] {
  const seen = new Set<string>();
  return posts.filter((p) => {
    if (seen.has(p.id)) return false;
    seen.add(p.id);
    return true;
  });
}
