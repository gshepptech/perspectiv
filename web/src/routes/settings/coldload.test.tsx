import { screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";
import { ok, preloadAccountRoutes, renderRoute, USER_ID } from "@/routes/auth/harness.testutil";

// A page module's first import is a cold Vite transform. On a loaded host it
// took longer than findBy's 1000 ms, so the first test in a file timed out
// before the lazily loaded page rendered. This stands in for that host by
// holding the settings module's first load for 1500 ms.
// The page has no loader export; the router reads one, and a vi.mock module
// throws on any export its factory did not return, so it is named as absent.
vi.mock("@/routes/settings/SettingsPage", async (importOriginal) => {
  await new Promise((resolve) => setTimeout(resolve, 1500));
  return { loader: undefined, ...(await importOriginal<object>()) };
});

preloadAccountRoutes();
beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

describe("SettingsPage cold load", () => {
  it("renders the first test's page although its module loads slower than a findBy timeout", async () => {
    renderRoute(
      "/settings",
      {
        "UserService/GetProfile": () =>
          ok({
            profile: { userId: USER_ID, username: "wren", bio: "", avatarUrl: "", joinedAt: "2026-09-01T10:00:00Z" },
            account: { email: "wren@example.com", emailVerified: true, role: "ROLE_USER", blocked: [] },
          }),
        "NotifyService/GetPreferences": () => ok({ preferences: [] }),
      },
      { signedIn: true },
    );
    expect(await screen.findByRole("region", { name: "Account" })).toHaveTextContent("wren@example.com");
  });
});
