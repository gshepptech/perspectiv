import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  type BookPage,
  forgetPlace,
  nextPage,
  pageFrom,
  pageOfPlace,
  placeKey,
  positionOf,
  previousPage,
  readPlace,
  searchOf,
  writePlace,
} from "./bookPages";

// Three chapters: 2 parts, 1 part, 3 parts.
const parts = [2, 1, 3];
const ch = (chapter: number, part: number): BookPage => ({ kind: "chapter", chapter, part });

describe("pageFrom", () => {
  const cases: { name: string; search: string; parts?: number[]; want: BookPage }[] = [
    { name: "no address is the title page", search: "", want: { kind: "title" } },
    { name: "at=contents", search: "?at=contents", want: { kind: "contents" } },
    { name: "at=end", search: "?at=end", want: { kind: "end" } },
    { name: "a chapter and part", search: "?chapter=3&part=2", want: ch(3, 2) },
    { name: "a chapter without a part opens part 1", search: "?chapter=2", want: ch(2, 1) },
    { name: "a malformed part opens part 1", search: "?chapter=1&part=x", want: ch(1, 1) },
    { name: "a zero part opens part 1", search: "?chapter=1&part=0", want: ch(1, 1) },
    { name: "a part past the end opens the last", search: "?chapter=1&part=9", want: ch(1, 2) },
    { name: "a chapter past the end opens the last", search: "?chapter=9&part=2", want: ch(3, 2) },
    { name: "a malformed chapter is the title page", search: "?chapter=two", want: { kind: "title" } },
    { name: "a book with no chapters opens its contents", search: "?chapter=1", parts: [], want: { kind: "contents" } },
  ];
  for (const c of cases) {
    it(c.name, () => {
      expect(pageFrom(new URLSearchParams(c.search), c.parts ?? parts)).toEqual(c.want);
    });
  }
});

describe("searchOf", () => {
  const cases: { name: string; page: BookPage; want: string }[] = [
    { name: "title page", page: { kind: "title" }, want: "" },
    { name: "contents", page: { kind: "contents" }, want: "?at=contents" },
    { name: "end", page: { kind: "end" }, want: "?at=end" },
    { name: "chapter", page: ch(2, 1), want: "?chapter=2&part=1" },
  ];
  for (const c of cases) {
    it(`${c.name} reads back as the same page`, () => {
      expect(searchOf(c.page)).toBe(c.want);
      expect(pageFrom(new URLSearchParams(c.want), parts)).toEqual(c.page);
    });
  }
});

describe("turning pages", () => {
  // Every page of the book in reading order.
  const order: BookPage[] = [{ kind: "title" }, { kind: "contents" }, ch(1, 1), ch(1, 2), ch(2, 1), ch(3, 1), ch(3, 2), ch(3, 3), { kind: "end" }];

  it("forward visits every page once, across chapter boundaries, and stops at the end", () => {
    const seen: BookPage[] = [];
    let page: BookPage | null = { kind: "title" };
    while (page) {
      seen.push(page);
      page = nextPage(page, parts);
    }
    expect(seen).toEqual(order);
  });

  it("back visits every page in reverse and stops at the title page", () => {
    const seen: BookPage[] = [];
    let page: BookPage | null = { kind: "end" };
    while (page) {
      seen.push(page);
      page = previousPage(page, parts);
    }
    expect(seen).toEqual(order.toReversed());
  });

  const boundaries: { name: string; from: BookPage; next: BookPage | null; previous: BookPage | null }[] = [
    { name: "a chapter's last part leads to the next chapter's first", from: ch(1, 2), next: ch(2, 1), previous: ch(1, 1) },
    { name: "a chapter's first part leads back to the previous chapter's last", from: ch(3, 1), next: ch(3, 2), previous: ch(2, 1) },
    { name: "chapter 1's first part leads back to the contents", from: ch(1, 1), next: ch(1, 2), previous: { kind: "contents" } },
    { name: "the last chapter's last part leads to the end", from: ch(3, 3), next: { kind: "end" }, previous: ch(3, 2) },
  ];
  for (const b of boundaries) {
    it(b.name, () => {
      expect(nextPage(b.from, parts)).toEqual(b.next);
      expect(previousPage(b.from, parts)).toEqual(b.previous);
    });
  }

  it("goes straight from the contents to the end, and back, in a book with no chapters", () => {
    expect(nextPage({ kind: "contents" }, [])).toEqual({ kind: "end" });
    expect(previousPage({ kind: "end" }, [])).toEqual({ kind: "contents" });
  });
});

describe("positionOf", () => {
  const cases: { page: BookPage; want: string }[] = [
    { page: { kind: "title" }, want: "Title page" },
    { page: { kind: "contents" }, want: "Contents" },
    { page: ch(3, 2), want: "Chapter 3, part 2 of 3" },
    { page: { kind: "end" }, want: "The end" },
  ];
  for (const c of cases) {
    it(`says "${c.want}"`, () => expect(positionOf(c.page, parts)).toBe(c.want));
  }
});

describe("the remembered place", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it("is kept per lore on this device and forgotten on request", () => {
    writePlace("lore-1", { perspectiveId: "p-otter", part: 2 });
    expect(readPlace("lore-1")).toEqual({ perspectiveId: "p-otter", part: 2 });
    expect(readPlace("lore-2")).toBeNull();
    forgetPlace("lore-1");
    expect(readPlace("lore-1")).toBeNull();
  });

  const malformed: { name: string; raw: string }[] = [
    { name: "not JSON", raw: "{" },
    { name: "not an object", raw: "3" },
    { name: "no story", raw: JSON.stringify({ part: 1 }) },
    { name: "a part below 1", raw: JSON.stringify({ perspectiveId: "p", part: 0 }) },
    { name: "a fractional part", raw: JSON.stringify({ perspectiveId: "p", part: 1.5 }) },
  ];
  for (const m of malformed) {
    it(`reads ${m.name} as no place`, () => {
      localStorage.setItem(placeKey("lore-1"), m.raw);
      expect(readPlace("lore-1")).toBeNull();
    });
  }

  it("is no place, and never throws, when storage is blocked", () => {
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(blocked);
    expect(() => writePlace("lore-1", { perspectiveId: "p", part: 1 })).not.toThrow();
    expect(readPlace("lore-1")).toBeNull();
    expect(() => forgetPlace("lore-1")).not.toThrow();
  });

  const resolves: { name: string; place: { perspectiveId: string; part: number } | null; want: BookPage | null }[] = [
    { name: "a story still in the book opens its chapter", place: { perspectiveId: "b", part: 1 }, want: ch(2, 1) },
    { name: "a part past the chapter's end opens its last", place: { perspectiveId: "c", part: 7 }, want: ch(3, 3) },
    { name: "a story no longer in the book is no place", place: { perspectiveId: "gone", part: 1 }, want: null },
    { name: "no place is no place", place: null, want: null },
  ];
  for (const r of resolves) {
    it(r.name, () => expect(pageOfPlace(r.place, ["a", "b", "c"], parts)).toEqual(r.want));
  }
});
