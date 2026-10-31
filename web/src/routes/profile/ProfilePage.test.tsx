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
    const list = await screen.findByRole("list", { name: "Stories" });
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
    const list = await screen.findByRole("list", { name: "Stories" });
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

  it("shows the track record as four labelled counts under the bio, one word each", async () => {
    renderApp("/users/wren", {
      "UserService/GetProfile": () => ({
        body: { profile: { ...profile, storyCount: 12, upvotesReceived: 1, peopleFound: 5, placesWritten: 1, canonCount: 1, loreCount: 2 } },
      }),
      "PerspectiveService/ListByAuthor": () => ({ body: {} }),
    });
    const record = await screen.findByRole("list", { name: "Track record" });
    expect(within(record).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["12 stories", "1 upvote", "5 people found", "1 place"]);
    // The bio comes first, then the record; the badges stay in the header.
    const bio = screen.getByText("I write about the corner bookshop.");
    expect(bio.compareDocumentPosition(record) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByText("In 1 canon")).toBeInTheDocument();
  });

  it("counts zero, one and many, with a thousands separator", async () => {
    renderApp("/users/wren", {
      "UserService/GetProfile": () => ({ body: { profile: { ...profile, storyCount: 1, upvotesReceived: 12345, peopleFound: 1 } } }),
      "PerspectiveService/ListByAuthor": () => ({ body: {} }),
    });
    const record = await screen.findByRole("list", { name: "Track record" });
    expect(within(record).getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      "1 story",
      "12,345 upvotes",
      "1 person found",
      "0 places",
    ]);
  });
});

const post = (id: string, over: Record<string, unknown> = {}) => ({
  id,
  loreId: `lore-${id}`,
  loreName: "The long queue",
  arcId: "arc-1",
  arcTitle: "Saturday night",
  localDate: "2026-01-14",
  placeName: "Prairie Books",
  excerpt: `Post ${id} text`,
  createdAt: "2026-01-15T15:04:00Z",
  ...over,
});

describe("profile tabs", () => {
  it("opens on Stories, and Thread posts lists the writer's posts once chosen, putting the tab in the URL", async () => {
    const user = userEvent.setup();
    const { router, calls } = renderApp("/users/wren", {
      "UserService/GetProfile": () => ({ body: { profile } }),
      "PerspectiveService/ListByAuthor": () => ({ body: {} }),
      "UserService/ListUserPosts": () => ({ body: { posts: [post("1")] } }),
    });
    const stories = await screen.findByRole("tab", { name: "Stories" });
    expect(stories).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tablist", { name: "wren's writing" })).toBeInTheDocument();
    expect(calls("UserService/ListUserPosts")).toEqual([]);

    await user.click(screen.getByRole("tab", { name: "Thread posts" }));
    expect(screen.getByRole("tab", { name: "Thread posts" })).toHaveAttribute("aria-selected", "true");
    const list = await screen.findByRole("list", { name: "Thread posts" });
    expect(within(list).getByRole("link")).toHaveAttribute("href", "/lores/lore-1");
    expect(router.state.location.search).toBe("?tab=posts");
    expect(calls("UserService/ListUserPosts")).toEqual([{ userId: "u-1", pageSize: 20 }]);

    // Back to Stories drops the parameter; neither switch adds a history entry.
    await user.click(stories);
    expect(router.state.location.search).toBe("");
    expect(router.state.historyAction).toBe("REPLACE");
  });

  it("opens straight on Thread posts from ?tab=posts", async () => {
    renderApp("/users/wren?tab=posts", {
      "UserService/GetProfile": () => ({ body: { profile } }),
      "UserService/ListUserPosts": () => ({ body: { posts: [post("1")] } }),
    });
    expect(await screen.findByRole("tab", { name: "Thread posts" })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByRole("list", { name: "Thread posts" })).toBeInTheDocument();
  });

  it("reads an unknown tab as Stories", async () => {
    renderApp("/users/wren?tab=karma", {
      "UserService/GetProfile": () => ({ body: { profile } }),
      "PerspectiveService/ListByAuthor": () => ({ body: {} }),
    });
    expect(await screen.findByRole("tab", { name: "Stories" })).toHaveAttribute("aria-selected", "true");
  });

  it("shows each post's lore, day, excerpt and time, a reply marked, an unnamed lore said so, a page at a time", async () => {
    const user = userEvent.setup();
    const { calls } = renderApp("/users/wren?tab=posts", {
      "UserService/GetProfile": () => ({ body: { profile } }),
      "UserService/ListUserPosts": (body) =>
        body.pageToken
          ? { body: { posts: [post("3", { loreName: "", arcTitle: "Saturday night · 21:00", hour: 21 })] } }
          : {
              body: {
                posts: [post("1", { reply: true, excerpt: "Me. Sorry about Monk." }), post("2", { arcTitle: "Sunday" })],
                nextPageToken: "t-1",
              },
            },
    });
    const list = await screen.findByRole("list", { name: "Thread posts" });
    const [reply, plain] = within(list).getAllByRole("link");
    expect(reply).toHaveTextContent(/^The long queueSaturday night · Prairie Books · January 14, 2026Me\. Sorry about Monk\.Reply · Jan 15, 2026/);
    expect(plain).toHaveTextContent(/^The long queueSunday · Prairie Books · January 14, 2026Post 2 text/);
    expect(plain).not.toHaveTextContent(/Reply/);
    expect(reply?.querySelector("time")).toHaveAttribute("datetime", "2026-01-15T15:04:00.000Z");

    await user.click(screen.getByRole("button", { name: "Show more" }));
    const third = await within(list).findByRole("link", { name: /Unnamed lore/ });
    expect(third).toHaveAttribute("href", "/lores/lore-3");
    // The hour is in the title, so it is not said twice.
    expect(third).toHaveTextContent("Saturday night · 21:00 · Prairie Books · January 14, 2026Post 3 text");
    expect(third.closest("li")).toHaveFocus();
    expect(calls("UserService/ListUserPosts")).toEqual([
      { userId: "u-1", pageSize: 20 },
      { userId: "u-1", pageToken: "t-1", pageSize: 20 },
    ]);
  });

  it("says when the writer has posted in no thread", async () => {
    renderApp("/users/wren?tab=posts", {
      "UserService/GetProfile": () => ({ body: { profile } }),
      "UserService/ListUserPosts": () => ({ body: {} }),
    });
    expect(await screen.findByText("wren has not posted in a lore thread yet.")).toBeInTheDocument();
  });

  it("says when the posts could not be loaded", async () => {
    renderApp("/users/wren?tab=posts", {
      "UserService/GetProfile": () => ({ body: { profile } }),
      "UserService/ListUserPosts": () => connectError("unavailable"),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("wren's thread posts could not be loaded.");
  });
});
