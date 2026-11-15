import { useQueryClient, type QueryClient } from "@tanstack/react-query";

import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";

export const perspectiveKey = (perspectiveId: string) => ["perspective", "get", { perspectiveId }] as const;

/** The full perspective: the caller's vote, react counts and characters. */
export function perspectiveQuery(perspectiveId: string) {
  return {
    queryKey: perspectiveKey(perspectiveId),
    queryFn: async ({ signal }: { signal: AbortSignal }): Promise<Perspective | null> =>
      (await clients.perspective.get({ perspectiveId }, { signal })).perspective ?? null,
  };
}

/** Every page of the caller's Saved stories, as the Saved page reads them. */
export const savedListKey = ["perspective", "listSaved"] as const;

const arcKey = (arcId: string) => ["arc", "getArc", { arcId }] as const;

/**
 * An arc's page as the arc page reads it: the arc, its canon and the first
 * page of every list. The story page reads the same entry for what follows
 * the story, so the two share one request.
 */
export function arcQuery(arcId: string) {
  return {
    queryKey: arcKey(arcId),
    queryFn: ({ signal }: { signal: AbortSignal }) => clients.arc.getArc({ arcId }, { signal }),
  };
}

/**
 * After a write that changes a perspective's counts or lore standing, both the
 * perspective and every arc listing that shows it are stale.
 */
export function invalidateAfterWrite(client: QueryClient, perspectiveId: string): Promise<void> {
  return Promise.all([
    client.invalidateQueries({ queryKey: perspectiveKey(perspectiveId) }),
    client.invalidateQueries({ queryKey: ["arc"] }),
  ]).then(() => undefined);
}

export function useInvalidateAfterWrite() {
  const client = useQueryClient();
  return (perspectiveId: string) => invalidateAfterWrite(client, perspectiveId);
}
