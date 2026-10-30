import { safeNext } from "./rules";

// Where a new member was headed when they signed up, for the page the
// verification email opens. That link opens in a new tab, which starts with
// an empty sessionStorage, so this lives in localStorage, holds only a
// same-site path, and lapses after a week. It is a per-reader convenience: a
// browser that refuses storage just loses it.
const KEY = "perspectiv.afterVerify";
const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

/** Keeps `next` for the verify page; "/" and off-site values are not kept. */
export function rememberAfterVerify(next: string | null, now = Date.now()): void {
  const to = safeNext(next);
  try {
    if (to === "/") localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, JSON.stringify({ to, at: now }));
  } catch {
    // Storage blocked: the verify page offers the home page instead.
  }
}

/**
 * The kept path, or null when there is none, it lapsed or it is not a
 * same-site path. `take` removes it, once the email is verified and the
 * reader has been offered it.
 */
export function readAfterVerify({ take = false, now = Date.now() } = {}): string | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    if (take) localStorage.removeItem(KEY);
    const v = JSON.parse(raw) as { to?: unknown; at?: unknown };
    if (typeof v.to !== "string" || typeof v.at !== "number" || now - v.at > WEEK_MS) return null;
    const to = safeNext(v.to);
    return to === "/" ? null : to;
  } catch {
    return null;
  }
}
