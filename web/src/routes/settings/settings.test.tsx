import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { expectFitsPhone, fail, ok, preloadAccountRoutes, renderRoute, USER_ID, type Handlers } from "@/routes/auth/harness.testutil";
import { currentSession, getAccessToken, resetSessionForTests } from "@/lib/session";

preloadAccountRoutes();
beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const KINDS = [
  "NOTIFICATION_KIND_CLAIM_RECEIVED",
  "NOTIFICATION_KIND_CLAIM_ACCEPTED",
  "NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC",
  "NOTIFICATION_KIND_VOTE_OUT_OPENED",
  "NOTIFICATION_KIND_CANON_CHANGE",
  "NOTIFICATION_KIND_REPORT_RESOLVED",
];

type Blocked = { userId: string; username: string };

function profileBody(opts: { username?: string; bio?: string; avatarUrl?: string; next?: string; blocked?: Blocked[]; verified?: boolean } = {}) {
  return {
    profile: {
      userId: USER_ID,
      username: opts.username ?? "wren",
      bio: opts.bio ?? "I was at the mall.",
      avatarUrl: opts.avatarUrl ?? "",
      joinedAt: "2026-09-01T10:00:00Z",
    },
    account: {
      email: "wren@example.com",
      emailVerified: opts.verified ?? true,
      role: "ROLE_USER",
      nextUsernameChangeAt: opts.next,
      blocked: opts.blocked ?? [],
    },
  };
}

const allOn = () => ok({ preferences: KINDS.map((kind) => ({ kind, inApp: true, email: true })) });

function settings(handlers: Handlers = {}) {
  return renderRoute(
    "/settings",
    {
      "UserService/GetProfile": (body) => (body.username ? fail("not_found") : ok(profileBody())),
      "NotifyService/GetPreferences": allOn,
      ...handlers,
    },
    { signedIn: true },
  );
}

