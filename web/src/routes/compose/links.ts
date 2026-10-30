import { safeNext } from "@/routes/auth/rules";

/** The place, day and (for an hour arc) hour a new perspective is written about. */
export type Where = { placeId: string; localDate: string; hour?: number };

/**
 * The composer for a new perspective at a place on a day. `back` is a
 * same-site path the writer returns to once that perspective is published
 * (a story they want to claim a character in); the composer page keeps it
 * for the story page to act on after publishing.
 */
export function composeHref(where: Where, back?: string): string {
  const q = new URLSearchParams({ placeId: where.placeId, date: where.localDate });
  if (where.hour !== undefined) q.set("hour", String(where.hour));
  if (back && safeNext(back) === back) q.set("return", back);
  return `/compose?${q.toString()}`;
}

// sessionStorage: the return belongs to this tab's journey only. It is a
// per-reader convenience, so a browser that refuses storage just loses it.
const AFTER_PUBLISH = "perspectiv.afterPublish";

type Pending = { to: string; placeId: string; localDate: string; hour: number | null };

/** Remembers where to go once a perspective at `where` is published. */
export function rememberAfterPublish(where: Where, to: string): void {
  if (safeNext(to) !== to) return;
  const pending: Pending = { to, placeId: where.placeId, localDate: where.localDate, hour: where.hour ?? null };
  try {
    sessionStorage.setItem(AFTER_PUBLISH, JSON.stringify(pending));
  } catch {
    // Storage blocked: the writer lands on their own story as usual.
  }
}

/**
 * Where to go now that a perspective at `where` is published, once: the
 * remembered return when it was for this same place, day and hour, else
 * null. A return for another arc is left for its own publish.
 */
export function takeAfterPublish(where: Where): string | null {
  try {
    const raw = sessionStorage.getItem(AFTER_PUBLISH);
    if (!raw) return null;
    const p = JSON.parse(raw) as Partial<Pending>;
    const same = p.placeId === where.placeId && p.localDate === where.localDate && (p.hour ?? null) === (where.hour ?? null);
    if (!same || typeof p.to !== "string" || safeNext(p.to) !== p.to) return null;
    sessionStorage.removeItem(AFTER_PUBLISH);
    return p.to;
  } catch {
    return null;
  }
}
