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
  "NOTIFICATION_KIND_INVITE_ANSWERED",
  "NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE",
  "NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER",
  "NOTIFICATION_KIND_THREAD_REPLY",
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

const laptop = { sessionId: "s-1", device: "Firefox on macOS", signedInAt: "2026-10-20T10:00:00Z", lastUsedAt: "2026-10-30T10:00:00Z", current: true };
const phone = { sessionId: "s-2", device: "Safari on iPhone", signedInAt: "2026-10-02T10:00:00Z", lastUsedAt: "2026-10-28T10:00:00Z", current: false };

const allOn = () => ok({ preferences: KINDS.map((kind) => ({ kind, inApp: true, email: true })) });

function settings(handlers: Handlers = {}) {
  return renderRoute(
    "/settings",
    {
      "UserService/GetProfile": (body) => (body.username ? fail("not_found") : ok(profileBody())),
      "NotifyService/GetPreferences": allOn,
      "AuthService/ListSessions": () => ok({ sessions: [laptop] }),
      "UserService/ListFollowedWriters": () => ok({ writers: [] }),
      "AuthService/GetTwoFactorStatus": () => ok({ enabled: false, recoveryCodesLeft: 0 }),
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
    expect(await screen.findByRole("alert")).toHaveTextContent("Avatar must be under 1 MB.");
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
    expect(await screen.findByRole("alert")).toHaveTextContent("You can change your username once every 30 days.");
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

describe("SettingsPage writers you follow", () => {
  it("lists followed writers, newest first, each linking to their profile, and unfollows one", async () => {
    let writers = [
      { userId: "u-5", username: "otter", avatarUrl: "/avatars/otter.png", followedAt: "2026-10-29T10:00:00Z" },
      { userId: "u-6", username: "heron", avatarUrl: "", followedAt: "2026-10-01T10:00:00Z" },
    ];
    const { api, user } = settings({
      "UserService/ListFollowedWriters": () => ok({ writers }),
      "UserService/FollowUser": (body) => {
        writers = writers.filter((w) => w.username !== body.username);
        return ok({ following: false });
      },
    });
    const region = await screen.findByRole("region", { name: "Writers you follow" });
    const links = await within(region).findAllByRole("link");
    expect(links.map((l) => [l.textContent, l.getAttribute("href")])).toEqual([
      ["otter", "/users/otter"],
      ["heron", "/users/heron"],
    ]);
    await user.click(within(region).getByRole("button", { name: "Unfollow otter" }));
    await waitFor(() => expect(within(region).queryByText("otter")).not.toBeInTheDocument());
    expect(within(region).getByText("heron")).toBeInTheDocument();
    expect(api.sent("UserService/FollowUser")).toEqual([{ username: "otter" }]);
    // The button that had focus left with its row: focus is on the line that says so.
    const said = within(region).getByRole("status");
    expect(said).toHaveTextContent("You no longer follow otter.");
    expect(said).toHaveFocus();
  });

  it("says when the member follows no one", async () => {
    settings();
    const region = await screen.findByRole("region", { name: "Writers you follow" });
    expect(await within(region).findByText("You do not follow any writers yet.")).toBeInTheDocument();
  });

  it("sits just above Blocked users", async () => {
    settings();
    await screen.findByRole("region", { name: "Writers you follow" });
    const names = screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent);
    expect(names.indexOf("Blocked users") - names.indexOf("Writers you follow")).toBe(1);
  });
});

describe("SettingsPage notification toggles", () => {
  it("offers an in-app and an email toggle for each kind", async () => {
    settings();
    const region = await screen.findByRole("region", { name: "Notifications" });
    const labels = [
      "Someone claims a character in your story",
      "Your claim is accepted",
      "A new story on your day",
      "A vote to remove opens",
      "The canon of your day changes",
      "A report you made is resolved",
      "Someone you invited writes their side",
      "A new story at a place you follow",
      "A new story by a writer you follow",
      "Someone replies to your post in a lore's thread",
    ];
    for (const label of labels) {
      expect(await within(region).findByRole("checkbox", { name: `${label}: in app` })).toBeChecked();
      expect(within(region).getByRole("checkbox", { name: `${label}: email` })).toBeChecked();
    }
    // Two per kind, and the daily-summary switch.
    expect(within(region).getAllByRole("checkbox")).toHaveLength(labels.length * 2 + 1);
  });

  it("turns one toggle off from the keyboard and keeps the server's answer", async () => {
    const { api, user } = settings({
      "NotifyService/UpdatePreferences": () =>
        ok({
          preferences: KINDS.map((kind) => ({ kind, inApp: true, email: kind !== "NOTIFICATION_KIND_CANON_CHANGE" })),
        }),
    });
    const box = await screen.findByRole("checkbox", { name: "The canon of your day changes: email" });
    box.focus();
    await user.keyboard(" ");
    await waitFor(() => expect(box).not.toBeChecked());
    expect(api.sent("NotifyService/UpdatePreferences")).toEqual([
      { preferences: [{ kind: "NOTIFICATION_KIND_CANON_CHANGE", inApp: true }] },
    ]);
    expect(screen.getByRole("checkbox", { name: "The canon of your day changes: in app" })).toBeChecked();
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

describe("SettingsPage sign-in and security", () => {
  it("changes the password with the current one and says other devices are signed out", async () => {
    const { api, user } = settings({ "AuthService/ChangePassword": () => ok() });
    const form = await screen.findByRole("form", { name: "Change password" });
    await user.type(within(form).getByLabelText("Current password"), "old secret here");
    await user.type(within(form).getByLabelText("New password"), "a new long secret");
    await user.type(within(form).getByLabelText("Repeat new password"), "a new long secret");
    await user.click(within(form).getByRole("button", { name: "Change password" }));
    expect(await within(form).findByRole("status")).toHaveTextContent("Every other device is signed out.");
    expect(api.sent("AuthService/ChangePassword")).toEqual([{ currentPassword: "old secret here", newPassword: "a new long secret" }]);
    expect(within(form).getByLabelText("Current password")).toHaveValue("");
  });

  it("refuses differing new passwords before asking the server", async () => {
    const { api, user } = settings();
    const form = await screen.findByRole("form", { name: "Change password" });
    await user.type(within(form).getByLabelText("Current password"), "old secret here");
    await user.type(within(form).getByLabelText("New password"), "a new long secret");
    await user.type(within(form).getByLabelText("Repeat new password"), "a new long secreT");
    await user.click(within(form).getByRole("button", { name: "Change password" }));
    expect(await within(form).findByText("The two passwords differ.")).toBeInTheDocument();
    expect(api.sent("AuthService/ChangePassword")).toHaveLength(0);
  });

  it("words a wrong current password as a typo", async () => {
    const { user } = settings({ "AuthService/ChangePassword": () => fail("permission_denied", "wrong") });
    const form = await screen.findByRole("form", { name: "Change password" });
    await user.type(within(form).getByLabelText("Current password"), "not it");
    await user.type(within(form).getByLabelText("New password"), "a new long secret");
    await user.type(within(form).getByLabelText("Repeat new password"), "a new long secret");
    await user.click(within(form).getByRole("button", { name: "Change password" }));
    // Under the field it is about, marked invalid, with focus on it; no
    // second, form-level alert repeats it.
    const field = within(form).getByLabelText("Current password");
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(field).toHaveAccessibleDescription("Your current password is not right.");
    expect(field).toHaveFocus();
    expect(within(form).getAllByRole("alert")).toHaveLength(1);
  });

  it("puts a wrong password under the email form's password field too", async () => {
    const { user } = settings({ "AuthService/RequestEmailChange": () => fail("permission_denied", "wrong") });
    const form = await screen.findByRole("form", { name: "Change email" });
    await user.type(within(form).getByLabelText("New email"), "wren@new.example");
    await user.type(within(form).getByLabelText("Current password"), "not it");
    await user.click(within(form).getByRole("button", { name: "Send confirmation link" }));
    const field = within(form).getByLabelText("Current password");
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(field).toHaveAccessibleDescription("Your current password is not right.");
    expect(within(form).getAllByRole("alert")).toHaveLength(1);
  });

  it("clears a success line once the form is edited again", async () => {
    const { user } = settings({ "AuthService/ChangePassword": () => ok() });
    const form = await screen.findByRole("form", { name: "Change password" });
    await user.type(within(form).getByLabelText("Current password"), "old secret here");
    await user.type(within(form).getByLabelText("New password"), "a new long secret");
    await user.type(within(form).getByLabelText("Repeat new password"), "a new long secret");
    await user.click(within(form).getByRole("button", { name: "Change password" }));
    expect(await within(form).findByRole("status")).toHaveTextContent("Your password is changed.");
    await user.type(within(form).getByLabelText("New password"), "x");
    expect(within(form).queryByRole("status")).not.toBeInTheDocument();
    await user.click(within(form).getByRole("button", { name: "Change password" }));
    expect(await within(form).findByText("Enter a password.")).toBeInTheDocument();
    expect(within(form).queryByText(/Your password is changed/)).not.toBeInTheDocument();
  });

  it("sends a confirmation link to the new email and keeps the old one shown", async () => {
    const { api, user } = settings({ "AuthService/RequestEmailChange": () => ok() });
    const form = await screen.findByRole("form", { name: "Change email" });
    expect(form).toHaveTextContent(
      "We email a confirmation link to the new address. Until you follow it, your email stays wren@example.com.",
    );
    await user.type(within(form).getByLabelText("New email"), "wren@new.example");
    await user.type(within(form).getByLabelText("Current password"), "my secret");
    await user.click(within(form).getByRole("button", { name: "Send confirmation link" }));
    expect(await within(form).findByRole("status")).toHaveTextContent("Check wren@new.example for a link");
    expect(api.sent("AuthService/RequestEmailChange")).toEqual([{ newEmail: "wren@new.example", currentPassword: "my secret" }]);
  });

  it("shows the server's reason when the new email is taken", async () => {
    const { user } = settings({
      "AuthService/RequestEmailChange": () => fail("already_exists", "an account with that email already exists"),
    });
    const form = await screen.findByRole("form", { name: "Change email" });
    await user.type(within(form).getByLabelText("New email"), "taken@example.com");
    await user.type(within(form).getByLabelText("Current password"), "my secret");
    await user.click(within(form).getByRole("button", { name: "Send confirmation link" }));
    expect(await within(form).findByRole("alert")).toHaveTextContent("An account with that email already exists.");
  });

  it("signs out everywhere after confirming, and lands on sign in", async () => {
    const { api, router, user } = settings({ "AuthService/SignOutEverywhere": () => ok() });
    await user.click(await screen.findByRole("button", { name: "Sign out everywhere…" }));
    const dialog = await screen.findByRole("dialog", { name: "Sign out everywhere?" });
    await user.click(within(dialog).getByRole("button", { name: "Sign out everywhere" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(await screen.findByRole("status")).toHaveTextContent("You are signed out on every device.");
    expect(api.sent("AuthService/SignOutEverywhere")).toHaveLength(1);
    expect(currentSession()).toBeNull();
    expect(getAccessToken()).toBeNull();
  });
});

describe("SettingsPage layout", () => {
  it("fits a 375px viewport with a long email and many blocks", async () => {
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

describe("SettingsPage your data", () => {
  it("downloads everything held about the account as a JSON file", async () => {
    const created: Blob[] = [];
    vi.stubGlobal("URL", Object.assign(URL, {
      createObjectURL: vi.fn((b: Blob) => {
        created.push(b);
        return "blob:export";
      }),
      revokeObjectURL: vi.fn(),
    }));
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    const { api, user } = settings({ "UserService/ExportMyData": () => ok({ json: '{"account":{"username":"wren"}}' }) });
    const region = await screen.findByRole("region", { name: "Your data" });
    await user.click(within(region).getByRole("button", { name: "Download my data" }));
    expect(await within(region).findByText("Your data was downloaded.")).toBeInTheDocument();
    expect(api.sent("UserService/ExportMyData")).toHaveLength(1);
    expect(click).toHaveBeenCalledTimes(1);
    expect(await created[0]?.text()).toBe('{"account":{"username":"wren"}}');
    click.mockRestore();
  });
});

describe("SettingsPage daily summary", () => {
  it("turns the email digest on and keeps the server's answer", async () => {
    const { api, user } = settings({
      "NotifyService/UpdatePreferences": (body) => ok({ preferences: [], emailDigest: body.emailDigest === true }),
    });
    const region = await screen.findByRole("region", { name: "Notifications" });
    const box = await within(region).findByRole("checkbox", { name: /Send my emails as one daily summary/ });
    expect(box).not.toBeChecked();
    await user.click(box);
    await waitFor(() => expect(box).toBeChecked());
    expect(api.sent("NotifyService/UpdatePreferences")).toEqual([{ emailDigest: true }]);
  });
});

describe("UnsubscribePage", () => {
  it("changes nothing on opening, and stops emails once asked", async () => {
    const { api, user } = renderRoute("/unsubscribe?u=user-1&t=sig", { "NotifyService/Unsubscribe": () => ok() });
    await screen.findByRole("heading", { name: "Stop notification emails?" });
    expect(api.sent("NotifyService/Unsubscribe")).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "Stop emails" }));
    expect(await screen.findByRole("heading", { name: "You will get no more notification emails" })).toHaveFocus();
    expect(api.sent("NotifyService/Unsubscribe")).toEqual([{ userId: "user-1", token: "sig" }]);
  });

  it("points to Settings when the link is tampered with or damaged", async () => {
    const { user } = renderRoute("/unsubscribe?u=user-1&t=bad", {
      "NotifyService/Unsubscribe": () => fail("invalid_argument", "this unsubscribe link is not valid"),
    });
    await user.click(await screen.findByRole("button", { name: "Stop emails" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Sign in and turn emails off in Settings.");
    expect(within(alert).getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/settings");
    // The server said the link is bad; asking again cannot help.
    expect(screen.queryByRole("button", { name: "Stop emails" })).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "This link no longer works" })).toHaveFocus();
  });

  it("asks nothing of the server when the link is incomplete", async () => {
    const { api } = renderRoute("/unsubscribe?u=user-1");
    expect(await screen.findByRole("heading", { name: "This link no longer works" })).toBeInTheDocument();
    expect(api.sent("NotifyService/Unsubscribe")).toHaveLength(0);
  });
});

describe("SettingsPage signed-in devices", () => {
  it("lists each device by browser and system and signs another one out", async () => {
    let signedIn = [laptop, phone];
    const { api, user } = settings({
      "AuthService/ListSessions": () => ok({ sessions: signedIn }),
      "AuthService/RevokeSession": () => {
        signedIn = [laptop];
        return ok();
      },
    });
    const list = await screen.findByRole("list", { name: "Signed-in devices" });
    const [mine, other] = within(list).getAllByRole("listitem");
    expect(mine).toHaveTextContent("Firefox on macOS This device");
    expect(within(mine!).queryByRole("button")).not.toBeInTheDocument();
    expect(other).toHaveTextContent("Safari on iPhone");
    // The name carries the sign-in day, so two rows for one browser differ.
    const signOut = within(other!).getByRole("button", { name: /^Sign out Safari on iPhone, signed in / });
    expect(signOut).toHaveAccessibleName(`Sign out Safari on iPhone, signed in ${new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(phone.signedInAt))}`);
    await user.click(signOut);
    const status = await screen.findByText("Safari on iPhone is signed out.");
    expect(status).toHaveAttribute("role", "status");
    expect(status).toHaveFocus();
    expect(api.sent("AuthService/RevokeSession")).toEqual([{ sessionId: "s-2" }]);
    await waitFor(() => expect(within(list).getAllByRole("listitem")).toHaveLength(1));
  });
});
