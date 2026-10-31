import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { polyfillRadix } from "@/components/actions/testing.testutil";
import { sessionReply } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const ME = "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10";
const me = { userId: ME, username: "teller" };
const wren = { userId: "u-wren", username: "wren" };
const otter = { userId: "u-otter", username: "otter" };
const at = "2026-10-10T12:00:00Z";

const lore = {
  id: "lore-1",
  arcId: "arc-1",
  standing: "LORE_STANDING_CANON",
  memberCount: 2,
  members: [
    { perspectiveId: "p-wren", user: wren },
    { perspectiveId: "p-otter", user: otter },
  ],
};

const reply = (id: string, author: typeof wren, body: string, more: Record<string, unknown> = {}) => ({
  id,
  author,
  body,
  createdAt: at,
  replyToId: "lp-1",
  ...more,
});

/** Otter's post with five replies, three of them inline; then one of the viewer's own. */
const thread = [
  {
    id: "lp-1",
    author: otter,
    body: "Who brought the jumper cables?",
    createdAt: at,
    editedAt: at,
    upvotes: 4,
    byMember: true,
    replyCount: 5,
    moreRepliesToken: "after-3",
    replies: [
      reply("r-1", wren, "That was me."),
      reply("r-2", me, "You saved the night.", { replyingTo: wren, upvotes: 2, viewerUpvoted: true }),
      reply("r-3", wren, "Any time."),
    ],
  },
  { id: "lp-2", author: me, body: "I was by the door.", createdAt: at },
];

function handlers(posts: unknown[] = thread, extra: Record<string, (body: Record<string, unknown>) => unknown> = {}) {
  return {
    "LoreService/GetLore": () => ({ body: { lore } }),
    "ArcService/GetArc": () => ({
      body: { arc: { id: "arc-1", title: "Open mic night" }, viewer: { perspectiveId: "p-mine", loreId: "" } },
    }),
    "PerspectiveService/Get": () => ({ body: {} }),
    "LoreService/ListLorePosts": () => ({ body: { posts } }),
    ...extra,
  };
}

const signedIn = { refresh: () => sessionReply({ token: "tok" }) };

