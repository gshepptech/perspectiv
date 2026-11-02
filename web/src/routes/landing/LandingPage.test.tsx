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
  kinds: () => { status?: number; body: unknown } = () => ({ body: { categories: [] } }),
) {
  return renderApp("/", {
    "PlaceService/SearchPlaces": search,
    "PlaceService/ListChildren": () => ({ body: states }),
    "PlaceService/GetEpic": epic,
    "PlaceService/ListStoriedCategories": kinds,
  });
}

describe("landing page (AC-006, OT-036)", () => {
  it("renders a place search box and no feed for a signed-out reader", async () => {
    const { sent } = landing();
    expect(await screen.findByRole("combobox", { name: "Search places" })).toHaveAttribute("maxlength", "200");
    expect(screen.getByRole("search")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sign in" })).toBeInTheDocument();
    // No feed of posts: the only listings are of places.
    await screen.findByRole("link", { name: "Ohio" });
    const fetched = sent.map((r) => r.procedure).filter((p) => !p.startsWith("AuthService/"));
    expect(fetched.toSorted()).toEqual(["PlaceService/GetEpic", "PlaceService/ListChildren", "PlaceService/ListStoriedCategories"]);
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
    const draft = await within(yours).findByRole("link", { name: /^Frost on the fountain/ });
    expect(draft).toHaveAttribute("href", "/compose?perspectiveId=d-1");
    // A draft row ends with Continue where a published row has Edit.
    expect(within(yours).getByRole("link", { name: "Continue Frost on the fountain" })).toHaveAttribute(
      "href",
      "/compose?perspectiveId=d-1",
    );
    expect(within(yours).queryByRole("link", { name: "Continue The night the lights went out" })).toBeNull();
    expect(within(yours).getByText(/Draft/)).toBeInTheDocument();
    expect(within(yours).getByRole("link", { name: /^The night the lights went out/ })).toHaveAttribute("href", "/perspectives/p-2");
    expect(within(yours).getByRole("link", { name: "Edit The night the lights went out" })).toHaveAttribute(
      "href",
      "/compose?perspectiveId=p-2",
    );
    // One read of the list; the welcome asks separately whether anything is published.
    expect(calls("PerspectiveService/ListMine").filter((b) => b.status === undefined)).toHaveLength(1);
  });

  it("titles each draft by its day, else its first words, never a bare Untitled", async () => {
    const draft = (id: string, over: Record<string, unknown>) => ({
      id,
      status: "PERSPECTIVE_STATUS_DRAFT",
      place: { id: "ml-wh", kind: "PLACE_KIND_MALL", name: "White Oaks Mall" },
      localDate: "2026-01-14",
      ...over,
    });
    const mine = {
      perspectives: [
        // Joins a day someone else started: the server sends that day's title.
        draft("d-join", { arcTitle: "Frost on the fountain", parts: [{ position: 1, body: "I was by the doors." }] }),
        draft("d-words", { parts: [{ position: 1, body: "  The escalator stopped between floors and nobody moved for a minute." }] }),
        draft("d-short", { localDate: "2026-01-15", parts: [{ position: 1, body: "Snow again." }] }),
        draft("d-empty", { localDate: "2026-01-16", parts: [] }),
      ],
    };
    renderApp(
      "/",
      { "PlaceService/ListChildren": () => ({ body: states }), "PerspectiveService/ListMine": () => ({ body: mine }) },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    const yours = await screen.findByRole("region", { name: "Your perspectives" });
    const titles = await waitFor(() => {
      // Each row's title link; the rows' Continue links are not titles.
      const links = within(yours)
        .getAllByRole("link")
        .filter((l) => !l.textContent?.startsWith("Continue"));
      expect(links).toHaveLength(4);
      return links.map((l) => l.querySelector(".font-serif")?.textContent);
    });
    expect(titles).toEqual(["Frost on the fountain", "The escalator stopped between floors and…", "Snow again.", "Untitled draft"]);
    expect(yours).not.toHaveTextContent(/Untitled(?! draft)/);
  });

  it("marks a draft the writer's own story has superseded, and deletes it only when asked", async () => {
    const user = userEvent.setup();
    let mine = [
      {
        id: "d-dead",
        status: "PERSPECTIVE_STATUS_DRAFT",
        arcTitle: "The night the lights went out",
        supersededById: "p-2",
        place: { id: "ci-spr", kind: "PLACE_KIND_CITY", name: "Springfield" },
        localDate: "2026-02-02",
        parts: [{ position: 1, body: "It went dark." }],
      },
      {
        id: "d-live",
        status: "PERSPECTIVE_STATUS_DRAFT",
        titleIfNewArc: "Frost on the fountain",
        place: { id: "ml-wh", kind: "PLACE_KIND_MALL", name: "White Oaks Mall" },
        localDate: "2026-01-14",
        hour: 9,
      },
      {
        id: "p-2",
        arcId: "arc-2",
        arcTitle: "The night the lights went out",
        status: "PERSPECTIVE_STATUS_PUBLISHED",
        place: { id: "ci-spr", kind: "PLACE_KIND_CITY", name: "Springfield" },
        localDate: "2026-02-02",
      },
    ];
    const { calls } = renderApp(
      "/",
      {
        "PlaceService/ListChildren": () => ({ body: states }),
        "PerspectiveService/ListMine": () => ({ body: { perspectives: mine } }),
        "PerspectiveService/Delete": (body) => {
          mine = mine.filter((p) => p.id !== body.perspectiveId);
          return { body: {} };
        },
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    const yours = await screen.findByRole("region", { name: "Your perspectives" });
    const dead = (await within(yours).findByRole("link", { name: /You already published for this day/ })).closest("li")!;
    expect(dead).toHaveTextContent("Draft · You already published for this day");
    // Only the superseded draft offers Delete, and nothing is deleted unasked.
    expect(within(yours).getAllByRole("button", { name: /^Delete draft/ })).toHaveLength(1);
    expect(calls("PerspectiveService/Delete")).toEqual([]);
    expect(within(yours).getByRole("link", { name: /^Frost on the fountain/ })).not.toHaveTextContent("already published");
    // A draft that can never be published offers Delete, not Continue.
    expect(within(dead).queryByRole("link", { name: /^Continue/ })).toBeNull();

    await user.click(within(dead).getByRole("button", { name: "Delete draft The night the lights went out" }));
    await waitFor(() => expect(within(yours).queryByRole("link", { name: /already published/ })).toBeNull());
    expect(calls("PerspectiveService/Delete")).toEqual([{ perspectiveId: "d-dead" }]);
    expect(within(yours).getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByRole("heading", { name: "Your perspectives" })).toHaveFocus();
  });

  it("says an hour's draft was superseded for that hour", async () => {
    const mine = {
      perspectives: [
        {
          id: "d-dead",
          status: "PERSPECTIVE_STATUS_DRAFT",
          arcTitle: "Close of play",
          supersededById: "p-9",
          place: { id: "ci-spr", kind: "PLACE_KIND_CITY", name: "Springfield" },
          localDate: "2026-02-02",
          hour: 21,
        },
      ],
    };
    renderApp(
      "/",
      { "PlaceService/ListChildren": () => ({ body: states }), "PerspectiveService/ListMine": () => ({ body: mine }) },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    const yours = await screen.findByRole("region", { name: "Your perspectives" });
    expect(await within(yours).findByText("Draft · You already published for this hour")).toBeInTheDocument();
  });

  it("welcomes a member who has not published yet, asks them to write, and shows how a day works", async () => {
    const { calls } = renderApp(
      "/",
      {
        "PlaceService/ListChildren": () => ({ body: states }),
        "PerspectiveService/ListMine": () => ({ body: { perspectives: [] } }),
        "NotifyService/ListInbox": () => ({ body: { unreadCount: 0 } }),
        "LoreService/ListClaims": () => ({ body: { claims: [] } }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    expect(await screen.findByRole("link", { name: "Write about a place you were at" })).toHaveAttribute("href", "/compose");
    expect(screen.getByRole("heading", { level: 1, name: "Welcome, wren" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: /Welcome back/ })).toBeNull();
    expect(calls("PerspectiveService/ListMine")).toContainEqual({ status: "PERSPECTIVE_STATUS_PUBLISHED", pageSize: 1 });
    const how = screen.getByRole("region", { name: "How a day works" });
    expect(within(how).getAllByRole("listitem")).toHaveLength(4);
    expect(how).toHaveTextContent("Everyone who was there writes their own side.");
    // Point 3 says what a lore is.
    expect(within(how).getAllByRole("listitem")[2]).toHaveTextContent(
      "If someone else's story describes you, claim that character; your two stories link, and linked stories make a lore.",
    );
    // The explainer sits right under the call to action, so no link points at it.
    expect(within(how).getByRole("heading", { level: 2, name: "How a day works" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "How a day works" })).toBeNull();
  });

  it("does not ask a member who has published to start, and welcomes them back", async () => {
    renderApp(
      "/",
      {
        "PlaceService/ListChildren": () => ({ body: states }),
        "PerspectiveService/ListMine": (body) => ({
          body: {
            perspectives:
              body.status === "PERSPECTIVE_STATUS_PUBLISHED"
                ? [{ id: "p-2", arcId: "arc-2", arcTitle: "The night the lights went out", status: "PERSPECTIVE_STATUS_PUBLISHED" }]
                : [],
          },
        }),
        "NotifyService/ListInbox": () => ({ body: { unreadCount: 0 } }),
        "LoreService/ListClaims": () => ({ body: { claims: [] } }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    expect(await screen.findByRole("heading", { level: 1, name: "Welcome back, wren" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Write about a place you were at" })).toBeNull();
    expect(screen.queryByRole("region", { name: "How a day works" })).toBeNull();
  });

  it("returns matching places with their parent chain as typed", async () => {
    const user = userEvent.setup();
    const { calls } = landing();
    await user.type(await screen.findByRole("combobox", { name: "Search places" }), "Spring");
    const list = await screen.findByRole("listbox", { name: "Matching places" });
    const items = within(list).getAllByRole("option");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveAccessibleName(/^Springfield/);
    expect(items[0]).toHaveAttribute("href", "/places/ci-spr");
    expect(items[0]).toHaveTextContent("City in Illinois");
    expect(items[1]).toHaveAccessibleName(/^White Oaks Mall/);
    expect(items[1]).toHaveAttribute("href", "/places/ml-wh");
    expect(items[1]).toHaveTextContent("Mall in Springfield, Illinois");
    await waitFor(() => expect(calls("PlaceService/SearchPlaces").at(-1)).toEqual({ query: "Spring" }));
  });

  it("does not search for a blank query", async () => {
    const user = userEvent.setup();
    const { calls } = landing();
    await user.type(await screen.findByRole("combobox", { name: "Search places" }), "   ");
    await new Promise((r) => setTimeout(r, 400));
    expect(calls("PlaceService/SearchPlaces")).toEqual([]);
    expect(screen.queryByRole("listbox", { name: "Matching places" })).not.toBeInTheDocument();
  });

  it("says so when nothing matches", async () => {
    const user = userEvent.setup();
    landing(() => ({ body: { places: [] } }));
    await user.type(await screen.findByRole("combobox", { name: "Search places" }), "Atlantis");
    expect(await screen.findByText("No places match “Atlantis”.")).toBeInTheDocument();
  });

  it("says when more places match than it lists, and how to narrow them", async () => {
    const user = userEvent.setup();
    landing(() => ({ body: { ...results, more: true } }));
    await user.type(await screen.findByRole("combobox", { name: "Search places" }), "Spring");
    expect(await screen.findByText("Showing the first 2. Add a town or state to narrow it.")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Showing the first 2 places that match “Spring”. Add a town or state to narrow it.");
    expect(screen.getByRole("status")).not.toHaveTextContent("2 places match");
  });

  it("shows an error when the search fails", async () => {
    const user = userEvent.setup();
    landing(() => connectError("unavailable"));
    await user.type(await screen.findByRole("combobox", { name: "Search places" }), "Spring");
    expect(await screen.findByRole("alert")).toHaveTextContent("Search is unavailable");
  });

  it("keeps the query in the address so back returns to the results", async () => {
    const user = userEvent.setup();
    const { router } = landing();
    await user.type(await screen.findByRole("combobox", { name: "Search places" }), "Spring");
    await waitFor(() => expect(router.state.location.search).toBe("?q=Spring"));
    await user.click(await screen.findByRole("option", { name: /White Oaks Mall/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/places/ml-wh"));
    await router.navigate(-1);
    expect(await screen.findByRole("combobox", { name: "Search places" })).toHaveValue("Spring");
    expect(await screen.findByRole("listbox", { name: "Matching places" })).toBeInTheDocument();
  });

  it("links the kinds of place with days to their best days, above the states", async () => {
    // The server answers in enum order; the row keeps the place page's chip order.
    landing(undefined, undefined, () => ({
      body: { categories: ["PLACE_CATEGORY_FOOD_AND_DRINK", "PLACE_CATEGORY_SPORTS_AND_RECREATION", "PLACE_CATEGORY_LODGING", "PLACE_CATEGORY_TRAVEL"] },
    }));
    const kinds = await screen.findByRole("navigation", { name: "Browse by kind of place" });
    const links = within(kinds).getAllByRole("link");
    expect(links.map((l) => [l.textContent, l.getAttribute("href")])).toEqual([
      ["Food & drink", "/best/food-and-drink"],
      ["Sports", "/best/sports-and-recreation"],
      ["Hotels", "/best/lodging"],
      ["Travel", "/best/travel"],
    ]);
    const states = await screen.findByRole("navigation", { name: "Browse by state" });
    expect(kinds.compareDocumentPosition(states) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it.each([
    { name: "no kind of place has a day", kinds: () => ({ body: { categories: [] } }) },
    { name: "the list fails", kinds: () => connectError("unavailable") },
  ])("leaves the kinds of place out when $name", async ({ kinds }) => {
    const { calls } = landing(undefined, undefined, kinds);
    await screen.findByRole("navigation", { name: "Browse by state" });
    await waitFor(() => expect(calls("PlaceService/ListStoriedCategories")).toHaveLength(1));
    expect(screen.queryByRole("navigation", { name: "Browse by kind of place" })).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows the kinds of place to a returning member too", async () => {
    renderApp(
      "/",
      {
        "PlaceService/ListChildren": () => ({ body: states }),
        "PlaceService/ListStoriedCategories": () => ({ body: { categories: ["PLACE_CATEGORY_NIGHTLIFE"] } }),
        "PerspectiveService/ListMine": () => ({ body: { perspectives: [] } }),
        "NotifyService/ListInbox": () => ({ body: { unreadCount: 0 } }),
        "LoreService/ListClaims": () => ({ body: { claims: [] } }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    const kinds = await screen.findByRole("navigation", { name: "Browse by kind of place" });
    expect(within(kinds).getByRole("link", { name: "Nightlife" })).toHaveAttribute("href", "/best/nightlife");
  });

  it("lets a reader drill down from the states", async () => {
    landing();
    const browse = await screen.findByRole("navigation", { name: "Browse by state" });
    expect(within(browse).getByRole("heading", { level: 2 })).toHaveTextContent(/^Browse by state$/);
    expect(within(browse).getByRole("link", { name: "Illinois" })).toHaveAttribute("href", "/places/st-il");
  });

  it("explains Perspectiv to a newcomer with a labelled example, not a feed", async () => {
    landing();
    expect(await screen.findByRole("heading", { level: 1, name: "Same place. Same day. Every side of the story." })).toBeInTheDocument();
    const example = screen.getByRole("figure", { name: /Example/ });
    expect(within(example).getByText(/The power went out halfway through open mic/)).toBeInTheDocument();
    expect(within(example).getByText(/I don’t sing/)).toBeInTheDocument();
    expect(within(example).getByText(/that was me/)).toBeInTheDocument();
    // Two facing pages split by a hairline gutter, not cards (Folio, Landing).
    expect(example.querySelector(".shadow-sm, .rounded-xl, .rounded-2xl")).toBeNull();
    const how = screen.getByRole("list", { name: "How Perspectiv works" });
    expect(how.tagName).toBe("OL");
    expect(within(how).getAllByRole("listitem")).toHaveLength(4);
    expect(how).toHaveTextContent("the lore the people who were there favour most becomes canon");
    expect(how).not.toHaveTextContent(/readers/);
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

  it.each([
    { name: "tells a returning member when nothing is waiting on them", published: true, nothing: true },
    { name: "says nothing about waiting to a member who has not published", published: false, nothing: false },
  ])("$name", async ({ published, nothing }) => {
    const story = { id: "p-2", arcId: "arc-2", arcTitle: "Lights out", status: "PERSPECTIVE_STATUS_PUBLISHED" };
    renderApp(
      "/",
      {
        "PlaceService/ListChildren": () => ({ body: states }),
        "PerspectiveService/ListMine": () => ({ body: { perspectives: published ? [story] : [] } }),
        "NotifyService/ListInbox": () => ({ body: { unreadCount: 0 } }),
        "LoreService/ListClaims": () => ({ body: { claims: [] } }),
      },
      { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
    );
    await screen.findByRole("heading", { level: 1, name: published ? "Welcome back, wren" : "Welcome, wren" });
    expect(screen.getByRole("combobox", { name: "Search places" })).toBeInTheDocument();
    if (nothing) {
      expect(await screen.findByRole("list", { name: "Waiting on you" })).toHaveTextContent("Nothing is waiting on you.");
    } else {
      await screen.findByRole("region", { name: "How a day works" });
      // Both counts have answered, and no empty list stands in for the line.
      await waitFor(() => expect(screen.queryByText("Nothing is waiting on you.")).toBeNull());
      expect(screen.queryByRole("list", { name: "Waiting on you" })).toBeNull();
    }
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
    // A place with new stories opens on its Newest tab, where they are marked.
    expect(links[0]).toHaveAttribute("href", "/places/ci-spr?tab=newest");
    expect(links[2]).toHaveAttribute("href", "/places/ci-chi");
  });

  it("offers to add a place that search does not find", async () => {
    const user = userEvent.setup();
    landing(() => ({ body: { places: [] } }));
    await user.type(await screen.findByRole("combobox", { name: "Search places" }), "Dollar General");
    const add = await screen.findByRole("link", { name: "Not here? Add “Dollar General” as a new place" });
    expect(add).toHaveAttribute("href", "/compose?add=Dollar+General");
  });

  it.each([
    {
      name: "a name and its town: the town goes to Where",
      typed: "joes crab shack tampa",
      split: { name: "joes crab shack", localityId: "ci-tpa", localityLabel: "Tampa, Florida" },
      link: "Not here? Add “Joes Crab Shack” in Tampa, Florida",
      href: "/compose?add=Joes+Crab+Shack&in=ci-tpa",
    },
    {
      name: "a capital typed keeps the name as typed",
      typed: "McDonald's, Tampa",
      split: { name: "McDonald's", localityId: "ci-tpa", localityLabel: "Tampa, Florida" },
      link: "Not here? Add “McDonald's” in Tampa, Florida",
      href: "/compose?add=McDonald%27s&in=ci-tpa",
    },
    {
      name: "no split: the whole search, title-cased",
      typed: "corner books",
      split: undefined,
      link: "Not here? Add “Corner Books” as a new place",
      href: "/compose?add=Corner+Books",
    },
  ])("adds a place from the split the server read: $name", async ({ typed, split, link, href }) => {
    const user = userEvent.setup();
    landing(() => ({ body: { places: [], split } }));
    await user.type(await screen.findByRole("combobox", { name: "Search places" }), typed);
    expect(await screen.findByRole("link", { name: link })).toHaveAttribute("href", href);
  });
});
