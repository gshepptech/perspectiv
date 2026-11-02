import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { connectError } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

import { placeKey } from "./bookPages";

beforeEach(() => localStorage.clear());
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const wren = { userId: "u-wren", username: "wren" };
const otter = { userId: "u-otter", username: "otter" };
const ivy = { userId: "u-ivy", username: "ivy" };

// Four members in the lore's order; ivy's story was hidden by a moderator.
const lore = {
  id: "lore-1",
  arcId: "arc-1",
  standing: "LORE_STANDING_CANON",
  memberCount: 4,
  upvotes: 5,
  members: [
    { perspectiveId: "p-wren", user: wren },
    { perspectiveId: "p-ivy", user: ivy },
    { perspectiveId: "p-otter", user: otter },
    { perspectiveId: "p-jay", user: { userId: "u-jay", username: "jay" } },
  ],
};

function story(id: string, author: typeof wren, bodies: string[], more: Record<string, unknown> = {}) {
  return {
    perspective: {
      id,
      arcId: "arc-1",
      arcTitle: "Open mic night",
      author,
      genre: "GENRE_COMEDY",
      status: "PERSPECTIVE_STATUS_PUBLISHED",
      loreStatus: "LORE_STATUS_IN_LORE",
      loreId: "lore-1",
      place: { id: "pl-1", kind: "PLACE_KIND_STORE", name: "Moonbean Coffee" },
      localDate: "2026-10-09",
      parts: bodies.map((body, i) => ({ position: i + 1, body })),
      characters: [],
      reacts: [],
      ...more,
    },
  };
}

const stories: Record<string, () => unknown> = {
  "p-wren": () =>
    story("p-wren", wren, ["The power went out.", "Someone kept singing in the dark."], {
      characters: [{ id: "c-1", position: 1, label: "the guy in the yellow raincoat", hasAcceptedClaim: true, claimedBy: otter }],
    }),
  "p-ivy": () => story("p-ivy", ivy, ["Removed words."], { hidden: true }),
  "p-otter": () => story("p-otter", otter, ["I don't sing."]),
  "p-jay": () => story("p-jay", { userId: "u-jay", username: "jay" }, ["Deleted words."], { deleted: true }),
};

const handlers = {
  "LoreService/GetLore": () => ({ body: { lore } }),
  "ArcService/GetArc": () => ({ body: { arc: { id: "arc-1", title: "Open mic night" } } }),
  "PerspectiveService/Get": (body: Record<string, unknown>) => ({ body: stories[String(body.perspectiveId)]?.() }),
  "LoreService/ListLorePosts": () => ({ body: { posts: [] } }),
};

/**
 * Finds a heading and waits for it to take focus. A turn renders in the
 * router's transition and the book focuses the new page in a passive effect
 * after that commit, which React may run a task later than the DOM change
 * that lets findByRole resolve.
 */
async function focused(level: number, name: string) {
  const heading = await screen.findByRole("heading", { level, name });
  await waitFor(() => expect(heading).toHaveFocus());
  return heading;
}

const status = () => screen.getAllByRole("status").find((el) => el.classList.contains("sr-only"));
const search = (router: ReturnType<typeof renderApp>["router"]) => router.state.location.search;

