import { describe, expect, it } from "vitest";

import { safeNext } from "./rules";

describe("safeNext", () => {
  it.each([
    ["/perspectives/p-1?claim=c-2", "/perspectives/p-1?claim=c-2"],
    ["/", "/"],
    [null, "/"],
    ["", "/"],
    ["https://evil.example", "/"],
    ["//evil.example", "/"],
    ["/\\evil.example", "/"],
    // URL parsing drops tabs and newlines, so these become "//evil.example".
    ["/\t/evil.example", "/"],
    ["/\n/evil.example", "/"],
    ["/\r/evil.example", "/"],
    ["/a\\b", "/"],
    ["javascript:alert(1)", "/"],
  ])("%j goes to %j", (next, want) => {
    expect(safeNext(next)).toBe(want);
  });
});
