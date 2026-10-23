import { screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";
import { installApi, ok, renderRoute, userId } from "@/routes/compose/api.testutil";

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
