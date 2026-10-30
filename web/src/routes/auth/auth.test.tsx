import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { currentSession, getAccessToken, resetSessionForTests } from "@/lib/session";

import { readAfterVerify, rememberAfterVerify } from "./afterVerify";
import { expectFitsPhone, fail, ok, preloadAccountRoutes, renderRoute, sessionBody } from "./harness.testutil";

preloadAccountRoutes();
beforeEach(() => {
  resetSessionForTests();
  localStorage.clear();
});
afterEach(() => vi.unstubAllGlobals());

const inbox = { "NotifyService/ListInbox": () => ok({ notifications: [], unreadCount: 0 }) };

describe("SignUpPage", () => {
  it("AC-040: states in its terms that real names and handles of non-users are prohibited", async () => {
    renderRoute("/signup");
    const terms = await screen.findByRole("region", { name: "Rules for writing here" });
    expect(terms).toHaveTextContent(/real names and handles of people who are not users are prohibited/i);
    expect(terms).toHaveTextContent(/report/i);
  });

  it("AC-003: refuses to submit without the 18-or-over confirmation", async () => {
    const { api, user } = renderRoute("/signup", { "AuthService/SignUp": () => ok({ userId: "u-1" }) });
    await user.type(await screen.findByLabelText("Email"), "wren@example.com");
    await user.type(screen.getByLabelText("Username"), "wren_writes");
    await user.type(screen.getByLabelText("Password"), "a long enough secret");
    await user.click(screen.getByRole("button", { name: "Sign up" }));
    expect(await screen.findByText("You must be 18 or over to sign up.")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /I am 18 or over/ })).toHaveAttribute("aria-invalid", "true");
    expect(api.sent("AuthService/SignUp")).toHaveLength(0);
  });

  it("AC-003: sends the affirmative age confirmation, keyboard only, and asks the user to check their email", async () => {
    const { api, user } = renderRoute("/signup", { "AuthService/SignUp": () => ok({ userId: "u-1" }) });
    await user.click(await screen.findByLabelText("Email"));
    await user.keyboard("wren@example.com");
    await user.tab();
    await user.keyboard("wren_writes");
    await user.tab();
    await user.keyboard("a long enough secret");
    // The terms come before the agreement: their three links, then the box.
    for (const name of ["terms", "privacy notice", "community guidelines"]) {
      await user.tab();
      expect(screen.getByRole("link", { name })).toHaveFocus();
    }
    await user.tab();
    expect(screen.getByRole("checkbox", { name: /I am 18 or over/ })).toHaveFocus();
    await user.keyboard(" ");
    await user.tab();
    expect(screen.getByRole("button", { name: "Sign up" })).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(await screen.findByRole("heading", { name: "Check your email" })).toHaveFocus();
    expect(api.sent("AuthService/SignUp")).toEqual([
      { email: "wren@example.com", username: "wren_writes", password: "a long enough secret", ageConfirmed: true },
    ]);
  });

  it("says the password rule before the form is sent", async () => {
    renderRoute("/signup");
    expect(await screen.findByLabelText("Password")).toHaveAccessibleDescription("At least 10 characters; a common password is refused.");
  });

  it("keeps where the reader was headed through sign-up, for the page the emailed link opens", async () => {
    const { user } = renderRoute(`/signup?next=${encodeURIComponent("/perspectives/p-1?claim=1")}`, {
      "AuthService/SignUp": () => ok({ userId: "u-1", ...sessionBody({ username: "wren_writes" }) }),
    });
    await screen.findByRole("heading", { name: "Sign up" });
    expect(within(screen.getByRole("main")).getByRole("link", { name: "Sign in" })).toHaveAttribute(
      "href",
      "/login?next=%2Fperspectives%2Fp-1%3Fclaim%3D1",
    );
    await user.type(await screen.findByLabelText("Email"), "wren@example.com");
    await user.type(screen.getByLabelText("Username"), "wren_writes");
    await user.type(screen.getByLabelText("Password"), "a long enough secret");
    await user.click(screen.getByRole("checkbox", { name: /I am 18 or over/ }));
    await user.click(screen.getByRole("button", { name: "Sign up" }));
    await screen.findByRole("heading", { name: "Check your email" });
    // Signing up signs the member in, so the way on is the page itself.
    expect(within(screen.getByRole("main")).getByRole("link", { name: "Continue where you were" })).toHaveAttribute(
      "href",
      "/perspectives/p-1?claim=1",
    );
    expect(JSON.parse(localStorage.getItem("perspectiv.afterVerify") ?? "{}")).toMatchObject({ to: "/perspectives/p-1?claim=1" });
    expect(currentSession()?.username).toBe("wren_writes");
  });

  it("keeps nothing for an off-site next", async () => {
    const { user } = renderRoute(`/signup?next=${encodeURIComponent("//evil.example/x")}`, {
      "AuthService/SignUp": () => ok({ userId: "u-1" }),
    });
    await user.type(await screen.findByLabelText("Email"), "wren@example.com");
    await user.type(screen.getByLabelText("Username"), "wren_writes");
    await user.type(screen.getByLabelText("Password"), "a long enough secret");
    await user.click(screen.getByRole("checkbox", { name: /I am 18 or over/ }));
    await user.click(screen.getByRole("button", { name: "Sign up" }));
    await screen.findByRole("heading", { name: "Check your email" });
    expect(localStorage.getItem("perspectiv.afterVerify")).toBeNull();
  });

  it("checks the username rules before asking the server", async () => {
    const { api, user } = renderRoute("/signup");
    await user.type(await screen.findByLabelText("Username"), "Ab");
    await user.click(screen.getByRole("button", { name: "Sign up" }));
    expect(await screen.findByText("Use 3 to 20 letters, digits or underscores.")).toBeInTheDocument();
    await user.clear(screen.getByLabelText("Username"));
    await user.type(screen.getByLabelText("Username"), "has space");
    expect(await screen.findByText("Use 3 to 20 letters, digits or underscores.")).toBeInTheDocument();
    expect(api.sent("AuthService/SignUp")).toHaveLength(0);
  });

  it("shows the server's reason when the username or email is taken", async () => {
    const { user } = renderRoute("/signup", {
      "AuthService/SignUp": () => fail("already_exists", "that username is taken"),
    });
    await user.type(await screen.findByLabelText("Email"), "wren@example.com");
    await user.type(screen.getByLabelText("Username"), "Wren");
    await user.type(screen.getByLabelText("Password"), "a long enough secret");
    await user.click(screen.getByRole("checkbox", { name: /I am 18 or over/ }));
    await user.click(screen.getByRole("button", { name: "Sign up" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("That username is taken.");
  });

  it("OT-037: fits a 375px viewport", async () => {
    const { view } = renderRoute("/signup");
    await screen.findByRole("heading", { name: "Sign up" });
    expectFitsPhone(view.container);
  });
});

describe("LoginPage", () => {
  it("signs in from the keyboard, holds the token in memory and goes to the next page", async () => {
    const { api, router, user } = renderRoute("/login?next=%2Finbox", {
      "AuthService/Login": () => ok(sessionBody({ token: "fresh", username: "wren" })),
      ...inbox,
    });
    await user.click(await screen.findByLabelText("Email"));
    await user.keyboard("wren@example.com");
    await user.tab();
    await user.keyboard("secret password{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/inbox"));
    expect(api.sent("AuthService/Login")).toEqual([{ email: "wren@example.com", password: "secret password" }]);
    expect(getAccessToken()).toBe("fresh");
    expect(currentSession()?.username).toBe("wren");
    expect(localStorage.length + sessionStorage.length).toBe(0);
  });

  for (const next of ["//evil.example/x", "https://evil.example/", "/\\evil.example"]) {
    it(`ignores an off-site next (${next}) and goes home`, async () => {
      const { router, user } = renderRoute(`/login?next=${encodeURIComponent(next)}`, {
        "AuthService/Login": () => ok(sessionBody()),
      });
      await user.type(await screen.findByLabelText("Email"), "wren@example.com");
      await user.type(screen.getByLabelText("Password"), "secret password");
      await user.click(screen.getByRole("button", { name: "Sign in" }));
      await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    });
  }

  it("says the credentials are wrong without saying which", async () => {
    const { user } = renderRoute("/login", { "AuthService/Login": () => fail("unauthenticated") });
    await user.type(await screen.findByLabelText("Email"), "wren@example.com");
    await user.type(screen.getByLabelText("Password"), "nope");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("That email and password do not match an account.");
    expect(currentSession()).toBeNull();
  });

  it("explains the lockout after too many attempts", async () => {
    const { user } = renderRoute("/login", { "AuthService/Login": () => fail("resource_exhausted") });
    await user.type(await screen.findByLabelText("Email"), "wren@example.com");
    await user.type(screen.getByLabelText("Password"), "nope");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/Too many attempts/);
  });

  it("links to password reset and sign up", async () => {
    renderRoute("/login");
    expect(await screen.findByRole("link", { name: "Forgot your password?" })).toHaveAttribute("href", "/forgot-password");
    const main = screen.getByRole("main");
    expect(within(main).getByRole("link", { name: "Create an account" })).toHaveAttribute("href", "/signup");
  });

  it("carries where the reader was headed on to Create an account", async () => {
    renderRoute(`/login?next=${encodeURIComponent("/perspectives/p-1?claim=1")}`);
    const main = await screen.findByRole("main");
    expect(await within(main).findByRole("link", { name: "Create an account" })).toHaveAttribute(
      "href",
      "/signup?next=%2Fperspectives%2Fp-1%3Fclaim%3D1",
    );
  });

  it("OT-037: fits a 375px viewport", async () => {
    const { view } = renderRoute("/login");
    await screen.findByRole("heading", { name: "Sign in" });
    expectFitsPhone(view.container);
  });
});

