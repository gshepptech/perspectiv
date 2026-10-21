import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import { bodiesFor, ME, polyfillRadix, renderApp } from "@/components/actions/testing.testutil";
import { connectError, type FakeHandler } from "@/lib/fakeApi.testutil";
import { resetSessionForTests } from "@/lib/session";

import { Component as ArcPage } from "./ArcPage";
import { arc, dayArcResponse, lore, OTTER, perspective, summary } from "./fixtures.testutil";

beforeAll(polyfillRadix);
beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const route = { path: "/arcs/:arcId", Component: ArcPage };

function open(path: string, getArc: FakeHandler, opts: { signedIn?: boolean; routes?: Record<string, FakeHandler> } = {}) {
  return renderApp(route, path, {
    signedIn: opts.signedIn,
    routes: {
      "ArcService/GetArc": getArc,
      "PerspectiveService/Get": () => ({ body: perspective("any", { characters: [{ id: "c-any", position: 1, label: "a stranger" }] }) }),
      ...opts.routes,
    },
  });
}

const before = (a: Element, b: Element) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0;

describe("arc page reading order", () => {
  it("AC-030 FR-030 OT-032: renders canon, then contenders, then stories standing alone, with a demoted lore still listed", async () => {
    await open("/arcs/arc-day", () => ({ body: dayArcResponse() }));

    expect(await screen.findByRole("heading", { level: 1, name: "Closing night at Moonbean" })).toBeInTheDocument();
    const canon = screen.getByRole("region", { name: "Canon" });
    const sublores = screen.getByRole("region", { name: "Contenders" });
    const alone = screen.getByRole("region", { name: "Standing alone" });
    expect(before(canon, sublores)).toBe(true);
    expect(before(sublores, alone)).toBe(true);

    expect(within(canon).getByText("Canon of the day")).toBeInTheDocument();
    expect(within(canon).getByRole("heading", { name: "wren, otter and 2 others" })).toBeInTheDocument();
    expect(within(canon).getByText("Canon text by otter")).toBeInTheDocument();
    expect(within(canon).getByRole("link", { name: /Open the canon/ })).toHaveAttribute("href", "/lores/lore-canon");
    // The former canon, now a 3-member contender, keeps every story listed.
    const former = within(sublores).getByRole("region", { name: "moth, finch and 1 other" });
    for (const name of ["moth", "finch", "vole"]) {
      expect(within(former).getByRole("article", { name: `${name}'s perspective` })).toBeInTheDocument();
    }
    expect(within(former).getByText("1 more person to be eligible for canon")).toBeInTheDocument();
    expect(within(former).getByRole("link", { name: /Open this lore/ })).toHaveAttribute("href", "/lores/lore-former");
    // Stories in no lore: the one still waiting for a link first, then the tales.
    expect(before(within(alone).getByText("Just published"), within(alone).getByText("A tale nobody claimed"))).toBe(true);
    expect(within(alone).getByText("A story the lore voted out")).toBeInTheDocument();
    expect(within(alone).getByText(/Removed from lore/)).toBeInTheDocument();
    expect(within(alone).getByText(/Waiting for a link/)).toBeInTheDocument();
    // Every story links to its own page.
    expect(within(alone).getByRole("link", { name: "Read solo_teller's perspective" })).toHaveAttribute("href", "/perspectives/p-solo");
  });

  it("shows the place chain, the date and the arc's places as links", async () => {
    await open("/arcs/arc-day", () => ({ body: dayArcResponse() }));
    const crumbs = await screen.findByRole("navigation", { name: "Place" });
    expect(within(crumbs).getByRole("link", { name: "Portland" })).toHaveAttribute("href", "/places/pl-city");
    expect(within(crumbs).getByRole("link", { name: "Moonbean Coffee" })).toHaveAttribute("href", "/places/pl-store");
    expect(screen.getByText("October 1, 2026")).toBeInTheDocument();
  });

  it("says when the arc has no canon and how close the leading contender is", async () => {
    const res = dayArcResponse();
    await open("/arcs/arc-day", () => ({
      body: { ...res, arc: arc({ canonLoreId: "" }), canon: undefined, hours: [] },
    }));
    const canon = await screen.findByRole("region", { name: "Canon" });
    expect(canon).toHaveTextContent("No canon yet");
    expect(canon).toHaveTextContent("The leading contender has 3 of 4 members");
    expect(screen.getByRole("region", { name: "Contenders" })).toHaveTextContent("moth");
  });

  it("shows a placeholder for a deleted or hidden perspective without its actions", async () => {
    const res = dayArcResponse();
    res.tales = [
      summary({ id: "p-del", loreStatus: "LORE_STATUS_SOLO", excerpt: "", deleted: true }),
      summary({ id: "p-hid", loreStatus: "LORE_STATUS_SOLO", excerpt: "", hidden: true }),
    ];
    await open("/arcs/arc-day", () => ({ body: res }));
    const alone = await screen.findByRole("region", { name: "Standing alone" });
    expect(within(alone).getByText("This perspective was deleted.")).toBeInTheDocument();
    expect(within(alone).getByText("This perspective was hidden by a moderator.")).toBeInTheDocument();
    for (const gone of within(alone).getAllByRole("article", { name: "Unavailable perspective" })) {
      expect(within(gone).queryByRole("button")).toBeNull();
      expect(within(gone).queryByRole("link")).toBeNull();
    }
  });

  it("explains a missing arc", async () => {
    await open("/arcs/nope", () => connectError("not_found", "arc not found"));
    expect(await screen.findByRole("heading", { name: "Arc not found" })).toBeInTheDocument();
  });

  it("offers a retry when the arc cannot be loaded", async () => {
    let calls = 0;
    await open("/arcs/arc-day", () => {
      calls += 1;
      return calls === 1 ? connectError("unavailable", "down") : { body: dayArcResponse() };
    });
    const retry = await screen.findByRole("button", { name: "Try again" });
    await userEvent.click(retry);
    expect(await screen.findByRole("heading", { level: 1, name: "Closing night at Moonbean" })).toBeInTheDocument();
  });
});

