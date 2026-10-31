import { useQuery } from "@tanstack/react-query";
import { Plus, Search } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { Link } from "react-router";

import { placeChain, storiesLabel } from "@/components/reader/format";
import { Input } from "@/components/ui/Input";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";

/** Wait this long after the last keystroke before searching. */
export const PLACE_SEARCH_DEBOUNCE_MS = 200;

export function useDebounced(value: string, ms: number): string {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

/** The longest place search the server takes, in characters. */
export const PLACE_QUERY_MAX = 200;

/** What a search that reached its cap says under the list. */
export const MORE_PLACES = "More places match: add a town to narrow it";

/** The line under a capped list of places, telling the reader to narrow it. */
export function MorePlacesNote({ className }: { className?: string }) {
  return <p className={cn("text-sm text-ink-muted", className)}>{MORE_PLACES}</p>;
}

/**
 * The place search field, labelled "Search places". `large` is the front
 * page's 56px rounded call to action; otherwise it is an ordinary field.
 */
export function PlaceSearchField({
  value,
  onChange,
  large = false,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  large?: boolean;
  className?: string;
}) {
  const inputId = useId();
  return (
    <form role="search" className={className} onSubmit={(e) => e.preventDefault()}>
      <label htmlFor={inputId} className="sr-only">
        Search places
      </label>
      <div className="relative">
        <Search
          aria-hidden
          size={large ? 20 : 18}
          className={cn("pointer-events-none absolute top-1/2 -translate-y-1/2 text-ink-muted", large ? "left-5" : "left-3")}
        />
        <Input
          id={inputId}
          type="search"
          autoComplete="off"
          maxLength={PLACE_QUERY_MAX}
          placeholder="A store, mall, town or city"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className={large ? "min-h-14 rounded-full pr-5 pl-13 text-lg shadow-sm" : "pl-10"}
        />
      </div>
    </form>
  );
}

/**
 * The places matching a query, each a link to its place page with what and
 * where it is and its story count, then the "more" line when the search hit
 * its cap and the way to add a place it does not know. `onFollow` runs when
 * a link is followed (the header dialog closes on it).
 */
export function PlaceSearchResults({ query, onFollow }: { query: string; onFollow?: () => void }) {
  const search = useQuery({
    queryKey: ["place", "searchPlaces", { query }],
    queryFn: ({ signal }) => clients.place.searchPlaces({ query }, { signal }),
  });
  const places = search.data?.places ?? [];
  const more = search.data?.more ?? false;
  // One status line, always mounted, so a screen reader hears each result.
  const status = search.isPending
    ? "Searching…"
    : search.isError
      ? ""
      : places.length === 0
        ? `No places match “${query}”.`
        : `${places.length} ${places.length === 1 ? "place matches" : "places match"} “${query}”.${more ? ` ${MORE_PLACES}.` : ""}`;
  return (
    <>
      <p role="status" className={places.length > 0 ? "sr-only" : "mt-4 text-sm text-ink-muted"}>
        {status}
      </p>
      {search.isError && (
        <p role="alert" className="mt-4 text-sm text-error">
          Search is unavailable right now. Try again in a moment.
        </p>
      )}
      {places.length > 0 && (
        <ul aria-label="Matching places" className="mt-4 divide-y divide-rule border-y border-rule">
          {places.map((place) => (
            <li key={place.id}>
              <Link
                to={`/places/${place.id}`}
                onClick={onFollow}
                className="flex min-h-11 min-w-0 flex-col justify-center py-2 hover:bg-surface sm:px-2"
              >
                <span className="font-serif text-lg wrap-anywhere text-accent underline-offset-4 hover:underline">{place.name}</span>
                <span className="text-sm wrap-anywhere text-ink-muted">
                  {placeChain(place.kind, place.ancestors, place.category)} · {storiesLabel(place.stories)}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
      {more && <MorePlacesNote className="mt-3" />}
      {search.isSuccess && <AddPlaceLink name={query} onFollow={onFollow} />}
    </>
  );
}

/**
 * The way in when the place someone was at is not listed: it opens the
 * composer's add form with the name they searched for. A signed-out reader
 * is sent to sign in first and comes back here.
 */
function AddPlaceLink({ name, onFollow }: { name: string; onFollow?: () => void }) {
  return (
    <Link
      to={`/compose?add=${encodeURIComponent(name)}`}
      onClick={onFollow}
      className="mt-3 inline-flex min-h-11 min-w-0 items-center gap-2 rounded-full border border-dashed border-control px-4 text-sm text-ink hover:border-accent hover:text-accent"
    >
      <Plus aria-hidden size={16} className="shrink-0" />
      <span className="min-w-0 wrap-anywhere">Not here? Add “{name}” as a new place</span>
    </Link>
  );
}
