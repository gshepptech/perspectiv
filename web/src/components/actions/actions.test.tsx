import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import { Component as ArcPage } from "@/routes/arc/ArcPage";
import { dayArcResponse, lore, perspective, summary } from "@/routes/arc/fixtures.testutil";
import { connectError, type FakeHandler } from "@/lib/fakeApi.testutil";
import { resetSessionForTests } from "@/lib/session";

import { bodiesFor, currentBody, polyfillRadix, renderApp } from "./testing.testutil";

beforeAll(polyfillRadix);
beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const route = { path: "/arcs/:arcId", Component: ArcPage };

type Store = Record<string, ReturnType<typeof perspective>["perspective"]>;

/** A stateful PerspectiveService/Get backed by `store`, defaulting unknown ids to a plain perspective. */
function getFrom(store: Store): FakeHandler {
  return () => {
    const id = String(currentBody().perspectiveId);
    return { body: { perspective: store[id] ?? perspective(id).perspective } };
  };
}

function openSignedIn(routes: Record<string, FakeHandler>, store: Store = {}, arcBody = dayArcResponse()) {
  return renderApp(route, "/arcs/arc-day", {
    signedIn: true,
    routes: {
      "ArcService/GetArc": () => ({ body: arcBody }),
      "PerspectiveService/Get": getFrom(store),
      ...routes,
    },
  });
}

async function card(region: string, author: string) {
  const section = await screen.findByRole("region", { name: region });
  return within(section).getByRole("article", { name: `${author}'s perspective` });
}

describe("upvote", () => {
  it("toggles a participant's up-vote on another perspective and shows the server's count", async () => {
    const store: Store = { "p-otter": perspective("p-otter", { upvotes: 2, voted: false }).perspective };
    const app = await openSignedIn({
      "VoteService/Vote": () => {
        const req = currentBody();
        const on = req.on === true;
        store["p-otter"] = { ...store["p-otter"]!, voted: on, upvotes: on ? 3 : 2 };
        return { body: { upvotes: on ? 3 : 2, on } };
      },
    }, store);

    const otter = await card("Canon", "otter");
    const button = await within(otter).findByRole("button", { name: "Upvote" });
    expect(button).toHaveAttribute("aria-pressed", "false");
    expect(within(otter).getByText("2 upvotes")).toBeInTheDocument();

    await userEvent.click(button);
    const pressed = await within(otter).findByRole("button", { name: "Remove upvote" });
    expect(pressed).toHaveAttribute("aria-pressed", "true");
    expect(within(otter).getByText("3 upvotes")).toBeInTheDocument();
    expect(bodiesFor(app.api, "VoteService/Vote")[0]).toEqual({ perspectiveId: "p-otter", on: true });

    await userEvent.click(pressed);
    await within(otter).findByRole("button", { name: "Upvote" });
    expect(bodiesFor(app.api, "VoteService/Vote")[1]).toEqual({ perspectiveId: "p-otter" });
  });

  it("is not offered on the viewer's own perspective", async () => {
    await openSignedIn({});
    const mine = await card("Canon", "wren");
    await within(mine).findByRole("button", { name: /^Heart/ });
    expect(within(mine).queryByRole("button", { name: "Upvote" })).toBeNull();
    expect(within(mine).queryByRole("button", { name: "Claim a character" })).toBeNull();
    expect(within(mine).queryByRole("button", { name: "Report" })).toBeNull();
  });

  it("shows the server's refusal inline", async () => {
    await openSignedIn({ "VoteService/Vote": () => connectError("failed_precondition", "verify your email first") });
    const otter = await card("Canon", "otter");
    await userEvent.click(await within(otter).findByRole("button", { name: "Upvote" }));
    expect(await within(otter).findByRole("alert")).toHaveTextContent("verify your email first");
  });
});

