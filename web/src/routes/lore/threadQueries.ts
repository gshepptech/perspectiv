import { infiniteQueryOptions, useQueryClient } from "@tanstack/react-query";

import type { LorePostSort } from "@/gen/perspectiv/v1/enums_pb";
import { clients } from "@/lib/connect";

/** Every page of a lore's thread in one order. */
export function threadQuery(loreId: string, sort: LorePostSort) {
  return infiniteQueryOptions({
    queryKey: ["lore", "listLorePosts", { loreId, sort }] as const,
    queryFn: ({ pageParam, signal }) => clients.lore.listLorePosts({ loreId, sort, pageToken: pageParam, pageSize: 20 }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
}

/** The replies under a post after the ones that came inline with it, from `from`. */
export function repliesQuery(postId: string, from: string) {
  return infiniteQueryOptions({
    queryKey: ["lore", "listLorePostReplies", { postId, from }] as const,
    queryFn: ({ pageParam, signal }) => clients.lore.listLorePostReplies({ postId, pageToken: pageParam, pageSize: 20 }, { signal }),
    initialPageParam: from,
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
}

/** After any write to a thread, every order of it and every opened reply page is stale. */
export function useInvalidateThread(): () => Promise<void> {
  const client = useQueryClient();
  return async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ["lore", "listLorePosts"] }),
      client.invalidateQueries({ queryKey: ["lore", "listLorePostReplies"] }),
    ]);
  };
}
