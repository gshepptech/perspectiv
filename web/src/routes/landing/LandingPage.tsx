import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Plus, Search } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { Link, useSearchParams } from "react-router";

import { formatDay, placeChain } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";

import { ExampleArc, HowItWorks } from "./ExampleArc";
import { Following } from "./Following";
import { Storied } from "./Storied";

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
    return (
      <>
        <p className="mt-4 text-ink-muted">No places match “{query}”.</p>
        <AddPlaceLink name={query} />
      </>
    );
  }
  return (
    <>
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
    <AddPlaceLink name={query} />
    </>
  );
}

/**
 * The way in when the place someone was at is not listed: it opens the
 * composer's add form with the name they searched for. A signed-out reader
 * is sent to sign in first and comes back here.
 */
function AddPlaceLink({ name }: { name: string }) {
  return (
    <Link
      to={`/compose?add=${encodeURIComponent(name)}`}
      className="mt-3 inline-flex min-h-11 min-w-0 items-center gap-2 rounded-full border border-dashed border-control px-4 text-sm text-ink hover:border-accent hover:text-accent"
    >
      <Plus aria-hidden size={16} className="shrink-0" />
      <span className="min-w-0 wrap-anywhere">Not here? Add “{name}” as a new place</span>
    </Link>
  );
}

function BrowseStates() {
  const states = useQuery({
    queryKey: ["place", "listChildren", { parentId: "" }],
    queryFn: ({ signal }) => clients.place.listChildren({ parentId: "", pageSize: 100 }, { signal }),
  });
  if (!states.data || states.data.places.length === 0) return null;
  return (
    <nav aria-label="Browse by state" className="mt-16">
      <h2 className="text-xl">Or browse by state</h2>
      <ul className="mt-4 flex flex-wrap gap-2">
        {states.data.places.map((state) => (
          <li key={state.id}>
            <Link
              to={`/places/${state.id}`}
              className="inline-flex min-h-11 items-center rounded-full border border-rule bg-paper px-3.5 text-sm text-ink hover:border-accent hover:text-accent sm:min-h-9"
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
    <section className="mx-auto min-w-0 max-w-shell pt-6 pb-16 sm:pt-12" aria-labelledby="landing-title">
      {/* Phones read hero, example, then notes; from lg the notes sit under the search. */}
      <div className="grid min-w-0 items-start gap-10 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)] lg:gap-x-14">
        <div className="min-w-0 lg:col-start-1 lg:row-start-1 lg:pt-6">
          <h1 id="landing-title" className="text-4xl leading-[1.05] tracking-tight sm:text-6xl">
            Same place. Same day. Every side of the story.
          </h1>
          <p className="mt-5 max-w-xl text-lg text-ink-muted">
            Perspectiv is where people who were in the same place at the same time each write what they saw, and find
            the strangers who were in their story.
          </p>
          <form role="search" className="mt-8" onSubmit={(e) => e.preventDefault()}>
            <label htmlFor={inputId} className="sr-only">
              Search places
            </label>
            <div className="relative">
              <Search aria-hidden size={20} className="pointer-events-none absolute top-1/2 left-5 -translate-y-1/2 text-ink-muted" />
              <Input
                id={inputId}
                type="search"
                autoComplete="off"
                placeholder="Where were you? A store, mall, town or city"
                value={text}
                onChange={(e) => setText(e.target.value)}
                className="min-h-14 rounded-full pr-5 pl-13 text-lg shadow-sm"
              />
            </div>
          </form>
          {query ? (
            <SearchResults query={query} />
          ) : (
            !session && <p className="mt-3 text-sm text-ink-muted">Anyone can read. Sign up to write your side.</p>
          )}
        </div>
        <div className="min-w-0 lg:col-start-2 lg:row-span-2 lg:row-start-1">
          <ExampleArc />
        </div>
        <div className="min-w-0 lg:col-start-1 lg:row-start-2">
          <HowItWorks />
        </div>
      </div>
      {session && <Following />}
      <Storied />
      {session && <YourPerspectives />}
      <BrowseStates />
    </section>
  );
}
