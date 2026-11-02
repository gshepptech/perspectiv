import { useQuery } from "@tanstack/react-query";
import { MapPin } from "lucide-react";
import { useId, useState } from "react";

import { placeChain, storiesLabel } from "@/components/reader/format";
import { MorePlacesNote, PLACE_QUERY_MAX } from "@/components/search/PlaceSearch";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import type { Place } from "@/gen/perspectiv/v1/place_pb";
import { clients } from "@/lib/connect";

import { AddPlace } from "./AddPlace";
import { MIN_QUERY, SEARCH_DEBOUNCE_MS, useDebounced } from "./search";

/**
 * The first step of writing: find the place the story happened, or add the
 * mall or store when the search does not know it. Each result says what and
 * where it is and how many stories it holds, in the server's order (places
 * with stories first within a match tier), so friends pick the same entry.
 */
export function PlacePicker({
  onPick,
  addName,
  addIn = "",
}: {
  onPick: (place: Place) => void;
  /** Opens the add form at once, with this name filled in. */
  addName?: string;
  /** Opens the add form at once, with this place chosen as where it is. */
  addIn?: string;
}) {
  const inputId = useId();
  const [text, setText] = useState(addName ?? "");
  const [adding, setAdding] = useState(addName !== undefined || addIn !== "");
  const query = useDebounced(text.trim(), SEARCH_DEBOUNCE_MS);
  const search = useQuery({
    queryKey: ["place", "searchPlaces", query],
    queryFn: () => clients.place.searchPlaces({ query }),
    enabled: query.length >= MIN_QUERY,
  });
  const places = search.data?.places ?? [];
  // How the server read the search in the field as a name and the town or
  // state it is in, when it did: the add form starts from it.
  const split = query === text.trim() ? search.data?.split : undefined;

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
          maxLength={PLACE_QUERY_MAX}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="A shop, restaurant, park, town or city"
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
                  <span className="text-sm text-ink-muted [overflow-wrap:anywhere]">
                    {placeChain(p.kind, p.ancestors, p.category)} · {storiesLabel(p.stories)}
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {search.data?.more && <MorePlacesNote count={places.length} />}
      {adding ? (
        <AddPlace
          initialName={split?.name || text.trim()}
          initialParentId={addIn || split?.localityId || ""}
          onAdded={onPick}
          onCancel={() => setAdding(false)}
        />
      ) : (
        <div className="grid justify-items-start gap-2">
          <p className="text-sm text-ink-muted">Not listed? Add it yourself.</p>
          <Button type="button" variant="secondary" onClick={() => setAdding(true)}>
            Add a place
          </Button>
        </div>
      )}
    </section>
  );
}
