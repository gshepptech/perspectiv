import { useQuery } from "@tanstack/react-query";
import { MapPin } from "lucide-react";
import { useEffect, useId, useState } from "react";

import { Input } from "@/components/ui/Input";
import type { Place } from "@/gen/perspectiv/v1/place_pb";
import { clients } from "@/lib/connect";

// Wait this long after the last keystroke before searching.
const SEARCH_DEBOUNCE_MS = 300;
const MIN_QUERY = 2;

function useDebounced(value: string, ms: number): string {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

/** The first step of writing: find the place the story happened. */
export function PlacePicker({ onPick }: { onPick: (place: Place) => void }) {
  const inputId = useId();
  const [text, setText] = useState("");
  const query = useDebounced(text.trim(), SEARCH_DEBOUNCE_MS);
  const search = useQuery({
    queryKey: ["place", "searchPlaces", query],
    queryFn: () => clients.place.searchPlaces({ query }),
    enabled: query.length >= MIN_QUERY,
  });
  const places = search.data?.places ?? [];

  return (
    <section aria-labelledby="pick-title" className="mx-auto grid w-full max-w-measure min-w-0 gap-4">
      <h1 id="pick-title" className="text-2xl sm:text-3xl">
        Write a perspective
      </h1>
      <div className="grid gap-1.5">
        <label htmlFor={inputId} className="text-sm font-medium text-ink">
          Where were you?
        </label>
        <Input
          id={inputId}
          type="search"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="A store, mall or town"
          autoComplete="off"
        />
      </div>
      {search.isError && (
        <p role="alert" className="text-sm font-medium text-error">
          Search failed. Try again.
        </p>
      )}
      {search.isSuccess && places.length === 0 && (
        <p className="text-sm text-ink-muted">No places match “{query}”.</p>
      )}
      {places.length > 0 && (
        <ul aria-label="Matching places" className="grid gap-2">
          {places.map((p) => (
            <li key={p.id}>
              <button
                type="button"
                onClick={() => onPick(p)}
                className="flex w-full min-w-0 items-start gap-2 rounded-md border border-rule bg-surface px-3 py-2 text-left hover:border-ink"
              >
                <MapPin aria-hidden size={16} className="mt-1 shrink-0 text-accent" />
                <span className="grid min-w-0">
                  <span className="font-medium text-ink [overflow-wrap:anywhere]">{p.name}</span>
                  {p.ancestors.length > 0 && (
                    <span className="text-sm text-ink-muted [overflow-wrap:anywhere]">
                      {p.ancestors.map((a) => a.name).reverse().join(", ")}
                    </span>
                  )}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
