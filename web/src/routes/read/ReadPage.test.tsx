import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { polyfillRadix } from "@/components/actions/testing.testutil";
import { connectError, sessionReply } from "@/lib/fakeApi.testutil";
import { dayArcResponse, perspective as arcPerspective } from "@/routes/arc/fixtures.testutil";

import { renderApp } from "./renderApp.testutil";

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

/** A paragraph of `n` words, enough to make a story long enough to page. */
const filler = (n: number) => Array.from({ length: n }, (_, i) => `word${i}`).join(" ");

const perspective = {
  id: "p-1",
  arcId: "arc-1",
  arcTitle: "The night the fountain froze",
  author: { userId: "u-1", username: "wren" },
  genre: "GENRE_MYSTERY",
  status: "PERSPECTIVE_STATUS_PUBLISHED",
  loreStatus: "LORE_STATUS_IN_LORE",
  loreId: "lore-1",
  place: { id: "so-bk", kind: "PLACE_KIND_STORE", name: "Prairie Books" },
  localDate: "2026-01-14",
  // Over 600 words in all, so it reads a part per page.
  parts: [
    { position: 1, body: `The doors opened at nine.\n\n${filler(210)}` },
    { position: 2, body: `I followed him past the fountain.\n\n${filler(210)}` },
    { position: 3, body: `He bought nothing.\n\n${filler(210)}` },
  ],
  publishedAt: "2026-01-14T22:00:00Z",
};

function readPage(path: string, reply: () => { status?: number; body: unknown } = () => ({ body: { perspective } }), opts = {}) {
  return renderApp(path, { "PerspectiveService/Get": reply }, opts);
}

