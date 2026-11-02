import { useQuery } from "@tanstack/react-query";
import { Plus, Search } from "lucide-react";
import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";

import { placeChain, storiesLabel } from "@/components/reader/format";
import { Input } from "@/components/ui/Input";
import type { Place, SearchSplit } from "@/gen/perspectiv/v1/place_pb";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";

import { suggestPlaceName } from "./placeName";

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

/** What a search that reached its cap says under its `count` places. */
export function morePlaces(count: number): string {
  return `Showing the first ${count}. Add a town or state to narrow it.`;
}

/** The line under a capped list of places, telling the reader to narrow it. */
export function MorePlacesNote({ count, className }: { count: number; className?: string }) {
  return <p className={cn("text-sm text-ink-muted", className)}>{morePlaces(count)}</p>;
}

/** The status line's words for one answer of the search (Folio, Place search). */
function statusText(query: string, pending: boolean, failed: boolean, count: number, more: boolean): string {
  if (pending) return "Searching…";
  if (failed) return "";
  if (count === 0) return `No places match “${query}”.`;
  if (more) return `Showing the first ${count} places that match “${query}”. Add a town or state to narrow it.`;
  return `${count} ${count === 1 ? "place matches" : "places match"} “${query}”.`;
}

/**
 * The place search: a WAI-ARIA combobox whose field keeps focus while ↓ and
 * ↑ move through the matching places, Enter opens one and Escape closes
 * the list, then clears the field (Folio, Place search). `text` is the
 * field's value and `query` the trimmed text once typing pauses, which is
 * what is searched. `large` is the front page's 56px call to action.
 * `onFollow` runs when a result is opened (the header dialog closes on it).
 */
