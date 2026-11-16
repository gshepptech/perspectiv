import { useQuery } from "@tanstack/react-query";

import { clients } from "@/lib/connect";

/**
 * Under the inbox's own key prefix, so marking notifications read on the
 * inbox page refreshes every count shown elsewhere.
 */
const unreadCountKey = ["notify", "listInbox", "unreadCount"] as const;

/**
 * How many of the signed-in reader's notifications are unread, from the
 * server. The count is re-read every minute while a page shows it, so the
 * header badge never stays on a number the inbox no longer has.
 */
export function useUnreadCount(enabled: boolean) {
  return useQuery({
    queryKey: unreadCountKey,
    queryFn: async ({ signal }) => (await clients.notify.listInbox({ pageSize: 1 }, { signal })).unreadCount,
    enabled,
    refetchInterval: 60_000,
  });
}
