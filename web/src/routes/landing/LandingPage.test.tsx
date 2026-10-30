import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { connectError, sessionReply } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const illinois = { id: "st-il", kind: "PLACE_KIND_STATE", name: "Illinois" };
const springfield = { id: "ci-spr", kind: "PLACE_KIND_CITY", name: "Springfield" };

const results = {
  places: [
    { ...springfield, parentId: "st-il", ancestors: [illinois] },
    {
      id: "ml-wh",
      kind: "PLACE_KIND_MALL",
      name: "White Oaks Mall",
      parentId: "ci-spr",
      ancestors: [illinois, springfield],
    },
  ],
};

const states = { places: [{ ...illinois }, { id: "st-oh", kind: "PLACE_KIND_STATE", name: "Ohio" }] };

const storeRef = (id: string, name: string) => ({ id, kind: "PLACE_KIND_STORE", name });
const epicArc = (arcId: string, place: ReturnType<typeof storeRef>, title: string, over: Record<string, unknown> = {}) => ({
  arcId,
  title,
  place,
  localDate: "2026-10-03",
  canonLoreId: "",
  memberCount: 0,
  perspectiveCount: 2,
  city: "Springfield",
  excerpt: `Opening of ${title}`,
  ...over,
});
// The most remembered days anywhere, best first; Corner Books appears twice.
const everywhere = {
  arcs: [
    epicArc("a-1", storeRef("pl-books", "Corner Books"), "Rain on the windows", { canonLoreId: "lore-1", perspectiveCount: 5 }),
    epicArc("a-2", storeRef("pl-books", "Corner Books"), "A quieter day"),
    epicArc("a-3", storeRef("pl-diner", "Night Owl Diner"), "After the show"),
  ],
};

function landing(
  search: (body: Record<string, unknown>) => { status?: number; body: unknown } = () => ({ body: results }),
  epic: () => { status?: number; body: unknown } = () => ({ body: { arcs: [] } }),
) {
  return renderApp("/", {
    "PlaceService/SearchPlaces": search,
    "PlaceService/ListChildren": () => ({ body: states }),
    "PlaceService/GetEpic": epic,
  });
}

