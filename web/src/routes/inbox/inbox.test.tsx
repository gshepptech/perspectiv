import { create } from "@bufbuild/protobuf";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { NotificationKind } from "@/gen/perspectiv/v1/enums_pb";
import { NotificationSchema } from "@/gen/perspectiv/v1/notify_pb";
import { resetSessionForTests } from "@/lib/session";
import { expectFitsPhone, ok, fail, preloadAccountRoutes, renderRoute, type Handlers } from "@/routes/auth/harness.testutil";

import { describeNotification } from "./describe";

preloadAccountRoutes();
beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const actor = { userId: "u-7", username: "rook" };
const base = { arcId: "a-1", arcTitle: "Food court, Saturday", createdAt: "2026-10-15T18:00:00Z" };

// One of each of the six kinds; the first three unread.
const six = [
  { id: "n-1", kind: "NOTIFICATION_KIND_CLAIM_RECEIVED", actor, claimId: "c-1", perspectiveId: "p-1", ...base },
  { id: "n-2", kind: "NOTIFICATION_KIND_CLAIM_ACCEPTED", actor, claimId: "c-2", loreId: "l-1", ...base },
  { id: "n-3", kind: "NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC", actor, perspectiveId: "p-9", ...base },
  { id: "n-4", kind: "NOTIFICATION_KIND_VOTE_OUT_OPENED", perspectiveId: "p-2", ...base, readAt: "2026-10-15T19:00:00Z" },
  { id: "n-5", kind: "NOTIFICATION_KIND_CANON_CHANGE", loreId: "l-2", ...base, readAt: "2026-10-15T19:00:00Z" },
  { id: "n-6", kind: "NOTIFICATION_KIND_REPORT_RESOLVED", reportId: "r-1", createdAt: base.createdAt, readAt: "2026-10-15T19:00:00Z" },
];

function inbox(handlers: Handlers = {}) {
  return renderRoute(
    "/inbox",
    {
      "NotifyService/ListInbox": () => ok({ notifications: six, unreadCount: 3 }),
      "NotifyService/MarkRead": () => ok({ unreadCount: 2 }),
      ...handlers,
    },
    { signedIn: true },
  );
}

const items = () => within(screen.getByRole("list", { name: "Notifications" })).getAllByRole("listitem");

