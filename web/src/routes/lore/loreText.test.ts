import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";

import { PerspectiveSchema } from "@/gen/perspectiv/v1/perspective_pb";

import { chapterTitles, openingWords } from "./loreText";

const wren = { userId: "u-wren", username: "wren" };
const otter = { userId: "u-otter", username: "otter" };
const ivy = { userId: "u-ivy", username: "ivy" };
const members = new Set(["u-wren", "u-otter", "u-ivy"]);

function story(author: typeof wren, body: string, characters: { position: number; label: string; claimedBy?: typeof wren }[] = []) {
  return create(PerspectiveSchema, {
    id: `p-${author.username}`,
    author,
    parts: [{ position: 1, body }],
    characters: characters.map((c, i) => ({ id: `c-${author.username}-${i}`, ...c })),
  });
}

describe("openingWords", () => {
  it.each([
    { name: "a short opening whole", body: "The power went out.", want: "The power went out." },
    { name: "cut after the last whole word in 40 characters", body: "The rain came in sideways and the bookshop filled up.", want: "The rain came in sideways and the…" },
    { name: "a single overlong word cut inside it", body: `${"a".repeat(50)} b`, want: `${"a".repeat(40)}…` },
    { name: "spacing and line breaks fold to single spaces", body: "  One\n two   three ", want: "One two three" },
  ])("$name", ({ body, want }) => {
    expect(openingWords(story(wren, body))).toBe(want);
  });

  it("reads the first part with words in it", () => {
    const p = create(PerspectiveSchema, {
      parts: [
        { position: 2, body: "Later." },
        { position: 1, body: "   " },
      ],
    });
    expect(openingWords(p)).toBe("Later.");
  });
});

describe("chapterTitles", () => {
  it("titles a placed writer by their role and anyone else by their story's opening", () => {
    const chapters = [
      story(wren, "The power went out.", [
        { position: 2, label: "a woman humming", claimedBy: ivy },
        { position: 1, label: "the guy in the yellow raincoat", claimedBy: otter },
      ]),
      story(otter, "I don't sing.", [{ position: 1, label: "the woman crying into a hot dog wrapper", claimedBy: ivy }]),
      story(ivy, "Nobody saw me."),
    ];
    // Ivy is in two stories: the first chapter's character wins.
    expect(chapterTitles(chapters, members)).toEqual(["The power went out.", "The guy in the yellow raincoat", "A woman humming"]);
  });

  it("leaves out a claim by someone who is not one of the lore's members", () => {
    const chapters = [story(wren, "Rain.", [{ position: 1, label: "the barista", claimedBy: otter }]), story(otter, "Oat milk everywhere.")];
    expect(chapterTitles(chapters, new Set(["u-wren"]))).toEqual(["Rain.", "Oat milk everywhere."]);
  });

  it("falls back to the writer's name for a story with no words", () => {
    expect(chapterTitles([story(wren, "   ")], members)).toEqual(["wren"]);
  });
});