describe("VerifyEmailPage", () => {
  it("verifies the token from the link once", async () => {
    const { api } = renderRoute("/verify-email?token=abc123", { "AuthService/VerifyEmail": () => ok() });
    expect(await screen.findByRole("heading", { name: "Your email is verified" })).toBeInTheDocument();
    expect(api.sent("AuthService/VerifyEmail")).toEqual([{ token: "abc123" }]);
  });

  it("re-reads a signed-in session from the server so writing unlocks without a reload", async () => {
    let verified = false;
    const { api } = renderRoute("/verify-email?token=abc123", {
      "AuthService/Refresh": () => ok(sessionBody({ emailVerified: verified })),
      "AuthService/VerifyEmail": () => {
        verified = true;
        return ok();
      },
    });
    await screen.findByRole("heading", { name: "Your email is verified" });
    expect(currentSession()?.emailVerified).toBe(true);
    // The shell's unread badge reads the inbox too; that is not the verify flow.
    const flow = api.calls.map((c) => c.procedure).filter((p) => p.startsWith("AuthService/"));
    expect(flow).toEqual(["AuthService/Refresh", "AuthService/VerifyEmail", "AuthService/Refresh"]);
  });

  it("offers to continue where the reader was, through sign-in when this tab is signed out", async () => {
    localStorage.setItem("perspectiv.afterVerify", JSON.stringify({ to: "/perspectives/p-1?claim=1", at: Date.now() }));
    renderRoute("/verify-email?token=abc123", { "AuthService/VerifyEmail": () => ok() });
    await screen.findByRole("heading", { name: "Your email is verified" });
    expect(screen.getByRole("link", { name: "Continue where you were" })).toHaveAttribute(
      "href",
      "/login?next=%2Fperspectives%2Fp-1%3Fclaim%3D1",
    );
    // Offered once: it is given up once the email is verified.
    expect(localStorage.getItem("perspectiv.afterVerify")).toBeNull();
  });

  it("continues straight there when this tab is signed in", async () => {
    localStorage.setItem("perspectiv.afterVerify", JSON.stringify({ to: "/arcs/a-1", at: Date.now() }));
    renderRoute("/verify-email?token=abc123", { "AuthService/VerifyEmail": () => ok(), ...inbox }, { signedIn: true });
    await screen.findByRole("heading", { name: "Your email is verified" });
    expect(screen.getByRole("link", { name: "Continue where you were" })).toHaveAttribute("href", "/arcs/a-1");
  });

  it("ignores a kept path that is off-site", async () => {
    localStorage.setItem("perspectiv.afterVerify", JSON.stringify({ to: "//evil.example/", at: Date.now() }));
    renderRoute("/verify-email?token=abc123", { "AuthService/VerifyEmail": () => ok() });
    await screen.findByRole("heading", { name: "Your email is verified" });
    expect(screen.queryByRole("link", { name: "Continue where you were" })).toBeNull();
    expect(within(screen.getByRole("main")).getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
  });

  it("explains an expired or used link and offers a way on", async () => {
    renderRoute("/verify-email?token=old", { "AuthService/VerifyEmail": () => fail("invalid_argument", "token expired") });
    expect(await screen.findByRole("heading", { name: "This link no longer works" })).toBeInTheDocument();
    expect(screen.getByRole("main")).toHaveTextContent(/expired or was already used/);
  });

  it("asks nothing of the server when the link has no token", async () => {
    const { api } = renderRoute("/verify-email");
    expect(await screen.findByRole("heading", { name: "This link no longer works" })).toBeInTheDocument();
    expect(api.sent("AuthService/VerifyEmail")).toHaveLength(0);
  });

  it("shows the route error when the server is unreachable", async () => {
    renderRoute("/verify-email?token=abc", { "AuthService/VerifyEmail": () => fail("unavailable") });
    expect(await screen.findByRole("heading", { name: "Perspectiv is unreachable" })).toBeInTheDocument();
  });
});

describe("the path kept for the verify page", () => {
  it("lapses after a week and is read back otherwise", () => {
    const now = Date.parse("2026-10-20T12:00:00Z");
    rememberAfterVerify("/arcs/a-1", now);
    expect(readAfterVerify({ now: now + 6 * 24 * 3600_000 })).toBe("/arcs/a-1");
    expect(readAfterVerify({ now: now + 8 * 24 * 3600_000 })).toBeNull();
  });

  it("keeps nothing for the home page and survives a browser that refuses storage", () => {
    rememberAfterVerify("/");
    expect(localStorage.getItem("perspectiv.afterVerify")).toBeNull();
    const refuse = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(readAfterVerify()).toBeNull();
    refuse.mockRestore();
  });
});

describe("ConfirmEmailPage", () => {
  it("confirms the new email from the link once", async () => {
    const { api } = renderRoute("/confirm-email?token=abc123", { "AuthService/ConfirmEmailChange": () => ok() });
    expect(await screen.findByRole("heading", { name: "Your email is changed" })).toBeInTheDocument();
    expect(api.sent("AuthService/ConfirmEmailChange")).toEqual([{ token: "abc123" }]);
  });

  it("says plainly when another account took the address first", async () => {
    renderRoute("/confirm-email?token=abc", {
      "AuthService/ConfirmEmailChange": () => fail("already_exists", "an account with that email already exists"),
    });
    expect(await screen.findByRole("heading", { name: "This link no longer works" })).toBeInTheDocument();
    expect(screen.getByRole("main")).toHaveTextContent(/Another account started using that address/);
  });

  it("explains an expired, used or replaced link", async () => {
    renderRoute("/confirm-email?token=old", { "AuthService/ConfirmEmailChange": () => fail("invalid_argument", "expired") });
    expect(await screen.findByRole("heading", { name: "This link no longer works" })).toBeInTheDocument();
    expect(screen.getByRole("main")).toHaveTextContent(/a newer change replaced it/);
    // A used link may have worked the first time, so the page never claims
    // the email was left alone; it points to where the current one shows.
    expect(screen.getByRole("main")).not.toHaveTextContent(/not changed/);
    expect(screen.getByRole("main")).toHaveTextContent("Settings shows the email your account uses now.");
    expect(within(screen.getByRole("main")).getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login?next=%2Fsettings");
  });

  it("asks nothing of the server when the link has no token", async () => {
    const { api } = renderRoute("/confirm-email");
    expect(await screen.findByRole("heading", { name: "This link no longer works" })).toBeInTheDocument();
    expect(api.sent("AuthService/ConfirmEmailChange")).toHaveLength(0);
  });
});

describe("ForgotPasswordPage", () => {
  it("sends the email and answers the same way whether or not an account exists", async () => {
    const { api, user } = renderRoute("/forgot-password", { "AuthService/RequestPasswordReset": () => ok() });
    await user.type(await screen.findByLabelText("Email"), "wren@example.com");
    await user.keyboard("{Enter}");
    expect(await screen.findByRole("heading", { name: "Check your email" })).toBeInTheDocument();
    expect(screen.getByRole("main")).toHaveTextContent(/If an account uses wren@example.com/);
    expect(api.sent("AuthService/RequestPasswordReset")).toEqual([{ email: "wren@example.com" }]);
  });

  it("rejects a malformed email before asking the server", async () => {
    const { api, user } = renderRoute("/forgot-password");
    await user.type(await screen.findByLabelText("Email"), "not-an-email");
    await user.keyboard("{Enter}");
    expect(await screen.findByText("Enter a valid email address.")).toBeInTheDocument();
    expect(api.sent("AuthService/RequestPasswordReset")).toHaveLength(0);
  });
});

describe("ResetPasswordPage", () => {
  it("sets the new password with the token from the link", async () => {
    const { api, user } = renderRoute("/reset-password?token=t-9", { "AuthService/ResetPassword": () => ok() });
    await user.type(await screen.findByLabelText("New password"), "another long secret");
    await user.type(screen.getByLabelText("Repeat new password"), "another long secret");
    await user.click(screen.getByRole("button", { name: "Set new password" }));
    expect(await screen.findByRole("heading", { name: "Your password is changed" })).toBeInTheDocument();
    expect(within(screen.getByRole("main")).getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
    expect(api.sent("AuthService/ResetPassword")).toEqual([{ token: "t-9", newPassword: "another long secret" }]);
  });

  it("says the password rule before the form is sent", async () => {
    renderRoute("/reset-password?token=t-9");
    expect(await screen.findByLabelText("New password")).toHaveAccessibleDescription(
      "At least 10 characters; a common password is refused.",
    );
  });

  it("catches a mistyped repeat before asking the server", async () => {
    const { api, user } = renderRoute("/reset-password?token=t-9");
    await user.type(await screen.findByLabelText("New password"), "another long secret");
    await user.type(screen.getByLabelText("Repeat new password"), "another long secreT");
    await user.click(screen.getByRole("button", { name: "Set new password" }));
    expect(await screen.findByText("The two passwords differ.")).toBeInTheDocument();
    expect(api.sent("AuthService/ResetPassword")).toHaveLength(0);
  });

  it("shows the server's reason for a weak password or a dead token", async () => {
    const { user } = renderRoute("/reset-password?token=t-9", {
      "AuthService/ResetPassword": () => fail("invalid_argument", "password must be at least 10 characters"),
    });
    await user.type(await screen.findByLabelText("New password"), "short");
    await user.type(screen.getByLabelText("Repeat new password"), "short");
    await user.click(screen.getByRole("button", { name: "Set new password" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Password must be at least 10 characters.");
  });

  it("without a token, sends the reader to ask for a new link", async () => {
    renderRoute("/reset-password");
    expect(await screen.findByRole("link", { name: "Ask for a new link" })).toHaveAttribute("href", "/forgot-password");
    expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();
  });

  it("OT-037: fits a 375px viewport", async () => {
    const { view } = renderRoute("/reset-password?token=t");
    await screen.findByLabelText("New password");
    expectFitsPhone(view.container);
  });
});

describe("sign up during the invite-only beta", () => {
  const beta = { "AuthService/GetSignUpInfo": () => ok({ inviteOnly: true }) };

  async function fillRest(user: ReturnType<typeof renderRoute>["user"]) {
    await user.type(screen.getByLabelText("Email"), "wren@example.com");
    await user.type(screen.getByLabelText("Username"), "wren_writes");
    await user.type(screen.getByLabelText("Password"), "a long enough secret");
    await user.click(screen.getByRole("checkbox", { name: /I am 18 or over/ }));
  }

  it("asks for the invite code from the link and sends it", async () => {
    const { api, user } = renderRoute("/signup?code=ucf-launch", { ...beta, "AuthService/SignUp": () => ok({ userId: "u-1" }) });
    const code = await screen.findByRole("textbox", { name: "Invite code" });
    expect(code).toHaveValue("ucf-launch");
    expect(screen.getByText(/invite-only beta/)).toBeInTheDocument();
    await fillRest(user);
    await user.click(screen.getByRole("button", { name: "Sign up" }));
    expect(await screen.findByRole("heading", { name: "Check your email" })).toBeInTheDocument();
    expect(api.sent("AuthService/SignUp")[0]).toMatchObject({ accessCode: "ucf-launch", username: "wren_writes" });
  });

  it("will not send without a code, and shows the server's reason for a bad one", async () => {
    const { api, user } = renderRoute("/signup", {
      ...beta,
      "AuthService/SignUp": () => fail("permission_denied", "Perspectiv is invite-only for now: that code is not valid. Join the waitlist and we will email you."),
    });
    await screen.findByRole("textbox", { name: "Invite code" });
    await fillRest(user);
    await user.click(screen.getByRole("button", { name: "Sign up" }));
    expect(await screen.findByText("Enter your invite code.")).toBeInTheDocument();
    expect(api.sent("AuthService/SignUp")).toHaveLength(0);
    await user.type(screen.getByRole("textbox", { name: "Invite code" }), "nope");
    await user.click(screen.getByRole("button", { name: "Sign up" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("that code is not valid");
  });

  it("lets someone without a code join the waitlist", async () => {
    const { api, user } = renderRoute("/signup", { ...beta, "AuthService/JoinWaitlist": () => ok({}) });
    await user.click(await screen.findByRole("button", { name: "Join the waitlist" }));
    const panel = screen.getByRole("region", { name: "Join the waitlist" });
    await user.type(within(panel).getByLabelText("Email"), "pat@example.com");
    await user.type(within(panel).getByLabelText(/Where would you write about/), "  UCF games  ");
    await user.click(within(panel).getByRole("button", { name: "Request an invite" }));
    expect(await screen.findByRole("status")).toHaveTextContent("You're on the list.");
    expect(api.sent("AuthService/JoinWaitlist")).toEqual([{ email: "pat@example.com", note: "UCF games" }]);
  });

  it("shows none of this while sign-up is open", async () => {
    renderRoute("/signup", { "AuthService/GetSignUpInfo": () => ok({ inviteOnly: false }) });
    await screen.findByLabelText("Email");
    expect(screen.queryByRole("textbox", { name: "Invite code" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Join the waitlist" })).toBeNull();
  });
});

describe("sign-up honeypot", () => {
  it("is hidden from people: not in the accessibility tree and not in the tab order", async () => {
    renderRoute("/signup");
    await screen.findByLabelText("Email");
    expect(screen.queryByRole("textbox", { name: "Website" })).toBeNull();
    const field = document.querySelector<HTMLInputElement>('input[name="website"]');
    expect(field).not.toBeNull();
    expect(field?.tabIndex).toBe(-1);
  });
});
