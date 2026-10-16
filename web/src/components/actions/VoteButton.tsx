import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowBigUp } from "lucide-react";

import { Button } from "@/components/ui/Button";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";

import { actionError } from "./errors";
import { perspectiveKey, useInvalidateAfterWrite } from "./queries";
import { useWriteGate } from "./useWriteGate";

type Props = {
  perspectiveId: string;
  upvotes: number;
  voted: boolean;
  /** False hides the toggle and leaves the count: the viewer may not vote here. */
  canVote: boolean;
};

/** The up-vote toggle. There is no downvote: pressing it again clears the vote. */
export function VoteButton({ perspectiveId, upvotes, voted, canVote }: Props) {
  const { gate } = useWriteGate();
  const client = useQueryClient();
  const invalidate = useInvalidateAfterWrite();
  const vote = useMutation({
    mutationFn: (on: boolean) => clients.vote.vote({ perspectiveId, on }),
    // The server's answer goes into the cache at once; the refetch that
    // follows brings everything else the vote changed.
    onSuccess: (res) =>
      client.setQueryData<Perspective | null>(perspectiveKey(perspectiveId), (old) =>
        old ? { ...old, voted: res.on, upvotes: res.upvotes } : old,
      ),
    onSettled: () => invalidate(perspectiveId),
  });
  return (
    <div className="flex flex-wrap items-center gap-2">
      {canVote && (
        <Button
          variant="secondary"
          size="sm"
          aria-pressed={voted}
          disabled={vote.isPending}
          className={voted ? "border-accent text-accent" : undefined}
          onClick={() => {
            if (gate()) vote.mutate(!voted);
          }}
        >
          <ArrowBigUp aria-hidden size={16} />
          {voted ? "Remove upvote" : "Upvote"}
        </Button>
      )}
      <span className="text-sm font-bold text-ink">{`${upvotes} ${upvotes === 1 ? "upvote" : "upvotes"}`}</span>
      {vote.isError && (
        <p role="alert" className="w-full text-sm font-medium text-error">
          {actionError(vote.error)}
        </p>
      )}
    </div>
  );
}
