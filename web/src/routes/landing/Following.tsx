import { useQuery } from "@tanstack/react-query";
import { Bell } from "lucide-react";
import { Link } from "react-router";

import { clients } from "@/lib/connect";

// The server stops counting a place's new stories here.
const COUNT_CAP = 100;

const newLabel = (n: number) => (n >= COUNT_CAP ? `${COUNT_CAP}+` : String(n));

/**
 * The places a signed-in reader follows, as quick links back to them. A
 * place with stories from the last week says how many, and the server puts
 * those places first.
 */
export function Following() {
  const following = useQuery({
    queryKey: ["place", "listFollowing"],
    queryFn: ({ signal }) => clients.place.listFollowing({}, { signal }),
  });
  const places = following.data?.places ?? [];
  if (places.length === 0) return null;
  const total = places.reduce((sum, p) => sum + p.newThisWeek, 0);
  const capped = places.some((p) => p.newThisWeek >= COUNT_CAP);
  return (
    <nav aria-labelledby="landing-following" className="mt-16 min-w-0">
      <h2 id="landing-following" className="text-xl">
        Places you follow
      </h2>
      {total > 0 && (
        <p className="mt-1 text-sm text-ink-muted">
          <strong className="font-bold text-ink">
            {total}
            {capped ? "+" : ""}
          </strong>{" "}
          new {total === 1 && !capped ? "story" : "stories"} this week
        </p>
      )}
      <ul className="mt-4 flex flex-wrap gap-2">
        {places.map((p) => (
          <li key={p.id} className="min-w-0">
            <Link
              to={`/places/${p.id}`}
              className="inline-flex min-h-11 max-w-full items-center gap-1.5 rounded-full border border-rule bg-paper px-3.5 text-sm text-ink hover:border-accent hover:text-accent"
            >
              <Bell aria-hidden size={14} className="shrink-0 text-accent" />
              <span className="min-w-0 truncate">{p.name}</span>
              {p.newThisWeek > 0 && (
                <span className="shrink-0">
                  <span aria-hidden className="text-ink-muted">
                    ·{" "}
                  </span>
                  <strong className="font-bold text-accent">{newLabel(p.newThisWeek)}</strong> new
                </span>
              )}
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}
