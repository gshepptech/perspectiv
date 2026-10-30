import { useMutation, useQueryClient } from "@tanstack/react-query";

import type { Emoji } from "@/gen/perspectiv/v1/enums_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import type { ReactCount } from "@/gen/perspectiv/v1/react_pb";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";

import { actionError } from "./errors";
import { emojis } from "./labels";
import { perspectiveKey, useInvalidateAfterWrite } from "./queries";
import { useWriteGate } from "./useWriteGate";

/**
 * One toggle per emoji in the fixed set, each with its count. Any signed-in
 * reader may react; holding an emoji is shown by an accent border and
 * aria-pressed, never by opacity.
 */
export function ReactBar({ perspectiveId, counts }: { perspectiveId: string; counts: readonly ReactCount[] }) {
  const { gate } = useWriteGate();
  const client = useQueryClient();
  const invalidate = useInvalidateAfterWrite();
  const react = useMutation({
    mutationFn: (v: { emoji: Emoji; on: boolean }) => clients.react.react({ perspectiveId, ...v }),
    onSuccess: (res) =>
      client.setQueryData<Perspective | null>(perspectiveKey(perspectiveId), (old) =>
        old ? { ...old, reacts: res.counts } : old,
      ),
    onSettled: () => invalidate(perspectiveId),
  });
  const byEmoji = new Map(counts.map((c) => [c.emoji, c]));
  return (
    <div className="flex flex-col gap-1">
      <div role="group" aria-label="Reacts" className="flex flex-wrap gap-2">
        {emojis.map(({ value, glyph, name }) => {
          const count = byEmoji.get(value)?.count ?? 0;
          const mine = byEmoji.get(value)?.mine ?? false;
          return (
            <button
              key={value}
              type="button"
              aria-label={`${name}, ${count}`}
              aria-pressed={mine}
              aria-disabled={react.isPending || undefined}
              onClick={() => {
                if (!react.isPending && gate()) react.mutate({ emoji: value, on: !mine });
              }}
              className={cn(
                "inline-flex min-h-11 items-center gap-1 rounded-full border px-2.5 text-sm",
                "aria-disabled:opacity-60",
                mine ? "border-accent bg-surface text-ink" : "border-control text-ink-muted hover:border-ink hover:text-ink",
              )}
            >
              <span aria-hidden>{glyph}</span>
              <span aria-hidden className={cn(mine && "font-bold")}>
                {count}
              </span>
            </button>
          );
        })}
      </div>
      {react.isError && (
        <p role="alert" className="text-sm font-medium text-error">
          {actionError(react.error)}
        </p>
      )}
    </div>
  );
}
