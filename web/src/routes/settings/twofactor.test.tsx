import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { currentSession, resetSessionForTests } from "@/lib/session";
import { expectFitsPhone, fail, ok, preloadAccountRoutes, renderRoute, sessionBody, USER_ID, type Handlers } from "@/routes/auth/harness.testutil";

preloadAccountRoutes();
beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const URI = "otpauth://totp/Perspectiv:wren?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=Perspectiv&algorithm=SHA1&digits=6&period=30";
const SECRET = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
const CODES = Array.from({ length: 10 }, (_, i) => `abcd-efgh-jkmn-pq${String(i).padStart(2, "0")}`);

function profileBody() {
  return {
    profile: { userId: USER_ID, username: "wren", bio: "", avatarUrl: "", joinedAt: "2026-09-01T10:00:00Z" },
    account: { email: "wren@example.com", emailVerified: true, role: "ROLE_USER", blocked: [] },
  };
}

const off = () => ok({ enabled: false, recoveryCodesLeft: 0 });
const on = (left = 10) => ok({ enabled: true, enabledAt: "2026-10-03T10:00:00Z", recoveryCodesLeft: left });

/** Settings with a two-factor status that the test can change between reads. */
function settings(handlers: Handlers = {}) {
  return renderRoute(
    "/settings",
    {
      "UserService/GetProfile": () => ok(profileBody()),
      "NotifyService/GetPreferences": () => ok({ preferences: [] }),
      "AuthService/ListSessions": () => ok({ sessions: [] }),
      "UserService/ListFollowedWriters": () => ok({ writers: [] }),
      "AuthService/GetTwoFactorStatus": off,
      ...handlers,
    },
    { signedIn: true },
  );
}

async function block() {
  return screen.findByRole("group", { name: "Two-factor sign-in" });
}