describe("InboxPage", () => {
  it("FR-037: shows each of the six kinds in words, naming only usernames", async () => {
    inbox();
    await screen.findByRole("list", { name: "Notifications" });
    const text = items().map((li) => li.textContent);
    expect(text[0]).toContain("rook claimed a character in your perspective in Food court, Saturday");
    expect(text[1]).toContain("rook accepted your claim in Food court, Saturday");
    expect(text[2]).toContain("rook published a new perspective in Food court, Saturday");
    expect(text[3]).toContain("A vote to remove a perspective opened in Food court, Saturday");
    expect(text[4]).toContain("The canon of Food court, Saturday changed");
    expect(text[5]).toContain("A report you made was resolved");
    expect(screen.getByRole("main")).not.toHaveTextContent("@");
  });

  it("marks unread items with a word, not colour alone, and shows the unread count", async () => {
    inbox();
    await screen.findByRole("list", { name: "Notifications" });
    expect(screen.getByText("3 unread")).toBeInTheDocument();
    const all = items();
    expect(within(all[0]!).getByText("New")).toBeInTheDocument();
    expect(within(all[4]!).queryByText("New")).not.toBeInTheDocument();
  });

  it("links each kind to where it happened", async () => {
    inbox();
    await screen.findByRole("list", { name: "Notifications" });
    const href = (i: number) => within(items()[i]!).queryByRole("link")?.getAttribute("href") ?? null;
    expect(href(0)).toBe("/claims");
    expect(href(1)).toBe("/arcs/a-1");
    expect(href(2)).toBe("/perspectives/p-9");
    expect(href(3)).toBe("/perspectives/p-2");
    expect(href(4)).toBe("/arcs/a-1");
    expect(href(5)).toBeNull();
  });

  it("marks a notification read when it is opened", async () => {
    const { api, router, user } = inbox();
    await screen.findByRole("list", { name: "Notifications" });
    await user.click(within(items()[1]!).getByRole("link"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/arcs/a-1"));
    await waitFor(() => expect(api.sent("NotifyService/MarkRead")).toEqual([{ notificationIds: ["n-2"] }]));
  });

  it("does not mark an already read notification again", async () => {
    const { api, user } = inbox();
    await screen.findByRole("list", { name: "Notifications" });
    await user.click(within(items()[4]!).getByRole("link"));
    await new Promise((r) => setTimeout(r, 50));
    expect(api.sent("NotifyService/MarkRead")).toEqual([]);
  });

  it("marks everything read from the keyboard and refreshes the list", async () => {
    let read = false;
    const { api, user } = inbox({
      "NotifyService/ListInbox": () =>
        ok({
          notifications: six.map((n) => (read ? { ...n, readAt: "2026-10-16T00:00:00Z" } : n)),
          unreadCount: read ? 0 : 3,
        }),
      "NotifyService/MarkRead": () => {
        read = true;
        return ok({});
      },
    });
    await screen.findByRole("list", { name: "Notifications" });
    const button = screen.getByRole("button", { name: "Mark all read" });
    expect(button).toBeEnabled();
    button.focus();
    await user.keyboard("{Enter}");
    expect(await screen.findByText("0 unread")).toBeInTheDocument();
    expect(api.sent("NotifyService/MarkRead")).toEqual([{ all: true }]);
    expect(screen.queryByText("New")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Mark all read" })).toBeDisabled();
  });

  it("filters to unread only through the server", async () => {
    const { api, user } = inbox({
      "NotifyService/ListInbox": (body) =>
        ok({ notifications: body.unreadOnly ? six.slice(0, 3) : six, unreadCount: 3 }),
    });
    await screen.findByRole("list", { name: "Notifications" });
    const unread = screen.getByRole("button", { name: "Unread" });
    expect(unread).toHaveAttribute("aria-pressed", "false");
    await user.click(unread);
    await waitFor(() => expect(items()).toHaveLength(3));
    expect(unread).toHaveAttribute("aria-pressed", "true");
    expect(api.sent("NotifyService/ListInbox")).toContainEqual({ unreadOnly: true });
  });

  it("loads the next page with the cursor the server returned", async () => {
    const { api, user } = inbox({
      "NotifyService/ListInbox": (body) =>
        body.pageToken === "cursor-2"
          ? ok({ notifications: six.slice(3), unreadCount: 3 })
          : ok({ notifications: six.slice(0, 3), nextPageToken: "cursor-2", unreadCount: 3 }),
    });
    await screen.findByRole("list", { name: "Notifications" });
    expect(items()).toHaveLength(3);
    await user.click(screen.getByRole("button", { name: "Show older" }));
    await waitFor(() => expect(items()).toHaveLength(6));
    expect(api.sent("NotifyService/ListInbox")).toEqual([{}, { pageToken: "cursor-2" }]);
    expect(screen.queryByRole("button", { name: "Show older" })).not.toBeInTheDocument();
  });

  it("says so when the inbox is empty", async () => {
    inbox({ "NotifyService/ListInbox": () => ok({ notifications: [], unreadCount: 0 }) });
    expect(await screen.findByText(/Nothing here yet/)).toBeInTheDocument();
  });

  it("shows an error when the inbox cannot load", async () => {
    inbox({ "NotifyService/ListInbox": () => fail("unavailable") });
    expect(await screen.findByRole("alert")).toHaveTextContent(/unreachable/);
  });

  it("OT-037: fits a 375px viewport with a long arc title", async () => {
    const long = { ...six[0]!, arcTitle: "A".repeat(120) };
    const { view } = inbox({ "NotifyService/ListInbox": () => ok({ notifications: [long], unreadCount: 1 }) });
    await screen.findByRole("list", { name: "Notifications" });
    expectFitsPhone(view.container);
    expect(within(items()[0]!).getByRole("link").className).toMatch(/break-words/);
  });
});

describe("a new story at a followed place", () => {
  it("names the writer and the arc, and opens the story", () => {
    const n = create(NotificationSchema, {
      kind: NotificationKind.NEW_STORY_AT_FOLLOWED_PLACE,
      actor: { userId: "u-1", username: "otter" },
      arcTitle: "Fourth quarter",
      arcId: "arc-1",
      perspectiveId: "p-1",
    });
    expect(describeNotification(n)).toEqual({ text: "otter wrote a new story in Fourth quarter, at a place you follow.", to: "/perspectives/p-1" });
  });
});
