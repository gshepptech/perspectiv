import { useQuery } from "@tanstack/react-query";

import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";

import type { ViewerRights } from "./PerspectiveActions";

/** Members a lore needs before its members may vote one out (CANON_MIN_MEMBERS). */
export const CANON_MIN_MEMBERS = 4;

/**
 * The viewer's rights on one story, for a page that shows the story alone:
 * whether they wrote in its arc, and whether they share its lore once that
 * lore has enough members to vote one out. A signed-out reader has neither
 * and nothing is fetched.
 */
export function useStoryRights(arcId: string, loreId: string): ViewerRights {
  const session = useSession();
  const arc = useQuery({
    queryKey: ["arc", "getArc", { arcId, pageSize: 1 }],
    queryFn: ({ signal }) => clients.arc.getArc({ arcId, pageSize: 1 }, { signal }),
    enabled: !!session && arcId !== "",
  });
  const viewer = arc.data?.viewer;
  const sameLore = !!session && !!loreId && viewer?.loreId === loreId;
  const lore = useQuery({
    queryKey: ["lore", "getLore", { loreId }],
    queryFn: ({ signal }) => clients.lore.getLore({ loreId }, { signal }),
    enabled: sameLore,
  });
  return {
    participant: !!session && !!viewer?.perspectiveId,
    loreVoter: sameLore && (lore.data?.lore?.memberCount ?? 0) >= CANON_MIN_MEMBERS,
    pending: !!session && arcId !== "" && arc.isPending,
  };
}
