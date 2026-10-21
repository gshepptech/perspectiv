import { Code, ConnectError } from "@connectrpc/connect";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Crown, PenLine, Plus } from "lucide-react";
import { useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";

import { formatDay, genreLabel, loreStatusLabel, placeCategories, placeCategoryLabel, placeKindLabel } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/Select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { EpicTab, Genre, PlaceCategory, PlaceKind } from "@/gen/perspectiv/v1/enums_pb";
import type { EpicArc, EpicTale, Place } from "@/gen/perspectiv/v1/place_pb";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";
import { AuthorLink } from "@/components/reader/AuthorLink";
import { useDebounced } from "@/routes/compose/search";

// The tab and genre live in the address as lower-case words (?tab=tales,
// ?genre=slice_of_life) so a filtered epic is linkable; the defaults (Ranked,
// every genre) are the bare address.
const tabs = [
  { slug: "ranked", label: "Ranked", tab: EpicTab.RANKED },
  { slug: "newest", label: "Newest", tab: EpicTab.NEWEST },
  { slug: "tales", label: "Tales", tab: EpicTab.TALES },
] as const;
type TabSlug = (typeof tabs)[number]["slug"];

const genres = [Genre.DRAMA, Genre.COMEDY, Genre.ROMANCE, Genre.MYSTERY, Genre.HORROR, Genre.SLICE_OF_LIFE, Genre.ACTION];
const ALL_GENRES = "all";
const genreSlug = (g: Genre) => Genre[g].toLowerCase();

function parseTab(value: string | null): TabSlug {
  return tabs.find((t) => t.slug === value)?.slug ?? "ranked";
}

function parseGenre(value: string | null): Genre {
  return genres.find((g) => genreSlug(g) === value) ?? Genre.UNSPECIFIED;
}

function isNotFound(err: unknown): boolean {
  return ConnectError.from(err).code === Code.NotFound;
}

function ArcItem({ arc }: { arc: EpicArc }) {
  return (
    <li className="py-3">
      <Link to={`/arcs/${arc.arcId}`} className="font-serif text-lg wrap-anywhere text-accent underline-offset-4 hover:underline">
        {arc.title}
      </Link>
      <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-ink-muted">
        {arc.canonLoreId && (
          <span className="inline-flex items-center gap-1 font-medium text-ink">
            <Crown aria-hidden size={16} className="text-accent" />
            Canon
          </span>
        )}
        <span className="wrap-anywhere">{arc.place?.name}</span>
        <span>{formatDay(arc.localDate, arc.hour)}</span>
        {arc.memberCount > 0 && <span>{arc.memberCount} in lore</span>}
        <span>
          {arc.perspectiveCount} {arc.perspectiveCount === 1 ? "perspective" : "perspectives"}
        </span>
      </p>
    </li>
  );
}

function TaleItem({ tale }: { tale: EpicTale }) {
  return (
    <li className="py-3">
      <Link
        to={`/perspectives/${tale.perspectiveId}`}
        className="block font-serif text-prose wrap-anywhere text-ink underline-offset-4 hover:underline"
      >
        {tale.excerpt || "Read this tale"}
      </Link>
      <p className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-ink-muted">
        {tale.author && <AuthorLink username={tale.author.username} />}
        <span>{loreStatusLabel(tale.loreStatus)}</span>
        {tale.genre !== Genre.UNSPECIFIED && <span>{genreLabel(tale.genre)}</span>}
        <span className="wrap-anywhere">{tale.place?.name}</span>
        <span>{formatDay(tale.localDate, tale.hour)}</span>
        <Link to={`/arcs/${tale.arcId}`} className="wrap-anywhere text-accent underline underline-offset-4">
          {tale.arcTitle}
        </Link>
      </p>
    </li>
  );
}

/**
 * What an empty epic says. A place with nothing yet asks the reader whether
 * they were there and offers the first story; a filter that empties the list
 * says so, so the place does not look abandoned when it is not.
 */
function EmptyEpic({ place, tab, genre }: { place: Place; tab: EpicTab; genre: Genre }) {
  if (genre !== Genre.UNSPECIFIED) {
    return <p className="text-ink-muted">No {genreLabel(genre).toLowerCase()} stories here yet.</p>;
  }
  if (tab === EpicTab.TALES) {
    return <p className="text-ink-muted">No tales here yet. A tale is a story no one else has linked to.</p>;
  }
  return (
    <div className="rounded-xl border border-dashed border-control p-6">
      <p className="font-serif text-xl wrap-anywhere text-ink">No one has written about {place.name} yet.</p>
      <p className="mt-2 text-ink-muted">
        Were you here? Write what you saw, and the people who were here with you can add their side.
      </p>
      <Button asChild className="mt-4">
        <Link to={`/compose?placeId=${encodeURIComponent(place.id)}`}>
          <PenLine aria-hidden size={16} />
          Write the first story
        </Link>
      </Button>
    </div>
  );
}

function EpicList({ place, tab, genre }: { place: Place; tab: EpicTab; genre: Genre }) {
  const placeId = place.id;
  const epic = useInfiniteQuery({
    queryKey: ["place", "getEpic", { placeId, tab, genre }],
    queryFn: ({ pageParam, signal }) => clients.place.getEpic({ placeId, tab, genre, pageToken: pageParam }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  if (epic.isPending) {
    return (
      <p role="status" className="text-sm text-ink-muted">
        Loading…
      </p>
    );
  }
  if (epic.isError) {
    return (
      <p role="alert" className="text-sm text-error">
        This list could not be loaded. Try again in a moment.
      </p>
    );
  }
  const isTales = tab === EpicTab.TALES;
  const arcs = epic.data.pages.flatMap((p) => p.arcs);
  const tales = epic.data.pages.flatMap((p) => p.tales);
  if ((isTales ? tales.length : arcs.length) === 0) {
    return <EmptyEpic place={place} tab={tab} genre={genre} />;
  }
  return (
    <>
      {isTales ? (
        <ul aria-label="Tales" className="divide-y divide-rule">
          {tales.map((tale) => (
            <TaleItem key={tale.perspectiveId} tale={tale} />
          ))}
        </ul>
      ) : (
        <ul aria-label="Arcs" className="divide-y divide-rule">
          {arcs.map((arc) => (
            <ArcItem key={arc.arcId} arc={arc} />
          ))}
        </ul>
      )}
      {epic.hasNextPage && (
        <Button variant="secondary" className="mt-4" disabled={epic.isFetchingNextPage} onClick={() => void epic.fetchNextPage()}>
          {epic.isFetchingNextPage ? "Loading…" : "Show more"}
        </Button>
      )}
    </>
  );
}

function PlaceLinks({ places, label }: { places: Place[]; label: string }) {
  return (
    <ul aria-label={label} className="mt-2 flex flex-wrap gap-x-4 md:block">
      {places.map((child) => (
        <li key={child.id}>
          <Link
            to={`/places/${child.id}`}
            className="inline-flex min-h-11 max-w-full items-center wrap-anywhere text-accent underline underline-offset-4"
          >
            {child.name}
          </Link>
        </li>
      ))}
    </ul>
  );
}

/**
 * The places inside this one. Those with stories come first, since they are
 * what a reader came for; then a filter, because a city holds thousands of
 * places; then every place in name order, a page at a time.
 */
function Children({ place }: { place: Place }) {
  // A store is a leaf of the hierarchy.
  const enabled = place.kind !== PlaceKind.STORE;
  const [filter, setFilter] = useState("");
  // A city holds hundreds of places; the full list waits until asked for.
  const [browsing, setBrowsing] = useState(false);
  const [category, setCategory] = useState<PlaceCategory>(PlaceCategory.UNSPECIFIED);
  const query = useDebounced(filter.trim(), 200);
  const storied = useQuery({
    queryKey: ["place", "listChildren", { parentId: place.id, withStories: true }],
    queryFn: ({ signal }) => clients.place.listChildren({ parentId: place.id, withStories: true, pageSize: 20 }, { signal }),
    enabled,
  });
  const children = useInfiniteQuery({
    queryKey: ["place", "listChildren", { parentId: place.id, category }],
    queryFn: ({ pageParam, signal }) =>
      clients.place.listChildren({ parentId: place.id, category, pageToken: pageParam, pageSize: 100 }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
    enabled: enabled && query === "" && browsing,
  });
  const found = useQuery({
    queryKey: ["place", "searchPlaces", { query, parentId: place.id }],
    queryFn: ({ signal }) => clients.place.searchPlaces({ query, parentId: place.id }, { signal }),
    enabled: enabled && query.length >= 2,
  });
  const withStories = storied.data?.places ?? [];
  const all = children.data?.pages.flatMap((p) => p.places) ?? [];
  if (!enabled) return null;
  const filterId = `filter-${place.id}`;
  return (
    <nav aria-label={`Places in ${place.name}`} className="grid min-w-0 gap-6">
      {withStories.length > 0 && (
        <section aria-labelledby={`storied-${place.id}`} className="min-w-0">
          <h2 id={`storied-${place.id}`} className="text-xl wrap-anywhere">
            Where people wrote
          </h2>
          <PlaceLinks places={withStories} label="Places with stories" />
        </section>
      )}
      <section aria-labelledby={`all-${place.id}`} className="min-w-0">
        <h2 id={`all-${place.id}`} className="text-xl wrap-anywhere">
          Places in {place.name}
        </h2>
        <label htmlFor={filterId} className="sr-only">
          Find a place in {place.name}
        </label>
        <Input
          id={filterId}
          type="search"
          autoComplete="off"
          placeholder={`Find a place in ${place.name}`}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="mt-2"
        />
        {place.kind !== PlaceKind.STATE && query === "" && (
          <div role="group" aria-label="Kinds of place" className="mt-3 flex flex-wrap gap-1.5">
            {[PlaceCategory.UNSPECIFIED, ...placeCategories].map((c) => (
              <button
                key={c}
                type="button"
                aria-pressed={browsing && category === c}
                onClick={() => {
                  setCategory(c);
                  setBrowsing(true);
                }}
                className={cn(
                  "min-h-9 rounded-full border px-3 text-sm",
                  browsing && category === c ? "border-accent bg-accent/10 text-accent" : "border-rule text-ink hover:border-accent",
                )}
              >
                {c === PlaceCategory.UNSPECIFIED ? "All" : placeCategoryLabel(c)}
              </button>
            ))}
          </div>
        )}
        {query.length >= 2 ? (
          found.isSuccess && found.data.places.length === 0 ? (
            <p className="mt-2 text-sm text-ink-muted">No place in {place.name} matches “{query}”.</p>
          ) : (
            <PlaceLinks places={found.data?.places ?? []} label="Matching places" />
          )
        ) : (
          browsing ? (
            <>
              {all.length === 0 && children.isSuccess && (
                <p className="mt-2 text-sm text-ink-muted">No {placeCategoryLabel(category).toLowerCase()} places here yet.</p>
              )}
              <PlaceLinks places={all} label={`All places in ${place.name}`} />
              {children.hasNextPage && (
                <Button variant="ghost" size="sm" disabled={children.isFetchingNextPage} onClick={() => void children.fetchNextPage()}>
                  More places
                </Button>
              )}
            </>
          ) : (
            <Button variant="ghost" size="sm" className="mt-2" onClick={() => setBrowsing(true)}>
              Browse all places in {place.name}
            </Button>
          )
        )}
      </section>
    </nav>
  );
}

/**
 * A place page is its epic: arcs at the place and at every place inside it,
 * Ranked (the server's order: canon first, then member count, then newest),
 * Newest, or the Tales told there. The order is the server's and is never
 * re-sorted here.
 */
export function Component() {
  const { placeId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const tab = parseTab(params.get("tab"));
  const genre = parseGenre(params.get("genre"));

  const placeQuery = useQuery({
    queryKey: ["place", "getPlace", { placeId }],
    queryFn: ({ signal }) => clients.place.getPlace({ placeId }, { signal }),
  });

  function update(key: "tab" | "genre", value: string, fallback: string) {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === fallback) next.delete(key);
        else next.set(key, value);
        return next;
      },
      { replace: true },
    );
  }

  if (placeQuery.isPending) {
    return (
      <p role="status" className="text-ink-muted">
        Loading…
      </p>
    );
  }
  if (placeQuery.isError || !placeQuery.data.place) {
    const missing = !placeQuery.data?.place || isNotFound(placeQuery.error);
    return (
      <section className="mx-auto max-w-measure py-12" aria-labelledby="place-missing">
        <h1 id="place-missing" className="text-2xl sm:text-3xl">
          {missing ? "Place not found" : "This place could not be loaded"}
        </h1>
        <p className="mt-3 text-ink-muted">{missing ? "It may have been removed or hidden." : "Try again in a moment."}</p>
        <p className="mt-6">
          <Link to="/" className="text-accent underline underline-offset-4">
            Search for a place
          </Link>
        </p>
      </section>
    );
  }
  const place = placeQuery.data.place;
  const epicTab = tabs.find((t) => t.slug === tab)!.tab;

  return (
    <div className="min-w-0">
      <header className="min-w-0">
        {place.ancestors.length > 0 && (
          <nav aria-label="Breadcrumb">
            <ol className="flex flex-wrap items-center gap-x-2 text-sm text-ink-muted">
              {place.ancestors.map((a, i) => (
                <li key={a.id} className="inline-flex items-center gap-2">
                  {i > 0 && <span aria-hidden>›</span>}
                  <Link to={`/places/${a.id}`} className="inline-flex min-h-11 items-center wrap-anywhere text-accent underline underline-offset-4">
                    {a.name}
                  </Link>
                </li>
              ))}
            </ol>
          </nav>
        )}
        <h1 className="text-2xl wrap-anywhere sm:text-3xl">{place.name}</h1>
        <p className="mt-1 text-sm text-ink-muted">{placeKindLabel(place.kind)}</p>
        <div className="mt-4 flex flex-wrap gap-2">
          <Button asChild variant="secondary">
            <Link to={`/compose?placeId=${encodeURIComponent(place.id)}`}>
              <PenLine aria-hidden size={16} />
              Write about this place
            </Link>
          </Button>
          {/* A city, town or mall holds smaller places; a store holds none. */}
          {place.kind !== PlaceKind.STORE && place.kind !== PlaceKind.STATE && (
            <Button asChild variant="ghost">
              <Link to={`/compose?in=${encodeURIComponent(place.id)}`}>
                <Plus aria-hidden size={16} />
                Add a place here
              </Link>
            </Button>
          )}
        </div>
      </header>

      <div className="mt-8 grid min-w-0 gap-8 md:grid-cols-[minmax(0,1fr)_16rem]">
        <section aria-label="Epic" className="min-w-0">
          <Tabs value={tab} onValueChange={(v) => update("tab", v, "ranked")}>
            <div className="flex min-w-0 flex-wrap items-end justify-between gap-3">
              <TabsList aria-label="Epic listing">
                {tabs.map((t) => (
                  <TabsTrigger key={t.slug} value={t.slug}>
                    {t.label}
                  </TabsTrigger>
                ))}
              </TabsList>
              <Select value={genre === Genre.UNSPECIFIED ? ALL_GENRES : genreSlug(genre)} onValueChange={(v) => update("genre", v, ALL_GENRES)}>
                <SelectTrigger aria-label="Genre" className="w-auto min-w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL_GENRES}>Every genre</SelectItem>
                  {genres.map((g) => (
                    <SelectItem key={g} value={genreSlug(g)}>
                      {genreLabel(g)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {tabs.map((t) => (
              <TabsContent key={t.slug} value={t.slug}>
                {t.slug === tab && <EpicList place={place} tab={epicTab} genre={genre} />}
              </TabsContent>
            ))}
          </Tabs>
        </section>
        <aside className="min-w-0">
          <Children place={place} />
        </aside>
      </div>
    </div>
  );
}