export function PlaceSearch({
  text,
  onTextChange,
  query,
  large = false,
  className,
  onFollow,
}: {
  text: string;
  onTextChange: (text: string) => void;
  query: string;
  large?: boolean;
  className?: string;
  onFollow?: () => void;
}) {
  const inputId = useId();
  const listId = useId();
  const listRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();
  const search = useQuery({
    queryKey: ["place", "searchPlaces", { query }],
    queryFn: ({ signal }) => clients.place.searchPlaces({ query }, { signal }),
    enabled: query !== "",
  });
  const places = query === "" ? [] : (search.data?.places ?? []);
  // The active result is held by id, so a new answer that lacks it leaves
  // none active without an effect to reset it.
  const [activeId, setActiveId] = useState<string | null>(null);
  const active = places.findIndex((p) => p.id === activeId);
  // Escape closes the list until the text changes or an arrow reopens it.
  const [closed, setClosed] = useState(false);
  // Enter pressed before the answer for the text in the field arrived.
  const [enterWaiting, setEnterWaiting] = useState(false);
  const expanded = places.length > 0 && !closed;
  // The answer on screen is for the text in the field, not an earlier one.
  const answered = query !== "" && query === text.trim() && !search.isPending;

  const optionId = (place: Place) => `${listId}-${place.id}`;

  function open(place: Place) {
    onFollow?.();
    void navigate(`/places/${place.id}`);
  }

  // An Enter that came early opens the first result once the answer is in.
  useEffect(() => {
    if (!enterWaiting || !answered) return;
    setEnterWaiting(false);
    const first = search.data?.places[0];
    if (search.isSuccess && first) {
      onFollow?.();
      void navigate(`/places/${first.id}`);
    }
  }, [enterWaiting, answered, search.isSuccess, search.data, onFollow, navigate]);

  // Keep the active row in sight when the list scrolls (the dialog on a phone).
  useEffect(() => {
    if (active < 0) return;
    listRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: "nearest" });
  }, [active]);

  function move(step: 1 | -1) {
    if (places.length === 0) return;
    setClosed(false);
    const from = active < 0 ? (step === 1 ? -1 : places.length) : active;
    const next = (from + step + places.length) % places.length;
    setActiveId(places[next]!.id);
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    // Keys that finish composing a character (an IME) belong to the text.
    if (e.nativeEvent.isComposing) return;
    switch (e.key) {
      case "ArrowDown":
      case "ArrowUp":
        e.preventDefault();
        move(e.key === "ArrowDown" ? 1 : -1);
        return;
      case "Enter": {
        e.preventDefault();
        if (text.trim() === "") return;
        const chosen = expanded ? (places[active] ?? places[0]) : undefined;
        if (answered && chosen) open(chosen);
        else if (!answered) setEnterWaiting(true);
        return;
      }
      case "Escape":
        if (expanded) {
          e.preventDefault();
          setClosed(true);
          setActiveId(null);
        } else if (text !== "") {
          e.preventDefault();
          onTextChange("");
        }
        return;
      case "ArrowLeft":
      case "ArrowRight":
      case "Home":
      case "End":
        // The caret moves in the text, as in any field; the list lets go.
        setActiveId(null);
        return;
    }
  }

  const more = search.data?.more ?? false;
  return (
    <>
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
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={expanded}
            aria-controls={listId}
            aria-activedescendant={expanded && active >= 0 ? optionId(places[active]!) : undefined}
            enterKeyHint="search"
            autoComplete="off"
            spellCheck={false}
            maxLength={PLACE_QUERY_MAX}
            placeholder="A store, mall, town or city"
            value={text}
            onChange={(e) => {
              onTextChange(e.target.value);
              setActiveId(null);
              setClosed(false);
              setEnterWaiting(false);
            }}
            onKeyDown={onKeyDown}
            className={large ? "min-h-14 pr-4 pl-13 text-lg" : "pl-10"}
          />
        </div>
      </form>
      {query !== "" && (
        // One status line, always mounted while there is a query, so a screen
        // reader hears each answer.
        <p role="status" className={places.length > 0 ? "sr-only" : "mt-4 text-sm text-ink-muted"}>
          {statusText(query, search.isPending, search.isError, places.length, more)}
        </p>
      )}
      {query !== "" && search.isError && (
        <p role="alert" className="mt-4 text-sm text-error">
          Search is unavailable right now. Try again in a moment.
        </p>
      )}
      <div
        ref={listRef}
        id={listId}
        role="listbox"
        aria-label="Matching places"
        hidden={!expanded}
        className="mt-4 divide-y divide-rule border-y border-rule"
      >
        {places.map((place, i) => (
          <Link
            key={place.id}
            id={optionId(place)}
            role="option"
            aria-selected={i === active}
            tabIndex={-1}
            to={`/places/${place.id}`}
            onClick={onFollow}
            className={cn(
              "flex min-h-11 min-w-0 flex-col justify-center py-2 hover:bg-surface sm:px-2",
              i === active && "bg-surface outline-2 -outline-offset-2 outline-accent",
            )}
          >
            <span className="font-serif text-lg wrap-anywhere text-accent underline-offset-4 hover:underline">{place.name}</span>
            {/* The row is the option's name; the comma parts its two lines when read aloud. */}
            <span className="sr-only">{", "}</span>
            <span className="text-sm wrap-anywhere text-ink-muted">
              {placeChain(place.kind, place.ancestors, place.category)} · {storiesLabel(place.stories)}
            </span>
          </Link>
        ))}
      </div>
      {!closed && more && places.length > 0 && <MorePlacesNote count={places.length} className="mt-3" />}
      {!closed && query !== "" && search.isSuccess && <AddPlaceLink query={query} split={search.data.split} onFollow={onFollow} />}
    </>
  );
}

/**
 * The way in when the place someone was at is not listed: it opens the
 * composer's add form with the name searched for, and with the town or
 * state the search read in it when it read one (Folio, Place search). A
 * signed-out reader is sent to sign in first and comes back here.
 */
function AddPlaceLink({ query, split, onFollow }: { query: string; split?: SearchSplit; onFollow?: () => void }) {
  const name = suggestPlaceName(split?.name || query);
  const params = new URLSearchParams({ add: name });
  if (split?.localityId) params.set("in", split.localityId);
  return (
    <Link
      to={`/compose?${params.toString()}`}
      onClick={onFollow}
      className="mt-3 inline-flex min-h-11 min-w-0 items-center gap-2 rounded-full border border-dashed border-control px-4 text-sm text-ink hover:border-accent hover:text-accent"
    >
      <Plus aria-hidden size={16} className="shrink-0" />
      <span className="min-w-0 wrap-anywhere">
        {split?.localityId ? `Not here? Add “${name}” in ${split.localityLabel}` : `Not here? Add “${name}” as a new place`}
      </span>
    </Link>
  );
}
