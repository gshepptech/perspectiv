import { describe, expect, it } from "vitest";

import { lastUsed } from "./DeviceList";

describe("lastUsed", () => {
  const now = new Date("2026-10-30T12:00:00Z");
  it.each([
    ["2026-10-30T11:59:30Z", "just now"],
    ["2026-10-30T11:55:00Z", "5 minutes ago"],
    ["2026-10-30T09:00:00Z", "3 hours ago"],
    ["2026-10-29T12:00:00Z", "yesterday"],
    ["2026-10-25T12:00:00Z", "5 days ago"],
  ])("%s reads %s", (at, want) => {
    expect(lastUsed(new Date(at), now)).toBe(want);
  });
});
