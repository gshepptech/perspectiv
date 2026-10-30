import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Bell, BellOff } from "lucide-react";

import { actionError } from "@/components/actions/errors";
import { useWriteGate } from "@/components/actions/useWriteGate";
import { Button } from "@/components/ui/Button";
import type { GetProfileResponse } from "@/gen/perspectiv/v1/user_pb";
import { clients } from "@/lib/connect";
import { followedWritersKey } from "@/routes/settings/queries";

function followerCount(n: number): string {
  return `${n} ${n === 1 ? "follower" : "followers"}`;
}

/**
 * A writer's follower count, and for someone else's live profile the Follow
 * toggle: each new story they write lands in the follower's inbox. A
 * signed-out reader who presses it is sent to sign in first.
 */
export function FollowWriter({
  lookup,
  userId,
  username,
  following,
  followers,
}: {
  /** The username the page looked the profile up by: its query key. */
  lookup: string;
  userId: string;
  username: string;
  following: boolean;
  followers: number;
}) {
  const { session, gate } = useWriteGate();
  const client = useQueryClient();
  const key = ["user", "getProfile", { username: lookup }];
  const follow = useMutation({
    mutationFn: (on: boolean) => clients.user.followUser({ username, on }),
    onSuccess: (res) => {
      client.setQueryData<GetProfileResponse>(key, (old) =>
        old && old.following !== res.following
          ? { ...old, following: res.following, followers: Math.max(0, old.followers + (res.following ? 1 : -1)) }
          : old,
      );
    },
    onSettled: () => Promise.all([client.invalidateQueries({ queryKey: key }), client.invalidateQueries({ queryKey: followedWritersKey })]),
  });
  const own = session?.userId === userId;
  return (
    <div className="mt-4 flex min-w-0 flex-wrap items-center gap-3">
      {!own && (
        <Button
          variant={following ? "secondary" : "ghost"}
          aria-pressed={following}
          aria-label={`${following ? "Following" : "Follow"} ${username}`}
          pending={follow.isPending}
          onClick={() => {
            if (gate()) follow.mutate(!following);
          }}
        >
          {following ? <BellOff aria-hidden size={16} /> : <Bell aria-hidden size={16} />}
          {following ? "Following" : "Follow"}
        </Button>
      )}
      <p className="text-sm text-ink-muted">{followerCount(followers)}</p>
      {follow.isError && (
        <p role="alert" className="w-full text-sm text-error">
          {actionError(follow.error)}
        </p>
      )}
    </div>
  );
}
