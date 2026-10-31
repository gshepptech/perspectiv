import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "react-router";

import { formatDayBeside } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import type { UserPost } from "@/gen/perspectiv/v1/user_pb";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";

const PAGE_SIZE = 20;

const postedAt = new Intl.DateTimeFormat("en-US", { dateStyle: "medium", timeStyle: "short" });

function PostRow({ post }: { post: UserPost }) {
  const day = [post.arcTitle, post.placeName, formatDayBeside(post.arcTitle, post.localDate, post.hour)].filter(Boolean).join(" · ");
  const at = post.createdAt ? timestampDate(post.createdAt) : undefined;
  return (
    <li className="py-3">
      <Link to={`/lores/${encodeURIComponent(post.loreId)}`} className="group flex min-h-11 min-w-0 flex-col justify-center gap-0.5">
        <span className="font-medium wrap-anywhere text-accent underline-offset-4 group-hover:underline">
          {post.loreName || <em>Unnamed lore</em>}
        </span>
        <span className="text-sm wrap-anywhere text-ink-muted">{day}</span>
        <span className="line-clamp-3 wrap-anywhere text-ink">{post.excerpt}</span>
        <span className="text-sm text-ink-muted">
          {post.reply && "Reply · "}
          {at && <time dateTime={at.toISOString()}>{postedAt.format(at)}</time>}
        </span>
      </Link>
    </li>
  );
}

/**
 * The writer's lore thread posts, newest first, each linking to its lore.
 * The server lists only posts a reader of the lore page sees.
 */
export function ThreadPosts({ userId, username }: { userId: string; username: string }) {
  const list = useInfiniteQuery({
    queryKey: ["user", "listUserPosts", { userId }],
    queryFn: ({ pageParam, signal }) => clients.user.listUserPosts({ userId, pageToken: pageParam, pageSize: PAGE_SIZE }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const posts = list.data?.pages.flatMap((page) => page.posts) ?? [];
  const more = useShowMore(posts.length, list.fetchNextPage);
  return (
    <>
      {list.isPending && (
        <p role="status" className="text-ink-muted">
          Loading…
        </p>
      )}
      {list.isError && (
        <p role="alert" className="text-sm text-error">
          {username}&apos;s thread posts could not be loaded. Try again in a moment.
        </p>
      )}
      {list.isSuccess && posts.length === 0 && <p className="text-ink-muted">{username} has not posted in a lore thread yet.</p>}
      {posts.length > 0 && (
        <ul ref={more.list} aria-label="Thread posts" className="divide-y divide-rule border-y border-rule">
          {posts.map((p) => (
            <PostRow key={p.id} post={p} />
          ))}
        </ul>
      )}
      {list.hasNextPage && (
        <Button variant="secondary" className="mt-3" pending={list.isFetchingNextPage} onClick={more.showMore}>
          Show more
        </Button>
      )}
    </>
  );
}
