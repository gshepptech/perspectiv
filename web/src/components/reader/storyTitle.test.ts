import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";

import { PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import { PerspectiveSchema } from "@/gen/perspectiv/v1/perspective_pb";

import { storyTitle } from "./storyTitle";

const draft = PerspectiveStatus.DRAFT;
const published = PerspectiveStatus.PUBLISHED;

describe("storyTitle", () => {
  it.each([
    { name: "a published story takes its day's title", p: { status: published, arcTitle: "Rain on the windows" }, want: "Rain on the windows" },
    { name: "a published story with no title it may show", p: { status: published }, want: "A story" },
    {
      name: "a draft joining a day takes the day's title over its own",
      p: { status: draft, arcTitle: "Rain on the windows", titleIfNewArc: "Mine", parts: [{ position: 1, body: "Words." }] },
      want: "Rain on the windows",
    },
    { name: "a draft starting a day takes the title it gave", p: { status: draft, titleIfNewArc: "Frost on the fountain" }, want: "Frost on the fountain" },
    {
      name: "a draft with no title takes its first six words",
      p: { status: draft, parts: [{ position: 2, body: "Later part." }, { position: 1, body: " One  two three\nfour five six seven" }] },
      want: "One two three four five six…",
    },
    { name: "a short draft takes all its words", p: { status: draft, parts: [{ position: 1, body: "Snow again." }] }, want: "Snow again." },
    {
      name: "a draft whose first part is blank takes the next",
      p: { status: draft, parts: [{ position: 1, body: "   " }, { position: 2, body: "Second part." }] },
      want: "Second part.",
    },
    { name: "an empty draft", p: { status: draft }, want: "Untitled draft" },
  ])("$name", ({ p, want }) => {
    expect(storyTitle(create(PerspectiveSchema, p))).toBe(want);
  });
});
