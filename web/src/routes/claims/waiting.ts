import { useQuery } from "@tanstack/react-query";

import { ClaimDirection, ClaimStatus } from "@/gen/perspectiv/v1/enums_pb";
import { clients } from "@/lib/connect";

/** Under the requests page's key prefix, so accepting or rejecting one refreshes this count. */
const waitingRequestsKey = ["lore", "listClaims", "waiting"] as const;

/** The most requests one read counts; more than this reads "100+". */
const COUNTED = 100;

/**
 * Requests on the reader's characters still waiting for them to accept or
 * reject. ListClaims has no total, so one page of up to 100 is counted and
 * `more` says the list goes on. Re-read every minute while shown, like the
 * inbox count, so the header badge never stays on a stale number.
 */
export function useWaitingRequests(enabled: boolean) {
  return useQuery({
    queryKey: waitingRequestsKey,
    queryFn: async ({ signal }) => {
      const res = await clients.lore.listClaims(
        { direction: ClaimDirection.INCOMING, status: ClaimStatus.PENDING, pageSize: COUNTED },
        { signal },
      );
      return { count: res.claims.length, more: res.nextPageToken !== "" };
    },
    enabled,
    refetchInterval: 60_000,
  });
}
