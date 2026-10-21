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
            { id: "c-1", position: 1, label: "the guy in the yellow raincoat", hasAcceptedClaim: true, claimedBy: otter },
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
    expect(await screen.findByRole("heading", { level: 1, name: "Open mic night" })).toBeInTheDocument();
    expect(screen.getByText("Canon of the day")).toBeInTheDocument();
    const cast = await screen.findByRole("region", { name: "Who was who" });
    expect(cast).toHaveTextContent("otter was “the guy in the yellow raincoat” in wren’s story");
    expect(await screen.findByText("The power went out.")).toBeInTheDocument();
    expect(screen.getByText("I don't sing.")).toBeInTheDocument();
    const thread = screen.getByRole("region", { name: "Thread" });
    expect(await within(thread).findByText("That was the best night.")).toBeInTheDocument();
    expect(within(thread).getByText("In this lore")).toBeInTheDocument();
    expect(within(thread).getByText("Write your perspective in this arc to join the thread.")).toBeInTheDocument();
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

  it("says when the lore is gone", async () => {
    renderApp("/lores/nope", { "LoreService/GetLore": () => connectError("not_found") });
    expect(await screen.findByRole("heading", { name: "Lore not found" })).toBeInTheDocument();
  });
});
