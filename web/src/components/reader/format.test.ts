import { describe, expect, it } from "vitest";

import { formatDayBeside } from "./format";

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