describe("react", () => {
  it("a signed-in non-participant toggles one emoji and sees per-emoji counts", async () => {
    const arc = dayArcResponse();
    arc.canon.perspectives = arc.canon.perspectives.filter((p) => p.author?.username !== "wren");
    arc.viewer = { perspectiveId: "", loreId: "" };
    const store: Store = {};
    const app = await openSignedIn(
      {
        "ReactService/React": () => {
          const req = currentBody();
          const counts = ["EMOJI_HEART", "EMOJI_LAUGH", "EMOJI_WOW", "EMOJI_SAD", "EMOJI_FIRE", "EMOJI_CLAP"].map((emoji) => ({
            emoji,
            count: emoji === req.emoji ? 4 : 1,
            mine: emoji === req.emoji && req.on === true,
          }));
          store["p-otter"] = perspective("p-otter", { reacts: counts }).perspective;
          return { body: { counts } };
        },
      },
      store,
      arc,
    );
    const otter = await card("Canon", "otter");
    // Not a participant: no upvote, but reacting is open to any signed-in reader.
    expect(within(otter).queryByRole("button", { name: "Upvote" })).toBeNull();
    const group = await within(otter).findByRole("group", { name: "Reacts" });
    expect(within(group).getAllByRole("button")).toHaveLength(6);

    const fire = within(group).getByRole("button", { name: "Fire, 0" });
    fire.focus();
    await userEvent.keyboard("{Enter}");
    expect(await within(group).findByRole("button", { name: "Fire, 4" })).toHaveAttribute("aria-pressed", "true");
    expect(within(group).getByRole("button", { name: "Heart, 1" })).toHaveAttribute("aria-pressed", "false");
    expect(bodiesFor(app.api, "ReactService/React")[0]).toEqual({ perspectiveId: "p-otter", emoji: "EMOJI_FIRE", on: true });
  });
});

