import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { Link, useSearchParams } from "react-router";

import { formatDay, placeChain } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";

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

const MINE_PAGE_SIZE = 20;

function MineRow({ p }: { p: Perspective }) {
  const isDraft = p.status === PerspectiveStatus.DRAFT;
  const title = p.arcTitle || p.titleIfNewArc || "Untitled";
  const edit = `/compose?perspectiveId=${encodeURIComponent(p.id)}`;
  const where = [p.place?.name, formatDay(p.localDate, p.hour)].filter(Boolean).join(" · ");
  return (
    <li className="flex min-w-0 flex-wrap items-center justify-between gap-x-4 py-2">
      <Link to={isDraft ? edit : `/perspectives/${encodeURIComponent(p.id)}`} className="flex min-h-11 min-w-0 flex-col justify-center">
        <span className="font-serif text-lg wrap-anywhere text-accent underline-offset-4 hover:underline">{title}</span>
        <span className="text-sm wrap-anywhere text-ink-muted">
          {isDraft ? "Draft · " : "Published · "}
          {where}
        </span>
      </Link>
      {!isDraft && (
        <Link to={edit} aria-label={`Edit ${title}`} className="inline-flex min-h-11 items-center text-sm text-accent underline underline-offset-4">
          Edit
        </Link>
      )}
    </li>
  );
}

/**
 * The signed-in author's own drafts and published perspectives, newest edit
 * first. It is their own writing, not a feed: signed-out readers see nothing
 * here and nothing of anyone else's is listed.
 */
function YourPerspectives() {
  const mine = useInfiniteQuery({
    queryKey: ["perspective", "listMine", {}],
    queryFn: ({ pageParam, signal }) => clients.perspective.listMine({ pageToken: pageParam, pageSize: MINE_PAGE_SIZE }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  if (mine.isPending) return null;
  if (mine.isError) {
    return (
      <p role="alert" className="mt-12 text-sm text-error">
        Your perspectives could not be loaded. Try again in a moment.
      </p>
    );
  }
  const items = mine.data.pages.flatMap((page) => page.perspectives);
  if (items.length === 0) return null;
  return (
    <section aria-labelledby="landing-mine" className="mt-12">
      <h2 id="landing-mine" className="text-xl">
        Your perspectives
      </h2>
      <ul className="mt-3 divide-y divide-rule border-y border-rule">
        {items.map((p) => (
          <MineRow key={p.id} p={p} />
        ))}
      </ul>
      {mine.hasNextPage && (
        <Button variant="secondary" className="mt-3" disabled={mine.isFetchingNextPage} onClick={() => void mine.fetchNextPage()}>
          Show more
        </Button>
      )}
    </section>
  );
}

/**
 * The landing page is a place search over the hierarchy and no feed; a
 * signed-in author also finds their own drafts and perspectives here. Results are links to place pages. The query lives in the address
 * (replacing the entry as the reader types) so the back button from a place
 * returns to the same results.
 */
export function Component() {
  const [params, setParams] = useSearchParams();
  const [text, setText] = useState(() => params.get("q") ?? "");
  const query = useDebounced(text.trim(), DEBOUNCE_MS);
  const inputId = useId();
  const session = useSession();

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
      {session && <YourPerspectives />}
      <BrowseStates />
    </section>
  );
}
