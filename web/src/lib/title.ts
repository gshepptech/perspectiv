import { useEffect } from "react";

/** The tab title every page falls back to, and the front page keeps. */
export const SITE_TITLE = "Perspectiv";

/** "<what the page shows> · Perspectiv", or the bare site name when there is nothing to show yet. */
export function documentTitle(what: string | undefined): string {
  const trimmed = what?.trim() ?? "";
  return trimmed ? `${trimmed} · ${SITE_TITLE}` : SITE_TITLE;
}

/**
 * Titles the browser tab after what the page shows (docs/design-system.md,
 * Page titles). An empty or missing title, such as while the page loads,
 * leaves the site name; leaving the page puts the site name back.
 */
export function useDocumentTitle(what: string | undefined): void {
  const title = documentTitle(what);
  useEffect(() => {
    document.title = title;
  }, [title]);
  useEffect(
    () => () => {
      document.title = SITE_TITLE;
    },
    [],
  );
}
