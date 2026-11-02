import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Bell, BellOff } from "lucide-react";

import { actionError } from "@/components/actions/errors";
import { useWriteGate } from "@/components/actions/useWriteGate";
import { Button } from "@/components/ui/Button";
import type { GetPlaceResponse } from "@/gen/perspectiv/v1/place_pb";
import { clients } from "@/lib/connect";

/**
 * Follow a place: a new story at it, or anywhere under it, lands in the
 * follower's inbox. A signed-out reader is sent to sign in first.
 */
export function FollowButton({ placeId, following }: { placeId: string; following: boolean }) {
  const { gate } = useWriteGate();
  const client = useQueryClient();
  const key = ["place", "getPlace", { placeId }];
  const follow = useMutation({
    mutationFn: (on: boolean) => clients.place.followPlace({ placeId, on }),
    onSuccess: (res) => {
      client.setQueryData<GetPlaceResponse>(key, (old) =>
        old?.place ? { ...old, place: { ...old.place, following: res.following } } : old,
      );
    },
    onSettled: () => Promise.all([client.invalidateQueries({ queryKey: key }), client.invalidateQueries({ queryKey: ["place", "listFollowing"] })]),
  });
  return (
    <>
      <Button
        variant={following ? "secondary" : "ghost"}
        // One size with the place page's Report and Add (Folio, Place page header).
        size="sm"
        className="min-h-11"
        aria-pressed={following}
        pending={follow.isPending}
        onClick={() => {
          if (gate()) follow.mutate(!following);
        }}
      >
        {following ? <BellOff aria-hidden size={16} /> : <Bell aria-hidden size={16} />}
        {following ? "Following" : "Follow"}
      </Button>
      {follow.isError && (
        <p role="alert" className="w-full text-sm text-error">
          {actionError(follow.error)}
        </p>
      )}
    </>
  );
}
