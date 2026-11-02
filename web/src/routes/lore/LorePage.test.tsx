import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { polyfillRadix } from "@/components/actions/testing.testutil";
import { connectError, sessionReply } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const ME = "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10";
const wren = { userId: "u-wren", username: "wren" };
const otter = { userId: "u-otter", username: "otter" };

const lore = {
  id: "lore-1",
  arcId: "arc-1",
  standing: "LORE_STANDING_CANON",
  memberCount: 2,
  upvotes: 5,
  members: [
    { perspectiveId: "p-wren", user: wren },
    { perspectiveId: "p-otter", user: otter },
  ],
};

function story(id: string, author: typeof wren, body: string, characters: unknown[] = []) {
  return {
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
      parts: [{ position: 1, body }],
      characters,
      reacts: [],
    },
  };
}

const handlers = (posts: unknown[] = []) => ({
  "LoreService/GetLore": () => ({ body: { lore } }),
  "ArcService/GetArc": () => ({ body: { arc: { id: "arc-1", title: "Open mic night" } } }),
  "PerspectiveService/Get": (body: Record<string, unknown>) =>
    body.perspectiveId === "p-wren"
      ? {
          body: story("p-wren", wren, "The power went out.", [
            { id: "c-1", position: 1, label: "the guy in the yellow raincoat", hasAcceptedClaim: true, claimedBy: otter, claimedByStoryId: "p-otter" },
          ]),
        }
      : { body: story("p-otter", otter, "I don't sing.") },
  "LoreService/ListLorePosts": () => ({ body: { posts } }),
});

