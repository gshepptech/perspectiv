import { Code, ConnectError } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { PLACE_QUERY_MAX } from "@/components/search/PlaceSearch";
import { suggestPlaceName } from "@/components/search/placeName";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { PlaceKind } from "@/gen/perspectiv/v1/enums_pb";
import type { Place } from "@/gen/perspectiv/v1/place_pb";
import { clients } from "@/lib/connect";

import { MIN_QUERY, SEARCH_DEBOUNCE_MS, useDebounced } from "./search";

// The server's limit on a place name, in characters.
const MAX_NAME = 120;

const schema = z.object({
  name: z
    .string()
    .refine((n) => n.trim() !== "", "Give the place a name.")
    .refine((n) => [...n.trim()].length <= MAX_NAME, `Keep the name to ${MAX_NAME} characters.`),
  kind: z.enum([String(PlaceKind.STORE), String(PlaceKind.MALL)]),
  parentId: z.string().min(1, "Choose the place it is in."),
});
type Values = z.infer<typeof schema>;

const kindChoices = [
  { value: String(PlaceKind.STORE), label: "A place: a shop, restaurant, bar, park or venue" },
  { value: String(PlaceKind.MALL), label: "A mall or shopping center" },
];

// A place can sit only under a place of a higher level: STATE, CITY, TOWN,
// MALL, STORE, in that order, as the PlaceKind values run.
function canHold(parent: Place, kind: PlaceKind): boolean {
  return parent.kind !== PlaceKind.UNSPECIFIED && parent.kind < kind;
}

function refusal(error: unknown, name: string): string {
  const e = ConnectError.from(error);
  switch (e.code) {
    case Code.AlreadyExists:
      return `There is already a place called “${name}” there. Search for it above and pick it instead.`;
    case Code.NotFound:
      return "The place you chose is no longer available. Choose another one.";
    case Code.InvalidArgument:
      return `That place cannot be added: ${e.rawMessage}.`;
    case Code.PermissionDenied:
      return "Verify your email address before adding a place.";
    default:
      return `The place could not be added: ${e.rawMessage}`;
  }
}

function placeTrail(p: Place): string {
  return p.ancestors
    .map((a) => a.name)
    .reverse()
    .join(", ");
}

/**
 * Adds a mall or store that the place search does not find, under a place the
 * author picks, then hands the new place to the composer. A name typed all
 * in lower case starts out title-cased, as the importer writes such names.
 */