describe("claim", () => {
  const characters = [
    { id: "c-1", position: 1, label: "The barista in the green apron", hasAcceptedClaim: false, hasPendingClaim: false },
    { id: "c-2", position: 2, label: "A man with a red umbrella", hasAcceptedClaim: true, hasPendingClaim: false },
    { id: "c-3", position: 3, label: "Someone by the window", hasAcceptedClaim: false, hasPendingClaim: true },
  ];

  it("a participant claims a character in another perspective from its character list", async () => {
    const store: Store = { "p-heron": perspective("p-heron", { characters }).perspective };
    // Like the server, the claim makes the viewer that character, so the
    // refetched story offers nothing more to claim.
    const app = await openSignedIn(
      {
        "LoreService/ClaimCharacter": () => {
          store["p-heron"] = perspective("p-heron", {
            characters: characters.map((c) => (c.id === "c-1" ? { ...c, hasPendingClaim: true, myClaim: "CLAIM_STATUS_PENDING" } : c)),
          }).perspective;
          return { body: { claimId: "cl-1", status: "CLAIM_STATUS_PENDING" } };
        },
      },
      store,
    );
    const moth = await card("Canon", "heron");
    await userEvent.click(await within(moth).findByRole("button", { name: "Claim a character" }));
    const dialog = await screen.findByRole("dialog", { name: "Claim a character" });

    const taken = within(dialog).getByRole("listitem", { name: "A man with a red umbrella" });
    expect(within(taken).getByText("Already claimed")).toBeInTheDocument();
    expect(within(taken).queryByRole("button")).toBeNull();
    expect(within(within(dialog).getByRole("listitem", { name: "Someone by the window" })).getByText("Someone has claimed this")).toBeInTheDocument();

    const open = within(dialog).getByRole("listitem", { name: "The barista in the green apron" });
    await userEvent.click(within(open).getByRole("button", { name: "That was me" }));
    expect(await within(dialog).findByRole("status")).toHaveTextContent("Claim sent");
    expect(bodiesFor(app.api, "LoreService/ClaimCharacter")).toEqual([{ characterId: "c-1" }]);
    // The refetched story no longer offers a claim; the confirmation stays up
    // until the reader closes it.
    expect(await within(moth).findByText(/waiting for heron to accept/)).toBeInTheDocument();
    expect(within(moth).queryByRole("button", { name: "Claim a character", hidden: true })).toBeNull();
    expect(within(screen.getByRole("dialog", { name: "Claim a character" })).getByRole("status")).toHaveTextContent("Claim sent");

    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("shows the server's reason when a claim is refused", async () => {
    const store: Store = { "p-heron": perspective("p-heron", { characters }).perspective };
    await openSignedIn(
      { "LoreService/ClaimCharacter": () => connectError("failed_precondition", "write your perspective in this arc first") },
      store,
    );
    const moth = await card("Canon", "heron");
    await userEvent.click(await within(moth).findByRole("button", { name: "Claim a character" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "That was me" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("write your perspective in this arc first");
  });

  it("offers no claim on a perspective that names no characters", async () => {
    await openSignedIn({});
    const moth = await card("Canon", "heron");
    await within(moth).findByRole("button", { name: /^Heart/ });
    expect(within(moth).queryByRole("button", { name: "Claim a character" })).toBeNull();
  });

  it("says who the viewer is in a story instead of offering a second claim", async () => {
    const mine = [
      { id: "c-1", position: 1, label: "The barista in the green apron", myClaim: "CLAIM_STATUS_PENDING", hasPendingClaim: true },
      { id: "c-2", position: 2, label: "Someone by the window" },
    ];
    await openSignedIn({}, { "p-heron": perspective("p-heron", { characters: mine }).perspective });
    const moth = await card("Canon", "heron");
    expect(await within(moth).findByText(/waiting for heron to accept/)).toBeInTheDocument();
    expect(within(moth).queryByRole("button", { name: "Claim a character" })).toBeNull();
  });

  it("tags a character with who it turned out to be", async () => {
    const tagged = [{ id: "c-1", position: 1, label: "A man with a red umbrella", hasAcceptedClaim: true, claimedBy: { userId: "u-otter", username: "otter" } }];
    await openSignedIn({}, { "p-heron": perspective("p-heron", { characters: tagged }).perspective });
    const moth = await card("Canon", "heron");
    const cast = await within(moth).findByRole("list", { name: "Cast" });
    expect(within(cast).getByRole("link", { name: "otter" })).toHaveAttribute("href", "/users/otter");
  });

  it("is not offered on a REMOVED perspective", async () => {
    await openSignedIn({});
    const removed = await card("Tales", "voted_out");
    await within(removed).findByRole("button", { name: /^Heart/ });
    expect(within(removed).queryByRole("button", { name: "Claim a character" })).toBeNull();
  });
});

describe("remove vote", () => {
  it("a member of a 4-member lore votes to remove another member's perspective after confirming", async () => {
    const app = await openSignedIn({
      "VoteService/VoteRemove": () => ({ body: { removeVotes: 1, eligibleVoters: 3, removed: false } }),
    });
    const otter = await card("Canon", "otter");
    await userEvent.click(await within(otter).findByRole("button", { name: "Vote to remove" }));
    const dialog = await screen.findByRole("dialog", { name: "Vote to remove otter's perspective?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Vote to remove" }));
    expect(await within(otter).findByRole("status")).toHaveTextContent("1 of 3 members have voted to remove");
    expect(bodiesFor(app.api, "VoteService/VoteRemove")).toEqual([{ perspectiveId: "p-otter" }]);
  });

  it("reports when the vote reached a majority", async () => {
    await openSignedIn({ "VoteService/VoteRemove": () => ({ body: { removeVotes: 2, eligibleVoters: 3, removed: true } }) });
    const otter = await card("Canon", "otter");
    await userEvent.click(await within(otter).findByRole("button", { name: "Vote to remove" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Vote to remove" }));
    expect(await within(otter).findByRole("status")).toHaveTextContent("removed from the lore");
  });

  it("is offered only inside the viewer's own lore of 4 or more members", async () => {
    await openSignedIn({});
    // Another lore in the same arc is a contender card, with no actions on the arc page.
    const contenders = await screen.findByRole("region", { name: "Contenders" });
    expect(within(contenders).queryByRole("button", { name: "Vote to remove" })).toBeNull();
    // The viewer's own perspective.
    const mine = await card("Canon", "wren");
    expect(within(mine).queryByRole("button", { name: "Vote to remove" })).toBeNull();
  });

  it("is not offered when the lore has fewer than 4 members", async () => {
    const arc = dayArcResponse();
    const members = arc.canon.lore.members.slice(0, 3);
    arc.canon.lore = lore("lore-canon", "CANON", members);
    arc.canon.perspectives = arc.canon.perspectives.slice(0, 3);
    await openSignedIn({}, {}, arc);
    const otter = await card("Canon", "otter");
    await within(otter).findByRole("button", { name: /^Heart/ });
    expect(within(otter).queryByRole("button", { name: "Vote to remove" })).toBeNull();
  });
});

describe("report", () => {
  it("AC-040 OT-043: the report form offers the real-name-or-handle reason and sends it for the perspective", async () => {
    const app = await openSignedIn({ "ModerationService/Report": () => ({ body: { reportId: "r-9" } }) });
    const otter = await card("Canon", "otter");
    await userEvent.click(await within(otter).findByRole("button", { name: "Report" }));
    const dialog = await screen.findByRole("dialog", { name: "Report otter's perspective" });

    await userEvent.click(within(dialog).getByRole("button", { name: "Send report" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Choose a reason");
    expect(bodiesFor(app.api, "ModerationService/Report")).toHaveLength(0);

    within(dialog).getByRole("combobox", { name: "Reason" }).focus();
    await userEvent.keyboard("{Enter}");
    const option = await screen.findByRole("option", { name: "Real name or handle of a non-user" });
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
      "Real name or handle of a non-user",
      "Harassment",
      "Hateful content",
      "Sexual content",
      "Spam",
      "Other",
    ]);
    await userEvent.click(option);
    await userEvent.type(within(dialog).getByRole("textbox", { name: /Details/ }), "Names my coworker");
    await userEvent.click(within(dialog).getByRole("button", { name: "Send report" }));

    expect(await within(dialog).findByRole("status")).toHaveTextContent("A moderator will review it");
    expect(bodiesFor(app.api, "ModerationService/Report")).toEqual([
      {
        targetKind: "REPORT_TARGET_KIND_PERSPECTIVE",
        targetId: "p-otter",
        reason: "REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER",
        details: "Names my coworker",
      },
    ]);
  });

  it("shows a rate-limit refusal inline", async () => {
    await openSignedIn({ "ModerationService/Report": () => connectError("resource_exhausted", "too many reports, try later") });
    const otter = await card("Canon", "otter");
    await userEvent.click(await within(otter).findByRole("button", { name: "Report" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("combobox", { name: "Reason" }));
    await userEvent.click(await screen.findByRole("option", { name: "Spam" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Send report" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("too many reports, try later");
  });
});

describe("deleted perspectives", () => {
  it("never fetch or render actions", async () => {
    const arc = dayArcResponse();
    arc.tales = [summary({ id: "p-del", loreStatus: "LORE_STATUS_SOLO", deleted: true, excerpt: "" })];
    const app = await openSignedIn({}, {}, arc);
    await screen.findByText("This perspective was deleted.");
    await waitFor(() => expect(bodiesFor(app.api, "PerspectiveService/Get").length).toBeGreaterThan(0));
    expect(bodiesFor(app.api, "PerspectiveService/Get").map((b) => b.perspectiveId)).not.toContain("p-del");
  });
});
