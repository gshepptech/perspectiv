import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { connectError } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const profile = {
  userId: "u-1",
  username: "wren",
  bio: "I write about the corner bookshop.",
  avatarUrl: "/avatars/u-1.png",
  joinedAt: "2026-03-02T15:00:00Z",
};

describe("public profile page", () => {
  it("shows a signed-out reader the username, avatar, bio and join date, looked up by username", async () => {
    const { calls } = renderApp("/users/wren", {
      "UserService/GetProfile": () => ({ body: { profile, account: { email: "wren@example.com" } } }),
    });
    expect(await screen.findByRole("heading", { level: 1, name: "wren" })).toBeInTheDocument();
    expect(screen.getByText("I write about the corner bookshop.")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "wren's avatar" })).toHaveAttribute("src", "/avatars/u-1.png");
    expect(screen.getByText(/Joined March 2026/)).toBeInTheDocument();
    expect(screen.queryByText(/wren@example\.com/)).toBeNull();
    expect(calls("UserService/GetProfile")).toEqual([{ username: "wren" }]);
  });

  it("says so when no user has the username", async () => {
    renderApp("/users/nobody", { "UserService/GetProfile": () => connectError("not_found") });
    expect(await screen.findByRole("heading", { level: 1, name: "User not found" })).toBeInTheDocument();
  });

  it("shows a deleted account without its details", async () => {
    renderApp("/users/gone", {
      "UserService/GetProfile": () => ({ body: { profile: { userId: "u-2", deleted: true } } }),
    });
    expect(await screen.findByText("This account was deleted.")).toBeInTheDocument();
  });

  it("lists the user's published perspectives, tales included, newest first, a page at a time", async () => {
    const user = userEvent.setup();
    const written = (id: string, arcTitle: string, loreStatus: string) => ({
      id,
      arcId: `arc-${id}`,
      arcTitle,
      author: { userId: "u-1", username: "wren" },
      status: "PERSPECTIVE_STATUS_PUBLISHED",
      loreStatus,
      place: { id: "pl-1", kind: "PLACE_KIND_STORE", name: "Prairie Books" },
      localDate: "2026-01-14",
      parts: [{ position: 1, body: "text" }],
    });
    const { calls } = renderApp("/users/wren", {
      "UserService/GetProfile": () => ({ body: { profile } }),
      "PerspectiveService/ListByAuthor": (body) =>
        body.pageToken
          ? { body: { perspectives: [written("p-2", "An older night", "LORE_STATUS_IN_LORE")] } }
          : { body: { perspectives: [written("p-1", "The night the fountain froze", "LORE_STATUS_SOLO")], nextPageToken: "t-1" } },
    });
    const list = await screen.findByRole("list");
    const first = within(list).getByRole("link", { name: /The night the fountain froze/ });
    expect(first).toHaveAttribute("href", "/perspectives/p-1");
    expect(first).toHaveTextContent(/Prairie Books/);
    expect(first).toHaveTextContent(/Tale/);
    await user.click(screen.getByRole("button", { name: "Show more" }));
    expect(await within(list).findByRole("link", { name: /An older night/ })).toHaveAttribute("href", "/perspectives/p-2");
    expect(calls("PerspectiveService/ListByAuthor")).toEqual([
      { userId: "u-1", pageSize: 20 },
      { userId: "u-1", pageToken: "t-1", pageSize: 20 },
    ]);
  });

  it("tells stories apart by place, day and their own opening, even when their days share a title", async () => {
    const row = (id: string, place: string, localDate: string, body: string) => ({
      id,
      arcId: `arc-${id}`,
      arcTitle: "Saturday night",
      author: { userId: "u-1", username: "wren" },
      status: "PERSPECTIVE_STATUS_PUBLISHED",
      loreStatus: "LORE_STATUS_IN_LORE",
      place: { id: `pl-${id}`, kind: "PLACE_KIND_STORE", name: place },
      localDate,
      parts: [
        { position: 2, body: "Later on." },
        { position: 1, body: body },
      ],
    });
    renderApp("/users/wren", {
      "UserService/GetProfile": () => ({ body: { profile } }),
      "PerspectiveService/ListByAuthor": () => ({
        body: {
          perspectives: [
            row("p-1", "Prairie Books", "2026-01-14", "The doors opened at nine."),
            row("p-2", "Moonbean Coffee", "2026-02-07", `The power went out ${"and stayed out ".repeat(30)}until dawn.`),
          ],
        },
      }),
    });
    const list = await screen.findByRole("list");
    const [first, second] = within(list).getAllByRole("link");
    expect(first).toHaveTextContent(/^Prairie Books · January 14, 2026Saturday nightThe doors opened at nine\.In lore$/);
    expect(second).toHaveTextContent(/^Moonbean Coffee · February 7, 2026Saturday nightThe power went out and stayed out/);
    // A long opening is cut at a word, not mid-word.
    expect(second?.textContent).toMatch(/ (and|stayed|out)…In lore$/);
    expect(document.title).toBe("wren · Perspectiv");
  });

  it("says when the user has published nothing", async () => {
    renderApp("/users/wren", {
      "UserService/GetProfile": () => ({ body: { profile } }),
      "PerspectiveService/ListByAuthor": () => ({ body: {} }),
    });
    expect(await screen.findByText("wren has not published a perspective yet.")).toBeInTheDocument();
  });

  it("shows the user's canon and lore badges", async () => {
    renderApp("/users/wren", {
      "UserService/GetProfile": () => ({ body: { profile: { ...profile, canonCount: 2, loreCount: 3 } } }),
      "PerspectiveService/ListByAuthor": () => ({ body: {} }),
    });
    expect(await screen.findByText("In 2 canons")).toBeInTheDocument();
    expect(screen.getByText("In 3 lores")).toBeInTheDocument();
  });
});