describe("hour timeline", () => {
  it("AC-033 OT-035: lists hour child arcs in ascending hour order after the day canon, each with its canon", async () => {
    await open("/arcs/arc-day", () => ({ body: dayArcResponse() }));
    const hours = await screen.findByRole("region", { name: "Hours" });
    const canon = screen.getByRole("region", { name: "Canon" });
    const sublores = screen.getByRole("region", { name: "Contenders" });
    expect(before(canon, hours)).toBe(true);
    expect(before(hours, sublores)).toBe(true);

    const items = within(hours).getAllByRole("listitem").filter((li) => li.parentElement?.closest("li") === null);
    expect(items.map((li) => within(li).getByRole("heading", { level: 3 }).textContent)).toEqual([
      "09:00 The morning queue",
      "11:00 Late breakfast",
      "15:00 The afternoon rush",
    ]);
    expect(within(items[0]!).getByText("No canon yet")).toBeInTheDocument();
    expect(within(items[1]!).getByText("Hour eleven canon text")).toBeInTheDocument();
    expect(within(items[2]!).getByText("Hour fifteen canon text")).toBeInTheDocument();
    expect(within(items[2]!).getByRole("link", { name: /Open the 15:00 arc/ })).toHaveAttribute("href", "/arcs/arc-h15");
    // The stitched canons come after the day-level canon's own perspectives.
    expect(before(within(canon).getByText("Canon text by wren"), within(items[1]!).getByText("Hour eleven canon text"))).toBe(true);
  });

  it("an hour arc links back to its day arc and has no timeline", async () => {
    await open("/arcs/arc-h15", () => ({
      body: { arc: arc({ id: "arc-h15", hour: 15, parentArcId: "arc-day", title: "The afternoon rush" }), sublores: [], tales: [], hours: [] },
    }));
    expect(await screen.findByRole("heading", { level: 1, name: "The afternoon rush" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /whole day/ })).toHaveAttribute("href", "/arcs/arc-day");
    expect(screen.getByText(/15:00/)).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Hours" })).toBeNull();
  });
});

