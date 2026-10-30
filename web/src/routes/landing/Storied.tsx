import { useQuery } from "@tanstack/react-query";
import { Crown } from "lucide-react";
import { Link } from "react-router";

import { CanonName } from "@/components/reader/CanonName";
import { formatDay } from "@/components/reader/format";
import { EpicTab } from "@/gen/perspectiv/v1/enums_pb";
import type { EpicArc } from "@/gen/perspectiv/v1/place_pb";
import { clients } from "@/lib/connect";

/** Cards shown; the listing is read a little deeper so repeat places can be skipped. */
const SHOWN = 6;
const READ = 30;

/** The most remembered day at each place, best first, one per place. */
function onePerPlace(arcs: EpicArc[]): EpicArc[] {
  const seen = new Set<string>();
  const out: EpicArc[] = [];
  for (const a of arcs) {
    const id = a.place?.id ?? "";
    if (!id || seen.has(id)) continue;
    seen.add(id);
    out.push(a);
    if (out.length === SHOWN) break;
  }
  return out;
}

function PlaceCard({ a }: { a: EpicArc }) {
  const where = [a.place?.name, a.city].filter(Boolean).join(", ");
  return (
    <li className="min-w-0">
      <Link
        to={`/places/${a.place?.id ?? ""}`}
        className="flex h-full min-w-0 flex-col rounded-xl border border-rule bg-paper p-4 shadow-sm hover:border-accent"
      >
        <span className="font-sans text-base font-medium wrap-anywhere text-ink">{where}</span>
        <span className="mt-0.5 text-sm text-ink-muted">
          {/* An hour arc's title already ends with its hour. */}
          {formatDay(a.localDate)} · <span className="text-ink">{a.title}</span>
        </span>
        {a.excerpt && <span className="mt-2 line-clamp-2 font-serif text-ink">“{a.excerpt}”</span>}
        <span className="mt-auto pt-3 text-sm">
          {a.canonLoreId && (
            <span className="mr-2 inline-flex items-center gap-1 font-medium text-accent">
              <Crown aria-hidden size={16} />
              <CanonName name={a.canonLoreName} />
            </span>
          )}
          <span className="text-ink-muted">
            {a.perspectiveCount} {a.perspectiveCount === 1 ? "story" : "stories"}
          </span>
        </span>
      </Link>
    </li>
  );
}

/**
 * Where people are writing: the places with the most remembered days, one
 * card per place, each opening its place page. The way in stays a place
 * (A-049); this only shows which places have stories.
 */
export function Storied() {
  const top = useQuery({
    queryKey: ["place", "getEpic", { placeId: "", tab: EpicTab.RANKED, pageSize: READ }],
    queryFn: ({ signal }) => clients.place.getEpic({ placeId: "", tab: EpicTab.RANKED, pageSize: READ }, { signal }),
  });
  const cards = onePerPlace(top.data?.arcs ?? []);
  if (cards.length === 0) return null;
  return (
    <section aria-labelledby="landing-storied" className="mt-16 min-w-0">
      <h2 id="landing-storied" className="text-xl">
        Where people are writing
      </h2>
      <p className="mt-1 text-sm text-ink-muted">Places with stories, and the day people there remember most.</p>
      <ul className="mt-4 grid min-w-0 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {cards.map((a) => (
          <PlaceCard key={a.arcId} a={a} />
        ))}
      </ul>
    </section>
  );
}