export function AddPlace({
  initialName,
  initialParentId = "",
  onAdded,
  onCancel,
}: {
  initialName: string;
  /** The place it is in, already chosen: the page the author added it from. */
  initialParentId?: string;
  onAdded: (place: Place) => void;
  onCancel: () => void;
}) {
  const titleId = useId();
  const nameId = useId();
  const nameErrorId = useId();
  const nameHintId = useId();
  const whereId = useId();
  const parentErrorId = useId();
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: suggestPlaceName(initialName), kind: String(PlaceKind.STORE), parentId: initialParentId },
  });
  const kind = Number(form.watch("kind")) as PlaceKind;
  const where = form.watch("parentId");
  const [whereText, setWhereText] = useState("");
  const query = useDebounced(whereText.trim(), SEARCH_DEBOUNCE_MS);
  const search = useQuery({
    queryKey: ["place", "searchPlaces", query],
    queryFn: () => clients.place.searchPlaces({ query }),
    enabled: query.length >= MIN_QUERY,
  });
  const preset = useQuery({
    queryKey: ["place", "getPlace", initialParentId],
    queryFn: () => clients.place.getPlace({ placeId: initialParentId }),
    enabled: initialParentId !== "",
  });
  // The preset place stays offered (and chosen) until the author searches for another.
  const presetPlace = preset.data?.place;
  const found = search.data?.places ?? [];
  const parents = [...(presetPlace && query === "" ? [presetPlace] : []), ...found].filter((p) => canHold(p, kind));
  const create = useMutation({
    mutationFn: (v: Values) =>
      clients.place.createPlace({ name: v.name.trim(), kind: Number(v.kind) as PlaceKind, parentId: v.parentId }),
    onSuccess: (res) => {
      if (res.place) onAdded(res.place);
    },
  });
  const { errors } = form.formState;

  return (
    <form
      aria-labelledby={titleId}
      noValidate
      onSubmit={form.handleSubmit((v) => create.mutate(v))}
      className="grid min-w-0 gap-4 rounded-md border border-rule p-4"
    >
      <h2 id={titleId} className="text-xl">
        Add a place
      </h2>
      <div className="grid gap-1.5">
        <label htmlFor={nameId} className="text-sm font-medium text-ink">
          Name
        </label>
        <Input
          id={nameId}
          maxLength={MAX_NAME}
          autoComplete="off"
          aria-invalid={errors.name ? true : undefined}
          aria-describedby={errors.name ? `${nameErrorId} ${nameHintId}` : nameHintId}
          {...form.register("name")}
        />
        <p id={nameHintId} className="text-sm text-ink-muted">
          More than one in town? Add the street, like “Dollar General, Main St”.
        </p>
        {errors.name && (
          <p id={nameErrorId} className="text-sm font-medium text-error">
            {errors.name.message}
          </p>
        )}
      </div>
      <fieldset className="grid gap-1">
        <legend className="mb-1 text-sm font-medium text-ink">What is it?</legend>
        {kindChoices.map((c) => (
          <label key={c.value} className="inline-flex min-h-11 cursor-pointer items-center gap-3 text-base text-ink">
            <input
              type="radio"
              value={c.value}
              {...form.register("kind", {
                // A mall cannot sit in the mall a store could: changing the kind
                // drops a chosen place that can no longer hold it.
                onChange: () => form.setValue("parentId", ""),
              })}
              className="size-5 shrink-0 accent-accent"
            />
            <span>{c.label}</span>
          </label>
        ))}
      </fieldset>
      <div className="grid gap-1.5">
        <label htmlFor={whereId} className="text-sm font-medium text-ink">
          Where is it?
        </label>
        <Input
          id={whereId}
          type="search"
          maxLength={PLACE_QUERY_MAX}
          value={whereText}
          onChange={(e) => setWhereText(e.target.value)}
          placeholder="The mall, town or city it is in"
          autoComplete="off"
          aria-describedby={errors.parentId ? parentErrorId : undefined}
        />
        {search.isError && (
          <p role="alert" className="text-sm font-medium text-error">
            Search failed. Try again.
          </p>
        )}
        {search.isSuccess && parents.length === 0 && (
          <p className="text-sm text-ink-muted">No place it could be in matches “{query}”.</p>
        )}
        {parents.length > 0 && (
          <div role="radiogroup" aria-label="Places it can be in" className="grid gap-1">
            {parents.map((p) => (
              <label
                key={p.id}
                className="flex min-h-11 min-w-0 cursor-pointer items-start gap-3 py-1 text-base text-ink"
              >
                <input
                  type="radio"
                  value={p.id}
                  checked={where === p.id}
                  {...form.register("parentId")}
                  className="mt-1 size-5 shrink-0 accent-accent"
                />
                <span className="grid min-w-0">
                  <span className="font-medium [overflow-wrap:anywhere]">{p.name}</span>
                  {p.ancestors.length > 0 && (
                    <span className="text-sm text-ink-muted [overflow-wrap:anywhere]">{placeTrail(p)}</span>
                  )}
                </span>
              </label>
            ))}
          </div>
        )}
        {errors.parentId && (
          <p id={parentErrorId} className="text-sm font-medium text-error">
            {errors.parentId.message}
          </p>
        )}
      </div>
      {create.isError && (
        <p role="alert" className="text-sm font-medium text-error [overflow-wrap:anywhere]">
          {refusal(create.error, form.getValues("name").trim())}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" pending={create.isPending}>
          Add and write
        </Button>
        <Button type="button" variant="secondary" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
