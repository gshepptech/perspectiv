import { useId } from "react";

import { AuthorLink } from "@/components/reader/AuthorLink";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";

/**
 * "otter was the guy in the yellow raincoat in wren's story", for every link
 * inside the lore. The lore page shows it under its header and the lore's
 * book on its contents page.
 */
export function WhoWasWho({ stories, memberIds, className = "mt-8" }: { stories: readonly Perspective[]; memberIds: Set<string>; className?: string }) {
  const headingId = useId();
  const lines = stories.flatMap((p) =>
    p.characters
      .filter((c) => c.claimedBy && memberIds.has(c.claimedBy.userId))
      .map((c) => ({ id: c.id, who: c.claimedBy?.username ?? "", label: c.label, author: p.author?.username ?? "" })),
  );
  if (lines.length === 0) return null;
  return (
    <section aria-labelledby={headingId} className={className}>
      <h2 id={headingId} className="text-xl">
        Who was who
      </h2>
      <ul className="mt-3 grid gap-2">
        {lines.map((l) => (
          <li key={l.id} className="min-w-0 wrap-anywhere text-ink">
            <AuthorLink username={l.who} /> was <span className="font-serif">“{l.label}”</span> in{" "}
            <AuthorLink username={l.author} />’s story
          </li>
        ))}
      </ul>
    </section>
  );
}