describe("lore page", () => {
  it("shows the canon's standing, who was who, every story in full, and its thread", async () => {
    polyfillRadix();
    renderApp(
      "/lores/lore-1",
      handlers([{ id: "lp-1", author: otter, body: "That was the best night.", byMember: true, createdAt: "2026-10-10T12:00:00Z" }]),
    );
    // Unnamed, the lore is never titled with its day's own title.
    expect(await screen.findByRole("heading", { level: 1, name: "Unnamed lore" })).toBeInTheDocument();
    expect(screen.getByText(/^Open mic night · /)).toBeInTheDocument();
    // Flairs are words with an icon, never a pill (Folio, Flairs and tags).
    expect(screen.getByText("Canon of the day").className).not.toMatch(/rounded-full|bg-accent/);
    const cast = await screen.findByRole("region", { name: "Who was who" });
    expect(cast).toHaveTextContent("otter was “the guy in the yellow raincoat” in wren’s story");
    expect(await screen.findByText("The power went out.")).toBeInTheDocument();
    expect(screen.getByText("I don't sing.")).toBeInTheDocument();
    // Each story is a plain article under a hairline, headed in the serif (Folio, Lore page: stories).
    const story = screen.getByRole("article", { name: "wren's perspective" });
    expect(story).toHaveClass("border-t", "border-rule");
    expect(story.className).not.toMatch(/rounded|shadow|\bp-5\b/);
    expect(within(story).getByRole("heading", { level: 3 }).className).not.toMatch(/font-sans/);
    const thread = screen.getByRole("region", { name: "Thread" });
    expect(await within(thread).findByText("That was the best night.")).toBeInTheDocument();
    expect(within(thread).getByText("In this lore")).toBeInTheDocument();
    expect(within(thread).getByText("Anyone can read this thread. Only the people who wrote about this day can post in it.")).toBeInTheDocument();
    expect(within(thread).queryByText(/Write your/)).toBeNull();
    expect(document.title).toBe("Unnamed lore in Open mic night · Perspectiv");
    // A tag opens the claimant's own story of the day.
    const wrens = screen.getByRole("article", { name: "wren's perspective" });
    expect(await within(wrens).findByRole("link", { name: "otter’s story" })).toHaveAttribute("href", "/perspectives/p-otter");
  });

  it("calls an hour arc's canon the canon of this hour, and says only its writers post", async () => {
    polyfillRadix();
    renderApp("/lores/lore-1", {
      ...handlers(),
      "ArcService/GetArc": () => ({ body: { arc: { id: "arc-1", title: "Open mic night · 21:00", hour: 21 } } }),
    });
    expect(await screen.findByText("Canon of this hour")).toBeInTheDocument();
    expect(screen.queryByText("Canon of the day")).toBeNull();
    const thread = screen.getByRole("region", { name: "Thread" });
    expect(await within(thread).findByText("Nobody has posted yet.")).toBeInTheDocument();
    expect(within(thread).getByText("Anyone can read this thread. Only the people who wrote about this hour can post in it.")).toBeInTheDocument();
  });

  it("lets someone with a perspective in the arc post to the thread", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    const { calls } = renderApp(
      "/lores/lore-1",
      {
        ...handlers(),
        "ArcService/GetArc": () => ({
          body: { arc: { id: "arc-1", title: "Open mic night" }, viewer: { perspectiveId: "p-mine", loreId: "" } },
        }),
        "LoreService/AddLorePost": () => ({ body: { post: { id: "lp-2", author: { userId: ME, username: "teller" }, body: "I was at the bar." } } }),
      },
      { refresh: () => sessionReply({ token: "tok" }) },
    );
    const box = await screen.findByRole("textbox", { name: "Add to the thread" });
    await user.type(box, "  I was at the bar.  ");
    await user.click(screen.getByRole("button", { name: "Post" }));
    await waitFor(() => expect(calls("LoreService/AddLorePost")).toEqual([{ loreId: "lore-1", body: "I was at the bar." }]));
  });

  it("lets a member name the lore, and shows the name as its title", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    let named = "";
    const { calls } = renderApp(
      "/lores/lore-1",
      {
        ...handlers(),
        "LoreService/GetLore": () => ({
          body: { lore: named ? { ...lore, name: named, namedBy: { userId: ME, username: "wren" } } : lore },
        }),
        "ArcService/GetArc": () => ({
          body: { arc: { id: "arc-1", title: "Open mic night" }, viewer: { perspectiveId: "p-wren", loreId: "lore-1" } },
        }),
        "LoreService/NameLore": (body: Record<string, unknown>) => {
          named = String(body.name).trim().replace(/\s+/g, " ");
          return { body: { name: named } };
        },
      },
      { refresh: () => sessionReply({ token: "tok" }) },
    );
    await user.click(await screen.findByRole("button", { name: "Name this lore" }));
    const dialog = await screen.findByRole("dialog", { name: "Name this lore" });
    const field = within(dialog).getByRole("textbox", { name: "Name" });
    await user.click(within(dialog).getByRole("button", { name: "Save name" }));
    expect(await within(dialog).findByText("Give the lore a name.")).toBeInTheDocument();
    await user.type(field, "The  open mic   crew");
    await user.click(within(dialog).getByRole("button", { name: "Save name" }));

    expect(await screen.findByRole("heading", { level: 1, name: "The open mic crew" })).toBeInTheDocument();
    expect(calls("LoreService/NameLore")).toEqual([{ loreId: "lore-1", name: "The  open mic   crew" }]);
    expect(screen.getByText(/Named by/)).toHaveTextContent("Named by wren");
    expect(screen.getByText(/Open mic night · Moonbean Coffee/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Rename this lore" })).toBeInTheDocument();
  });

  it("offers naming only to the lore's members", async () => {
    polyfillRadix();
    renderApp(
      "/lores/lore-1",
      {
        ...handlers(),
        "ArcService/GetArc": () => ({
          body: { arc: { id: "arc-1", title: "Open mic night" }, viewer: { perspectiveId: "p-mine", loreId: "lore-other" } },
        }),
      },
      { refresh: () => sessionReply({ token: "tok" }) },
    );
    await screen.findByRole("textbox", { name: "Add to the thread" });
    expect(screen.queryByRole("button", { name: /Name this lore/ })).not.toBeInTheDocument();
  });

  it("lets a reader report a thread post, but not their own", async () => {
    polyfillRadix();
    const user = userEvent.setup();
    const { calls } = renderApp(
      "/lores/lore-1",
      {
        ...handlers([
          { id: "lp-1", author: otter, body: "That cashier is Jo Smith.", byMember: true, createdAt: "2026-10-10T12:00:00Z" },
          { id: "lp-2", author: { userId: ME, username: "teller" }, body: "Mine.", byMember: false, createdAt: "2026-10-10T13:00:00Z" },
        ]),
        "ModerationService/Report": () => ({ body: { reportId: "r-1" } }),
      },
      { refresh: () => sessionReply({ token: "tok" }) },
    );
    const posts = await screen.findByRole("list", { name: "Posts" });
    const [theirs, mine] = within(posts).getAllByRole("listitem");
    expect(within(mine!).queryByRole("button", { name: /^Report:/ })).not.toBeInTheDocument();
    await user.click(within(theirs!).getByRole("button", { name: /^Report:/ }));
    const dialog = await screen.findByRole("dialog", { name: "Report otter's post" });
    await user.click(within(dialog).getByRole("combobox", { name: "Reason" }));
    await user.click(await screen.findByRole("option", { name: "Real name or handle of a non-user" }));
    await user.click(within(dialog).getByRole("button", { name: "Send report" }));
    await waitFor(() =>
      expect(calls("ModerationService/Report")).toEqual([
        { targetKind: "REPORT_TARGET_KIND_LORE_POST", targetId: "lp-1", reason: "REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER" },
      ]),
    );
  });

  it("offers a reader outside the lore a way to report its name", async () => {
    polyfillRadix();
    renderApp(
      "/lores/lore-1",
      { ...handlers(), "LoreService/GetLore": () => ({ body: { lore: { ...lore, name: "Bad name", namedBy: wren } } }) },
      { refresh: () => sessionReply({ token: "tok" }) },
    );
    expect(await screen.findByRole("heading", { level: 1, name: "Bad name" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Report this name/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Name this lore|Rename/ })).not.toBeInTheDocument();
  });

  it("says when the lore is gone", async () => {
    renderApp("/lores/nope", { "LoreService/GetLore": () => connectError("not_found") });
    expect(await screen.findByRole("heading", { name: "Lore not found" })).toBeInTheDocument();
  });
});
