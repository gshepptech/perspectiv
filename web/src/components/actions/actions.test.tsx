import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import { Component as ArcPage } from "@/routes/arc/ArcPage";
import { Component as ReadPage } from "@/routes/read/ReadPage";
import { dayArcResponse, HERON, lore, MOTH, OTTER, perspective, summary, user, WREN } from "@/routes/arc/fixtures.testutil";
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

const storyRoute = { path: "/perspectives/:perspectiveId", Component: ReadPage };

/**
 * Opens one story's own page, where a reader responds to it. GetArc says
 * where the viewer stands in the arc and GetLore how big their lore is.
 */
function openStory(
  id: string,
  author: { userId: string; username: string },
  opts: {
    routes?: Record<string, FakeHandler>;
    store?: Store;
    over?: Record<string, unknown>;
    arcBody?: ReturnType<typeof dayArcResponse>;
    loreMembers?: number;
    signedOut?: boolean;
    path?: string;
  } = {},
) {
  const store = opts.store ?? {};
  store[id] ??= perspective(id, { author, loreId: "lore-canon", ...opts.over }).perspective;
  const arcBody = opts.arcBody ?? dayArcResponse();
  const canonLore = arcBody.canon.lore;
  const members = canonLore.members.slice(0, opts.loreMembers ?? canonLore.members.length);
  return renderApp(storyRoute, opts.path ?? `/perspectives/${id}`, {
    signedIn: !opts.signedOut,
    routes: {
      "ArcService/GetArc": () => ({ body: arcBody }),
      "LoreService/GetLore": () => ({ body: { lore: lore("lore-canon", "CANON", members) } }),
      "PerspectiveService/Get": getFrom(store),
      ...opts.routes,
    },
  });
}

/** The reader's actions under the story. */
function story() {
  return screen.findByRole("region", { name: "Respond to this story" });
}

describe("upvote rail on the arc page", () => {
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
    const button = await within(otter).findByRole("button", { name: /^Upvote/, pressed: false });
    expect(button).toHaveAttribute("aria-pressed", "false");
    expect(otter).toHaveTextContent("2 upvotes");

    await userEvent.click(button);
    const pressed = await within(otter).findByRole("button", { name: /^Upvote/, pressed: true });
    expect(pressed).toHaveAttribute("aria-pressed", "true");
    await waitFor(() => expect(otter).toHaveTextContent("3 upvotes"));
    expect(bodiesFor(app.api, "VoteService/Vote")[0]).toEqual({ perspectiveId: "p-otter", on: true });

    await userEvent.click(pressed);
    await within(otter).findByRole("button", { name: /^Upvote/, pressed: false });
    expect(bodiesFor(app.api, "VoteService/Vote")[1]).toEqual({ perspectiveId: "p-otter" });
  });

  it("is not offered on the viewer's own perspective", async () => {
    await openSignedIn({});
    const otter = await card("Canon", "otter");
    await within(otter).findByRole("button", { name: /^Upvote/, pressed: false });
    const mine = await card("Canon", "wren");
    expect(within(mine).queryByRole("button", { name: /^Upvote/, pressed: false })).toBeNull();
    expect(mine).toHaveTextContent("2 upvotes");
  });

  it("shows the server's refusal", async () => {
    await openSignedIn({ "VoteService/Vote": () => connectError("failed_precondition", "verify your email first") });
    const otter = await card("Canon", "otter");
    await userEvent.click(await within(otter).findByRole("button", { name: /^Upvote/, pressed: false }));
    expect(await within(otter).findByRole("alert")).toHaveTextContent("verify your email first");
  });

  it("keeps the arc page to the rail: reacting, claiming and reporting happen on the story's page", async () => {
    await openSignedIn({});
    const otter = await card("Canon", "otter");
    await within(otter).findByRole("button", { name: /^Upvote/, pressed: false });
    expect(within(otter).queryByRole("group", { name: "Reacts" })).toBeNull();
    expect(within(otter).queryByRole("button", { name: "Claim a character" })).toBeNull();
    expect(within(otter).queryByRole("button", { name: /^Report:/ })).toBeNull();
    expect(within(otter).getByRole("link", { name: "Read otter's perspective" })).toHaveAttribute("href", "/perspectives/p-otter");
  });
});

