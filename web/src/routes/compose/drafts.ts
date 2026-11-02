import { useQuery } from "@tanstack/react-query";

import { PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import { clients } from "@/lib/connect";

import { isLocalDate } from "./localDate";

export const RESUMED_NOTICE = "You started a draft for this day, so it is open again.";

export const RESUMED_NEWEST_NOTICE =
  "You started more than one draft for this day, so the one you changed last is open. The others are under Your stories on the front page.";

/** The note the composer opens with after an emptied draft is deleted. */
export const DELETED_NOTICE = "Your empty draft is deleted.";

/** The note shown when a draft is resumed: whether it was one of several. */
export function resumedNotice(found: number): string {
  return found > 1 ? RESUMED_NEWEST_NOTICE : RESUMED_NOTICE;
}

/**
 * The writer's drafts for one place, day and hour, most recently changed
 * first (ListMine's order). Two at most are read: the newest is the one to
 * resume, and a second only says there are several. Read fresh every time,
 * so a draft saved in another tab is found.
 */
export function useDraftsFor(placeId: string, localDate: string | undefined, hour: number | undefined, enabled = true) {
  return useQuery({
    queryKey: ["perspective", "listMine", { status: PerspectiveStatus.DRAFT, placeId, localDate, hour }],
    queryFn: async ({ signal }) => {
      const res = await clients.perspective.listMine(
        { status: PerspectiveStatus.DRAFT, placeId, localDate, hour, pageSize: 2 },
        { signal },
      );
      return res.perspectives;
    },
    enabled: enabled && isLocalDate(localDate),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
}
