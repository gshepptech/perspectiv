import { useId } from "react";
import { Link } from "react-router";

import { PerspectiveActions, type ViewerRights } from "@/components/actions/PerspectiveActions";
import type { PerspectiveSummary } from "@/gen/perspectiv/v1/perspective_pb";
import { cn } from "@/lib/cn";

import { genreLabel, loreStatusLabel } from "./labels";

const tones = { muted: "text-ink-muted", warning: "text-warning", success: "text-success" } as const;

/**
 * One perspective on the arc page: author, genre, standing, the opening of its
 * first part and a link into the reader. `rights` adds the action bar; without
 * it the card is read-only (the stitched hour canons on a day arc).
 */
export function PerspectiveCard({
  summary,
  rights,
  level = 3,
}: {
  summary: PerspectiveSummary;
  rights?: ViewerRights;
  /** The card heading's level under the section that lists it. */
  level?: 3 | 4;
}) {
  const headingId = useId();
  const Heading = level === 3 ? "h3" : "h4";
  const name = summary.author?.username;
  if (summary.deleted || summary.hidden) {
    return (
      <article aria-label="Unavailable perspective" className="rounded-md border border-dashed border-rule p-4">
        <p className="text-sm text-ink-muted">
          {summary.deleted ? "This perspective was deleted." : "This perspective was hidden by a moderator."}
        </p>
      </article>
    );
  }
  const status = loreStatusLabel(summary.loreStatus);
  const genre = genreLabel(summary.genre);
  return (
    <article aria-labelledby={headingId} className="min-w-0 rounded-md border border-rule bg-paper p-4">
      <header className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <Heading id={headingId} className="min-w-0 font-sans text-base font-medium break-words">
          {name ? `${name}'s perspective` : "A perspective"}
        </Heading>
        <p className="flex flex-wrap gap-x-2 text-sm text-ink-muted">
          {genre && <span>{genre}</span>}
          <span>
            {summary.partCount} {summary.partCount === 1 ? "part" : "parts"}
          </span>
          {status && <span className={cn("font-medium", tones[status.tone])}>{status.text}</span>}
        </p>
      </header>
      {summary.excerpt && (
        <p className="mt-2 max-w-measure font-serif text-prose break-words hyphens-auto">{summary.excerpt}</p>
      )}
      <p className="mt-2">
        <Link
          to={`/perspectives/${summary.id}`}
          className="inline-flex min-h-9 items-center text-sm font-medium text-accent underline underline-offset-4"
        >
          Read<span className="sr-only"> {name ? `${name}'s perspective` : "this perspective"}</span>
        </Link>
      </p>
      {rights && <PerspectiveActions summary={summary} rights={rights} />}
    </article>
  );
}
