import { useEffect, useRef, useState } from "react";

/**
 * "Show more" that moves focus to the first entry the new page brought. The
 * button goes away with the last page, and focus must not fall to <body>
 * when it does; the entry gets tabIndex -1 only so it can take that focus.
 *
 * Attach `list` to the element whose children are the entries, and call
 * `showMore` from the button. `elsewhere` names where focus goes when the
 * page brought nothing to this list, for a page that splits one query
 * across two lists.
 */
export function useShowMore<T extends HTMLElement = HTMLUListElement>(
  count: number,
  fetchNextPage: () => Promise<{ isError: boolean }>,
  elsewhere?: () => HTMLElement | null,
) {
  const list = useRef<T>(null);
  // How many entries there were when the reader asked for the page that has
  // just landed; null when no page of theirs is waiting to take focus.
  const from = useRef<number | null>(null);
  // Bumped when a page lands, so the effect runs once that page has rendered.
  const [landed, setLanded] = useState(0);
  useEffect(() => {
    const start = from.current;
    if (start === null) return;
    from.current = null;
    const entry = list.current?.children.item(start);
    if (entry instanceof HTMLElement) {
      entry.tabIndex = -1;
      entry.focus();
    } else {
      elsewhere?.()?.focus();
    }
  }, [landed, elsewhere]);
  return {
    list,
    showMore: () => {
      const start = count;
      void fetchNextPage().then((result) => {
        // A failed page leaves the button, and focus on it, where they were.
        if (result.isError) return;
        from.current = start;
        setLanded((n) => n + 1);
      });
    },
  };
}
