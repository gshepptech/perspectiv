/**
 * The pages of a lore read as a book, and where a reader is in it
 * (docs/design-system.md, Lore as a book). In order: the title page, the
 * contents, every chapter a part at a time, then the closing page. A chapter
 * is one readable member story; `parts` holds each chapter's part count.
 */
export type BookPage =
  | { kind: "title" }
  | { kind: "contents" }
  | { kind: "chapter"; chapter: number; part: number }
  | { kind: "end" };

/** A positive whole number from the address, or 0 for anything else. */
function positive(value: string | null): number {
  const n = Number.parseInt(value ?? "", 10);
  return Number.isFinite(n) && n > 0 ? n : 0;
}

/**
 * The page an address names. `?at=contents` and `?at=end` name those pages,
 * `?chapter=N&part=M` a chapter's part (a missing or bad part is part 1, and
 * a chapter or part past the end opens the last); anything else is the title
 * page. A book with no chapters opens its contents for any chapter.
 */
export function pageFrom(params: URLSearchParams, parts: readonly number[]): BookPage {
  const at = params.get("at");
  if (at === "contents") return { kind: "contents" };
  if (at === "end") return { kind: "end" };
  const chapter = positive(params.get("chapter"));
  if (chapter === 0) return { kind: "title" };
  if (parts.length === 0) return { kind: "contents" };
  const c = Math.min(chapter, parts.length);
  const total = Math.max(parts[c - 1] ?? 1, 1);
  return { kind: "chapter", chapter: c, part: Math.min(Math.max(positive(params.get("part")), 1), total) };
}

/** The address of a page, as a search string ("" for the title page). */
export function searchOf(page: BookPage): string {
  switch (page.kind) {
    case "title":
      return "";
    case "contents":
      return "?at=contents";
    case "end":
      return "?at=end";
    case "chapter":
      return `?chapter=${page.chapter}&part=${page.part}`;
  }
}

/** The page after this one, across chapter boundaries; null on the closing page. */
export function nextPage(page: BookPage, parts: readonly number[]): BookPage | null {
  switch (page.kind) {
    case "title":
      return { kind: "contents" };
    case "contents":
      return parts.length > 0 ? { kind: "chapter", chapter: 1, part: 1 } : { kind: "end" };
    case "chapter":
      if (page.part < (parts[page.chapter - 1] ?? 1)) return { ...page, part: page.part + 1 };
      return page.chapter < parts.length ? { kind: "chapter", chapter: page.chapter + 1, part: 1 } : { kind: "end" };
    case "end":
      return null;
  }
}

/** The page before this one: a chapter's first part goes back to the previous chapter's last. */
export function previousPage(page: BookPage, parts: readonly number[]): BookPage | null {
  switch (page.kind) {
    case "title":
      return null;
    case "contents":
      return { kind: "title" };
    case "chapter":
      if (page.part > 1) return { ...page, part: page.part - 1 };
      return page.chapter > 1 ? { kind: "chapter", chapter: page.chapter - 1, part: parts[page.chapter - 2] ?? 1 } : { kind: "contents" };
    case "end":
      return parts.length > 0 ? { kind: "chapter", chapter: parts.length, part: parts[parts.length - 1] ?? 1 } : { kind: "contents" };
  }
}

/** What the status line says on arriving at a page: "Chapter 2, part 3 of 4". */
export function positionOf(page: BookPage, parts: readonly number[]): string {
  switch (page.kind) {
    case "title":
      return "Title page";
    case "contents":
      return "Contents";
    case "end":
      return "The end";
    case "chapter":
      return `Chapter ${page.chapter}, part ${page.part} of ${parts[page.chapter - 1] ?? 1}`;
  }
}

/** Where a reader was in a lore's book: a story by its id, so a chapter that moves still resolves. */
export type BookPlace = { perspectiveId: string; part: number };

/** The device-local key a lore's place is kept under. */
export const placeKey = (loreId: string) => `perspectiv.book.${loreId}`;

/**
 * The reader's place in a lore's book on this device, or null when there is
 * none, it is malformed, or storage is blocked (a private window, site data
 * off). It is never sent to the server.
 */
export function readPlace(loreId: string): BookPlace | null {
  try {
    const raw = window.localStorage.getItem(placeKey(loreId));
    if (!raw) return null;
    const value = JSON.parse(raw) as unknown;
    if (typeof value !== "object" || value === null) return null;
    const { perspectiveId, part } = value as Record<string, unknown>;
    if (typeof perspectiveId !== "string" || perspectiveId === "" || typeof part !== "number" || !Number.isInteger(part) || part < 1) {
      return null;
    }
    return { perspectiveId, part };
  } catch {
    return null;
  }
}

/** Keeps the reader's place; a blocked store only means it is not remembered. */
export function writePlace(loreId: string, place: BookPlace): void {
  try {
    window.localStorage.setItem(placeKey(loreId), JSON.stringify(place));
  } catch {
    // Not remembered on this device; reading carries on.
  }
}

/** Forgets the reader's place, once they reach the end. */
export function forgetPlace(loreId: string): void {
  try {
    window.localStorage.removeItem(placeKey(loreId));
  } catch {
    // Nothing was remembered to forget.
  }
}

/**
 * A remembered place as a page of the book as it stands now, or null when its
 * story is no longer a chapter. A part past the chapter's end opens its last.
 */
export function pageOfPlace(place: BookPlace | null, chapterIds: readonly string[], parts: readonly number[]): BookPage | null {
  if (!place) return null;
  const i = chapterIds.indexOf(place.perspectiveId);
  if (i < 0) return null;
  return { kind: "chapter", chapter: i + 1, part: Math.min(place.part, Math.max(parts[i] ?? 1, 1)) };
}