describe("SettingsPage profile", () => {
  it("shows the caller's own account: username, private email and verification", async () => {
    settings({ "UserService/GetProfile": () => ok(profileBody({ verified: false })) });
    const account = await screen.findByRole("region", { name: "Account" });
    expect(account).toHaveTextContent("wren@example.com");
    expect(account).toHaveTextContent("Not verified");
    expect(account).toHaveTextContent(/only you see/i);
    expect(await screen.findByLabelText("Bio")).toHaveValue("I was at the mall.");
  });

  it("saves a new bio", async () => {
    const { api, user } = settings({
      "UserService/UpdateProfile": (body) => ok({ profile: { ...profileBody().profile, bio: body.bio } }),
    });
    const bio = await screen.findByLabelText("Bio");
    await user.clear(bio);
    await user.type(bio, "New bio");
    await user.click(screen.getByRole("button", { name: "Save profile" }));
    expect(await screen.findByText("Profile saved.")).toBeInTheDocument();
    expect(api.sent("UserService/UpdateProfile")).toEqual([{ bio: "New bio" }]);
  });

  it("uploads a chosen avatar image with its type", async () => {
    const { api, user } = settings({ "UserService/UpdateProfile": () => ok({ profile: profileBody().profile }) });
    const file = new File([new Uint8Array([137, 80, 78, 71])], "me.png", { type: "image/png" });
    await user.upload(await screen.findByLabelText("New avatar"), file);
    await user.click(screen.getByRole("button", { name: "Save profile" }));
    await screen.findByText("Profile saved.");
    expect(api.sent("UserService/UpdateProfile")).toEqual([
      { bio: "I was at the mall.", avatar: btoa(String.fromCharCode(137, 80, 78, 71)), avatarContentType: "image/png" },
    ]);
  });

  it("removes the current avatar", async () => {
    const { api, user } = settings({
      "UserService/GetProfile": () => ok(profileBody({ avatarUrl: "/avatars/a.png" })),
      "UserService/UpdateProfile": () => ok({ profile: profileBody().profile }),
    });
    expect(await screen.findByRole("img", { name: "Your avatar" })).toHaveAttribute("src", "/avatars/a.png");
    await user.click(screen.getByRole("checkbox", { name: "Remove avatar" }));
    await user.click(screen.getByRole("button", { name: "Save profile" }));
    await screen.findByText("Profile saved.");
    expect(api.sent("UserService/UpdateProfile")).toEqual([{ bio: "I was at the mall.", clearAvatar: true }]);
  });

  it("shows the server's reason when an avatar is refused", async () => {
    const { user } = settings({ "UserService/UpdateProfile": () => fail("invalid_argument", "avatar must be under 1 MB") });
    await user.upload(await screen.findByLabelText("New avatar"), new File(["x"], "big.png", { type: "image/png" }));
    await user.click(screen.getByRole("button", { name: "Save profile" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("avatar must be under 1 MB");
  });
});

describe("SettingsPage username", () => {
  it("changes the username and the header follows", async () => {
    const { api, user } = settings({
      "UserService/ChangeUsername": (body) => ok({ profile: { ...profileBody().profile, username: body.username } }),
    });
    const input = await screen.findByLabelText("New username");
    await user.clear(input);
    await user.type(input, "wren_two{Enter}");
    expect(await screen.findByText("Your username is now wren_two.")).toBeInTheDocument();
    expect(api.sent("UserService/ChangeUsername")).toEqual([{ username: "wren_two" }]);
    expect(currentSession()?.username).toBe("wren_two");
    expect(screen.getByRole("button", { name: "Account: wren_two" })).toBeInTheDocument();
  });

  it("checks the username rule before asking the server", async () => {
    const { api, user } = settings();
    const input = await screen.findByLabelText("New username");
    await user.clear(input);
    await user.type(input, "no{Enter}");
    expect(await screen.findByText("Use 3 to 20 letters, digits or underscores.")).toBeInTheDocument();
    expect(api.sent("UserService/ChangeUsername")).toHaveLength(0);
  });

  it("says when the next change is allowed and holds the field until then", async () => {
    settings({ "UserService/GetProfile": () => ok(profileBody({ next: "2099-11-03T12:00:00Z" })) });
    expect(await screen.findByLabelText("New username")).toBeDisabled();
    expect(screen.getByRole("region", { name: "Username" })).toHaveTextContent(/You can change it again on .*2099/);
    expect(screen.getByRole("button", { name: "Change username" })).toBeDisabled();
  });

  it("shows the server's refusal (taken, or inside the 30-day window)", async () => {
    const { user } = settings({
      "UserService/ChangeUsername": () => fail("failed_precondition", "you can change your username once every 30 days"),
    });
    const input = await screen.findByLabelText("New username");
    await user.clear(input);
    await user.type(input, "wren_two{Enter}");
    expect(await screen.findByRole("alert")).toHaveTextContent("you can change your username once every 30 days");
    expect(currentSession()?.username).toBe("wren");
  });
});

describe("SettingsPage blocks", () => {
  it("lists blocked users and unblocks one", async () => {
    let blocked: Blocked[] = [
      { userId: "u-2", username: "rook" },
      { userId: "u-3", username: "finch" },
    ];
    const { api, user } = settings({
      "UserService/GetProfile": () => ok(profileBody({ blocked })),
      "UserService/UnblockUser": (body) => {
        blocked = blocked.filter((b) => b.userId !== body.userId);
        return ok();
      },
    });
    const region = await screen.findByRole("region", { name: "Blocked users" });
    expect(await within(region).findByText("rook")).toBeInTheDocument();
    await user.click(within(region).getByRole("button", { name: "Unblock rook" }));
    await waitFor(() => expect(within(region).queryByText("rook")).not.toBeInTheDocument());
    expect(within(region).getByText("finch")).toBeInTheDocument();
    expect(api.sent("UserService/UnblockUser")).toEqual([{ userId: "u-2" }]);
  });

  it("blocks a user by username", async () => {
    let blocked: Blocked[] = [];
    const { api, user } = settings({
      "UserService/GetProfile": (body) =>
        body.username === "rook"
          ? ok({ profile: { userId: "u-2", username: "rook" } })
          : ok(profileBody({ blocked })),
      "UserService/BlockUser": (body) => {
        blocked = [{ userId: String(body.userId), username: "rook" }];
        return ok();
      },
    });
    const region = await screen.findByRole("region", { name: "Blocked users" });
    expect(within(region).getByText("You have not blocked anyone.")).toBeInTheDocument();
    await user.type(within(region).getByLabelText("Username to block"), "rook");
    await user.click(within(region).getByRole("button", { name: "Block" }));
    expect(await within(region).findByRole("button", { name: "Unblock rook" })).toBeInTheDocument();
    expect(api.sent("UserService/BlockUser")).toEqual([{ userId: "u-2" }]);
  });

  it("says when no user has that username", async () => {
    const { api, user } = settings();
    const region = await screen.findByRole("region", { name: "Blocked users" });
    await user.type(within(region).getByLabelText("Username to block"), "ghost");
    await user.click(within(region).getByRole("button", { name: "Block" }));
    expect(await within(region).findByRole("alert")).toHaveTextContent("No user is called ghost.");
    expect(api.sent("UserService/BlockUser")).toHaveLength(0);
  });
});

describe("SettingsPage notification toggles", () => {
  it("FR-037: offers an in-app and an email toggle for each of the six kinds", async () => {
    settings();
    const region = await screen.findByRole("region", { name: "Notifications" });
    const labels = [
      "Someone claims a character in your perspective",
      "Your claim is accepted",
      "A new perspective in your arc",
      "A vote to remove opens",
      "The canon of your arc changes",
      "A report you made is resolved",
    ];
    for (const label of labels) {
      expect(await within(region).findByRole("checkbox", { name: `${label}: in app` })).toBeChecked();
      expect(within(region).getByRole("checkbox", { name: `${label}: email` })).toBeChecked();
    }
    expect(within(region).getAllByRole("checkbox")).toHaveLength(12);
  });

  it("turns one toggle off from the keyboard and keeps the server's answer", async () => {
    const { api, user } = settings({
      "NotifyService/UpdatePreferences": () =>
        ok({
          preferences: KINDS.map((kind) => ({ kind, inApp: true, email: kind !== "NOTIFICATION_KIND_CANON_CHANGE" })),
        }),
    });
    const box = await screen.findByRole("checkbox", { name: "The canon of your arc changes: email" });
    box.focus();
    await user.keyboard(" ");
    await waitFor(() => expect(box).not.toBeChecked());
    expect(api.sent("NotifyService/UpdatePreferences")).toEqual([
      { preferences: [{ kind: "NOTIFICATION_KIND_CANON_CHANGE", inApp: true }] },
    ]);
    expect(screen.getByRole("checkbox", { name: "The canon of your arc changes: in app" })).toBeChecked();
  });

  it("reverts and says so when a toggle cannot be saved", async () => {
    const { user } = settings({ "NotifyService/UpdatePreferences": () => fail("unavailable") });
    const box = await screen.findByRole("checkbox", { name: "Your claim is accepted: in app" });
    await user.click(box);
    expect(await screen.findByRole("alert")).toHaveTextContent(/unreachable/);
    expect(box).toBeChecked();
  });
});

describe("SettingsPage delete account", () => {
  it("deletes the account after the password is confirmed, signs out and goes home", async () => {
    const { api, router, user } = settings({ "UserService/DeleteAccount": () => ok() });
    await user.click(await screen.findByRole("button", { name: "Delete account…" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete your account" });
    await user.type(within(dialog).getByLabelText("Password"), "my secret");
    await user.click(within(dialog).getByRole("button", { name: "Delete my account" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(api.sent("UserService/DeleteAccount")).toEqual([{ password: "my secret" }]);
    expect(currentSession()).toBeNull();
    expect(getAccessToken()).toBeNull();
  });

  it("keeps the account and says so when the password is wrong", async () => {
    const { user } = settings({ "UserService/DeleteAccount": () => fail("permission_denied", "wrong password") });
    await user.click(await screen.findByRole("button", { name: "Delete account…" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete your account" });
    await user.type(within(dialog).getByLabelText("Password"), "nope");
    await user.click(within(dialog).getByRole("button", { name: "Delete my account" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("That password is not right.");
    expect(currentSession()?.username).toBe("wren");
  });

  it("closes on Escape without deleting anything", async () => {
    const { api, user } = settings();
    const open = await screen.findByRole("button", { name: "Delete account…" });
    open.focus();
    await user.keyboard("{Enter}");
    await screen.findByRole("dialog", { name: "Delete your account" });
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(open).toHaveFocus();
    expect(api.sent("UserService/DeleteAccount")).toHaveLength(0);
  });
});

describe("SettingsPage layout", () => {
  it("OT-037: fits a 375px viewport with a long email and many blocks", async () => {
    const { view } = settings({
      "UserService/GetProfile": () =>
        ok({
          ...profileBody({ blocked: Array.from({ length: 5 }, (_, i) => ({ userId: `u-${i}`, username: `blocked_user_${i}` })) }),
        }),
    });
    await screen.findByRole("region", { name: "Notifications" });
    await screen.findByRole("checkbox", { name: "Your claim is accepted: email" });
    expectFitsPhone(view.container);
  });

  it("shows an error, not a blank page, when the profile cannot load", async () => {
    settings({ "UserService/GetProfile": () => fail("unavailable") });
    expect(await screen.findByRole("alert")).toHaveTextContent(/unreachable/);
  });
});
