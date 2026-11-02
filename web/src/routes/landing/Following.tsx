import { useQuery } from "@tanstack/react-query";
import { Bell, PenLine } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";

import { clients } from "@/lib/connect";
import { followedWritersKey } from "@/routes/settings/queries";

// The server stops counting a place's, or a writer's, new stories here.
const COUNT_CAP = 100;

const newLabel = (n: number) => (n >= COUNT_CAP ? `${COUNT_CAP}+` : String(n));

const pillClass =
  "inline-flex min-h-11 max-w-full items-center gap-1.5 rounded-full border border-rule bg-paper px-3.5 text-sm text-ink hover:border-accent hover:text-accent";

/** One followed place or writer: its icon, its name and, when there is any, "· 3 new". */
function Pill({ to, icon, name, fresh }: { to: string; icon: ReactNode; name: string; fresh: number }) {
  return (
    <li className="min-w-0">
      <Link to={to} className={pillClass}>
        {icon}
        <span className="min-w-0 truncate">{name}</span>
        {fresh > 0 && (
          <span className="shrink-0">
            <span aria-hidden className="text-ink-muted">
              ·{" "}
            </span>
            <strong className="font-bold text-accent">{newLabel(fresh)}</strong> new
          </span>
        )}
      </Link>
    </li>
  );
}

/**
 * The places and writers a signed-in reader follows, as quick links back to
 * them. A place with stories from the last week says how many and opens on
 * its Newest tab, and the server puts those places first. A followed writer
 * shows only while they have a story from the last week, most first, and
 * opens their profile. The reader's own stories never count as new.
 */
export function Following() {
  const following = useQuery({
    queryKey: ["place", "listFollowing"],
    queryFn: ({ signal }) => clients.place.listFollowing({}, { signal }),
  });
  const followed = useQuery({
    queryKey: followedWritersKey,
    queryFn: ({ signal }) => clients.user.listFollowedWriters({}, { signal }),
  });
  const places = following.data?.places ?? [];
  const writers = (followed.data?.writers ?? []).filter((w) => w.newThisWeek > 0).sort((a, b) => b.newThisWeek - a.newThisWeek);
  if (places.length === 0 && writers.length === 0) return null;
  const total = places.reduce((sum, p) => sum + p.newThisWeek, 0);
  const capped = places.some((p) => p.newThisWeek >= COUNT_CAP);
  return (
    <div className="mt-16 grid min-w-0 gap-10">
      {places.length > 0 && (
        <nav aria-labelledby="landing-following" className="min-w-0">
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
              <Pill
                key={p.id}
                // What is new is on the Newest tab, where the new days are marked.
                to={p.newThisWeek > 0 ? `/places/${p.id}?tab=newest` : `/places/${p.id}`}
                icon={<Bell aria-hidden size={14} className="shrink-0 text-accent" />}
                name={p.name}
                fresh={p.newThisWeek}
              />
            ))}
          </ul>
        </nav>
      )}
      {writers.length > 0 && (
        <nav aria-labelledby="landing-writers" className="min-w-0">
          <h2 id="landing-writers" className="text-xl">
            Writers you follow
          </h2>
          <ul className="mt-4 flex flex-wrap gap-2">
            {writers.map((w) => (
              <Pill
                key={w.userId}
                to={`/users/${encodeURIComponent(w.username)}`}
                icon={<PenLine aria-hidden size={14} className="shrink-0 text-accent" />}
                name={w.username}
                fresh={w.newThisWeek}
              />
            ))}
          </ul>
        </nav>
      )}
    </div>
  );
}
