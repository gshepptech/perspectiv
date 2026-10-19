import { useEffect, useState } from "react";

// Wait this long after the last keystroke before searching places.
export const SEARCH_DEBOUNCE_MS = 300;
// Searches start at this many characters.
export const MIN_QUERY = 2;

export function useDebounced(value: string, ms: number): string {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}
