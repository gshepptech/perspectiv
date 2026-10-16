import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { Link, useSearchParams } from "react-router";

import { placeChain } from "@/components/reader/format";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";

const DEBOUNCE_MS = 200;

function useDebounced(value: string, ms: number): string {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

function SearchResults({ query }: { query: string }) {
  const search = useQuery({
    queryKey: ["place", "searchPlaces", { query }],
    queryFn: ({ signal }) => clients.place.searchPlaces({ query }, { signal }),
  });
  if (search.isPending) {
    return (
      <p role="status" className="mt-4 text-sm text-ink-muted">
        Searching…
      </p>
    );
  }
  if (search.isError) {
    return (
      <p role="alert" className="mt-4 text-sm text-error">
        Search is unavailable right now. Try again in a moment.
      </p>
    );
  }
  const places = search.data.places;
  if (places.length === 0) {
    return <p className="mt-4 text-ink-muted">No places match “{query}”.</p>;
  }
  return (
    <ul aria-label="Matching places" className="mt-4 divide-y divide-rule border-y border-rule">
      {places.map((place) => (
        <li key={place.id}>
          <Link
            to={`/places/${place.id}`}
            className="flex min-h-11 min-w-0 flex-col justify-center py-2 hover:bg-surface sm:px-2"
          >
            <span className="font-serif text-lg wrap-anywhere text-accent underline-offset-4 hover:underline">{place.name}</span>
            <span className="text-sm wrap-anywhere text-ink-muted">{placeChain(place.kind, place.ancestors)}</span>
          </Link>
        </li>
      ))}
    </ul>
  );
}

function BrowseStates() {
  const states = useQuery({
    queryKey: ["place", "listChildren", { parentId: "" }],
    queryFn: ({ signal }) => clients.place.listChildren({ parentId: "", pageSize: 100 }, { signal }),
  });
  if (!states.data || states.data.places.length === 0) return null;
  return (
    <nav aria-label="Browse by state" className="mt-12">
      <h2 className="text-xl">Or browse by state</h2>
      <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1">
        {states.data.places.map((state) => (
          <li key={state.id}>
            <Link to={`/places/${state.id}`} className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
              {state.name}
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}

/**
 * The landing page is a place search over the hierarchy and nothing else: no
 * feed. Results are links to place pages. The query lives in the address
 * (replacing the entry as the reader types) so the back button from a place
 * returns to the same results.
 */
export function Component() {
  const [params, setParams] = useSearchParams();
  const [text, setText] = useState(() => params.get("q") ?? "");
  const query = useDebounced(text.trim(), DEBOUNCE_MS);
  const inputId = useId();

  useEffect(() => {
    if ((params.get("q") ?? "") === query) return;
    setParams(query ? { q: query } : {}, { replace: true });
  }, [query, params, setParams]);

  return (
    <section className="mx-auto max-w-measure min-w-0 pt-6 sm:pt-12" aria-labelledby="landing-title">
      <h1 id="landing-title" className="text-2xl sm:text-3xl">
        Where were you?
      </h1>
      <p className="mt-2 text-ink-muted">Find a place to read what the people who were there wrote about it.</p>
      <form role="search" className="mt-6" onSubmit={(e) => e.preventDefault()}>
        <label htmlFor={inputId} className="sr-only">
          Search places
        </label>
        <div className="relative">
          <Search aria-hidden size={20} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-ink-muted" />
          <Input
            id={inputId}
            type="search"
            autoComplete="off"
            placeholder="A state, city, town, mall or store"
            value={text}
            onChange={(e) => setText(e.target.value)}
            className="pl-10"
          />
        </div>
      </form>
      {query && <SearchResults query={query} />}
      <BrowseStates />
    </section>
  );
}
