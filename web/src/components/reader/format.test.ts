import { describe, expect, it } from "vitest";

import { LoreStatus, PlaceCategory, PlaceKind } from "@/gen/perspectiv/v1/enums_pb";

import {
  categoryFromSlug,
  categorySlug,
  formatDayBeside,
  placeCategories,
  placeCategoryPlural,
  placeChain,
  placeLabel,
  storiesLabel,
  storyStanding,
  storyStandingNote,
} from "./format";

describe("formatDayBeside", () => {
  it.each([
    { name: "an hour arc's own title already says the hour", title: "Dryer number six · 23:00", hour: 23, want: "October 11, 2026" },
    { name: "a retitled hour arc keeps the hour on the date", title: "The spin cycle", hour: 23, want: "October 11, 2026, 23:00" },
    { name: "a different hour in the title is not this hour", title: "Late · 22:00", hour: 23, want: "October 11, 2026, 23:00" },
    { name: "a day arc has no hour", title: "Dryer number six", hour: undefined, want: "October 11, 2026" },
  ])("$name", ({ title, hour, want }) => {
    expect(formatDayBeside(title, "2026-10-11", hour)).toBe(want);
  });
});

describe("search result labels", () => {
  const tampa = [
    { id: "s", kind: PlaceKind.STATE, name: "Florida" },
    { id: "c", kind: PlaceKind.CITY, name: "Tampa" },
  ];
  it("names a categorised store by what it is, not as a store", () => {
    expect(placeChain(PlaceKind.STORE, tampa as never, PlaceCategory.SPORTS_AND_RECREATION)).toBe("Sports in Tampa, Florida");
    expect(placeChain(PlaceKind.STORE, tampa as never)).toBe("Store in Tampa, Florida");
    expect(placeChain(PlaceKind.MALL, tampa as never, PlaceCategory.ARTS_AND_ENTERTAINMENT)).toBe("Arts & entertainment in Tampa, Florida");
    expect(placeChain(PlaceKind.CITY, tampa.slice(0, 1) as never, PlaceCategory.SPORTS_AND_RECREATION)).toBe("City in Florida");
  });

  it("says what a place is on its own page the same way", () => {
    expect(placeLabel(PlaceKind.STORE, PlaceCategory.SPORTS_AND_RECREATION)).toBe("Sports");
    expect(placeLabel(PlaceKind.STORE)).toBe("Store");
    expect(placeLabel(PlaceKind.CITY, PlaceCategory.SPORTS_AND_RECREATION)).toBe("City");
  });

  it.each([
    [0, "No stories yet"],
    [1, "1 story"],
    [7, "7 stories"],
    [100, "100+ stories"],
  ])("%i stories reads %s", (n, want) => {
    expect(storiesLabel(n)).toBe(want);
  });
});

describe("storyStanding", () => {
  it.each([
    [LoreStatus.IN_LORE, false, "day", "In a lore"],
    [LoreStatus.IN_LORE, true, "day", "Canon of this day"],
    [LoreStatus.IN_LORE, true, "hour", "Canon of this hour"],
    [LoreStatus.PENDING, false, "day", "Waiting for a link"],
    [LoreStatus.SOLO, false, "day", "A tale (no linked stories)"],
    [LoreStatus.REMOVED, false, "day", "A tale (voted out of its lore)"],
    [LoreStatus.UNSPECIFIED, false, "day", ""],
  ] as const)("says %s (canon %s, %s) as %j", (status, canon, span, want) => {
    expect(storyStanding(status, canon, span)).toBe(want);
  });

  it("explains every standing a published story can have, and never credits readers with the canon", () => {
    for (const status of [LoreStatus.IN_LORE, LoreStatus.PENDING, LoreStatus.SOLO, LoreStatus.REMOVED]) {
      for (const canon of [false, true]) {
        const note = storyStandingNote(status, canon, "day");
        expect(note).not.toBe("");
        expect(note).not.toMatch(/reader/i);
      }
    }
  });
});

describe("kinds of place in the address and in a title", () => {
  it.each([
    [PlaceCategory.SPORTS_AND_RECREATION, "sports-and-recreation", "sports and recreation places"],
    [PlaceCategory.FOOD_AND_DRINK, "food-and-drink", "food and drink places"],
    [PlaceCategory.NIGHTLIFE, "nightlife", "bars and clubs"],
    [PlaceCategory.LODGING, "lodging", "hotels"],
  ])("%s is /best/%s, the best days at %s", (category, slug, plural) => {
    expect(categorySlug(category)).toBe(slug);
    expect(categoryFromSlug(slug)).toBe(category);
    expect(placeCategoryPlural(category)).toBe(plural);
  });

  it("gives every kind its own slug and a plural that reads after 'at'", () => {
    const slugs = placeCategories.map(categorySlug);
    expect(new Set(slugs).size).toBe(placeCategories.length);
    for (const c of placeCategories) {
      expect(categorySlug(c)).toMatch(/^[a-z]+(-[a-z]+)*$/);
      expect(categoryFromSlug(categorySlug(c))).toBe(c);
      expect(placeCategoryPlural(c)).toMatch(/^[a-z][a-z ]+$/);
    }
  });

  it.each(["", "unspecified", "Sports-And-Recreation", "sports_and_recreation", "bowling"])("names no kind for %j", (slug) => {
    expect(categoryFromSlug(slug)).toBeUndefined();
  });
});
