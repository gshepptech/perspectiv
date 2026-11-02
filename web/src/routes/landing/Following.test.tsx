import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { sessionReply } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const springfield = { id: "ci-spr", kind: "PLACE_KIND_CITY", name: "Springfield" };

/** A returning member's front page with the given followed places and writers. */
function front(places: unknown[], writers: unknown[]) {
  return renderApp(
    "/",
    {
      "PlaceService/ListChildren": () => ({ body: { places: [{ id: "st-il", kind: "PLACE_KIND_STATE", name: "Illinois" }] } }),
      "PerspectiveService/ListMine": () => ({ body: { perspectives: [] } }),
      "NotifyService/ListInbox": () => ({ body: { unreadCount: 0 } }),
      "LoreService/ListClaims": () => ({ body: { claims: [] } }),
      "PlaceService/ListFollowing": () => ({ body: { places } }),
      "UserService/ListFollowedWriters": () => ({ body: { writers } }),
    },
    { refresh: () => sessionReply({ token: "tok", username: "wren" }) },
  );
}

describe("writers you follow", () => {
  it("lists followed writers with stories this week under the followed places, most new first", async () => {
    front(
      [{ ...springfield, newThisWeek: 1 }],
      [
        { userId: "u-1", username: "otter", newThisWeek: 1 },
        { userId: "u-2", username: "quiet_quinn" },
        { userId: "u-3", username: "heron", newThisWeek: 100 },
      ],
    );
    const writers = await screen.findByRole("navigation", { name: "Writers you follow" });
    const places = screen.getByRole("navigation", { name: "Places you follow" });
    expect(places.compareDocumentPosition(writers) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const links = within(writers).getAllByRole("link");
    // A writer with nothing new this week is left out; the count stops at 100.
    expect(links.map((l) => l.textContent)).toEqual(["heron· 100+ new", "otter· 1 new"]);
    expect(links[1]).toHaveAttribute("href", "/users/otter");
  });

  it("shows the writers even when no place is followed", async () => {
    front([], [{ userId: "u-1", username: "otter", newThisWeek: 2 }]);
    const writers = await screen.findByRole("navigation", { name: "Writers you follow" });
    expect(within(writers).getByRole("link", { name: /otter/ })).toHaveTextContent("2 new");
    expect(screen.queryByRole("navigation", { name: "Places you follow" })).toBeNull();
  });

  it("leaves the writers out when none has anything new", async () => {
    front([springfield], [{ userId: "u-2", username: "quiet_quinn" }]);
    expect(await screen.findByRole("navigation", { name: "Places you follow" })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("navigation", { name: "Writers you follow" })).toBeNull());
  });
});
