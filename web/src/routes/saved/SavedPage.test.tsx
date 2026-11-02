import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { connectError, sessionReply } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const signedIn = () => sessionReply({ token: "tok", username: "wren" });

const saved = (id: string, arcTitle: string, writer: string) => ({
  id,
  arcId: `arc-${id}`,
  arcTitle,
  author: { userId: `u-${writer}`, username: writer },
  status: "PERSPECTIVE_STATUS_PUBLISHED",
  loreStatus: "LORE_STATUS_IN_LORE",
  place: { id: "pl-1", kind: "PLACE_KIND_STORE", name: "Prairie Books" },
  localDate: "2026-01-14",
  parts: [{ position: 1, body: "The bell over the door rang twice." }],
  saved: true,
});

describe("saved stories page", () => {
  it("is titled Saved stories and lists the saved stories a page at a time, each row naming its writer", async () => {
    const user = userEvent.setup();
    const { calls } = renderApp(
      "/saved",
      {
        "PerspectiveService/ListSaved": (body) =>
          body.pageToken
            ? { body: { perspectives: [saved("p-2", "An older night", "heron")] } }
            : { body: { perspectives: [saved("p-1", "The night the fountain froze", "otter")], nextPageToken: "t-1" } },
      },
      { refresh: signedIn },
    );
    expect(await screen.findByRole("heading", { level: 1, name: "Saved stories" })).toBeInTheDocument();
    await waitFor(() => expect(document.title).toBe("Saved stories · Perspectiv"));
    const list = await screen.findByRole("list", { name: "Saved stories" });
    const first = within(list).getByRole("link", { name: /The night the fountain froze/ });
    expect(first).toHaveAttribute("href", "/perspectives/p-1");
    expect(first).toHaveTextContent(/Prairie Books · .* · by otter/);
    expect(first).toHaveTextContent("The bell over the door rang twice.");

    await user.click(screen.getByRole("button", { name: "Show more" }));
    const second = await within(list).findByRole("link", { name: /An older night/ });
    expect(second).toHaveTextContent(/by heron/);
    await waitFor(() => expect(second.closest("li")).toHaveFocus());
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
    expect(calls("PerspectiveService/ListSaved")).toEqual([{ pageSize: 20 }, { pageToken: "t-1", pageSize: 20 }]);
  });

  it("unsaves from the row, keeps it with Removed and Undo until the page is left, and keeps focus on the toggle", async () => {
    const user = userEvent.setup();
    const { calls } = renderApp(
      "/saved",
      {
        "PerspectiveService/ListSaved": () => ({ body: { perspectives: [saved("p-1", "The night the fountain froze", "otter")] } }),
        "PerspectiveService/SavePerspective": (body) => ({ body: { saved: !!body.on } }),
      },
      { refresh: signedIn },
    );
    const list = await screen.findByRole("list", { name: "Saved stories" });
    const toggle = within(list).getByRole("button", { name: "Saved otter's perspective" });
    expect(toggle).toHaveAttribute("aria-pressed", "true");

    await user.click(toggle);
    const undo = await within(list).findByRole("button", { name: "Undo: save otter's perspective again" });
    // The same button, so focus never left it.
    expect(undo).toBe(toggle);
    expect(undo).toHaveFocus();
    expect(undo).not.toHaveAttribute("aria-pressed");
    expect(within(list).getByRole("status")).toHaveTextContent("Removed.");
    // The row stays: the list is not read again while the page is open.
    expect(within(list).getByRole("link", { name: /The night the fountain froze/ })).toBeInTheDocument();
    expect(calls("PerspectiveService/ListSaved")).toHaveLength(1);

    await user.click(undo);
    await waitFor(() => expect(within(list).getByRole("button", { name: "Saved otter's perspective" })).toHaveFocus());
    expect(within(list).getByRole("status")).toBeEmptyDOMElement();
    expect(calls("PerspectiveService/SavePerspective")).toEqual([
      { perspectiveId: "p-1" },
      { perspectiveId: "p-1", on: true },
    ]);
  });

  it("says how to save a story when none is saved", async () => {
    renderApp("/saved", { "PerspectiveService/ListSaved": () => ({ body: {} }) }, { refresh: signedIn });
    expect(
      await screen.findByText("You have not saved any stories yet. Press Save under a story to keep it here."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: "Saved stories" })).toBeNull();
  });

  it("says so when the list cannot be read", async () => {
    renderApp("/saved", { "PerspectiveService/ListSaved": () => connectError("internal") }, { refresh: signedIn });
    expect(await screen.findByRole("alert")).toHaveTextContent("Your saved stories could not be loaded.");
  });
});