describe("on a story's own page", () => {
  it("offers the author nothing to respond with", async () => {
    await openStory("p-wren", WREN);
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("region", { name: "Respond to this story" })).toBeNull();
  });

  it("a participant up-votes from under the story", async () => {
    const store: Store = {};
    const app = await openStory("p-otter", OTTER, {
      store,
      routes: {
        "VoteService/Vote": () => {
          store["p-otter"] = { ...store["p-otter"]!, voted: true, upvotes: 3 };
          return { body: { upvotes: 3, on: true } };
        },
      },
    });
    const actions = await story();
    await userEvent.click(await within(actions).findByRole("button", { name: /^Upvote/, pressed: false }));
    expect(await within(actions).findByRole("button", { name: /^Upvote/, pressed: true })).toHaveAttribute("aria-pressed", "true");
    expect(within(actions).getByText("3 upvotes")).toBeInTheDocument();
    expect(bodiesFor(app.api, "VoteService/Vote")).toEqual([{ perspectiveId: "p-otter", on: true }]);
  });
});

describe("signed-out reader on a story's page", () => {
  for (const [label, name] of [["report", /^Report:/], ["save", /^Save /]] as const) {
    it(`AC-009: the ${label} action redirects to login without calling the server`, async () => {
      const { api } = await openStory("p-otter", OTTER, {
        signedOut: true,
        over: { characters: [{ id: "c-any", position: 1, label: "a stranger" }] },
      });
      const actions = await story();
      await userEvent.click(await within(actions).findByRole("button", { name }));
      expect(await screen.findByTestId("login")).toHaveTextContent("next=%2Fperspectives%2Fp-otter");
      expect(api.calls.some((c) => /^(VoteService|ReactService|LoreService|ModerationService)|SavePerspective/.test(c.procedure))).toBe(false);
    });
  }

  // Upvote and claim need a perspective in the arc too, so signing in alone
  // would not be enough: the reader is told why, with the way to sign in.
  // Reacting needs only an account, and is explained the same way rather
  // than by leaving the page.
  for (const [label, name, why, back] of [
    ["upvote", /^Upvote/, "Only people who wrote about this day can upvote its stories, so the people who were there choose its canon.", "%2Fperspectives%2Fp-otter"],
    ["claim", "Claim a character", "To say this was you, write your side of this day first.", "%2Fperspectives%2Fp-otter%3Fclaim%3D1"],
    ["react", /^Heart/, "To react to a story, sign in or create an account.", "%2Fperspectives%2Fp-otter"],
  ] as const) {
    it(`AC-009: the ${label} action says why and offers sign-in and sign-up, without calling the server`, async () => {
      const { api } = await openStory("p-otter", OTTER, {
        signedOut: true,
        over: { characters: [{ id: "c-any", position: 1, label: "a stranger" }] },
      });
      const actions = await story();
      const button = await within(actions).findByRole("button", { name });
      expect(button).toHaveAttribute("aria-expanded", "false");
      await userEvent.click(button);
      expect(button).toHaveAttribute("aria-expanded", "true");
      expect(actions).toHaveTextContent(why);
      expect(within(actions).getByRole("link", { name: "Sign in" })).toHaveAttribute("href", `/login?next=${back}`);
      expect(within(actions).getByRole("link", { name: "Create an account" })).toHaveAttribute("href", `/signup?next=${back}`);
      expect(api.calls.some((c) => /^(VoteService|LoreService|ReactService)/.test(c.procedure))).toBe(false);
      expect(screen.queryByTestId("login")).toBeNull();
    });
  }
});

