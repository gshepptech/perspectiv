import { useQuery } from "@tanstack/react-query";
import { Check } from "lucide-react";
import { useId } from "react";

import { storiesLabel, titleEndsWithHour } from "@/components/reader/format";
import { EpicTab } from "@/gen/perspectiv/v1/enums_pb";
import type { EpicArc, Place } from "@/gen/perspectiv/v1/place_pb";
import { clients } from "@/lib/connect";
import { cn } from "@/lib/cn";

import { hourLabel } from "./localDate";

/** How many of the place's newest days are offered. */
const SHOWN = 5;
/**
 * How many of the place's newest arcs are read to find them. The epic of a
 * town or mall also lists the days at the places under it, which are left
 * out here, so more are read than are shown.
 */
const READ = 30;

/** "Oct 3", with the year only when it is not the year of `today`. */
function shortDay(localDate: string, today: string): string {
  const sameYear = localDate.slice(0, 4) === today.slice(0, 4);
  return new Date(`${localDate}T00:00:00Z`).toLocaleDateString("en-US", {
    timeZone: "UTC",
    month: "short",
    day: "numeric",
    ...(sameYear ? {} : { year: "numeric" }),
  });
}

/** "Oct 3, 14:00 · Title · 14 stories": one choice as the writer reads it. */
function dayChoiceLabel(arc: Pick<EpicArc, "localDate" | "hour" | "title" | "perspectiveCount">, today: string): string {
  const day = shortDay(arc.localDate, today);
  const when = arc.hour === undefined || titleEndsWithHour(arc.title, arc.hour) ? day : `${day}, ${hourLabel(arc.hour)}`;
  return [when, arc.title, storiesLabel(arc.perspectiveCount)].join(" · ");
}

/**
 * "Days already written about here": the place's newest days with stories,
 * read from its Newest epic, as one-tap choices that set the composer's day
 * and hour. Most writers come to a day that has already happened, often one
 * their friends wrote about, while the composer opens on today. Only days
 * filed at this place itself are offered: stories link only within one arc.
 * Left out while loading, on error and when the place has no days.
 */
export function RecentDays({
  place,
  today,
  localDate,
  hour,
  onChoose,
}: {
  place: Place;
  today: string;
  localDate: string;
  hour: number | undefined;
  onChoose: (localDate: string, hour: number | undefined) => void;
}) {
  const labelId = useId();
  const days = useQuery({
    queryKey: ["place", "getEpic", { placeId: place.id, tab: EpicTab.NEWEST, pageSize: READ }],
    queryFn: ({ signal }) => clients.place.getEpic({ placeId: place.id, tab: EpicTab.NEWEST, pageSize: READ }, { signal }),
    select: (res) => res.arcs.filter((a) => a.place?.id === place.id).slice(0, SHOWN),
  });
  if (!days.data || days.data.length === 0) return null;
  return (
    <div className="grid min-w-0 gap-2 sm:col-span-2">
      <p id={labelId} className="text-sm text-ink-muted">
        Days already written about here
      </p>
      <ul aria-labelledby={labelId} className="grid min-w-0 gap-2">
        {days.data.map((arc) => {
          const chosen = arc.localDate === localDate && arc.hour === hour;
          return (
            <li key={arc.arcId} className="min-w-0">
              <button
                type="button"
                aria-pressed={chosen}
                onClick={() => onChoose(arc.localDate, arc.hour)}
                className={cn(
                  "flex min-h-11 w-full min-w-0 items-center gap-2 rounded-md border bg-paper px-3 py-2 text-left text-sm text-ink hover:border-accent",
                  chosen ? "border-accent" : "border-control",
                )}
              >
                {chosen && <Check aria-hidden size={16} className="shrink-0 text-accent" />}
                <span className="min-w-0 [overflow-wrap:anywhere]">{dayChoiceLabel(arc, today)}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
