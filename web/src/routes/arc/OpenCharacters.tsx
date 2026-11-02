import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";

/** How many the panel shows before "Show all". */
const SHOWN = 6;

/**
 * "Is this you?": the people described in the day's stories whom nobody has
 * claimed yet, each with a link that opens the story with the claim dialog
 * up. The viewer's own characters are left out: nobody claims in their own
 * story. Claiming is how stories link, so this is the shortest way in.
 */
export function OpenCharacters({
  arcId,
  compact = false,
}: {
  arcId: string;
  /**
   * A one-line disclosure that opens to the list, for the top of the arc
   * page on phones, where the sidebar falls below every story.
   */
  compact?: boolean;
}) {
  const session = useSession();
  const [all, setAll] = useState(false);
  const open = useQuery({
    queryKey: ["arc", "listOpenCharacters", { arcId }],
    queryFn: ({ signal }) => clients.arc.listOpenCharacters({ arcId }, { signal }),
  });
  const chars = (open.data?.characters ?? []).filter((c) => !session || c.author?.userId !== session.userId);
  if (chars.length === 0) return null;
  const shown = all ? chars : chars.slice(0, SHOWN);
  const list = (
    <ul className="mt-3 grid gap-3">
      {shown.map((c) => (
        <li key={c.characterId} className="min-w-0">
          <p className="font-serif text-base wrap-anywhere text-ink">“{c.label}”</p>
          <p className="flex flex-wrap items-baseline gap-x-2 text-ink-muted">
            <span className="wrap-anywhere">in {c.author?.username ?? "someone"}'s story</span>
            <Link
              to={`/perspectives/${encodeURIComponent(c.perspectiveId)}?claim=${encodeURIComponent(c.characterId)}`}
              aria-label={`That was me: ${c.label}`}
              className="inline-flex min-h-11 items-center font-medium text-accent underline underline-offset-4"
            >
              That was me
            </Link>
          </p>
        </li>
      ))}
    </ul>
  );
  const more = chars.length > SHOWN && (
    <Button variant="ghost" size="sm" className="mt-2" onClick={() => setAll(!all)}>
      {all ? "Show fewer" : `Show all ${chars.length}`}
    </Button>
  );
  if (compact) {
    return (
      <details className="group min-w-0 rounded-md border border-rule bg-paper px-4 py-2 text-sm">
        <summary className="flex min-h-11 cursor-pointer list-none items-center gap-2 font-medium text-ink [&::-webkit-details-marker]:hidden">
          <ChevronRight aria-hidden size={16} className="shrink-0 transition-transform group-open:rotate-90" />
          Is this you? {chars.length === 1 ? "1 person" : `${chars.length} people`} nobody has claimed yet
        </summary>
        {list}
        {more}
      </details>
    );
  }
  return (
    <section aria-labelledby="arc-open" className="min-w-0 rounded-md border border-rule bg-paper p-4 text-sm">
      <h2 id="arc-open" className="text-lg">
        Is this you?
      </h2>
      <p className="mt-1 text-ink-muted">People described in these stories that nobody has claimed yet.</p>
      {list}
      {more}
    </section>
  );
}