describe("a signed-in reader with no perspective in the arc", () => {
  const characters = [{ id: "c-1", position: 1, label: "The barista in the green apron" }];
  const outsider = () => {
    const arcBody = dayArcResponse();
    arcBody.viewer = { perspectiveId: "", loreId: "" };
    return arcBody;
  };

  it("is told on Upvote who may vote, with the composer offered only to someone who was there", async () => {
    const { api } = await openStory("p-otter", OTTER, {
      arcBody: outsider(),
      over: { place: { id: "pl-9", name: "Moonbean" }, localDate: "2026-10-01" },
    });
    const actions = await story();
    await userEvent.click(await within(actions).findByRole("button", { name: /^Upvote/ }));
    expect(actions).toHaveTextContent(
      "Only people who wrote about this day can upvote its stories, so the people who were there choose its canon.",
    );
    expect(actions).toHaveTextContent("Were you there? Write your side");
    expect(actions).not.toHaveTextContent("Write your side first");
    expect(within(actions).getByRole("link", { name: "Write your side" })).toHaveAttribute(
      "href",
      "/compose?placeId=pl-9&date=2026-10-01&return=%2Fperspectives%2Fp-otter",
    );
    expect(api.calls.some((c) => c.procedure.startsWith("VoteService"))).toBe(false);
  });

  it("arriving from \"That was me\", sees why in place of the claim dialog, focused, with a way back to claim", async () => {
    await openStory("p-heron", HERON, {
      arcBody: outsider(),
      over: { characters, place: { id: "pl-9", name: "Moonbean" }, localDate: "2026-10-01", hour: 21 },
      path: "/perspectives/p-heron?claim=1",
    });
    const actions = await story();
    const why = await within(actions).findByText(/^To say this was you, write your side of this day first\./);
    await waitFor(() => expect(why.closest("div")).toHaveFocus());
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(within(actions).getByRole("button", { name: "Claim a character" })).toHaveAttribute("aria-expanded", "true");
    expect(within(actions).getByRole("link", { name: "Write your side" })).toHaveAttribute(
      "href",
      "/compose?placeId=pl-9&date=2026-10-01&hour=21&return=%2Fperspectives%2Fp-heron%3Fclaim%3D1",
    );
  });

  it("arriving from \"That was me\" on one character, keeps that character on the way back through the composer", async () => {
    const { router } = await openStory("p-heron", HERON, {
      arcBody: outsider(),
      over: { characters, place: { id: "pl-9", name: "Moonbean" }, localDate: "2026-10-01" },
      path: "/perspectives/p-heron?claim=c-1",
    });
    const actions = await story();
    expect(await within(actions).findByRole("link", { name: "Write your side" })).toHaveAttribute(
      "href",
      "/compose?placeId=pl-9&date=2026-10-01&return=%2Fperspectives%2Fp-heron%3Fclaim%3Dc-1",
    );
    await waitFor(() => expect(router.state.location.search).toBe(""));
  });

  it("is offered no claim when nobody in the story is open to claim", async () => {
    await openStory("p-heron", HERON, { arcBody: outsider(), over: { characters: [{ ...characters[0], hasAcceptedClaim: true }] } });
    const actions = await story();
    await within(actions).findByRole("button", { name: /^Heart/ });
    expect(within(actions).queryByRole("button", { name: "Claim a character" })).toBeNull();
  });
});

