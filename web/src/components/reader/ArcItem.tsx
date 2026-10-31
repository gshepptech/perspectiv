import { Crown } from "lucide-react";
import { Link } from "react-router";

import type { EpicArc } from "@/gen/perspectiv/v1/place_pb";

import { CanonName } from "./CanonName";
import { formatDayBeside } from "./format";

/**
 * One day in an epic listing (docs/design-system.md, Place page epic): its
 * title, the opening of its canon's first story (or its first story), then
 * its canon by name and how many are in it. `withCity` names the place with
 * its city, for a listing that spans cities.
 */
export function ArcItem({ arc, withCity = false }: { arc: EpicArc; withCity?: boolean }) {
  const where = withCity && arc.city ? `${arc.place?.name ?? ""}, ${arc.city}` : arc.place?.name;
  return (
    <li className="py-3">
      <Link to={`/arcs/${arc.arcId}`} className="inline-flex min-h-11 items-center font-serif text-lg wrap-anywhere text-accent underline-offset-4 hover:underline">
        {arc.title}
      </Link>
      {arc.excerpt && <p className="line-clamp-2 font-serif wrap-anywhere text-ink">{arc.excerpt}</p>}
      <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-ink-muted">
        {arc.canonLoreId && (
          <span className="inline-flex items-center gap-1 font-medium text-ink">
            <Crown aria-hidden size={16} className="text-accent" />
            <CanonName name={arc.canonLoreName} />
          </span>
        )}
        <span className="wrap-anywhere">{where}</span>
        <span>{formatDayBeside(arc.title, arc.localDate, arc.hour)}</span>
        {arc.memberCount > 0 && <span>{arc.memberCount} in lore</span>}
        <span>
          {arc.perspectiveCount} {arc.perspectiveCount === 1 ? "perspective" : "perspectives"}
        </span>
      </p>
    </li>
  );
}