describe("Settings two-factor sign-in", () => {
  it("turns on: password, then the QR code drawn in the page and a code, then the recovery codes once", async () => {
    let enabled = false;
    const { api, user } = settings({
      "AuthService/GetTwoFactorStatus": () => (enabled ? on() : off()),
      "AuthService/BeginTwoFactorSetup": (body) =>
        body.currentPassword === "right password" ? ok({ secret: SECRET, otpauthUri: URI }) : fail("permission_denied"),
      "AuthService/ConfirmTwoFactorSetup": (body) => {
        if (body.code !== "123456") return fail("invalid_argument", "that code is not right");
        enabled = true;
        return ok({ recoveryCodes: CODES });
      },
    });
    const twoFactor = await block();
    expect(await within(twoFactor).findByText(/^Off\. With it on, signing in asks for a code/)).toBeInTheDocument();
    await user.click(within(twoFactor).getByRole("button", { name: "Turn on two-factor sign-in…" }));

    // Step 1: the password.
    await user.type(within(twoFactor).getByLabelText("Current password"), "wrong password");
    await user.click(within(twoFactor).getByRole("button", { name: "Continue" }));
    expect(await within(twoFactor).findByText("Your current password is not right.")).toBeInTheDocument();
    await user.clear(within(twoFactor).getByLabelText("Current password"));
    await user.type(within(twoFactor).getByLabelText("Current password"), "right password");
    await user.click(within(twoFactor).getByRole("button", { name: "Continue" }));

    // Step 2: the QR code is an SVG drawn here from the URI; nothing else is fetched.
    const qr = await within(twoFactor).findByRole("img", { name: "QR code for adding Perspectiv to your authenticator app" });
    expect(qr.tagName.toLowerCase()).toBe("svg");
    expect(within(twoFactor).getByText("Add Perspectiv to your authenticator app")).toHaveFocus();
    expect(within(twoFactor).getByText("JBSW Y3DP EHPK 3PXP JBSW Y3DP EHPK 3PXP")).toBeInTheDocument();
    expect(api.calls.every((c) => c.procedure.includes("/"))).toBe(true);

    await user.type(within(twoFactor).getByLabelText("Code from your app"), "000000");
    await user.click(within(twoFactor).getByRole("button", { name: "Turn on" }));
    expect(
      await within(twoFactor).findByText("That code is not right. Check that your phone's clock is set automatically."),
    ).toBeInTheDocument();
    await user.clear(within(twoFactor).getByLabelText("Code from your app"));
    await user.type(within(twoFactor).getByLabelText("Code from your app"), "123456");
    await user.click(within(twoFactor).getByRole("button", { name: "Turn on" }));

    // Step 3: the codes, focused, with a file to keep.
    const heading = await within(twoFactor).findByRole("heading", { name: "Save your recovery codes" });
    await waitFor(() => expect(heading).toHaveFocus());
    expect(within(twoFactor).getByText("This is the only time we show them.", { exact: false })).toBeInTheDocument();
    const list = within(twoFactor).getByRole("list", { name: "Recovery codes" });
    expect(within(list).getAllByRole("listitem").map((li) => li.textContent)).toEqual(CODES);
    expect(currentSession()?.twoFactorEnabled).toBe(true);

    const created: Blob[] = [];
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: (b: Blob) => (created.push(b), "blob:codes"), revokeObjectURL: () => {} }));
    const clicked = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    await user.click(within(twoFactor).getByRole("button", { name: "Download as a text file" }));
    expect(clicked).toHaveBeenCalledTimes(1);
    const text = await created[0]!.text();
    expect(text).toContain("Perspectiv recovery codes for wren");
    for (const c of CODES) expect(text).toContain(`\n${c}`);
    clicked.mockRestore();

    await user.click(within(twoFactor).getByRole("button", { name: "I've saved my codes" }));
    expect(await within(twoFactor).findByText(/^On since/)).toBeInTheDocument();
    expect(within(twoFactor).getByText("10 recovery codes left.")).toBeInTheDocument();
    expect(within(twoFactor).queryByRole("list", { name: "Recovery codes" })).not.toBeInTheDocument();
    expect(api.sent("AuthService/BeginTwoFactorSetup").at(-1)).toEqual({ currentPassword: "right password" });
    expect(api.sent("AuthService/ConfirmTwoFactorSetup").at(-1)).toEqual({ code: "123456" });
  });

  it("warns when few recovery codes are left", async () => {
    settings({ "AuthService/GetTwoFactorStatus": () => on(2) });
    const twoFactor = await block();
    expect(await within(twoFactor).findByText("2 recovery codes left. Make new ones soon.")).toHaveClass("text-warning");
  });

  it("turns off behind the password and a code, saying a wrong code under its field", async () => {
    let enabled = true;
    const { api, user } = settings({
      "AuthService/GetTwoFactorStatus": () => (enabled ? on() : off()),
      "AuthService/DisableTwoFactor": (body) => {
        if (body.currentPassword !== "right password") return fail("permission_denied");
        if (body.code !== "654321") return fail("invalid_argument", "that code is not right");
        enabled = false;
        return ok({});
      },
    });
    const twoFactor = await block();
    await user.click(await within(twoFactor).findByRole("button", { name: "Turn off…" }));
    const dialog = await screen.findByRole("dialog", { name: "Turn off two-factor sign-in?" });
    await user.type(within(dialog).getByLabelText("Current password"), "right password");
    await user.type(within(dialog).getByLabelText("Code from your app or a recovery code"), "111111");
    await user.click(within(dialog).getByRole("button", { name: "Turn off" }));
    expect(await within(dialog).findByText("That code is not right.")).toBeInTheDocument();
    await user.clear(within(dialog).getByLabelText("Code from your app or a recovery code"));
    await user.type(within(dialog).getByLabelText("Code from your app or a recovery code"), "654321");
    await user.click(within(dialog).getByRole("button", { name: "Turn off" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(await within(twoFactor).findByRole("status")).toHaveTextContent("Two-factor sign-in is off.");
    expect(await within(twoFactor).findByRole("button", { name: "Turn on two-factor sign-in…" })).toBeInTheDocument();
    expect(api.sent("AuthService/DisableTwoFactor").at(-1)).toEqual({ currentPassword: "right password", code: "654321" });
    expect(currentSession()?.twoFactorEnabled).toBe(false);
  });

  it("makes new recovery codes and shows them once", async () => {
    const fresh = CODES.map((c) => c.replace("abcd", "wxyz"));
    const { user } = settings({
      "AuthService/GetTwoFactorStatus": () => on(1),
      "AuthService/RegenerateRecoveryCodes": () => ok({ recoveryCodes: fresh }),
    });
    const twoFactor = await block();
    await user.click(await within(twoFactor).findByRole("button", { name: "Make new recovery codes…" }));
    const dialog = await screen.findByRole("dialog", { name: "Make new recovery codes?" });
    expect(dialog).toHaveTextContent("Your old codes stop working.");
    await user.type(within(dialog).getByLabelText("Current password"), "right password");
    await user.type(within(dialog).getByLabelText("Code from your app or a recovery code"), "abcd-efgh-jkmn-pq01");
    await user.click(within(dialog).getByRole("button", { name: "Make new codes" }));
    const list = await within(twoFactor).findByRole("list", { name: "Recovery codes" });
    expect(within(list).getAllByRole("listitem").map((li) => li.textContent)).toEqual(fresh);
  });

  it("says how a member who lost everything gets back in", async () => {
    settings();
    expect(await block()).toHaveTextContent(/Lost your phone and every recovery code\? Write to us from this account's email address/);
  });

  it("OT-037: fits a 375px viewport through the QR step", async () => {
    const { user, view } = settings({ "AuthService/BeginTwoFactorSetup": () => ok({ secret: SECRET, otpauthUri: URI }) });
    const twoFactor = await block();
    await user.click(await within(twoFactor).findByRole("button", { name: "Turn on two-factor sign-in…" }));
    await user.type(within(twoFactor).getByLabelText("Current password"), "right password");
    await user.click(within(twoFactor).getByRole("button", { name: "Continue" }));
    await within(twoFactor).findByRole("img", { name: /QR code/ });
    expectFitsPhone(view.container);
  });
});

describe("admin two-factor notice", () => {
  const as = (role: string, twoFactorEnabled: boolean) => () => {
    const body = sessionBody({ username: "warden" });
    return ok({ ...body, user: { ...body.user, role, twoFactorEnabled } });
  };

  it("asks an admin without two-factor sign-in to turn it on, on every page, with no way to dismiss it", async () => {
    renderRoute("/settings", { "AuthService/Refresh": as("ROLE_ADMIN", false), ...baseSettings() });
    const notice = await screen.findByRole("region", { name: "Account security notice" });
    expect(notice).toHaveTextContent("Your admin account has no two-factor sign-in.");
    expect(within(notice).getByRole("link", { name: "Turn it on in Settings" })).toHaveAttribute("href", "/settings#two-factor");
    expect(within(notice).queryByRole("button")).not.toBeInTheDocument();
  });

  for (const [who, role, enabled] of [
    ["an admin with two-factor sign-in", "ROLE_ADMIN", true],
    ["a member", "ROLE_USER", false],
  ] as const) {
    it(`is not shown to ${who}`, async () => {
      renderRoute("/settings", { "AuthService/Refresh": as(role, enabled), ...baseSettings() });
      await screen.findByRole("heading", { level: 1, name: "Settings" });
      expect(screen.queryByRole("region", { name: "Account security notice" })).not.toBeInTheDocument();
    });
  }

  function baseSettings(): Handlers {
    return {
      "UserService/GetProfile": () => ok(profileBody()),
      "NotifyService/GetPreferences": () => ok({ preferences: [] }),
      "AuthService/ListSessions": () => ok({ sessions: [] }),
      "UserService/ListFollowedWriters": () => ok({ writers: [] }),
      "AuthService/GetTwoFactorStatus": off,
      "NotifyService/ListInbox": () => ok({ notifications: [], unreadCount: 0 }),
      "LoreService/ListClaims": () => ok({ claims: [] }),
    };
  }
});
