import { ArrowLeft, BookOpen } from "lucide-react";
import { Link } from "react-router";

import { arcSpan } from "@/components/reader/format";
import type { GetArcResponse } from "@/gen/perspectiv/v1/arc_pb";
import type { Perspective, PerspectiveSummary } from "@/gen/perspectiv/v1/perspective_pb";

/** How many of the day's other stories follow a story. */
const MORE_STORIES = 5;

/** Every story the arc's first pages hold, in the arc page's order: canon, contenders, waiting, tales. */
function dayStories(res: GetArcResponse): PerspectiveSummary[] {
  return [...(res.canon?.perspectives ?? []), ...res.sublores.flatMap((l) => l.perspectives), ...res.unlinked, ...res.tales];
}

const linkClass = "inline-flex min-h-11 items-center gap-1 font-medium text-accent underline underline-offset-4";

/**
 * What follows the last part of a story: its lore when it is in one, the
 * day's other stories, and the way back to the day, so finishing a story is
 * never a dead end. `res` is the arc page's own read, unset while it loads or
 * when it failed; the links that need nothing from it show regardless.
 */
export function MoreFromDay({ p, res }: { p: Perspective; res: GetArcResponse | undefined }) {
  const span = arcSpan(p.hour);
  const others = res ? dayStories(res).filter((s) => s.id !== p.id && !s.deleted && !s.hidden) : [];
  const lores = res ? [res.canon, ...res.sublores] : [];
  const lore = p.loreId ? lores.find((l) => l?.lore?.id === p.loreId)?.lore : undefined;
  const isCanon = !!p.loreId && res?.arc?.canonLoreId === p.loreId;
  return (
    <section aria-labelledby="more-from-day" className="mx-auto mt-10 max-w-measure border-t border-rule pt-6">
      <h2 id="more-from-day" className="text-xl">
        More from this {span}
      </h2>
      {p.loreId && (
        <p className="mt-3">
          <Link to={`/lores/${encodeURIComponent(p.loreId)}`} className={linkClass}>
            <BookOpen aria-hidden size={16} />
            {isCanon ? "Read the canon" : "Read its lore"}
            {lore?.name ? `, “${lore.name}”` : ""}: who was who, every story and the thread
          </Link>
        </p>
      )}
      {res && others.length === 0 && (
        <p className="mt-3 text-ink-muted">Nobody else has written about this {span} yet.</p>
      )}
      {others.length > 0 && (
        <ul aria-label={`Other stories from this ${span}`} className="mt-3 divide-y divide-rule border-y border-rule">
          {others.slice(0, MORE_STORIES).map((s) => (
            <li key={s.id}>
              <Link to={`/perspectives/${encodeURIComponent(s.id)}`} className="group flex min-h-11 min-w-0 flex-col justify-center py-3">
                <span className="text-sm text-ink-muted">{s.author?.username ?? "Someone"}</span>
                <span className="line-clamp-2 font-serif wrap-anywhere hyphens-manual text-ink group-hover:underline group-hover:underline-offset-4">
                  {s.excerpt || "Read this story"}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-3">
        <Link to={`/arcs/${encodeURIComponent(p.arcId)}`} className={linkClass}>
          <ArrowLeft aria-hidden size={16} />
          Back to the {span}: every story and lore
        </Link>
      </p>
    </section>
  );
}