describe("react", () => {
  it("a signed-in non-participant toggles one emoji and sees per-emoji counts", async () => {
    const arcBody = dayArcResponse();
    arcBody.viewer = { perspectiveId: "", loreId: "" };
    const store: Store = {};
    const app = await openStory("p-otter", OTTER, {
      store,
      arcBody,
      routes: {
        "ReactService/React": () => {
          const req = currentBody();
          const counts = ["EMOJI_HEART", "EMOJI_LAUGH", "EMOJI_WOW", "EMOJI_SAD", "EMOJI_FIRE", "EMOJI_CLAP"].map((emoji) => ({
            emoji,
            count: emoji === req.emoji ? 4 : 1,
            mine: emoji === req.emoji && req.on === true,
          }));
          store["p-otter"] = perspective("p-otter", { author: OTTER, reacts: counts }).perspective;
          return { body: { counts } };
        },
      },
    });
    const actions = await story();
    const group = await within(actions).findByRole("group", { name: "Reacts" });
    // Not a participant: no upvote, but reacting is open to any signed-in reader.
    expect(within(actions).queryByRole("button", { name: /^Upvote/, pressed: false })).toBeNull();
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
    const store: Store = {};
    // Like the server, the claim makes the viewer that character, so the
    // refetched story offers nothing more to claim.
    const app = await openStory("p-heron", HERON, {
      store,
      over: { characters },
      routes: {
        "LoreService/ClaimCharacter": () => {
          store["p-heron"] = perspective("p-heron", {
            author: HERON,
            loreId: "lore-canon",
            characters: characters.map((c) => (c.id === "c-1" ? { ...c, hasPendingClaim: true, myClaim: "CLAIM_STATUS_PENDING" } : c)),
          }).perspective;
          return { body: { claimId: "cl-1", status: "CLAIM_STATUS_PENDING" } };
        },
      },
    });
    const actions = await story();
    await userEvent.click(await within(actions).findByRole("button", { name: "Claim a character" }));
    const dialog = await screen.findByRole("dialog", { name: "Claim a character" });

    const taken = within(dialog).getByRole("listitem", { name: "A man with a red umbrella" });
    expect(within(taken).getByText("Already claimed")).toBeInTheDocument();
    expect(within(taken).queryByRole("button")).toBeNull();
    expect(within(within(dialog).getByRole("listitem", { name: "Someone by the window" })).getByText("Someone has claimed this")).toBeInTheDocument();

    const open = within(dialog).getByRole("listitem", { name: "The barista in the green apron" });
    await userEvent.click(within(open).getByRole("button", { name: "That was me: The barista in the green apron" }));
    expect(await within(dialog).findByRole("status")).toHaveTextContent("Claim sent");
    expect(bodiesFor(app.api, "LoreService/ClaimCharacter")).toEqual([{ characterId: "c-1" }]);
    // The refetched story no longer offers a claim; the confirmation stays up
    // until the reader closes it.
    expect(await within(actions).findByText(/waiting for heron to accept/)).toBeInTheDocument();
    expect(within(actions).queryByRole("button", { name: "Claim a character", hidden: true })).toBeNull();
    expect(within(screen.getByRole("dialog", { name: "Claim a character" })).getByRole("status")).toHaveTextContent("Claim sent");

    // The refetched story shows the viewer's own claim as theirs.
    expect(within(screen.getByRole("dialog", { name: "Claim a character" })).getByText("Your claim, waiting for heron")).toBeInTheDocument();
    // The pressed button gave way to that text, so focus sits on the confirmation.
    expect(within(screen.getByRole("dialog", { name: "Claim a character" })).getByRole("status")).toHaveFocus();

    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    // The trigger is gone, so focus lands on the line that replaced it.
    await waitFor(() => expect(within(actions).getByText(/waiting for heron to accept/).closest("p")).toHaveFocus());
  });

  it("shows the server's reason when a claim is refused", async () => {
    await openStory("p-heron", HERON, {
      over: { characters },
      routes: { "LoreService/ClaimCharacter": () => connectError("failed_precondition", "write your perspective in this arc first") },
    });
    const actions = await story();
    await userEvent.click(await within(actions).findByRole("button", { name: "Claim a character" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: /^That was me/ }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("write your perspective in this arc first");
  });

  it("offers no claim on a perspective that names no characters", async () => {
    await openStory("p-heron", HERON);
    const actions = await story();
    await within(actions).findByRole("button", { name: /^Heart/ });
    expect(within(actions).queryByRole("button", { name: "Claim a character" })).toBeNull();
  });

  it("says who the viewer is in a story instead of offering a second claim", async () => {
    const mine = [
      { id: "c-1", position: 1, label: "The barista in the green apron", myClaim: "CLAIM_STATUS_PENDING", hasPendingClaim: true },
      { id: "c-2", position: 2, label: "Someone by the window" },
    ];
    await openStory("p-heron", HERON, { over: { characters: mine } });
    const actions = await story();
    expect(await within(actions).findByText(/waiting for heron to accept/)).toBeInTheDocument();
    expect(within(actions).queryByRole("button", { name: "Claim a character" })).toBeNull();
  });

  it("tags a character with who it turned out to be", async () => {
    const tagged = [{ id: "c-1", position: 1, label: "A man with a red umbrella", hasAcceptedClaim: true, claimedBy: { userId: "u-otter", username: "otter" } }];
    await openStory("p-heron", HERON, { over: { characters: tagged } });
    const cast = await screen.findByRole("list", { name: "Cast" });
    expect(within(cast).getByRole("link", { name: "otter" })).toHaveAttribute("href", "/users/otter");
  });

  it("is not offered on a REMOVED perspective", async () => {
    await openStory("p-removed", user("u-gone", "voted_out"), { over: { loreStatus: "LORE_STATUS_REMOVED", loreId: "", characters } });
    const actions = await story();
    await within(actions).findByRole("button", { name: /^Heart/ });
    expect(within(actions).queryByRole("button", { name: "Claim a character" })).toBeNull();
  });
});

describe("claim from a \"That was me\" link", () => {
  const characters = [{ id: "c-1", position: 1, label: "The barista in the green apron", hasAcceptedClaim: false, hasPendingClaim: false }];

  it("opens the claim dialog on arrival", async () => {
    const store: Store = {};
    store["p-heron"] = perspective("p-heron", { author: HERON, loreId: "lore-canon", characters }).perspective;
    await renderApp(storyRoute, "/perspectives/p-heron?claim=1", {
      signedIn: true,
      routes: {
        "ArcService/GetArc": () => ({ body: dayArcResponse() }),
        "LoreService/GetLore": () => ({ body: { lore: lore("lore-canon", "CANON", []) } }),
        "PerspectiveService/Get": getFrom(store),
      },
    });
    const dialog = await screen.findByRole("dialog", { name: "Claim a character" });
    expect(within(dialog).getByRole("listitem", { name: "The barista in the green apron" })).toBeInTheDocument();
  });

  it("picks out the character the link quoted, starts focus on it, and drops the claim from the address", async () => {
    const two = [
      { id: "c-0", position: 1, label: "The man at the counter", hasAcceptedClaim: false, hasPendingClaim: false },
      { ...characters[0]!, position: 2 },
    ];
    const store: Store = {};
    store["p-heron"] = perspective("p-heron", { author: HERON, loreId: "lore-canon", characters: two }).perspective;
    const { router } = await renderApp(storyRoute, "/perspectives/p-heron?part=1&claim=c-1", {
      signedIn: true,
      routes: {
        "ArcService/GetArc": () => ({ body: dayArcResponse() }),
        "LoreService/GetLore": () => ({ body: { lore: lore("lore-canon", "CANON", []) } }),
        "PerspectiveService/Get": getFrom(store),
      },
    });
    const dialog = await screen.findByRole("dialog", { name: "Claim a character" });
    const picked = within(dialog).getByRole("listitem", { name: "The barista in the green apron" });
    expect(picked).toHaveAttribute("aria-current", "true");
    expect(within(dialog).getByRole("listitem", { name: "The man at the counter" })).not.toHaveAttribute("aria-current");
    await waitFor(() => expect(within(picked).getByRole("button", { name: "That was me: The barista in the green apron" })).toHaveFocus());
    // A reload must not open the dialog again; the rest of the address stays.
    await waitFor(() => expect(router.state.location.search).toBe("?part=1"));
  });

  it("tells a signed-out reader why, with sign-in and sign-up that come back to claim that character", async () => {
    const store: Store = {};
    store["p-heron"] = perspective("p-heron", { author: HERON, loreId: "lore-canon", characters }).perspective;
    await renderApp(storyRoute, "/perspectives/p-heron?claim=c-1", {
      routes: { "ArcService/GetArc": () => ({ body: dayArcResponse() }), "PerspectiveService/Get": getFrom(store) },
    });
    const actions = await story();
    expect(await within(actions).findByText(/^To say this was you, write your side of this day first\./)).toBeInTheDocument();
    expect(within(actions).getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login?next=%2Fperspectives%2Fp-heron%3Fclaim%3Dc-1");
    expect(within(actions).getByRole("link", { name: "Create an account" })).toHaveAttribute(
      "href",
      "/signup?next=%2Fperspectives%2Fp-heron%3Fclaim%3Dc-1",
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByTestId("login")).toBeNull();
  });
});

describe("remove vote", () => {
  it("a member of a 4-member lore votes to remove another member's perspective after confirming", async () => {
    const app = await openStory("p-otter", OTTER, {
      routes: { "VoteService/VoteRemove": () => ({ body: { removeVotes: 1, eligibleVoters: 3, removed: false } }) },
    });
    const actions = await story();
    await userEvent.click(await within(actions).findByRole("button", { name: "Vote to remove" }));
    const dialog = await screen.findByRole("dialog", { name: "Vote to remove otter's perspective?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Vote to remove" }));
    expect(await within(actions).findByRole("status")).toHaveTextContent("1 of 3 members have voted to remove");
    expect(bodiesFor(app.api, "VoteService/VoteRemove")).toEqual([{ perspectiveId: "p-otter" }]);
  });

  it("reports when the vote reached a majority", async () => {
    await openStory("p-otter", OTTER, {
      routes: { "VoteService/VoteRemove": () => ({ body: { removeVotes: 2, eligibleVoters: 3, removed: true } }) },
    });
    const actions = await story();
    await userEvent.click(await within(actions).findByRole("button", { name: "Vote to remove" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Vote to remove" }));
    expect(await within(actions).findByRole("status")).toHaveTextContent("removed from the lore");
  });

  it("is not offered on a story in another lore", async () => {
    await openStory("p-moth", MOTH, { over: { loreId: "lore-former" } });
    const actions = await story();
    await within(actions).findByRole("button", { name: /^Heart/ });
    expect(within(actions).queryByRole("button", { name: "Vote to remove" })).toBeNull();
  });

  it("is not offered when the lore has fewer than 4 members", async () => {
    await openStory("p-otter", OTTER, { loreMembers: 3 });
    const actions = await story();
    await within(actions).findByRole("button", { name: /^Heart/ });
    expect(within(actions).queryByRole("button", { name: "Vote to remove" })).toBeNull();
  });
});

describe("report", () => {
  it("AC-040 OT-043: the report form offers the real-name-or-handle reason and sends it for the perspective", async () => {
    const app = await openStory("p-otter", OTTER, { routes: { "ModerationService/Report": () => ({ body: { reportId: "r-9" } }) } });
    const actions = await story();
    await userEvent.click(await within(actions).findByRole("button", { name: /^Report:/ }));
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
    await openStory("p-otter", OTTER, {
      routes: { "ModerationService/Report": () => connectError("resource_exhausted", "too many reports, try later") },
    });
    const actions = await story();
    await userEvent.click(await within(actions).findByRole("button", { name: /^Report:/ }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("combobox", { name: "Reason" }));
    await userEvent.click(await screen.findByRole("option", { name: "Spam" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Send report" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("too many reports, try later");
  });
});

describe("save", () => {
  it("saves a story to read later and unsaves it, pressed state and words following the server", async () => {
    let isSaved = false;
    const app = await openStory("p-otter", OTTER, {
      routes: {
        "PerspectiveService/Get": () => ({
          body: { perspective: { ...perspective("p-otter", { author: OTTER, loreId: "lore-canon" }).perspective, saved: isSaved } },
        }),
        "PerspectiveService/SavePerspective": () => {
          isSaved = currentBody().on === true;
          return { body: { saved: isSaved } };
        },
      },
    });
    const actions = await story();
    const save = await within(actions).findByRole("button", { name: "Save otter's perspective", pressed: false });
    expect(save).toHaveTextContent("Save");

    await userEvent.click(save);
    const saved = await within(actions).findByRole("button", { name: "Saved otter's perspective", pressed: true });
    expect(saved).toHaveTextContent("Saved");

    await userEvent.click(saved);
    await within(actions).findByRole("button", { name: "Save otter's perspective", pressed: false });
    expect(bodiesFor(app.api, "PerspectiveService/SavePerspective")).toEqual([{ perspectiveId: "p-otter", on: true }, { perspectiveId: "p-otter" }]);
  });

  it("shows a story the reader saved earlier as Saved", async () => {
    await openStory("p-otter", OTTER, { over: { saved: true } });
    const actions = await story();
    expect(await within(actions).findByRole("button", { name: "Saved otter's perspective" })).toHaveAttribute("aria-pressed", "true");
  });

  it("says so in place when the story can no longer be saved", async () => {
    await openStory("p-otter", OTTER, {
      routes: { "PerspectiveService/SavePerspective": () => connectError("not_found", "perspective not found") },
    });
    const actions = await story();
    await userEvent.click(await within(actions).findByRole("button", { name: "Save otter's perspective" }));
    expect(await within(actions).findByRole("alert")).toHaveTextContent("perspective not found");
    expect(within(actions).getByRole("button", { name: "Save otter's perspective" })).toHaveAttribute("aria-pressed", "false");
  });
});

describe("the arc page's reads", () => {
  it("draws every row, deleted ones included, from GetArc alone, with no read per story", async () => {
    const arc = dayArcResponse();
    arc.tales = [summary({ id: "p-del", loreStatus: "LORE_STATUS_SOLO", deleted: true, excerpt: "" })];
    const app = await openSignedIn({}, {}, arc);
    await screen.findByText("This perspective was deleted.");
    await within(await card("Canon", "otter")).findByRole("button", { name: /^Upvote/, pressed: false });
    expect(bodiesFor(app.api, "PerspectiveService/Get")).toEqual([]);
  });

  it("shows the viewer's own upvotes from the listing", async () => {
    const arc = dayArcResponse();
    arc.canon.perspectives = arc.canon.perspectives.map((p) => (p.id === "p-otter" ? { ...p, voted: true, upvotes: 3 } : p));
    await openSignedIn({}, {}, arc);
    const otter = await card("Canon", "otter");
    expect(within(otter).getByRole("button", { name: /^Upvote/, pressed: true })).toHaveAttribute("aria-pressed", "true");
    expect(otter).toHaveTextContent("3 upvotes");
  });
});
