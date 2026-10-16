import { Genre, LoreStatus, PlaceKind } from "@/gen/perspectiv/v1/enums_pb";
import type { PlaceRef } from "@/gen/perspectiv/v1/place_pb";

/** The reader-facing word for a place kind. */
export function placeKindLabel(kind: PlaceKind): string {
  switch (kind) {
    case PlaceKind.STATE:
      return "State";
    case PlaceKind.CITY:
      return "City";
    case PlaceKind.TOWN:
      return "Town";
    case PlaceKind.MALL:
      return "Mall";
    case PlaceKind.STORE:
      return "Store";
    case PlaceKind.UNSPECIFIED:
      return "Place";
  }
}

/** "Mall in Springfield, Illinois": the kind, then the ancestors from the nearest up to the state. */
export function placeChain(kind: PlaceKind, ancestors: readonly PlaceRef[]): string {
  const label = placeKindLabel(kind);
  if (ancestors.length === 0) return label;
  return `${label} in ${[...ancestors].reverse().map((a) => a.name).join(", ")}`;
}

export function genreLabel(genre: Genre): string {
  switch (genre) {
    case Genre.DRAMA:
      return "Drama";
    case Genre.COMEDY:
      return "Comedy";
    case Genre.ROMANCE:
      return "Romance";
    case Genre.MYSTERY:
      return "Mystery";
    case Genre.HORROR:
      return "Horror";
    case Genre.SLICE_OF_LIFE:
      return "Slice of life";
    case Genre.ACTION:
      return "Action";
    case Genre.UNSPECIFIED:
      return "";
  }
}

export function loreStatusLabel(status: LoreStatus): string {
  switch (status) {
    case LoreStatus.PENDING:
      return "Waiting for a link";
    case LoreStatus.IN_LORE:
      return "In lore";
    case LoreStatus.SOLO:
      return "Tale";
    case LoreStatus.REMOVED:
      return "Tale, voted out of lore";
    case LoreStatus.UNSPECIFIED:
      return "";
  }
}

/**
 * A place-local day (YYYY-MM-DD) and optional hour as the reader sees it,
 * "October 1, 2026" or "October 1, 2026, 14:00". The day is the place's own
 * calendar day, so it is formatted as a plain date, never shifted into the
 * reader's time zone.
 */
export function formatDay(localDate: string, hour?: number): string {
  const day = /^\d{4}-\d{2}-\d{2}$/.test(localDate)
    ? new Date(`${localDate}T00:00:00Z`).toLocaleDateString("en-US", { timeZone: "UTC", dateStyle: "long" })
    : localDate;
  return hour === undefined ? day : `${day}, ${String(hour).padStart(2, "0")}:00`;
}
