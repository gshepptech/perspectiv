import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

import { connectError, sessionReply } from "@/lib/fakeApi.testutil";
import { renderApp, type BodyHandler } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

// Radix Select measures and captures the pointer; jsdom has neither.
beforeAll(() => {
  Element.prototype.hasPointerCapture ??= () => false;
  Element.prototype.releasePointerCapture ??= () => {};
  Element.prototype.scrollIntoView ??= () => {};
});

const illinois = { id: "st-il", kind: "PLACE_KIND_STATE", name: "Illinois" };
const springfield = {
  id: "ci-spr",
  kind: "PLACE_KIND_CITY",
  name: "Springfield",
  parentId: "st-il",
  timezone: "America/Chicago",
  ancestors: [illinois],
};
const store = { id: "so-bk", kind: "PLACE_KIND_STORE", name: "Prairie Books" };

const rankedArcs = [
  {
    arcId: "arc-canon",
    title: "The night the fountain froze",
    place: store,
    localDate: "2026-01-14",
    canonLoreId: "lore-1",
    canonScore: 9.5,
    memberCount: 5,
    perspectiveCount: 7,
  },
  {
    arcId: "arc-plain",
    title: "Parade day",
    place: { id: "ci-spr", kind: "PLACE_KIND_CITY", name: "Springfield" },
    localDate: "2026-07-04",
    hour: 14,
    memberCount: 9,
    perspectiveCount: 12,
  },
];

const newestArcs = [{ ...rankedArcs[1], arcId: "arc-new", title: "Snow on the square" }];

const tales = [
  {
    perspectiveId: "p-solo",
    arcId: "arc-plain",
    arcTitle: "Parade day",
    place: { id: "ci-spr", kind: "PLACE_KIND_CITY", name: "Springfield" },
    localDate: "2026-07-04",
    author: { userId: "u-1", username: "wren" },
    genre: "GENRE_MYSTERY",
    loreStatus: "LORE_STATUS_SOLO",
    publishedAt: "2026-07-04T20:00:00Z",
    excerpt: "Nobody else saw the float catch fire.",
  },
  {
    perspectiveId: "p-removed",
    arcId: "arc-plain",
    arcTitle: "Parade day",
    place: store,
    localDate: "2026-07-04",
    author: { userId: "u-2", username: "fen" },
    genre: "GENRE_DRAMA",
    loreStatus: "LORE_STATUS_REMOVED",
    publishedAt: "2026-07-03T20:00:00Z",
    excerpt: "I was the drummer.",
  },
];

const epic: BodyHandler = (body) => {
  const tab = body.tab;
  if (tab === "EPIC_TAB_TALES") return { body: { place: springfield, tales } };
  if (tab === "EPIC_TAB_NEWEST") return { body: { place: springfield, arcs: newestArcs } };
  return { body: { place: springfield, arcs: rankedArcs } };
};

function placePage(path = "/places/ci-spr", handlers: Record<string, BodyHandler> = {}, opts: Parameters<typeof renderApp>[2] = {}) {
  return renderApp(
    path,
    {
      "PlaceService/GetPlace": () => ({ body: { place: springfield } }),
      "PlaceService/GetEpic": epic,
      "PlaceService/ListChildren": (body) =>
        body.withStories ? { body: { places: [] } } : { body: { places: [{ ...store, parentId: "ci-spr" }] } },
      ...handlers,
    },
    opts,
  );
}

