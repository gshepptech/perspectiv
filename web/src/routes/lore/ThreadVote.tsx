import { useMutation } from "@tanstack/react-query";
import { ArrowBigUp } from "lucide-react";
import { useEffect, useState } from "react";

import { actionError } from "@/components/actions/errors";
import type { LorePost } from "@/gen/perspectiv/v1/lore_pb";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";

import { useInvalidateThread } from "./threadQueries";

/**
 * A thread post's upvote toggle and count. There is no downvote: pressing it
 * again clears the vote. Without `canVote` (signed out, no story in the arc,
 * or the viewer's own post) only the count shows.
 */
export function ThreadVote({ post, canVote, targetName }: { post: LorePost; canVote: boolean; targetName: string }) {
  const invalidate = useInvalidateThread();
  // The server's last answer, shown until the post refetches with it.
  const [answer, setAnswer] = useState<{ on: boolean; upvotes: number } | null>(null);
  useEffect(() => setAnswer(null), [post.viewerUpvoted, post.upvotes]);
  const vote = useMutation({
    mutationFn: (on: boolean) => clients.lore.voteLorePost({ postId: post.id, on }),
    onSuccess: (res) => setAnswer({ on: res.on, upvotes: res.upvotes }),
    onSettled: () => invalidate(),
  });
  const on = answer?.on ?? post.viewerUpvoted;
  const upvotes = answer?.upvotes ?? post.upvotes;
  return (
    <div className="flex items-center gap-0.5">
      {canVote ? (
        <button
          type="button"
          aria-pressed={on}
          aria-label={`Upvote ${targetName}`}
          aria-disabled={vote.isPending || undefined}
          className={cn(
            "inline-flex size-11 items-center justify-center rounded-md hover:bg-surface aria-disabled:opacity-60",
            on ? "text-accent" : "text-ink-muted",
          )}
          onClick={() => {
            if (!vote.isPending) vote.mutate(!on);
          }}
        >
          <ArrowBigUp aria-hidden size={20} fill={on ? "currentColor" : "none"} />
        </button>
      ) : (
        <ArrowBigUp aria-hidden size={20} className="mx-3 text-control" />
      )}
      <span className="pr-2 text-sm font-bold text-ink">
        {upvotes}
        <span className="sr-only"> {upvotes === 1 ? "upvote" : "upvotes"}</span>
      </span>
      {vote.isError && (
        <p role="alert" className="text-sm text-error">
          {actionError(vote.error)}
        </p>
      )}
    </div>
  );
}
