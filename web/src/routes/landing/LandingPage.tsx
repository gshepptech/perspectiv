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

/** A section break as a novel sets one: an asterism between hairlines. */
function Asterism({ className = "" }: { className?: string }) {
  return (
    <div aria-hidden className={`flex items-center gap-4 text-ink-muted ${className}`}>
      <span className="h-px flex-1 bg-rule" />
      <span className="font-serif text-lg">⁂</span>
      <span className="h-px flex-1 bg-rule" />
    </div>
  );
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
      <Asterism className="mb-8" />
      <h2 className="label-caps">Or browse by state</h2>
      <ul className="mt-4 gap-x-8 sm:columns-2 md:columns-3">
        {states.data.places.map((state) => (
          <li key={state.id} className="break-inside-avoid">
            <Link
              to={`/places/${state.id}`}
              className="inline-flex min-h-11 items-center font-serif text-lg text-accent underline-offset-4 hover:underline"
            >
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
      <Asterism className="mb-8" />
      <h2 id="landing-mine" className="label-caps">
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
    <section className="mx-auto max-w-measure min-w-0 pt-8 pb-16 sm:pt-16" aria-labelledby="landing-title">
      <Asterism className="mb-10 sm:mb-14" />
      <h1 id="landing-title" className="text-3xl sm:text-5xl">
        Where were you?
      </h1>
      <div className="prose-body mt-8">
        <p className="drop-cap">
          Everyone who stood in the same place on the same day saw a different afternoon. Here each of them writes it
          down: one perspective, in their own words, filed under that place and that day. When a writer recognises
          someone in another’s pages, the woman at the counter or the man who left without paying, the two stories link.
          Linked stories become a lore, and the lore readers keep returning to becomes the canon of the day.
        </p>
        <p className="text-ink-muted">
          Anyone can read. If you were there, you can write your part.
        </p>
      </div>
      <form role="search" className="mt-12" onSubmit={(e) => e.preventDefault()}>
        <p className="label-caps" aria-hidden>
          Begin with a place
        </p>
        <label htmlFor={inputId} className="sr-only">
          Search places
        </label>
        <div className="relative mt-2">
          <Search aria-hidden size={20} className="pointer-events-none absolute top-1/2 left-0 -translate-y-1/2 text-ink-muted" />
          {/* A ruled line rather than a box; the focus ring still draws on :focus-visible. */}
          <Input
            id={inputId}
            type="search"
            autoComplete="off"
            placeholder="A state, city, town, mall or store"
            value={text}
            onChange={(e) => setText(e.target.value)}
            className="rounded-none border-0 border-b border-control bg-transparent py-3 pr-0 pl-8 font-serif text-prose focus:border-accent"
          />
        </div>
      </form>
      {query && <SearchResults query={query} />}
      {session && <YourPerspectives />}
      <BrowseStates />
    </section>
  );
}