describe("following a place", () => {
  it("a signed-in reader follows and unfollows, and the place says so", async () => {
    const user = userEvent.setup();
    let following = false;
    const { calls } = placePage(
      "/places/ci-spr",
      {
        "PlaceService/GetPlace": () => ({ body: { place: { ...springfield, following } } }),
        "PlaceService/FollowPlace": (body) => {
          following = body.on === true;
          return { body: { following } };
        },
      },
      { refresh: () => sessionReply({ token: "tok" }) },
    );
    const follow = await screen.findByRole("button", { name: "Follow" });
    expect(follow).toHaveAttribute("aria-pressed", "false");
    await user.click(follow);
    expect(await screen.findByRole("button", { name: "Following" })).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "Following" }));
    expect(await screen.findByRole("button", { name: "Follow" })).toHaveAttribute("aria-pressed", "false");
    expect(calls("PlaceService/FollowPlace")).toEqual([{ placeId: "ci-spr", on: true }, { placeId: "ci-spr" }]);
  });

  it("sends a signed-out reader to sign in without calling the server", async () => {
    const user = userEvent.setup();
    const { router, calls } = placePage();
    await user.click(await screen.findByRole("button", { name: "Follow" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(calls("PlaceService/FollowPlace")).toEqual([]);
  });
});

describe("place page (AC-008, FR-066, FR-032)", () => {
  it("shows the place with its parent chain and Ranked, Newest and Tales tabs, Ranked first", async () => {
    placePage();
    expect(await screen.findByRole("heading", { level: 1, name: "Springfield" })).toBeInTheDocument();
    const crumbs = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(crumbs).getByRole("link", { name: "Illinois" })).toHaveAttribute("href", "/places/st-il");
    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((t) => t.textContent)).toEqual(["Ranked", "Newest", "Tales"]);
    expect(screen.getByRole("tab", { name: "Ranked" })).toHaveAttribute("aria-selected", "true");
  });

  it("lists ranked arcs in the server's order, including arcs at descendant places", async () => {
    const { calls } = placePage();
    const list = await screen.findByRole("list", { name: "Arcs" });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(within(items[0]!).getByRole("link", { name: "The night the fountain froze" })).toHaveAttribute("href", "/arcs/arc-canon");
    expect(items[0]).toHaveTextContent("Prairie Books");
    expect(items[0]).toHaveTextContent("Canon");
    expect(items[0]).toHaveTextContent("5 in lore");
    expect(items[0]).toHaveTextContent("January 14, 2026");
    expect(items[1]).toHaveTextContent("July 4, 2026, 14:00");
    expect(items[1]).not.toHaveTextContent("Canon");
    expect(calls("PlaceService/GetEpic")[0]).toEqual({ placeId: "ci-spr", tab: "EPIC_TAB_RANKED" });
  });

  it("switches to Newest from the keyboard and keeps the tab in the address", async () => {
    const user = userEvent.setup();
    const { router, calls } = placePage();
    await screen.findByRole("list", { name: "Arcs" });
    screen.getByRole("tab", { name: "Ranked" }).focus();
    await user.keyboard("{ArrowRight}");
    expect(await screen.findByRole("link", { name: "Snow on the square" })).toHaveAttribute("href", "/arcs/arc-new");
    expect(router.state.location.search).toBe("?tab=newest");
    expect(calls("PlaceService/GetEpic").at(-1)).toEqual({ placeId: "ci-spr", tab: "EPIC_TAB_NEWEST" });
  });

  it("lists tales newest first with author, excerpt and status, linking to the reader", async () => {
    const user = userEvent.setup();
    const { calls } = placePage();
    await user.click(await screen.findByRole("tab", { name: "Tales" }));
    const list = await screen.findByRole("list", { name: "Tales" });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(within(items[0]!).getByRole("link", { name: /Nobody else saw the float catch fire/ })).toHaveAttribute(
      "href",
      "/perspectives/p-solo",
    );
    expect(items[0]).toHaveTextContent("wren");
    expect(items[0]).toHaveTextContent("Tale");
    expect(items[0]).toHaveTextContent("Mystery");
    expect(items[1]).toHaveTextContent("voted out");
    expect(items[1]).toHaveTextContent("Prairie Books");
    expect(calls("PlaceService/GetEpic").at(-1)).toEqual({ placeId: "ci-spr", tab: "EPIC_TAB_TALES" });
  });

  it("opens on the tab named in the address", async () => {
    placePage("/places/ci-spr?tab=tales");
    expect(await screen.findByRole("list", { name: "Tales" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Tales" })).toHaveAttribute("aria-selected", "true");
  });

  it("loads the next page with the server's page token and appends it", async () => {
    const user = userEvent.setup();
    const { calls } = placePage("/places/ci-spr", {
      "PlaceService/GetEpic": (body) =>
        body.pageToken === "t2"
          ? { body: { place: springfield, arcs: [{ ...rankedArcs[1], arcId: "arc-3", title: "Third" }] } }
          : { body: { place: springfield, arcs: rankedArcs, nextPageToken: "t2" } },
    });
    await user.click(await screen.findByRole("button", { name: "Show more" }));
    expect(await screen.findByRole("link", { name: "Third" })).toBeInTheDocument();
    expect(within(screen.getByRole("list", { name: "Arcs" })).getAllByRole("listitem")).toHaveLength(3);
    expect(calls("PlaceService/GetEpic").at(-1)).toEqual({ placeId: "ci-spr", tab: "EPIC_TAB_RANKED", pageToken: "t2" });
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
  });

  it("filters by genre", async () => {
    const { router, calls } = placePage();
    const genre = await screen.findByRole("combobox", { name: "Genre" });
    genre.focus();
    fireEvent.keyDown(genre, { key: "Enter" });
    fireEvent.click(await screen.findByRole("option", { name: "Mystery" }));
    await waitFor(() => expect(calls("PlaceService/GetEpic").at(-1)).toEqual({ placeId: "ci-spr", tab: "EPIC_TAB_RANKED", genre: "GENRE_MYSTERY" }));
    expect(router.state.location.search).toBe("?genre=mystery");
  });

  it("invites the first story when the place has no arcs yet", async () => {
    placePage("/places/ci-spr", { "PlaceService/GetEpic": () => ({ body: { place: springfield } }) });
    expect(await screen.findByText("No one has written about Springfield yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Write the first story" })).toHaveAttribute("href", "/compose?placeId=ci-spr");
  });

  it("drills down to the places inside it", async () => {
    const user = userEvent.setup();
    placePage();
    await user.click(await screen.findByRole("button", { name: "Browse all places in Springfield" }));
    const inside = await screen.findByRole("navigation", { name: "Places in Springfield" });
    expect(within(inside).getByRole("link", { name: "Prairie Books" })).toHaveAttribute("href", "/places/so-bk");
  });

  it("lists the places with stories first, then every place, and filters within the place", async () => {
    const user = userEvent.setup();
    const diner = { id: "so-dn", kind: "PLACE_KIND_STORE", name: "Corner Diner", parentId: "ci-spr" };
    const { calls } = placePage("/places/ci-spr", {
      "PlaceService/ListChildren": (body) =>
        body.withStories ? { body: { places: [diner] } } : { body: { places: [{ ...store, parentId: "ci-spr" }, diner] } },
      "PlaceService/SearchPlaces": () => ({ body: { places: [{ ...store, parentId: "ci-spr" }] } }),
    });
    const storied = await screen.findByRole("list", { name: "Places with stories" });
    expect(within(storied).getByRole("link", { name: "Corner Diner" })).toHaveAttribute("href", "/places/so-dn");
    expect(screen.queryByRole("list", { name: "All places in Springfield" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Browse all places in Springfield" }));
    expect(within(await screen.findByRole("list", { name: "All places in Springfield" })).getAllByRole("link")).toHaveLength(2);
    await user.type(screen.getByRole("searchbox", { name: "Find a place in Springfield" }), "prairie");
    const matches = await screen.findByRole("list", { name: "Matching places" });
    expect(await within(matches).findByRole("link", { name: "Prairie Books" })).toBeInTheDocument();
    expect(calls("PlaceService/SearchPlaces").at(-1)).toEqual({ query: "prairie", parentId: "ci-spr" });
    expect(calls("PlaceService/ListChildren")).toContainEqual({ parentId: "ci-spr", withStories: true, pageSize: 20 });
  });

  it("narrows the places inside to one kind with a chip", async () => {
    const user = userEvent.setup();
    const { calls } = placePage();
    const kinds = await screen.findByRole("group", { name: "Kinds of place" });
    await user.click(within(kinds).getByRole("button", { name: "Nightlife" }));
    expect(within(kinds).getByRole("button", { name: "Nightlife" })).toHaveAttribute("aria-pressed", "true");
    await waitFor(() =>
      expect(calls("PlaceService/ListChildren")).toContainEqual({ parentId: "ci-spr", category: "PLACE_CATEGORY_NIGHTLIFE", pageSize: 100 }),
    );
  });

  it("offers writing here, which the shell sends a signed-out reader to sign in for", async () => {
    placePage();
    expect(await screen.findByRole("link", { name: "Write about this place" })).toHaveAttribute("href", "/compose?placeId=ci-spr");
  });

  it("offers adding a place inside a city, with the city already chosen", async () => {
    placePage();
    expect(await screen.findByRole("link", { name: "Add a place here" })).toHaveAttribute("href", "/compose?in=ci-spr");
  });

  it("shows not found for an unknown place", async () => {
    placePage("/places/nope", {
      "PlaceService/GetPlace": () => connectError("not_found"),
      "PlaceService/GetEpic": () => connectError("not_found"),
    });
    expect(await screen.findByRole("heading", { name: "Place not found" })).toBeInTheDocument();
  });
});
