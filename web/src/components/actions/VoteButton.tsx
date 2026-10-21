import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowBigUp } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/cn";
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
  /**
   * "rail" is the compact column at the left of a story row on the arc page:
   * an icon toggle over the count. The default is the full button and label.
   */
  variant?: "bar" | "rail";
};

/** The up-vote toggle. There is no downvote: pressing it again clears the vote. */
export function VoteButton({ perspectiveId, upvotes, voted, canVote, variant = "bar" }: Props) {
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
  const count = `${upvotes} ${upvotes === 1 ? "upvote" : "upvotes"}`;
  if (variant === "rail") {
    return (
      <div className="flex w-12 flex-col items-center gap-0.5">
        {canVote ? (
          <button
            type="button"
            aria-pressed={voted}
            aria-label={voted ? "Remove upvote" : "Upvote"}
            disabled={vote.isPending}
            className={cn(
              "inline-flex size-11 items-center justify-center rounded-md hover:bg-surface disabled:opacity-60",
              voted ? "text-accent" : "text-ink-muted",
            )}
            onClick={() => {
              if (gate()) vote.mutate(!voted);
            }}
          >
            <ArrowBigUp aria-hidden size={22} fill={voted ? "currentColor" : "none"} />
          </button>
        ) : (
          <ArrowBigUp aria-hidden size={22} className="mt-2.5 text-control" />
        )}
        <span className="text-sm font-bold text-ink">
          {upvotes} <span className="sr-only">{upvotes === 1 ? "upvote" : "upvotes"}</span>
        </span>
        {vote.isError && (
          <p role="alert" className="text-center text-xs text-error">
            <span aria-hidden>Not saved</span>
            <span className="sr-only">{actionError(vote.error)}</span>
          </p>
        )}
      </div>
    );
  }
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
      <span className="text-sm font-bold text-ink">{count}</span>
      {vote.isError && (
        <p role="alert" className="w-full text-sm font-medium text-error">
          {actionError(vote.error)}
        </p>
      )}
    </div>
  );
}
