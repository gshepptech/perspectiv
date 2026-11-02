import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { polyfillRadix } from "@/components/actions/testing.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

import { parseTarget } from "./useThreadTarget";

afterEach(() => vi.unstubAllGlobals());

const wren = { userId: "u-wren", username: "wren" };
const otter = { userId: "u-otter", username: "otter" };

const lore = (postCount: number) => ({
  id: "lore-1",
  arcId: "arc-1",
  standing: "LORE_STANDING_CONTENDER",
  memberCount: 2,
  upvotes: 1,
  members: [
    { perspectiveId: "p-wren", user: wren },
    { perspectiveId: "p-otter", user: otter },
  ],
  postCount,
});

const story = (id: string, author: typeof wren) => ({
  perspective: {
    id,
    arcId: "arc-1",
    arcTitle: "Open mic night",
    author,
    status: "PERSPECTIVE_STATUS_PUBLISHED",
    loreStatus: "LORE_STATUS_IN_LORE",
    loreId: "lore-1",
    place: { id: "pl-1", kind: "PLACE_KIND_STORE", name: "Moonbean Coffee" },
    localDate: "2026-10-09",
    parts: [{ position: 1, body: `${author.username} was there.` }],
    characters: [],
    reacts: [],
  },
});

const post = (id: string, author: typeof wren, body: string, replies: unknown[] = []) => ({
  id,
  author,
  body,
  createdAt: "2026-10-10T12:00:00Z",
  replyCount: replies.length,
  replies,
});

/** A thread of two Top pages; otter's reply sits under the second page's post. */
const twoPages = (body: Record<string, unknown>) =>
  body.pageToken === "page-2"
    ? { body: { posts: [post("lp-2", wren, "Who brought the candles?", [{ ...post("lr-9", otter, "I did."), replyToId: "lp-2" }])] } }
    : { body: { posts: [post("lp-1", otter, "Best night of the year.")], nextPageToken: "page-2" } };

function render(path: string, postCount = 3) {
  polyfillRadix();
  return renderApp(path, {
    "LoreService/GetLore": () => ({ body: { lore: lore(postCount) } }),
    "ArcService/GetArc": () => ({ body: { arc: { id: "arc-1", title: "Open mic night" } } }),
    "PerspectiveService/Get": (body) => ({ body: body.perspectiveId === "p-wren" ? story("p-wren", wren) : story("p-otter", otter) }),
    "LoreService/ListLorePosts": twoPages,
  });
}

describe("landing on a thread post", () => {
  it("reads the next Top page to find a reply, then focuses it with a brief outline", async () => {
    const { calls } = render("/lores/lore-1#post-lr-9");
    const reply = (await screen.findByText("I did.")).closest("article");
    expect(reply).toHaveAttribute("id", "post-lr-9");
    await waitFor(() => expect(reply).toHaveFocus());
    expect(reply).toHaveAttribute("data-highlight");
    // Both pages came through the thread's own Top query, the second once.
    expect(calls("LoreService/ListLorePosts").map((c) => c.pageToken ?? "")).toEqual(["", "page-2"]);
    // The stories above the thread are on the page, so nothing moves the post on.
    expect(screen.getByText("wren was there.")).toBeInTheDocument();
    expect(screen.getByText("otter was there.")).toBeInTheDocument();
    await waitFor(() => expect(reply).not.toHaveAttribute("data-highlight"), { timeout: 3000 });
  });

  it("falls back to the thread's heading, with a note, when the post cannot be found", async () => {
    render("/lores/lore-1#post-lp-gone");
    const heading = await screen.findByRole("heading", { level: 2, name: "Thread" });
    await waitFor(() => expect(heading).toHaveFocus());
    expect(heading).toHaveAccessibleDescription(
      "That post could not be shown here. It may have been deleted, or it is further down among the replies to its post.",
    );
  });

  it("links to the thread from the top of the page, naming how many posts it has", async () => {
    const user = userEvent.setup();
    const { router } = render("/lores/lore-1");
    const link = await screen.findByRole("link", { name: "Thread · 3 posts" });
    expect(link).toHaveAttribute("href", "/lores/lore-1#thread");
    await user.click(link);
    expect(router.state.location.hash).toBe("#thread");
    const thread = screen.getByRole("region", { name: "Thread" });
    await waitFor(() => expect(within(thread).getByRole("heading", { level: 2, name: "Thread" })).toHaveFocus());
    // Without a note: nothing was missed.
    expect(within(thread).getByRole("heading", { level: 2 })).not.toHaveAttribute("aria-describedby");
  });

  it.each([
    { count: 1, name: "Thread · 1 post" },
    { count: 0, name: "Thread · no posts yet" },
  ])("says $name", async ({ count, name }) => {
    render("/lores/lore-1", count);
    expect(await screen.findByRole("link", { name })).toBeInTheDocument();
  });
});

describe("parseTarget", () => {
  it.each([
    { hash: "#thread", want: { kind: "thread" } },
    { hash: "#post-lp-1", want: { kind: "post", id: "lp-1" } },
    { hash: "#post-", want: null },
    { hash: "", want: null },
    { hash: "#stories", want: null },
  ])("reads $hash", ({ hash, want }) => {
    expect(parseTarget(hash)).toEqual(want);
  });
});