describe("read page (US-007, OT-033, OT-036)", () => {
  it("lets a signed-out reader read the first part, with the arc, author, place and day", async () => {
    const { calls } = readPage("/perspectives/p-1");
    expect(await screen.findByRole("heading", { level: 1, name: "The night the fountain froze" })).toBeInTheDocument();
    expect(screen.getByText("The doors opened at nine.")).toBeInTheDocument();
    expect(screen.getByText("Part 1 of 3")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "wren" })).toHaveAttribute("href", "/users/wren");
    expect(screen.getByText(/Prairie Books/)).toBeInTheDocument();
    expect(screen.getByText(/January 14, 2026/)).toBeInTheDocument();
    expect(screen.getByText(/Mystery/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to the day" })).toHaveAttribute("href", "/arcs/arc-1");
    // A story in a lore links to the lore, where the stories it is linked with are.
    expect(screen.getByRole("link", { name: "In a lore: read the lore" })).toHaveAttribute("href", "/lores/lore-1");
    // The tab says what is being read, for a link shared anywhere.
    expect(document.title).toBe("The night the fountain froze by wren · Perspectiv");
    expect(screen.getByRole("link", { name: "Sign in" })).toBeInTheDocument();
    expect(calls("PerspectiveService/Get")).toEqual([{ perspectiveId: "p-1" }]);
    expect(document.querySelector("canvas")).toBeNull();
  });

  it("opens the part named in the address", async () => {
    readPage("/perspectives/p-1?part=2");
    expect(await screen.findByText("I followed him past the fountain.")).toBeInTheDocument();
    expect(screen.queryByText("The doors opened at nine.")).not.toBeInTheDocument();
  });

  it.each([
    ["99", "He bought nothing.", "Part 3 of 3"],
    ["0", "The doors opened at nine.", "Part 1 of 3"],
    ["two", "The doors opened at nine.", "Part 1 of 3"],
  ])("clamps an out-of-range part %s", async (part, text, label) => {
    readPage(`/perspectives/p-1?part=${part}`);
    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it("puts each page in the address so the back button returns to the previous part", async () => {
    const user = userEvent.setup();
    const { router } = readPage("/perspectives/p-1");
    await screen.findByText("The doors opened at nine.");
    await user.keyboard("{ArrowRight}");
    expect(await screen.findByText("I followed him past the fountain.")).toBeInTheDocument();
    expect(router.state.location.search).toBe("?part=2");
    await user.click(screen.getByRole("button", { name: "Next part" }));
    expect(await screen.findByText("He bought nothing.")).toBeInTheDocument();
    await router.navigate(-1);
    expect(await screen.findByText("I followed him past the fountain.")).toBeInTheDocument();
  });

  it("shows a placeholder for a deleted perspective", async () => {
    readPage("/perspectives/p-1", () => ({ body: { perspective: { ...perspective, parts: [], author: undefined, deleted: true } } }));
    expect(await screen.findByText("This story was deleted by its author.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Next part" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to the day" })).toHaveAttribute("href", "/arcs/arc-1");
  });

  it("shows a placeholder for a perspective hidden by moderation", async () => {
    readPage("/perspectives/p-1", () => ({ body: { perspective: { ...perspective, parts: [], author: undefined, hidden: true } } }));
    expect(await screen.findByText("This story was hidden by a moderator.")).toBeInTheDocument();
  });

  it("marks the author's own draft as a draft", async () => {
    readPage(
      "/perspectives/p-1",
      () => ({ body: { perspective: { ...perspective, arcId: "", arcTitle: "", titleIfNewArc: "Frost", status: "PERSPECTIVE_STATUS_DRAFT", loreStatus: undefined } } }),
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    expect(await screen.findByRole("heading", { level: 1, name: "Frost" })).toBeInTheDocument();
    expect(screen.getByText("Draft")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Edit draft" })).toHaveAttribute("href", "/compose?perspectiveId=p-1");
  });

  it("lets the author edit and delete their published perspective, then shows the removed placeholder", async () => {
    const user = userEvent.setup();
    const me = "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10";
    let deleted = false;
    const { calls } = renderApp(
      "/perspectives/p-1",
      {
        "PerspectiveService/Get": () => ({
          body: {
            perspective: deleted
              ? { ...perspective, parts: [], author: undefined, deleted: true }
              : { ...perspective, author: { userId: me, username: "wren" } },
          },
        }),
        "PerspectiveService/Delete": () => {
          deleted = true;
          return { body: {} };
        },
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    await screen.findByText("The doors opened at nine.");
    expect(screen.getByRole("link", { name: "Edit" })).toHaveAttribute("href", "/compose?perspectiveId=p-1");
    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this story?" });
    expect(calls("PerspectiveService/Delete")).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Delete story" }));
    expect(await screen.findByText("This story was deleted by its author.")).toBeInTheDocument();
    expect(calls("PerspectiveService/Delete")).toEqual([{ perspectiveId: "p-1" }]);
  });

  it("lets the author delete a draft and returns them to the landing page", async () => {
    const user = userEvent.setup();
    const { calls, router } = renderApp(
      "/perspectives/p-1",
      {
        "PerspectiveService/Get": () => ({
          body: { perspective: { ...perspective, arcId: "", arcTitle: "", titleIfNewArc: "Frost", status: "PERSPECTIVE_STATUS_DRAFT", loreStatus: undefined } },
        }),
        "PerspectiveService/Delete": () => ({ body: {} }),
        "PerspectiveService/ListMine": () => ({ body: { perspectives: [] } }),
        "PlaceService/ListChildren": () => ({ body: { places: [] } }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    await screen.findByRole("heading", { level: 1, name: "Frost" });
    await user.click(screen.getByRole("button", { name: "Delete" }));
    await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Delete draft" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(calls("PerspectiveService/Delete")).toEqual([{ perspectiveId: "p-1" }]);
  });

  it("offers no edit or delete on someone else's story", async () => {
    readPage("/perspectives/p-1", undefined, { refresh: () => sessionReply({ token: "tok", username: "teller" }) });
    await screen.findByText("The doors opened at nine.");
    expect(screen.queryByRole("link", { name: "Sign in" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Edit" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
  });

  it("shows not found for an unknown perspective", async () => {
    readPage("/perspectives/nope", () => connectError("not_found"));
    expect(await screen.findByRole("heading", { name: "Story not found" })).toBeInTheDocument();
  });

  it("shows an error when the server cannot be reached", async () => {
    readPage("/perspectives/p-1", () => connectError("unavailable"));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("could not be loaded"));
  });
});

describe("reader and arc page share one cached perspective", () => {
  it("goes back to the day after reading without crashing the action bar", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    renderApp("/perspectives/p-otter", {
      "PerspectiveService/Get": (body) => ({ body: arcPerspective(String(body.perspectiveId)) }),
      "ArcService/GetArc": () => ({ body: dayArcResponse() }),
    });
    await user.click(await screen.findByRole("link", { name: "Back to the day" }));
    const canon = await screen.findByRole("region", { name: "Canon" });
    // The row of the perspective just read uses the reader's cache entry.
    const otter = within(canon).getByRole("article", { name: "otter's story" });
    // Signed out, the rail shows the count with no button.
    expect(otter).toHaveTextContent("2 upvotes");
    expect(within(otter).queryByRole("button", { name: /^Upvote/ })).toBeNull();
    expect(screen.queryByText(/Something went wrong/)).not.toBeInTheDocument();
  });
});

describe("inviting who was there", () => {
  it("gives the author a link to send, optionally asking about one of their characters", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    const me = "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10";
    const mine = {
      ...perspective,
      author: { userId: me, username: "wren" },
      characters: [
        { id: "c-1", position: 1, label: "the man in the red coat" },
        { id: "c-2", position: 2, label: "the woman with the umbrella", hasAcceptedClaim: true, claimedBy: { userId: "u-9", username: "otter" } },
      ],
    };
    const { calls } = renderApp(
      "/perspectives/p-1",
      {
        "PerspectiveService/Get": () => ({ body: { perspective: mine } }),
        "PerspectiveService/CreateInvite": () => ({ body: { code: "abcdefgh23" } }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    await user.click(await screen.findByRole("button", { name: "Invite who was there" }));
    const dialog = await screen.findByRole("dialog", { name: "Who else was there?" });
    // A character someone already turned out to be is not asked about again.
    expect(within(dialog).getAllByRole("radio").map((r) => r.getAttribute("aria-label") ?? r.closest("label")?.textContent)).toEqual([
      "No one in particular",
      "Were you the man in the red coat?",
    ]);
    await user.click(within(dialog).getByRole("radio", { name: "Were you the man in the red coat?" }));
    await user.click(within(dialog).getByRole("button", { name: "Make the link" }));
    expect(await within(dialog).findByText(/\/i\/abcdefgh23/)).toBeInTheDocument();
    expect(within(dialog).getByText(/Were you the man in the red coat\? I wrote about Prairie Books/)).toBeInTheDocument();
    expect(calls("PerspectiveService/CreateInvite")).toEqual([{ perspectiveId: "p-1", characterId: "c-1" }]);
  });
});


describe("reading a story to its end", () => {
  /** wren's short story in the day arc the arc fixtures describe, in its canon. */
  const short = {
    ...perspective,
    id: "p-wren",
    arcId: "arc-day",
    loreId: "lore-canon",
    parts: [
      { position: 1, body: "The doors opened at nine." },
      { position: 2, body: "I followed him past the fountain." },
      { position: 3, body: "He bought nothing." },
    ],
    characters: [
      { id: "c-1", position: 1, label: "the man in the green coat", hasAcceptedClaim: true, claimedBy: { userId: "u-otter", username: "otter" }, claimedByStoryId: "p-otter" },
      { id: "c-2", position: 2, label: "a woman nobody knew", hasAcceptedClaim: true, claimedBy: { userId: "u-stranger", username: "stranger" } },
    ],
  };
  const withArc = (p: object) => ({
    "PerspectiveService/Get": () => ({ body: { perspective: p } }),
    "ArcService/GetArc": () => ({ body: dayArcResponse() }),
  });

  it("shows a short story whole on one page, without paging", async () => {
    renderApp("/perspectives/p-wren", withArc(short));
    expect(await screen.findByText("The doors opened at nine.")).toBeInTheDocument();
    expect(screen.getByText("I followed him past the fountain.")).toBeInTheDocument();
    expect(screen.getByText("He bought nothing.")).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "Part 3" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Next part" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Read all parts on one page" })).toBeNull();
  });

  it("after the story, offers its lore, the day's other stories and the way back to the day", async () => {
    renderApp("/perspectives/p-wren", withArc(short));
    const more = await screen.findByRole("region", { name: "More from this day" });
    expect(await within(more).findByRole("link", { name: /^Read the canon: who was who, every story and the thread/ })).toHaveAttribute(
      "href",
      "/lores/lore-canon",
    );
    const others = await within(more).findByRole("list", { name: "Other stories from this day" });
    const rows = within(others).getAllByRole("link");
    // Five at most, in the arc page's order, never the story just read.
    expect(rows.map((r) => r.getAttribute("href"))).toEqual([
      "/perspectives/p-otter",
      "/perspectives/p-heron",
      "/perspectives/p-lark",
      "/perspectives/p-moth",
      "/perspectives/p-finch",
    ]);
    expect(rows[0]).toHaveTextContent("otterCanon text by otter");
    expect(within(more).getByRole("link", { name: "Back to the day: every story and lore" })).toHaveAttribute("href", "/arcs/arc-day");
  });

  it("names the story's standing in plain words, with the way to how the day works", async () => {
    renderApp("/perspectives/p-wren", withArc(short));
    expect(await screen.findByRole("link", { name: "Canon of this day: read the lore" })).toHaveAttribute("href", "/lores/lore-canon");
    const note = screen.getByText(/^Its lore is the canon: the lore the people who wrote about this day favour most\./);
    expect(within(note).getByRole("link", { name: "How this day works" })).toHaveAttribute("href", "/arcs/arc-day#arc-how");
  });

  it("opens a tagged person's own story of the day from the cast, and their profile when it is not readable", async () => {
    renderApp("/perspectives/p-wren", withArc(short));
    const cast = await screen.findByRole("list", { name: "Cast" });
    expect(await within(cast).findByRole("link", { name: "otter’s story" })).toHaveAttribute("href", "/perspectives/p-otter");
    expect(within(cast).getByRole("link", { name: "stranger" })).toHaveAttribute("href", "/users/stranger");
  });

  it("keeps the end of a long story until its last part", async () => {
    const user = userEvent.setup();
    renderApp("/perspectives/p-wren", withArc({ ...short, parts: perspective.parts }));
    await screen.findByText("Part 1 of 3");
    expect(screen.queryByRole("region", { name: "More from this day" })).toBeNull();
    await user.keyboard("{ArrowRight}");
    await screen.findByText("Part 2 of 3");
    expect(screen.queryByRole("region", { name: "More from this day" })).toBeNull();
    await user.keyboard("{ArrowRight}");
    await screen.findByText("Part 3 of 3");
    expect(await screen.findByRole("region", { name: "More from this day" })).toBeInTheDocument();
  });

  it("lets a long story be read on one page, remembers it, and keeps ?part= when paging again", async () => {
    const user = userEvent.setup();
    const { unmount } = renderApp("/perspectives/p-1?part=2", { "PerspectiveService/Get": () => ({ body: { perspective } }) });
    await screen.findByText("I followed him past the fountain.");
    const toggle = screen.getByRole("button", { name: "Read all parts on one page" });
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("The doors opened at nine.")).toBeInTheDocument();
    expect(screen.getByText("He bought nothing.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Next part" })).toBeNull();
    // The part the reader was on is where they stay.
    expect(screen.getByRole("heading", { level: 2, name: "Part 2" })).toHaveFocus();
    expect(localStorage.getItem("perspectiv.reader.whole")).toBe("1");
    expect(screen.getByRole("region", { name: "More from this day" })).toBeInTheDocument();
    unmount();

    // The next story opens whole too; switching back pages at the address's part.
    const { router } = renderApp("/perspectives/p-1?part=3", { "PerspectiveService/Get": () => ({ body: { perspective } }) });
    const again = await screen.findByRole("button", { name: "Read all parts on one page" });
    expect(again).toHaveAttribute("aria-pressed", "true");
    await user.click(again);
    expect(await screen.findByText("Part 3 of 3")).toBeInTheDocument();
    expect(screen.queryByText("The doors opened at nine.")).toBeNull();
    expect(localStorage.getItem("perspectiv.reader.whole")).toBe("0");
    await user.click(screen.getByRole("button", { name: "Previous part" }));
    expect(await screen.findByText("Part 2 of 3")).toBeInTheDocument();
    expect(router.state.location.search).toBe("?part=2");
  });

  it("pages as usual when the browser blocks storage", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    const user = userEvent.setup();
    readPage("/perspectives/p-1");
    expect(await screen.findByText("Part 1 of 3")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Read all parts on one page" }));
    expect(screen.getByText("He bought nothing.")).toBeInTheDocument();
    vi.restoreAllMocks();
  });
});