describe("landing page (AC-006, OT-036)", () => {
  it("renders a place search box and no feed for a signed-out reader", async () => {
    const { sent } = landing();
    expect(await screen.findByRole("searchbox", { name: "Search places" })).toBeInTheDocument();
    expect(screen.getByRole("search")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sign in" })).toBeInTheDocument();
    // No feed of posts: the only listings are of places.
    await screen.findByRole("link", { name: "Ohio" });
    const fetched = sent.map((r) => r.procedure).filter((p) => !p.startsWith("AuthService/"));
    expect(fetched.toSorted()).toEqual(["PlaceService/GetEpic", "PlaceService/ListChildren"]);
    expect(screen.queryByRole("article")).not.toBeInTheDocument();
    // With no stories anywhere, the places section is left out.
    expect(screen.queryByRole("region", { name: "Where people are writing" })).not.toBeInTheDocument();
  });

  it("shows where people are writing, one card per place, each opening its place page", async () => {
    const { sent } = landing(undefined, () => ({ body: everywhere }));
    const section = await screen.findByRole("region", { name: "Where people are writing" });
    const cards = within(section).getAllByRole("link");
    expect(cards.map((c) => c.getAttribute("href"))).toEqual(["/places/pl-books", "/places/pl-diner"]);
    expect(cards[0]).toHaveTextContent("Corner Books, Springfield");
    expect(cards[0]).toHaveTextContent("Rain on the windows");
    expect(cards[0]).toHaveTextContent("“Opening of Rain on the windows”");
    expect(cards[0]).toHaveTextContent("Canon");
    expect(cards[0]).toHaveTextContent("5 stories");
    expect(cards[1]).not.toHaveTextContent("Canon");
    expect(sent.find((r) => r.procedure === "PlaceService/GetEpic")?.body).toEqual({ tab: "EPIC_TAB_RANKED", pageSize: 30 });
  });

  it("lists a signed-in author's drafts and published perspectives with ways to open, read and edit them", async () => {
    const mine = {
      perspectives: [
        {
          id: "d-1",
          status: "PERSPECTIVE_STATUS_DRAFT",
          titleIfNewArc: "Frost on the fountain",
          place: { id: "ml-wh", kind: "PLACE_KIND_MALL", name: "White Oaks Mall" },
          localDate: "2026-01-14",
          parts: [{ position: 1, body: "The doors opened at nine." }],
        },
        {
          id: "p-2",
          arcId: "arc-2",
          arcTitle: "The night the lights went out",
          status: "PERSPECTIVE_STATUS_PUBLISHED",
          loreStatus: "LORE_STATUS_SOLO",
          place: { id: "ci-spr", kind: "PLACE_KIND_CITY", name: "Springfield" },
          localDate: "2026-02-02",
          parts: [{ position: 1, body: "It went dark." }],
        },
      ],
    };
    const { calls } = renderApp(
      "/",
      {
        "PlaceService/ListChildren": () => ({ body: states }),
        "PerspectiveService/ListMine": () => ({ body: mine }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    const yours = await screen.findByRole("region", { name: "Your perspectives" });
    const draft = await within(yours).findByRole("link", { name: /Frost on the fountain/ });
    expect(draft).toHaveAttribute("href", "/compose?perspectiveId=d-1");
    expect(within(yours).getByText(/Draft/)).toBeInTheDocument();
    expect(within(yours).getByRole("link", { name: /^The night the lights went out/ })).toHaveAttribute("href", "/perspectives/p-2");
    expect(within(yours).getByRole("link", { name: "Edit The night the lights went out" })).toHaveAttribute(
      "href",
      "/compose?perspectiveId=p-2",
    );
    expect(calls("PerspectiveService/ListMine")).toHaveLength(1);
  });

  it("returns matching places with their parent chain as typed", async () => {
    const user = userEvent.setup();
    const { calls } = landing();
    await user.type(await screen.findByRole("searchbox", { name: "Search places" }), "Spring");
    const list = await screen.findByRole("list", { name: "Matching places" });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(within(items[0]!).getByRole("link", { name: /Springfield/ })).toHaveAttribute("href", "/places/ci-spr");
    expect(items[0]).toHaveTextContent("City in Illinois");
    expect(within(items[1]!).getByRole("link", { name: /White Oaks Mall/ })).toHaveAttribute("href", "/places/ml-wh");
    expect(items[1]).toHaveTextContent("Mall in Springfield, Illinois");
    await waitFor(() => expect(calls("PlaceService/SearchPlaces").at(-1)).toEqual({ query: "Spring" }));
  });

  it("does not search for a blank query", async () => {
    const user = userEvent.setup();
    const { calls } = landing();
    await user.type(await screen.findByRole("searchbox", { name: "Search places" }), "   ");
    await new Promise((r) => setTimeout(r, 400));
    expect(calls("PlaceService/SearchPlaces")).toEqual([]);
    expect(screen.queryByRole("list", { name: "Matching places" })).not.toBeInTheDocument();
  });

  it("says so when nothing matches", async () => {
    const user = userEvent.setup();
    landing(() => ({ body: { places: [] } }));
    await user.type(await screen.findByRole("searchbox", { name: "Search places" }), "Atlantis");
    expect(await screen.findByText("No places match “Atlantis”.")).toBeInTheDocument();
  });

  it("shows an error when the search fails", async () => {
    const user = userEvent.setup();
    landing(() => connectError("unavailable"));
    await user.type(await screen.findByRole("searchbox", { name: "Search places" }), "Spring");
    expect(await screen.findByRole("alert")).toHaveTextContent("Search is unavailable");
  });

  it("keeps the query in the address so back returns to the results", async () => {
    const user = userEvent.setup();
    const { router } = landing();
    await user.type(await screen.findByRole("searchbox", { name: "Search places" }), "Spring");
    await waitFor(() => expect(router.state.location.search).toBe("?q=Spring"));
    await user.click(await screen.findByRole("link", { name: /White Oaks Mall/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/places/ml-wh"));
    await router.navigate(-1);
    expect(await screen.findByRole("searchbox", { name: "Search places" })).toHaveValue("Spring");
    expect(await screen.findByRole("list", { name: "Matching places" })).toBeInTheDocument();
  });

  it("lets a reader drill down from the states", async () => {
    landing();
    const browse = await screen.findByRole("navigation", { name: "Browse by state" });
    expect(within(browse).getByRole("link", { name: "Illinois" })).toHaveAttribute("href", "/places/st-il");
  });

  it("explains Perspectiv to a newcomer with a labelled example, not a feed", async () => {
    landing();
    expect(await screen.findByRole("heading", { level: 1, name: "Same place. Same day. Every side of the story." })).toBeInTheDocument();
    const example = screen.getByRole("figure", { name: /Example/ });
    expect(within(example).getByText(/The power went out halfway through open mic/)).toBeInTheDocument();
    expect(within(example).getByText(/I don’t sing/)).toBeInTheDocument();
    expect(within(example).getByText(/that was me/)).toBeInTheDocument();
    const how = screen.getByRole("list", { name: "How Perspectiv works" });
    expect(within(how).getAllByRole("listitem")).toHaveLength(4);
    expect(screen.getByText("Anyone can read. Sign up to write your side.")).toBeInTheDocument();
  });

  it("welcomes a returning member back with what is waiting on them, their writing, then the search", async () => {
    const mine = {
      perspectives: [
        {
          id: "p-2",
          arcId: "arc-2",
          arcTitle: "The night the lights went out",
          status: "PERSPECTIVE_STATUS_PUBLISHED",
          place: { id: "ci-spr", kind: "PLACE_KIND_CITY", name: "Springfield" },
          localDate: "2026-02-02",
        },
      ],
    };
    const { calls } = renderApp(
      "/",
      {
        "PlaceService/ListChildren": () => ({ body: states }),
        "PerspectiveService/ListMine": () => ({ body: mine }),
        "NotifyService/ListInbox": () => ({ body: { unreadCount: 2 } }),
        "LoreService/ListClaims": () => ({ body: { claims: [{ id: "cl-1", status: "CLAIM_STATUS_PENDING" }] } }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    expect(await screen.findByRole("heading", { level: 1, name: "Welcome back, wren" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Same place. Same day. Every side of the story." })).toBeNull();
    expect(screen.queryByRole("figure", { name: /Example/ })).toBeNull();
    const waiting = await screen.findByRole("list", { name: "Waiting on you" });
    await waitFor(() => expect(waiting).toHaveTextContent("2 unread notifications"));
    expect(within(waiting).getByRole("link", { name: "Open your inbox" })).toHaveAttribute("href", "/inbox");
    await waitFor(() => expect(waiting).toHaveTextContent("1 request waiting on you"));
    expect(within(waiting).getByRole("link", { name: "Review requests" })).toHaveAttribute("href", "/claims");
    expect(calls("LoreService/ListClaims")[0]).toMatchObject({ direction: "CLAIM_DIRECTION_INCOMING", status: "CLAIM_STATUS_PENDING" });
    // Their writing comes before the search.
    const yours = await screen.findByRole("region", { name: "Your perspectives" });
    const search = screen.getByRole("search");
    expect(yours.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("tells a returning member when nothing is waiting on them", async () => {
    renderApp(
      "/",
      {
        "PlaceService/ListChildren": () => ({ body: states }),
        "PerspectiveService/ListMine": () => ({ body: { perspectives: [] } }),
        "NotifyService/ListInbox": () => ({ body: { unreadCount: 0 } }),
        "LoreService/ListClaims": () => ({ body: { claims: [] } }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    const waiting = await screen.findByRole("list", { name: "Waiting on you" });
    expect(waiting).toHaveTextContent("Nothing is waiting on you.");
    expect(screen.getByRole("searchbox", { name: "Search places" })).toBeInTheDocument();
  });

  it("puts followed places with new stories first and counts them", async () => {
    renderApp(
      "/",
      {
        "PlaceService/ListChildren": () => ({ body: states }),
        "PerspectiveService/ListMine": () => ({ body: { perspectives: [] } }),
        "NotifyService/ListInbox": () => ({ body: { unreadCount: 0 } }),
        "LoreService/ListClaims": () => ({ body: { claims: [] } }),
        "PlaceService/ListFollowing": () => ({
          body: {
            places: [
              { ...springfield, newThisWeek: 3 },
              { id: "pl-books", kind: "PLACE_KIND_STORE", name: "Corner Books", newThisWeek: 1 },
              { id: "ci-chi", kind: "PLACE_KIND_CITY", name: "Chicago" },
            ],
          },
        }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    const nav = await screen.findByRole("navigation", { name: "Places you follow" });
    expect(nav).toHaveTextContent("4 new stories this week");
    const links = within(nav).getAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual(["Springfield· 3 new", "Corner Books· 1 new", "Chicago"]);
    expect(links[0]).toHaveAttribute("href", "/places/ci-spr");
  });

  it("offers to add a place that search does not find", async () => {
    const user = userEvent.setup();
    landing(() => ({ body: { places: [] } }));
    await user.type(await screen.findByRole("searchbox", { name: "Search places" }), "Dollar General");
    const add = await screen.findByRole("link", { name: "Not here? Add “Dollar General” as a new place" });
    expect(add).toHaveAttribute("href", "/compose?add=Dollar%20General");
  });
});
