import { useQueryClient, type QueryClient } from "@tanstack/react-query";

import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";

export const perspectiveKey = (perspectiveId: string) => ["perspective", "get", { perspectiveId }] as const;

/** The full perspective: the caller's vote, react counts and characters. */
export function perspectiveQuery(perspectiveId: string) {
  return {
    queryKey: perspectiveKey(perspectiveId),
    queryFn: async (): Promise<Perspective | null> => (await clients.perspective.get({ perspectiveId })).perspective ?? null,
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