describe("a lore read as a book", () => {
  it("opens from the lore page's Read as a book link on its title page", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/lores/lore-1", handlers);
    const link = await screen.findByRole("link", { name: "Read as a book" });
    expect(link).toHaveAttribute("href", "/lores/lore-1/book");
    await user.click(link);
    // The lore page's own heading is "Unnamed lore" too: wait for the book's writers.
    const writers = await screen.findByRole("list", { name: "Writers" });
    expect(screen.getByRole("heading", { level: 1, name: "Unnamed lore" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/lores/lore-1/book");
    expect(screen.getByText(/^Open mic night · Moonbean Coffee · /)).toBeInTheDocument();
    // Its writers are the writers of its chapters: the hidden and deleted stories' are not listed.
    expect(within(writers).getAllByRole("link").map((a) => a.textContent)).toEqual(["wren", "otter"]);
    expect(document.title).toBe("Unnamed lore in Open mic night, as a book · Perspectiv");
    expect(screen.queryByRole("link", { name: "Continue where you left off" })).toBeNull();
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
    expect(screen.getByRole("link", { name: "Next page" })).toHaveAttribute("href", "/lores/lore-1/book?at=contents");
  });

  it("lists one chapter per readable story in the lore page's order, titled by who its writer was, then the cast", async () => {
    renderApp("/lores/lore-1/book?at=contents", { ...handlers, "LoreService/GetLore": () => ({ body: { lore: { ...lore, name: "The open mic crew" } } }) });
    expect(await screen.findByRole("heading", { level: 1, name: "Contents" })).toBeInTheDocument();
    const chapters = within(screen.getByRole("list", { name: "Chapters" })).getAllByRole("link");
    // Otter is the raincoat in wren's story; no one placed wren, so her
    // chapter takes her opening words. The writer is the small line under.
    expect(chapters.map((a) => a.textContent)).toEqual([
      "Chapter 1The power went out.by wren · 2 parts",
      "Chapter 2The guy in the yellow raincoatby otter · 1 part",
    ]);
    expect(chapters.map((a) => a.getAttribute("href"))).toEqual(["/lores/lore-1/book?chapter=1&part=1", "/lores/lore-1/book?chapter=2&part=1"]);
    expect(screen.getByRole("region", { name: "Who was who" })).toHaveTextContent("otter was “the guy in the yellow raincoat” in wren’s story");
    expect(screen.queryByText("Removed words.")).toBeNull();
    expect(document.title).toBe("The open mic crew, as a book · Perspectiv");
  });

  it("pages a chapter by part and turns across chapter boundaries with the arrow keys, focusing and announcing each page", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/lores/lore-1/book?chapter=1&part=2", handlers);
    expect(await screen.findByRole("heading", { level: 1, name: "Chapter 1: The power went out." })).toBeInTheDocument();
    expect(screen.getByText("Someone kept singing in the dark.")).toBeInTheDocument();
    expect(screen.getByText("Chapter 1, part 2 of 2", { selector: "nav span" })).toBeInTheDocument();
    // The book always pages: no whole-story toggle.
    expect(screen.queryByRole("button", { name: "Read all parts on one page" })).toBeNull();

    // Forward from a chapter's last part opens the next chapter's first.
    await user.keyboard("{ArrowRight}");
    await focused(1, "Chapter 2: The guy in the yellow raincoat");
    expect(search(router)).toBe("?chapter=2&part=1");
    expect(status()).toHaveTextContent("Chapter 2, part 1 of 1");
    expect(screen.getByText("I don't sing.")).toBeInTheDocument();

    // Back from a first part opens the previous chapter's last.
    await user.keyboard("{ArrowLeft}");
    await focused(1, "Chapter 1: The power went out.");
    expect(search(router)).toBe("?chapter=1&part=2");
    expect(status()).toHaveTextContent("Chapter 1, part 2 of 2");

    // Within a chapter the Reader turns the part and focuses its heading.
    await user.keyboard("{ArrowLeft}");
    await waitFor(() => expect(search(router)).toBe("?chapter=1&part=1"));
    await focused(2, "Part 1");
    expect(status()).toHaveTextContent("Chapter 1, part 1 of 2");

    // Back again, past the chapters, to the contents and the title page.
    await user.keyboard("{ArrowLeft}");
    await focused(1, "Contents");
    expect(status()).toHaveTextContent("Contents");
    await user.keyboard("{ArrowLeft}");
    await focused(1, "Unnamed lore");
    expect(search(router)).toBe("");
    expect(status()).toHaveTextContent("Title page");

    // Browser Back returns to the page before, which the address names.
    await act(() => router.navigate(-1));
    await focused(1, "Contents");
    expect(search(router)).toBe("?at=contents");
  });

  it("turns once per arrow key pressed faster than the pages render, across a chapter boundary", async () => {
    const { router } = renderApp("/lores/lore-1/book?chapter=1&part=1", handlers);
    await screen.findByRole("heading", { level: 1, name: "Chapter 1: The power went out." });
    // Three presses in one go, before any turn is on screen: the address
    // changes at once but the router renders in a transition, so each key
    // after the first arrives while the page shown is still the old one.
    act(() => {
      for (let i = 0; i < 3; i++) window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", cancelable: true }));
    });
    // Chapter 1's part 2, then chapter 2's only part, then the end.
    await waitFor(() => expect(search(router)).toBe("?at=end"));
    await focused(1, "The end");
    // Back from the end lands on chapter 2, one page per press.
    act(() => {
      for (let i = 0; i < 2; i++) window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", cancelable: true }));
    });
    await waitFor(() => expect(search(router)).toBe("?chapter=1&part=2"));
    await focused(1, "Chapter 1: The power went out.");
  });

  it("leaves arrow keys typed in a field alone", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/lores/lore-1/book?chapter=2&part=1", handlers);
    await screen.findByRole("heading", { level: 1, name: "Chapter 2: The guy in the yellow raincoat" });
    const field = document.createElement("input");
    field.setAttribute("aria-label", "Elsewhere");
    document.body.append(field);
    try {
      field.focus();
      await user.keyboard("{ArrowRight}{ArrowLeft}");
      expect(search(router)).toBe("?chapter=2&part=1");
    } finally {
      field.remove();
    }
  });

  it("turns across chapters with Next and Previous as the keys do, to the closing page that links to the lore and its thread", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/lores/lore-1/book?chapter=1&part=1", handlers);
    const heading = await screen.findByRole("heading", { level: 1, name: "Chapter 1: The power went out." });
    // The writer is the byline, never the title.
    expect(within(heading.closest("header") as HTMLElement).getByRole("link", { name: "wren" })).toHaveAttribute("href", "/users/wren");
    // Previous on chapter 1's first part goes to the contents; no part control is ever disabled.
    expect(screen.getByRole("button", { name: "Contents" })).toBeEnabled();
    // No second row of chapter links: the contents are the jump list.
    expect(screen.queryByRole("navigation", { name: "Chapters" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Next part" }));
    await waitFor(() => expect(search(router)).toBe("?chapter=1&part=2"));

    // Next on a chapter's last part opens the next chapter's first. The router
    // holds the new address before React renders the last part's controls, so
    // the button is awaited, not read at once.
    await user.click(await screen.findByRole("button", { name: "Next chapter" }));
    await focused(1, "Chapter 2: The guy in the yellow raincoat");
    expect(search(router)).toBe("?chapter=2&part=1");
    expect(status()).toHaveTextContent("Chapter 2, part 1 of 1");

    // Previous on a first part opens the previous chapter's last, as ArrowLeft does.
    await user.click(screen.getByRole("button", { name: "Previous chapter" }));
    await focused(1, "Chapter 1: The power went out.");
    expect(search(router)).toBe("?chapter=1&part=2");
    await user.keyboard("{ArrowRight}");
    await screen.findByRole("heading", { level: 1, name: "Chapter 2: The guy in the yellow raincoat" });

    // On the last chapter Next reads "The end" and opens the closing page.
    await user.click(screen.getByRole("button", { name: "The end" }));
    await focused(1, "The end");
    expect(search(router)).toBe("?at=end");
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
    expect(screen.getByRole("link", { name: "Back to the lore" })).toHaveAttribute("href", "/lores/lore-1");
    await user.click(screen.getByRole("link", { name: "Read its thread" }));
    // The lore page opens at its thread, whose heading takes focus once the
    // stories above it have loaded.
    const threadHeading = await screen.findByRole("heading", { level: 2, name: "Thread" });
    await waitFor(() => expect(threadHeading).toHaveFocus());
    expect(router.state.location.pathname).toBe("/lores/lore-1");
    expect(router.state.location.hash).toBe("#thread");
  });

  it("holds a part control pressed while a turn lands, as it holds a key", async () => {
    const { router } = renderApp("/lores/lore-1/book?chapter=1&part=2", handlers);
    await screen.findByRole("heading", { level: 1, name: "Chapter 1: The power went out." });
    // Two presses before the first turn is on screen: the second is the
    // same button on the same page, and it turns on from where the first
    // landed instead of repeating it.
    const next = screen.getByRole("button", { name: "Next chapter" });
    act(() => {
      next.click();
      next.click();
    });
    await waitFor(() => expect(search(router)).toBe("?at=end"));
    await focused(1, "The end");
  });

  it("remembers the reader's place on this device and offers to continue, until they reach the end", async () => {
    const user = userEvent.setup();
    const sent = renderApp("/lores/lore-1/book?chapter=1&part=2", handlers);
    await screen.findByRole("heading", { level: 1, name: "Chapter 1: The power went out." });
    await user.keyboard("{ArrowRight}");
    await screen.findByRole("heading", { level: 1, name: "Chapter 2: The guy in the yellow raincoat" });
    // Kept by the story's id on this device. The server only ever sees the
    // lore page's own reads: the place goes in no request.
    expect(JSON.parse(localStorage.getItem(placeKey("lore-1")) ?? "null")).toEqual({ perspectiveId: "p-otter", part: 1 });
    expect([...new Set(sent.sent.map((r) => r.procedure))].sort()).toEqual(["AuthService/Refresh", "LoreService/GetLore", "PerspectiveService/Get"]);
    expect(sent.sent.every((r) => !("part" in r.body))).toBe(true);
    sent.unmount();

    const back = renderApp("/lores/lore-1/book", handlers);
    const resume = await screen.findByRole("link", { name: "Continue where you left off" });
    expect(resume).toHaveAttribute("href", "/lores/lore-1/book?chapter=2&part=1");
    expect(screen.getByText("Chapter 2, part 1 of 1", { selector: "p:not(.sr-only)" })).toBeInTheDocument();
    await user.click(resume);
    await screen.findByRole("heading", { level: 1, name: "Chapter 2: The guy in the yellow raincoat" });
    await user.keyboard("{ArrowRight}");
    await screen.findByRole("heading", { level: 1, name: "The end" });
    expect(localStorage.getItem(placeKey("lore-1"))).toBeNull();
    back.unmount();
  });

  it("reads and turns pages with storage blocked, offering no place to continue from", async () => {
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(blocked);
    const user = userEvent.setup();
    const { router } = renderApp("/lores/lore-1/book", handlers);
    await screen.findByRole("heading", { level: 1, name: "Unnamed lore" });
    expect(screen.queryByRole("link", { name: "Continue where you left off" })).toBeNull();
    await user.keyboard("{ArrowRight}");
    await screen.findByRole("heading", { level: 1, name: "Contents" });
    await user.keyboard("{ArrowRight}");
    await focused(1, "Chapter 1: The power went out.");
    expect(search(router)).toBe("?chapter=1&part=1");
    expect(status()).toHaveTextContent("Chapter 1, part 1 of 2");
  });

  it("forgets a remembered story that is no longer a chapter", async () => {
    localStorage.setItem(placeKey("lore-1"), JSON.stringify({ perspectiveId: "p-ivy", part: 1 }));
    renderApp("/lores/lore-1/book", handlers);
    await screen.findByRole("heading", { level: 1, name: "Unnamed lore" });
    expect(screen.queryByRole("link", { name: "Continue where you left off" })).toBeNull();
  });

  it("says when the lore is gone", async () => {
    renderApp("/lores/nope/book", { "LoreService/GetLore": () => connectError("not_found") });
    expect(await screen.findByRole("heading", { name: "Lore not found" })).toBeInTheDocument();
  });
});
