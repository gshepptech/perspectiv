import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { currentSession, getAccessToken, resetSessionForTests } from "@/lib/session";

import { expectFitsPhone, fail, ok, preloadAccountRoutes, renderRoute, sessionBody, type Handlers } from "./harness.testutil";

preloadAccountRoutes();
beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const challenge = () =>
  ok({ twoFactorChallenge: "ch-1", twoFactorChallengeExpiresAt: new Date(Date.now() + 300_000).toISOString() });

const signedIn = () => {
  const body = sessionBody({ token: "after-code", username: "wren" });
  return ok({ ...body, user: { ...body.user, twoFactorEnabled: true } });
};

const inbox = { "NotifyService/ListInbox": () => ok({ notifications: [], unreadCount: 0 }) };

/** Signs in with the password and waits for the second step. */
async function toCodeStep(handlers: Handlers, path = "/login") {
  const r = renderRoute(path, { "AuthService/Login": challenge, ...inbox, ...handlers });
  await r.user.type(await screen.findByLabelText("Email"), "wren@example.com");
  await r.user.type(screen.getByLabelText("Password"), "secret password");
  await r.user.click(screen.getByRole("button", { name: "Sign in" }));
  await waitFor(() => expect(screen.getByRole("heading", { name: "Enter your code" })).toHaveFocus());
  return r;
}

describe("LoginPage second step", () => {
  it("asks for the app's code after a right password, holds no session until it is right, then goes on", async () => {
    const { api, router, user } = await toCodeStep({ "AuthService/CompleteTwoFactorLogin": signedIn }, "/login?next=%2Finbox");
    expect(currentSession()).toBeNull();
    expect(getAccessToken()).toBeNull();
    expect(screen.getByText("Open your authenticator app and enter the six-digit code for Perspectiv.")).toBeInTheDocument();
    const field = screen.getByLabelText("Code from your app");
    expect(field).toHaveAttribute("autocomplete", "one-time-code");
    expect(field).toHaveAttribute("inputmode", "numeric");

    await user.type(field, "123 456");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/inbox"));
    expect(api.sent("AuthService/CompleteTwoFactorLogin")).toEqual([{ challenge: "ch-1", code: "123456" }]);
    expect(getAccessToken()).toBe("after-code");
    expect(currentSession()?.twoFactorEnabled).toBe(true);
    expect(localStorage.length + sessionStorage.length).toBe(0);
  });

  it("checks the six digits before asking the server", async () => {
    const { api, user } = await toCodeStep({});
    await user.type(screen.getByLabelText("Code from your app"), "12345");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("Enter the six digits your app shows.")).toBeInTheDocument();
    expect(api.sent("AuthService/CompleteTwoFactorLogin")).toHaveLength(0);
  });

  it("says a wrong code under the field, and takes a recovery code instead", async () => {
    let tries = 0;
    const { api, router, user } = await toCodeStep({
      "AuthService/CompleteTwoFactorLogin": () => (++tries === 1 ? fail("unauthenticated", "that code is not right") : signedIn()),
    });
    await user.type(screen.getByLabelText("Code from your app"), "000000");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("That code is not right.")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText("Code from your app")).toHaveFocus());
    // Said once, under the field, not again as a form-level alert.
    expect(screen.getAllByRole("alert")).toHaveLength(1);

    const toggle = screen.getByRole("button", { name: "Use a recovery code" });
    await user.click(toggle);
    expect(toggle).toHaveTextContent("Use the code from your app");
    expect(toggle).toHaveFocus();
    const field = screen.getByLabelText("Recovery code");
    expect(field).toHaveAccessibleDescription("Like abcd-efgh-jkmn-pqrs. Each code works once.");
    expect(screen.queryByText("That code is not right.")).not.toBeInTheDocument();
    await user.type(field, "ABCD-EFGH-JKMN-PQRS");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(api.sent("AuthService/CompleteTwoFactorLogin").at(-1)).toEqual({ challenge: "ch-1", code: "ABCD-EFGH-JKMN-PQRS" });
  });

  it("sends the reader back to the password, email kept, when the sign-in timed out", async () => {
    const { user } = await toCodeStep({
      "AuthService/CompleteTwoFactorLogin": () => fail("failed_precondition", "this sign-in has timed out"),
    });
    await user.type(screen.getByLabelText("Code from your app"), "123456");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("That sign-in timed out. Enter your password again.");
    expect(screen.getByLabelText("Email")).toHaveValue("wren@example.com");
    expect(screen.getByLabelText("Password")).toHaveValue("");
  });

  it("starts again from the password on request", async () => {
    const { user } = await toCodeStep({});
    await user.click(screen.getByRole("button", { name: "Start again" }));
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveValue("wren@example.com");
  });

  it("explains the two-factor lockout", async () => {
    const { user } = await toCodeStep({ "AuthService/CompleteTwoFactorLogin": () => fail("resource_exhausted") });
    await user.type(screen.getByLabelText("Code from your app"), "123456");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Too many wrong codes. Two-factor sign-in for this account is paused; try again later.",
    );
  });

  it("is reachable by keyboard: the field, Sign in, then the two text buttons", async () => {
    const { user } = await toCodeStep({});
    await user.tab();
    expect(screen.getByLabelText("Code from your app")).toHaveFocus();
    await user.tab();
    expect(within(screen.getByRole("main")).getByRole("button", { name: "Sign in" })).toHaveFocus();
    await user.tab();
    expect(screen.getByRole("button", { name: "Use a recovery code" })).toHaveFocus();
    await user.tab();
    expect(screen.getByRole("button", { name: "Start again" })).toHaveFocus();
  });

  it("fits a 375px viewport", async () => {
    const { view } = await toCodeStep({});
    expectFitsPhone(view.container);
  });
});
