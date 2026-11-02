import type { ReactNode } from "react";
import { Link } from "react-router";

import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";

import { formatDayBeside, loreStatusLabel } from "./format";
import { storyTitle } from "./storyTitle";

/** How much of a story's first part a row quotes; the row clamps it to two lines. */
const OPENING_CHARS = 240;

/** The story's own opening: its first part, whitespace folded, cut at a word. */
function opening(p: Perspective): string {
  const first = [...p.parts].sort((a, b) => a.position - b.position)[0]?.body ?? "";
  const text = first.replace(/\s+/g, " ").trim();
  if (text.length <= OPENING_CHARS) return text;
  const cut = text.slice(0, OPENING_CHARS);
  return `${cut.slice(0, cut.lastIndexOf(" ") > 0 ? cut.lastIndexOf(" ") : OPENING_CHARS)}…`;
}

/**
 * One story in a list of stories, as the profile and the Saved page show it:
 * where and when first, then the day's title, then the story's own opening,
 * so stories whose days share a title still differ. `withAuthor` ends the
 * first line with the writer, for a list that holds many writers.
 * `children` sit under the link, inside the row (the Saved page's toggle).
 */
export function StoryLinkRow({ p, withAuthor = false, children }: { p: Perspective; withAuthor?: boolean; children?: ReactNode }) {
  const title = storyTitle(p);
  const by = withAuthor && p.author ? `by ${p.author.username}` : "";
  const where = [p.place?.name, formatDayBeside(p.arcTitle, p.localDate, p.hour), by].filter(Boolean).join(" · ");
  const standing = loreStatusLabel(p.loreStatus);
  const start = opening(p);
  return (
    <li className="py-3">
      <Link to={`/perspectives/${encodeURIComponent(p.id)}`} className="group flex min-h-11 min-w-0 flex-col justify-center gap-0.5">
        <span className="text-sm font-medium wrap-anywhere text-ink">{where}</span>
        <span className="font-serif text-lg wrap-anywhere text-accent underline-offset-4 group-hover:underline">{title}</span>
        {start && <span className="line-clamp-2 font-serif wrap-anywhere hyphens-manual text-ink-muted">{start}</span>}
        {standing && <span className="text-sm text-ink-muted">{standing}</span>}
      </Link>
      {children}
    </li>
  );
}
