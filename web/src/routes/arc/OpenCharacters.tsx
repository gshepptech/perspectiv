import { useQuery } from "@tanstack/react-query";
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
export function OpenCharacters({ arcId }: { arcId: string }) {
  const session = useSession();
  const [all, setAll] = useState(false);
  const open = useQuery({
    queryKey: ["arc", "listOpenCharacters", { arcId }],
    queryFn: ({ signal }) => clients.arc.listOpenCharacters({ arcId }, { signal }),
  });
  const chars = (open.data?.characters ?? []).filter((c) => !session || c.author?.userId !== session.userId);
  if (chars.length === 0) return null;
  const shown = all ? chars : chars.slice(0, SHOWN);
  return (
    <section aria-labelledby="arc-open" className="min-w-0 rounded-md border border-rule bg-paper p-4 text-sm">
      <h2 id="arc-open" className="font-sans text-base font-medium">
        Is this you?
      </h2>
      <p className="mt-1 text-ink-muted">People described in these stories that nobody has claimed yet.</p>
      <ul className="mt-3 grid gap-3">
        {shown.map((c) => (
          <li key={c.characterId} className="min-w-0">
            <p className="font-serif text-base wrap-anywhere text-ink">“{c.label}”</p>
            <p className="flex flex-wrap items-baseline gap-x-2 text-ink-muted">
              <span className="wrap-anywhere">in {c.author?.username ?? "someone"}'s story</span>
              <Link
                to={`/perspectives/${encodeURIComponent(c.perspectiveId)}?claim=1`}
                aria-label={`That was me: ${c.label}`}
                className="inline-flex min-h-9 items-center font-medium text-accent underline underline-offset-4"
              >
                That was me
              </Link>
            </p>
          </li>
        ))}
      </ul>
      {chars.length > SHOWN && (
        <Button variant="ghost" size="sm" className="mt-2" onClick={() => setAll(!all)}>
          {all ? "Show fewer" : `Show all ${chars.length}`}
        </Button>
      )}
    </section>
  );
}
