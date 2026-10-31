import { Genre, LoreStatus, PlaceCategory, PlaceKind } from "@/gen/perspectiv/v1/enums_pb";
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

/**
 * What a place is, in one word or phrase: the category of a store or mall
 * that has one ("Sports" for a stadium), else the kind ("Store", "City").
 */
export function placeLabel(kind: PlaceKind, category = PlaceCategory.UNSPECIFIED): string {
  const named = labelledCategory(kind, category);
  return named === PlaceCategory.UNSPECIFIED ? placeKindLabel(kind) : placeCategoryLabel(named);
}

/**
 * The category placeLabel names a place by: a store's or mall's own
 * category, or UNSPECIFIED when the label is the kind instead.
 */
export function labelledCategory(kind: PlaceKind, category = PlaceCategory.UNSPECIFIED): PlaceCategory {
  return kind === PlaceKind.STORE || kind === PlaceKind.MALL ? category : PlaceCategory.UNSPECIFIED;
}

/**
 * "Mall in Springfield, Illinois": the kind, then the ancestors from the
 * nearest up to the state. A store or venue with a category says what it is
 * instead ("Sports in Tampa, Florida"), since a stadium or a park is not a
 * store or a mall to anyone who was there.
 */
export function placeChain(kind: PlaceKind, ancestors: readonly PlaceRef[], category = PlaceCategory.UNSPECIFIED): string {
  const label = placeLabel(kind, category);
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

/** "day" or "hour": what an arc covers, for copy that names it ("Canon of this hour"). */
export type ArcSpan = "day" | "hour";

export function arcSpan(hour: number | undefined): ArcSpan {
  return hour === undefined ? "day" : "hour";
}

/**
 * A story's standing in plain words, for the byline of its own page.
 * `canon` is true when its lore is the arc's canon.
 */
export function storyStanding(status: LoreStatus, canon: boolean, span: ArcSpan): string {
  switch (status) {
    case LoreStatus.IN_LORE:
      return canon ? `Canon of this ${span}` : "In a lore";
    case LoreStatus.PENDING:
      return "Waiting for a link";
    case LoreStatus.SOLO:
      return "A tale (no linked stories)";
    case LoreStatus.REMOVED:
      return "A tale (voted out of its lore)";
    case LoreStatus.UNSPECIFIED:
      return "";
  }
}

/** What a story's standing means, in one line under its byline. */
export function storyStandingNote(status: LoreStatus, canon: boolean, span: ArcSpan): string {
  switch (status) {
    case LoreStatus.IN_LORE:
      return canon
        ? `Its lore is the canon: the lore the people who wrote about this ${span} favour most.`
        : "Its writer and others found each other in their stories, which links them as a lore.";
    case LoreStatus.PENDING:
      return `This story joins a lore when someone it describes claims their character, or when its author claims a character in another story from that ${span}.`;
    case LoreStatus.SOLO:
      return "Nobody linked to it within a week of publishing, so it stands alone.";
    case LoreStatus.REMOVED:
      return "The people in its lore voted it out, so it stands alone.";
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

/**
 * Whether an hour arc's title already ends with its hour (" · 23:00"), as the
 * title the server gives an hour arc when it is made does. A retitled hour arc
 * may not, and then the hour is shown beside it.
 */
export function titleEndsWithHour(title: string, hour?: number): boolean {
  return hour !== undefined && title.endsWith(` · ${String(hour).padStart(2, "0")}:00`);
}

/**
 * formatDay for a date shown beside a title: the hour is left off the date
 * when the title already ends with it, rather than said twice.
 */
export function formatDayBeside(title: string, localDate: string, hour?: number): string {
  return titleEndsWithHour(title, hour) ? formatDay(localDate) : formatDay(localDate, hour);
}

/** The filter chips a city's places can be narrowed by, in the order shown. */
export const placeCategories: readonly PlaceCategory[] = [
  PlaceCategory.FOOD_AND_DRINK,
  PlaceCategory.NIGHTLIFE,
  PlaceCategory.SHOPPING,
  PlaceCategory.OUTDOORS,
  PlaceCategory.ARTS_AND_ENTERTAINMENT,
  PlaceCategory.SPORTS_AND_RECREATION,
  PlaceCategory.CULTURE_AND_HISTORY,
  PlaceCategory.EDUCATION,
  PlaceCategory.COMMUNITY,
  PlaceCategory.LODGING,
  PlaceCategory.TRAVEL,
  PlaceCategory.SERVICES,
  PlaceCategory.HEALTH,
];

export function placeCategoryLabel(c: PlaceCategory): string {
  switch (c) {
    case PlaceCategory.FOOD_AND_DRINK:
      return "Food & drink";
    case PlaceCategory.NIGHTLIFE:
      return "Nightlife";
    case PlaceCategory.SHOPPING:
      return "Shopping";
    case PlaceCategory.OUTDOORS:
      return "Outdoors";
    case PlaceCategory.ARTS_AND_ENTERTAINMENT:
      return "Arts & entertainment";
    case PlaceCategory.SPORTS_AND_RECREATION:
      return "Sports";
    case PlaceCategory.CULTURE_AND_HISTORY:
      return "Culture & history";
    case PlaceCategory.EDUCATION:
      return "Campus & schools";
    case PlaceCategory.COMMUNITY:
      return "Community";
    case PlaceCategory.LODGING:
      return "Hotels";
    case PlaceCategory.TRAVEL:
      return "Travel";
    case PlaceCategory.SERVICES:
      return "Services";
    case PlaceCategory.HEALTH:
      return "Hospitals";
    case PlaceCategory.UNSPECIFIED:
      return "";
    default: {
      const unhandled: never = c;
      return unhandled;
    }
  }
}

/**
 * The places of a category in the words a reader uses, lower case, for
 * "The best days at …": "sports and recreation places", "bars and clubs".
 */
export function placeCategoryPlural(c: PlaceCategory): string {
  switch (c) {
    case PlaceCategory.FOOD_AND_DRINK:
      return "food and drink places";
    case PlaceCategory.NIGHTLIFE:
      return "bars and clubs";
    case PlaceCategory.SHOPPING:
      return "shops";
    case PlaceCategory.OUTDOORS:
      return "parks and outdoor places";
    case PlaceCategory.ARTS_AND_ENTERTAINMENT:
      return "arts and entertainment venues";
    case PlaceCategory.SPORTS_AND_RECREATION:
      return "sports and recreation places";
    case PlaceCategory.CULTURE_AND_HISTORY:
      return "museums and historic places";
    case PlaceCategory.EDUCATION:
      return "campuses and schools";
    case PlaceCategory.COMMUNITY:
      return "community places";
    case PlaceCategory.LODGING:
      return "hotels";
    case PlaceCategory.TRAVEL:
      return "travel and transport places";
    case PlaceCategory.SERVICES:
      return "service businesses";
    case PlaceCategory.HEALTH:
      return "hospitals and clinics";
    case PlaceCategory.UNSPECIFIED:
      return "places";
    default: {
      const unhandled: never = c;
      return unhandled;
    }
  }
}

/**
 * A category's URL slug, its proto enum name in lower case with hyphens:
 * SPORTS_AND_RECREATION is "sports-and-recreation". The server's sitemap
 * derives the same slug from the same enum (place.CategorySlug).
 */
export function categorySlug(c: PlaceCategory): string {
  return PlaceCategory[c].toLowerCase().replaceAll("_", "-");
}

/** The category a slug names, or undefined for anything else (UNSPECIFIED included). */
export function categoryFromSlug(slug: string): PlaceCategory | undefined {
  return placeCategories.find((c) => categorySlug(c) === slug);
}

/** How many stories a search result has; the server counts up to 100. */
export function storiesLabel(n: number): string {
  if (n <= 0) return "No stories yet";
  if (n >= 100) return "100+ stories";
  return n === 1 ? "1 story" : `${n} stories`;
}
