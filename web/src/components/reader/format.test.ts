import { describe, expect, it } from "vitest";

import { PlaceCategory, PlaceKind } from "@/gen/perspectiv/v1/enums_pb";

import { formatDayBeside, placeChain, storiesLabel } from "./format";

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

  it.each([
    [0, "No stories yet"],
    [1, "1 story"],
    [7, "7 stories"],
    [100, "100+ stories"],
  ])("%i stories reads %s", (n, want) => {
    expect(storiesLabel(n)).toBe(want);
  });
});
