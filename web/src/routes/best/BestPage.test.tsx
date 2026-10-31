import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { connectError } from "@/lib/fakeApi.testutil";
import { renderApp, type BodyHandler } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const gym = { id: "so-gym", kind: "PLACE_KIND_STORE", name: "Iron Gym" };
const ranked = [
  {
    arcId: "arc-final",
    title: "The night of the final",
    place: gym,
    city: "Tampa",
    localDate: "2026-10-03",
    canonLoreId: "lore-1",
    canonLoreName: "Last whistle",
    memberCount: 4,
    perspectiveCount: 6,
    excerpt: "The lights went out in the second half.",
  },
  {
    arcId: "arc-open",
    title: "Opening day",
    place: { id: "so-courts", kind: "PLACE_KIND_STORE", name: "Lake Courts" },
    city: "Chicago",
    localDate: "2026-09-01",
    memberCount: 0,
    perspectiveCount: 1,
  },
];
const newest = [{ ...ranked[1], arcId: "arc-new", title: "Rain delay" }];

const epic: BodyHandler = (body) => ({ body: { arcs: body.tab === "EPIC_TAB_NEWEST" ? newest : ranked } });

function bestPage(path = "/best/sports-and-recreation", handlers: Record<string, BodyHandler> = {}) {
  return renderApp(path, { "PlaceService/GetEpic": epic, ...handlers });
}

describe("best days at a kind of place", () => {
  it("titles the page by the kind of place and says what canon means here", async () => {
    bestPage();
    expect(await screen.findByRole("heading", { level: 1, name: "The best days at sports and recreation places" })).toBeInTheDocument();
    expect(screen.getByText("Days ranked by their canon across every place of this kind.")).toBeInTheDocument();
    await waitFor(() => expect(document.title).toBe("The best days at sports and recreation places · Perspectiv"));
  });

  it("lists every place's days of that kind in the server's ranked order, naming each place's city", async () => {
    const { calls } = bestPage();
    const list = await screen.findByRole("list", { name: "Days" });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(within(items[0]!).getByRole("link", { name: "The night of the final" })).toHaveAttribute("href", "/arcs/arc-final");
    expect(items[0]).toHaveTextContent("Iron Gym, Tampa");
    expect(items[0]).toHaveTextContent("Canon: Last whistle");
    expect(items[0]).toHaveTextContent("The lights went out in the second half.");
    expect(items[1]).toHaveTextContent("Lake Courts, Chicago");
    expect(screen.getByRole("tab", { name: "Ranked" })).toHaveAttribute("aria-selected", "true");
    // Everywhere: no place, the category as the filter.
    expect(calls("PlaceService/GetEpic")).toEqual([{ tab: "EPIC_TAB_RANKED", category: "PLACE_CATEGORY_SPORTS_AND_RECREATION" }]);
  });

  it("switches to Newest and keeps the tab in the address", async () => {
    const user = userEvent.setup();
    const { router, calls } = bestPage();
    await screen.findByRole("list", { name: "Days" });
    await user.click(screen.getByRole("tab", { name: "Newest" }));
    expect(await screen.findByRole("link", { name: "Rain delay" })).toHaveAttribute("href", "/arcs/arc-new");
    expect(router.state.location.search).toBe("?tab=newest");
    expect(calls("PlaceService/GetEpic").at(-1)).toEqual({ tab: "EPIC_TAB_NEWEST", category: "PLACE_CATEGORY_SPORTS_AND_RECREATION" });
  });

  it("opens on the tab named in the address", async () => {
    bestPage("/best/food-and-drink?tab=newest");
    expect(await screen.findByRole("heading", { level: 1, name: "The best days at food and drink places" })).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "Rain delay" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Newest" })).toHaveAttribute("aria-selected", "true");
  });

  it("loads the next page with the server's page token", async () => {
    const user = userEvent.setup();
    const { calls } = bestPage("/best/nightlife", {
      "PlaceService/GetEpic": (body) =>
        body.pageToken === "t2"
          ? { body: { arcs: [{ ...ranked[1], arcId: "arc-3", title: "Last call" }] } }
          : { body: { arcs: ranked, nextPageToken: "t2" } },
    });
    await user.click(await screen.findByRole("button", { name: "Show more" }));
    expect(await screen.findByRole("link", { name: "Last call" })).toBeInTheDocument();
    expect(within(screen.getByRole("list", { name: "Days" })).getAllByRole("listitem")).toHaveLength(3);
    expect(calls("PlaceService/GetEpic").at(-1)).toEqual({ tab: "EPIC_TAB_RANKED", category: "PLACE_CATEGORY_NIGHTLIFE", pageToken: "t2" });
  });

  it("says so when no one has written about a day there yet", async () => {
    bestPage("/best/lodging", { "PlaceService/GetEpic": () => ({ body: { arcs: [] } }) });
    expect(await screen.findByText(/No one has written about a day at hotels yet\./)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Search for a place" })).toHaveAttribute("href", "/");
  });

  it("says so when the list cannot be loaded", async () => {
    bestPage("/best/health", { "PlaceService/GetEpic": () => connectError("unavailable") });
    expect(await screen.findByRole("alert")).toHaveTextContent("This list could not be loaded");
  });

  it.each(["bowling", "unspecified", "SPORTS_AND_RECREATION"])("shows not found for the unknown kind %s without asking the server", async (slug) => {
    const { calls } = bestPage(`/best/${slug}`);
    expect(await screen.findByRole("heading", { level: 1, name: "Kind of place not found" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Search for a place" })).toHaveAttribute("href", "/");
    await waitFor(() => expect(document.title).toBe("Kind of place not found · Perspectiv"));
    expect(calls("PlaceService/GetEpic")).toEqual([]);
  });
});
