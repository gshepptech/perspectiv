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
 * One quiet, borderless toggle per emoji in the fixed set (Folio, Reacts).
 * An emoji the reader has not used is greyscale and faded and a zero count is
 * not drawn; the reader's own react is in colour with a bold count and an
 * accent underline, plus aria-pressed, never opacity alone. The accessible
 * name always carries the count. A signed-out reader is `blocked`: pressing
 * an emoji reveals why (the panel `controls` names) instead of reacting or
 * leaving the page.
 */
export function ReactBar({
  perspectiveId,
  counts,
  blocked,
}: {
  perspectiveId: string;
  counts: readonly ReactCount[];
  blocked?: { expanded: boolean; controls: string; onPress: () => void };
}) {
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
      <div role="group" aria-label="Reacts" className="flex flex-wrap gap-1">
        {emojis.map(({ value, glyph, name }) => {
          const count = byEmoji.get(value)?.count ?? 0;
          const mine = byEmoji.get(value)?.mine ?? false;
          return (
            <button
              key={value}
              type="button"
              aria-label={`${name}, ${count}`}
              aria-pressed={blocked ? undefined : mine}
              aria-expanded={blocked ? blocked.expanded : undefined}
              aria-controls={blocked?.expanded ? blocked.controls : undefined}
              aria-disabled={react.isPending || undefined}
              onClick={() => {
                if (blocked) blocked.onPress();
                else if (!react.isPending && gate()) react.mutate({ emoji: value, on: !mine });
              }}
              className={cn(
                "group inline-flex min-h-11 min-w-11 items-center justify-center gap-1 rounded-md border-b-2 px-2 text-sm first:-ms-2 hover:bg-surface",
                "aria-disabled:opacity-60",
                mine ? "border-accent text-ink" : "border-transparent text-ink-muted",
              )}
            >
              <span aria-hidden className={cn(!mine && "opacity-70 grayscale group-hover:opacity-100 group-hover:grayscale-0")}>
                {glyph}
              </span>
              {count > 0 && (
                <span aria-hidden className={cn(mine && "font-bold")}>
                  {count}
                </span>
              )}
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
