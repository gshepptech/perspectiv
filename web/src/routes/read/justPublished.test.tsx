import { screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";
import { installApi, ok, renderRoute, userId } from "@/routes/compose/api.testutil";
import { composeHref, rememberAfterPublish, takeAfterPublish } from "@/routes/compose/links";

beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const mine = {
  id: "p-mine",
  arcId: "arc-1",
  arcTitle: "Fourth quarter",
  author: { userId, username: "wren" },
  genre: "GENRE_DRAMA",
  status: "PERSPECTIVE_STATUS_PUBLISHED",
  loreStatus: "LORE_STATUS_PENDING",
  place: { id: "pl-1", kind: "PLACE_KIND_STORE", name: "UCF Stadium" },
  localDate: "2026-10-03",
  parts: [{ position: 1, body: "Section 212." }],
  characters: [],
  reacts: [],
};

function open(characters: unknown[]) {
  installApi({
    "PerspectiveService/Get": () => ok({ perspective: mine }),
    "ArcService/ListOpenCharacters": () => ok({ characters }),
  });
  renderRoute({ pathname: "/perspectives/p-mine", state: { justPublished: true } });
}

describe("right after publishing", () => {
  it("asks whether the writer is anyone already described that day", async () => {
    open([
      { characterId: "c-1", label: "the kid in gold paint", perspectiveId: "p-other", author: { userId: "u-2", username: "otter" } },
      { characterId: "c-2", label: "my own character", perspectiveId: "p-mine", author: { userId, username: "wren" } },
    ]);
    expect(await screen.findByText(/Published\. Others were there too\?/)).toBeInTheDocument();
    const panel = await screen.findByRole("region", { name: "Is this you?" });
    expect(within(panel).getByRole("link", { name: "That was me: the kid in gold paint" })).toHaveAttribute("href", "/perspectives/p-other?claim=1");
    expect(within(panel).queryByText("“my own character”")).toBeNull();
  });

  it("says nothing more when nobody else has been described yet", async () => {
    open([]);
    expect(await screen.findByText(/Published\. Others were there too\?/)).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Is this you?" })).toBeNull();
  });
});

describe("publishing to claim a character in someone else's story", () => {
  const other = {
    ...mine,
    id: "p-other",
    author: { userId: "u-2", username: "otter" },
    loreStatus: "LORE_STATUS_PENDING",
    characters: [{ id: "c-1", position: 1, label: "the kid in gold paint" }],
  };

  beforeEach(() => sessionStorage.clear());

  it("lands back on that story with the claim dialog up, reading where the writer stands afresh", async () => {
    rememberAfterPublish({ placeId: "pl-1", localDate: "2026-10-03" }, "/perspectives/p-other?claim=1");
    const api = installApi({
      "PerspectiveService/Get": (body) => ok({ perspective: body.perspectiveId === "p-other" ? other : mine }),
      "ArcService/ListOpenCharacters": () => ok({ characters: [] }),
      "ArcService/GetArc": () => ok({ arc: { id: "arc-1", title: "Fourth quarter" }, viewer: { perspectiveId: "p-mine", loreId: "" } }),
    });
    const router = renderRoute({ pathname: "/perspectives/p-mine", state: { justPublished: true } });
    const dialog = await screen.findByRole("dialog", { name: "Claim a character" });
    expect(within(dialog).getByRole("button", { name: "That was me: the kid in gold paint" })).toBeInTheDocument();
    expect(router.state.location.pathname + router.state.location.search).toBe("/perspectives/p-other?claim=1");
    expect(api.callsTo("ArcService/GetArc")).not.toHaveLength(0);
    // Used once.
    expect(sessionStorage.getItem("perspectiv.afterPublish")).toBeNull();
  });

  it("stays on the new story when the return was for another day", async () => {
    rememberAfterPublish({ placeId: "pl-1", localDate: "2026-10-04" }, "/perspectives/p-other?claim=1");
    installApi({
      "PerspectiveService/Get": () => ok({ perspective: mine }),
      "ArcService/ListOpenCharacters": () => ok({ characters: [] }),
    });
    const router = renderRoute({ pathname: "/perspectives/p-mine", state: { justPublished: true } });
    expect(await screen.findByText(/Published\. Others were there too\?/)).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/perspectives/p-mine");
    expect(sessionStorage.getItem("perspectiv.afterPublish")).not.toBeNull();
  });

  it("the composer keeps a same-site return from its link, and only that", async () => {
    installApi({
      "PlaceService/GetPlace": () => ok({ place: { id: "pl-1", kind: "PLACE_KIND_STORE", name: "UCF Stadium", timezone: "America/New_York" } }),
    });
    renderRoute(composeHref({ placeId: "pl-1", localDate: "2026-10-03" }, "/perspectives/p-other?claim=1"));
    await screen.findByRole("heading", { name: "Write a perspective" });
    expect(takeAfterPublish({ placeId: "pl-1", localDate: "2026-10-03" })).toBe("/perspectives/p-other?claim=1");
    expect(composeHref({ placeId: "pl-1", localDate: "2026-10-03" }, "//evil.example/")).toBe("/compose?placeId=pl-1&date=2026-10-03");
    rememberAfterPublish({ placeId: "pl-1", localDate: "2026-10-03" }, "https://evil.example/");
    expect(sessionStorage.getItem("perspectiv.afterPublish")).toBeNull();
  });

  it("explains 'Waiting for a link' under the byline", async () => {
    installApi({
      "PerspectiveService/Get": () => ok({ perspective: other }),
      "ArcService/GetArc": () => ok({ arc: { id: "arc-1" }, viewer: {} }),
    });
    renderRoute("/perspectives/p-other");
    expect(await screen.findByText(/^Waiting for a link: this story joins a lore when someone it describes claims/)).toBeInTheDocument();
  });
});