describe("lore thread", () => {
  it("shows replies one step in, who a reply answered, edits, scores and the viewer's own vote", async () => {
    polyfillRadix();
    renderApp("/lores/lore-1", handlers(), signedIn);
    const top = await screen.findByRole("article", { name: "otter's post" });
    expect(top).toHaveTextContent("edited");
    expect(within(top).getByRole("button", { name: "Upvote otter's post" })).toHaveAttribute("aria-pressed", "false");
    expect(top).toHaveTextContent("4 upvotes");

    const replies = screen.getByRole("list", { name: "Replies to otter" });
    expect(within(replies).getAllByRole("article")).toHaveLength(3);
    const mine = within(replies).getByRole("article", { name: "teller's post" });
    expect(mine).toHaveTextContent("Replying to @wren");
    // The viewer's own reply: a count, no toggle; Edit and Delete, no Report.
    expect(within(mine).queryByRole("button", { name: /^Upvote/ })).toBeNull();
    expect(mine).toHaveTextContent("2 upvotes");
    expect(within(mine).getByRole("button", { name: "Edit" })).toBeInTheDocument();
    expect(within(mine).getByRole("button", { name: "Delete" })).toBeInTheDocument();
    expect(within(mine).queryByRole("button", { name: /^Report/ })).toBeNull();
    expect(within(replies).getAllByRole("article")[0]).not.toHaveTextContent("Replying to");
  });

  it("pages the rest of the replies and moves focus to the first new one", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    const { calls } = renderApp(
      "/lores/lore-1",
      handlers(thread, {
        "LoreService/ListLorePostReplies": () => ({ body: { replies: [reply("r-4", otter, "Fourth."), reply("r-5", wren, "Fifth.")] } }),
      }),
    );
    await user.click(await screen.findByRole("button", { name: "Show 2 more replies" }));
    const replies = screen.getByRole("list", { name: "Replies to otter" });
    await waitFor(() => expect(within(replies).getAllByRole("article")).toHaveLength(5));
    expect(calls("LoreService/ListLorePostReplies")).toEqual([{ postId: "lp-1", pageToken: "after-3", pageSize: 20 }]);
    await waitFor(() => expect(document.activeElement).toBe(within(replies).getAllByRole("listitem")[3]));
    expect(screen.queryByRole("button", { name: /more repl/ })).toBeNull();
  });

  it("upvotes a post, and its pressed state follows the server", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    const { calls } = renderApp(
      "/lores/lore-1",
      handlers(thread, { "LoreService/VoteLorePost": () => ({ body: { upvotes: 5, on: true } }) }),
      signedIn,
    );
    const top = await screen.findByRole("article", { name: "otter's post" });
    await user.click(within(top).getByRole("button", { name: "Upvote otter's post" }));
    await waitFor(() => expect(within(top).getByRole("button", { name: "Upvote otter's post" })).toHaveAttribute("aria-pressed", "true"));
    expect(top).toHaveTextContent("5 upvotes");
    expect(calls("LoreService/VoteLorePost")).toEqual([{ postId: "lp-1", on: true }]);
  });

  it("switches between Top and New, asking the server for that order", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    const { calls } = renderApp("/lores/lore-1", handlers());
    const top = await screen.findByRole("tab", { name: "Top" });
    expect(top).toHaveAttribute("aria-selected", "true");
    await user.click(screen.getByRole("tab", { name: "New" }));
    expect(screen.getByRole("tab", { name: "New" })).toHaveAttribute("aria-selected", "true");
    await waitFor(() =>
      expect(calls("LoreService/ListLorePosts")).toEqual([
        { loreId: "lore-1", sort: "LORE_POST_SORT_TOP", pageSize: 20 },
        { loreId: "lore-1", sort: "LORE_POST_SORT_NEW", pageSize: 20 },
      ]),
    );
  });

  it("replies to a reply into the same group, and shows the reply at once", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    const { calls } = renderApp(
      "/lores/lore-1",
      handlers(thread, {
        "LoreService/AddLorePost": () => ({ body: { post: reply("r-new", me, "Me too.", { replyingTo: wren }) } }),
      }),
      signedIn,
    );
    const replies = await screen.findByRole("list", { name: "Replies to otter" });
    const wrens = within(replies).getAllByRole("article", { name: "wren's post" })[0]!;
    const replyButton = within(wrens).getByRole("button", { name: "Reply" });
    await user.click(replyButton);
    const box = within(wrens).getByRole("textbox", { name: "Reply to wren" });
    expect(box).toHaveFocus();
    // Escape closes the form and gives focus back to Reply.
    await user.keyboard("{Escape}");
    expect(within(wrens).queryByRole("textbox")).toBeNull();
    expect(replyButton).toHaveFocus();

    await user.click(replyButton);
    await user.type(within(wrens).getByRole("textbox", { name: "Reply to wren" }), "Me too.");
    await user.click(within(wrens).getByRole("button", { name: "Post reply" }));
    await waitFor(() => expect(calls("LoreService/AddLorePost")).toEqual([{ loreId: "lore-1", body: "Me too.", replyToId: "r-1" }]));
    const posted = await within(replies).findByText("Me too.");
    await waitFor(() => expect(posted.closest("article")).toHaveFocus());
  });

  it("edits and deletes the viewer's own post", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    const { calls } = renderApp(
      "/lores/lore-1",
      handlers(thread, {
        "LoreService/EditLorePost": () => ({ body: { post: { id: "lp-2", author: me, body: "I was by the back door.", editedAt: at } } }),
        "LoreService/DeleteLorePost": () => ({ body: {} }),
      }),
      signedIn,
    );
    const posts = await screen.findByRole("list", { name: "Posts" });
    const own = within(posts).getByText("I was by the door.").closest("article")!;
    await user.click(within(own).getByRole("button", { name: "Edit" }));
    const box = within(own).getByRole("textbox", { name: "Edit your post" });
    expect(box).toHaveValue("I was by the door.");
    await user.clear(box);
    await user.type(box, "I was by the back door.");
    await user.click(within(own).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(calls("LoreService/EditLorePost")).toEqual([{ postId: "lp-2", body: "I was by the back door." }]));

    await user.click(within(own).getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete your post?" });
    expect(dialog).toHaveTextContent("It leaves the thread.");
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(calls("LoreService/DeleteLorePost")).toEqual([{ postId: "lp-2" }]));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Thread" })).toHaveFocus());
  });

  it("keeps a deleted post with replies as [deleted], and offers a signed-out reader no toggles", async () => {
    polyfillRadix();
    renderApp(
      "/lores/lore-1",
      handlers([{ id: "lp-9", deleted: true, createdAt: at, replyCount: 1, replies: [reply("r-9", wren, "Still here.", { replyToId: "lp-9", upvotes: 1 })] }]),
    );
    const gone = await screen.findByRole("article", { name: "Deleted post" });
    expect(gone).toHaveTextContent("[deleted]");
    expect(within(gone).queryByRole("button")).toBeNull();
    const kept = screen.getByRole("article", { name: "wren's post" });
    expect(kept).toHaveTextContent("Still here.");
    expect(kept).toHaveTextContent("1 upvote");
    expect(within(kept).queryByRole("button", { name: /^Upvote|^Reply$/ })).toBeNull();
  });
});