describe("signed-out reader", () => {
  it("AC-009: reads canon, contenders and stories standing alone, and an upvote redirects to login", async () => {
    const { api } = await open("/arcs/arc-day", () => ({ body: dayArcResponse() }));
    const canon = await screen.findByRole("region", { name: "Canon" });
    expect(within(canon).getByText("Canon text by otter")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Standing alone" })).toHaveTextContent("A tale nobody claimed");

    const card = within(canon).getByRole("article", { name: "otter's perspective" });
    await userEvent.click(await within(card).findByRole("button", { name: "Upvote" }));
    expect(await screen.findByTestId("login")).toHaveTextContent("login at /login?next=%2Farcs%2Farc-day");
    expect(api.calls.some((c) => c.procedure.startsWith("VoteService"))).toBe(false);
  });
});

describe("participation", () => {
  it("offers a signed-in reader with no perspective here a way to write one", async () => {
    const res = dayArcResponse();
    // wren is in none of these perspectives.
    res.canon.perspectives = res.canon.perspectives.filter((p) => p.author?.userId !== ME);
    res.canon.lore = lore("lore-canon", "CANON", [{ perspectiveId: "p-otter", user: OTTER }]);
    res.viewer = { perspectiveId: "", loreId: "" };
    await open("/arcs/arc-day", () => ({ body: res }), { signedIn: true });
    const write = await screen.findByRole("link", { name: "Write your perspective" });
    expect(write).toHaveAttribute("href", "/compose?placeId=pl-store&date=2026-10-01");
    expect(screen.getByText(/Only people with a perspective here can upvote/)).toBeInTheDocument();
  });

  it("links a participant to their own perspective instead", async () => {
    await open("/arcs/arc-day", () => ({ body: dayArcResponse() }), { signedIn: true });
    expect(await screen.findByRole("link", { name: "Read yours" })).toHaveAttribute("href", "/perspectives/p-wren");
    expect(screen.queryByRole("link", { name: "Write your perspective" })).toBeNull();
  });

  it("the write link for an hour arc carries the hour", async () => {
    await open("/arcs/arc-h15", () => ({
      body: { arc: arc({ id: "arc-h15", hour: 15, parentArcId: "arc-day" }), sublores: [], tales: [], hours: [] },
    }), { signedIn: true });
    expect(await screen.findByRole("link", { name: "Write your perspective" })).toHaveAttribute(
      "href",
      "/compose?placeId=pl-store&date=2026-10-01&hour=15",
    );
  });

  it("reports the arc's place with the report form", async () => {
    const { api } = await open("/arcs/arc-day", () => ({ body: dayArcResponse() }), {
      signedIn: true,
      routes: { "ModerationService/Report": () => ({ body: { reportId: "r-1" } }) },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Report place" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Moonbean Coffee");
    await userEvent.click(within(dialog).getByRole("combobox", { name: "Reason" }));
    await userEvent.click(await screen.findByRole("option", { name: "Spam" }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Send report" }));
    await waitFor(() => expect(bodiesFor(api, "ModerationService/Report")).toHaveLength(1));
    expect(bodiesFor(api, "ModerationService/Report")[0]).toMatchObject({
      targetKind: "REPORT_TARGET_KIND_PLACE",
      targetId: "pl-store",
      reason: "REPORT_REASON_SPAM",
    });
  });
});

describe("arc page lists are paged (NFR-008)", () => {
  it("shows more tales from the token GetArc returned, after the first page", async () => {
    const user = userEvent.setup();
    const res = { ...dayArcResponse(), talesNextPageToken: "tales-2" };
    const { api } = await open("/arcs/arc-day", () => ({ body: res }), {
      routes: {
        "ArcService/ListArcSection": () => ({
          body: { perspectives: [summary({ id: "p-later", loreStatus: "LORE_STATUS_SOLO", excerpt: "A later tale" })] },
        }),
      },
    });
    const tales = await screen.findByRole("region", { name: "Standing alone" });
    expect(within(tales).queryByText("A later tale")).toBeNull();
    await user.click(within(tales).getByRole("button", { name: "Show more tales" }));
    expect(await within(tales).findByText("A later tale")).toBeInTheDocument();
    expect(within(tales).getByText("A tale nobody claimed")).toBeInTheDocument();
    expect(bodiesFor(api, "ArcService/ListArcSection")).toEqual([
      { arcId: "arc-day", section: "ARC_SECTION_TALES", pageToken: "tales-2", pageSize: 20 },
    ]);
    expect(within(tales).queryByRole("button", { name: "Show more tales" })).toBeNull();
  });

  it("offers no more when a list has no next page", async () => {
    await open("/arcs/arc-day", () => ({ body: dayArcResponse() }));
    await screen.findByRole("region", { name: "Standing alone" });
    expect(screen.queryByRole("button", { name: /^Show more/ })).toBeNull();
  });

  it("takes the viewer's standing from the server when their perspective is on a later page", async () => {
    const res = dayArcResponse();
    res.canon.perspectives = res.canon.perspectives.filter((p) => p.author?.userId !== ME);
    res.viewer = { perspectiveId: "p-page-three", loreId: "lore-elsewhere" };
    await open("/arcs/arc-day", () => ({ body: res }), { signedIn: true });
    expect(await screen.findByRole("link", { name: "Read yours" })).toHaveAttribute("href", "/perspectives/p-page-three");
    expect(screen.queryByText(/Only people with a perspective here can upvote/)).toBeNull();
  });
});

