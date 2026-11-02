import { describe, expect, it } from "vitest";

import { suggestPlaceName } from "./placeName";

describe("suggestPlaceName", () => {
  it.each([
    { name: "an all-lower-case name is title-cased", typed: "joes crab shack", want: "Joes Crab Shack" },
    { name: "minor words stay lower case inside it", typed: "the hall of fame and museum", want: "The Hall of Fame and Museum" },
    { name: "a minor word closing it is capitalised", typed: "the place to be in", want: "The Place to Be In" },
    { name: "a hyphen starts a word, an apostrophe does not", typed: "joe's bar-b-que", want: "Joe's Bar-B-Que" },
    { name: "a slash starts a word", typed: "bar/grill", want: "Bar/Grill" },
    { name: "a digit holds the next letter lower case", typed: "7-eleven 24hr", want: "7-Eleven 24hr" },
    { name: "a name with any capital is kept as typed", typed: "McDonald's", want: "McDonald's" },
    { name: "an all-capital name is kept as typed", typed: "IHOP", want: "IHOP" },
    { name: "spaces are collapsed", typed: "  joes   crab ", want: "Joes Crab" },
    { name: "nothing typed is nothing", typed: "   ", want: "" },
  ])("$name", ({ typed, want }) => {
    expect(suggestPlaceName(typed)).toBe(want);
  });
});
