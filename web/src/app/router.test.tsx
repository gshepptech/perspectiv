import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider, useLoaderData, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { connectError, installFakeApi, sessionReply, type FakeHandler } from "@/lib/fakeApi.testutil";
import { currentSession, resetSessionForTests } from "@/lib/session";

import { createQueryClient } from "./queryClient";
import { buildRoutes, globPageLoader, pages, type PageLoader, type RouteModule } from "./router";

beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

function stubPage(module: string): RouteModule {
  function Component() {
    const location = useLocation();
    return (
      <p data-testid="page">
        {module} at {location.pathname}
        {location.search}
      </p>
    );
  }
  return { Component };
}

function renderAt(path: string, opts: { refresh?: FakeHandler; load?: PageLoader; extra?: Record<string, FakeHandler> } = {}) {
  const requested: string[] = [];
  const load: PageLoader =
    opts.load ??
    (async (module) => {
      requested.push(module);
      return stubPage(module);
    });
  const api = installFakeApi({
    "AuthService/Refresh": opts.refresh ?? (() => connectError("unauthenticated")),
    ...opts.extra,
  });
  const router = createMemoryRouter(buildRoutes(load), { initialEntries: [path] });
  // The shell reads the unread count, as main.tsx provides a query client for.
  const client = createQueryClient();
  client.setDefaultOptions({ queries: { retry: false } });
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { router, requested, api };
}

const signedIn = (role = "ROLE_USER") => () => sessionReply({ token: "tok", username: "wren", role });

// The contracted route table: path -> module under src/routes.
const contract: [string, string][] = [
  ["/", "landing/LandingPage"],
  ["/places/p-1", "place/PlacePage"],
  ["/perspectives/x-1", "read/ReadPage"],
  ["/arcs/a-1", "arc/ArcPage"],
  ["/users/wren", "profile/ProfilePage"],
  ["/i/abcdefgh23", "invite/InvitePage"],
  ["/lores/l-1", "lore/LorePage"],
  ["/compose?placeId=p-1&date=2026-10-01&hour=14", "compose/ComposePage"],
  ["/claims", "claims/ClaimsPage"],
  ["/admin/reports", "admin/ReportsPage"],
  ["/admin/access", "admin/AccessPage"],
  ["/admin/users?q=wren", "admin/UsersPage"],
  ["/admin/audit", "admin/AuditPage"],
  ["/terms", "legal/TermsPage"],
  ["/privacy", "legal/PrivacyPage"],
  ["/guidelines", "legal/GuidelinesPage"],
  ["/login", "auth/LoginPage"],
  ["/signup", "auth/SignUpPage"],
  ["/verify-email?token=t", "auth/VerifyEmailPage"],
  ["/forgot-password", "auth/ForgotPasswordPage"],
  ["/reset-password?token=t", "auth/ResetPasswordPage"],
  ["/confirm-email?token=t", "auth/ConfirmEmailPage"],
  ["/unsubscribe?u=x&t=y", "settings/UnsubscribePage"],
  ["/settings", "settings/SettingsPage"],
  ["/inbox", "inbox/InboxPage"],
];

describe("route table", () => {
  it("declares exactly the contracted routes", () => {
    expect(pages.map((p) => p.module).sort()).toEqual(contract.map(([, m]) => m).sort());
  });

  for (const [path, module] of contract) {
    it(`${path} lazy-loads ${module} inside the shell for a signed-in user`, async () => {
      const { requested } = renderAt(path, { refresh: signedIn() });
      const page = await screen.findByTestId("page");
      expect(page).toHaveTextContent(`${module} at ${path}`);
      expect(requested).toEqual([module]);
      expect(page.closest("main#main")).not.toBeNull();
    });
  }
});

describe("signed-out reading and writing", () => {
  const publicPaths = ["/", "/places/p-1", "/perspectives/x-1", "/arcs/a-1", "/i/abcdefgh23", "/login", "/signup", "/terms", "/privacy", "/guidelines"];
  for (const path of publicPaths) {
    it(`${path} renders for a signed-out reader`, async () => {
      renderAt(path);
      expect(await screen.findByTestId("page")).toBeInTheDocument();
      expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
    });
  }

  const writePaths = ["/compose?placeId=p-1&date=2026-10-01", "/claims", "/admin/reports", "/admin/access", "/admin/users", "/admin/audit", "/settings", "/inbox"];
  for (const path of writePaths) {
    it(`${path} redirects a signed-out visitor to login with next`, async () => {
      const { router } = renderAt(path);
      expect(await screen.findByTestId("page")).toHaveTextContent("auth/LoginPage at /login?next=");
      expect(router.state.location.pathname).toBe("/login");
      expect(new URLSearchParams(router.state.location.search).get("next")).toBe(path);
    });
  }

  it("keeps public reading working when the session refresh cannot reach the server", async () => {
    renderAt("/arcs/a-1", { refresh: () => connectError("unavailable") });
    expect(await screen.findByTestId("page")).toHaveTextContent("arc/ArcPage");
    expect(screen.getByRole("link", { name: "Sign in" })).toBeInTheDocument();
  });

  it("shows an error, not the login page, when a signed-in surface cannot reach the server", async () => {
    renderAt("/inbox", { refresh: () => connectError("unavailable") });
    expect(await screen.findByRole("heading", { name: "Perspectiv is unreachable" })).toBeInTheDocument();
  });
});

describe("route loaders", () => {
  it("run after the session is resolved, so a route loader can read it", async () => {
    function Component() {
      const data = useLoaderData() as { seen: string | null };
      return <p data-testid="page">seen {data.seen ?? "nobody"}</p>;
    }
    const load: PageLoader = async () => ({
      Component,
      loader: () => ({ seen: currentSession()?.username ?? null }),
    });
    renderAt("/arcs/a-1", { refresh: signedIn(), load });
    expect(await screen.findByTestId("page")).toHaveTextContent("seen wren");
  });

  it("render the route's error element, inside the shell, when a module is missing", async () => {
    renderAt("/claims", {
      refresh: signedIn(),
      load: (module) => globPageLoader(`missing/${module}`),
    });
    expect(await screen.findByRole("heading", { name: "Something went wrong" })).toBeInTheDocument();
    expect(screen.getByRole("banner")).toBeInTheDocument();
  });
});

describe("globPageLoader", () => {
  it("names the module it cannot find", async () => {
    await expect(globPageLoader("nowhere/NowherePage")).rejects.toThrow("route module @/routes/nowhere/NowherePage does not exist");
  });
});

describe("shell", () => {
  it("renders a not-found page for an unknown path", async () => {
    renderAt("/no/such/page");
    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument();
  });

  it("puts a skip link first and gives every page the 16px gutter", async () => {
    renderAt("/");
    await screen.findByTestId("page");
    const skip = screen.getByRole("link", { name: "Skip to content" });
    expect(skip).toHaveAttribute("href", "#main");
    await userEvent.tab();
    expect(skip).toHaveFocus();
    expect(document.getElementById("main")).toHaveClass("px-4");
  });

  it("shows writing navigation to a signed-in user and hides Reports from a non-admin", async () => {
    const user = userEvent.setup();
    renderAt("/", { refresh: signedIn() });
    await screen.findByTestId("page");
    const nav = screen.getByRole("navigation", { name: "Primary" });
    expect(within(nav).getByRole("link", { name: "Write" })).toHaveAttribute("href", "/compose");
    expect(within(nav).getByRole("link", { name: "Inbox" })).toHaveAttribute("href", "/inbox");
    await user.click(screen.getByRole("button", { name: "Account: wren" }));
    await screen.findByRole("menuitem", { name: "Settings" });
    expect(screen.queryByRole("menuitem", { name: "Reports" })).not.toBeInTheDocument();
  });

  it("names every member item in visible words, calls claims Requests and counts unread notifications on Inbox", async () => {
    renderAt("/", { refresh: signedIn(), extra: { "NotifyService/ListInbox": () => ({ body: { unreadCount: 2 } }) } });
    await screen.findByTestId("page");
    const nav = screen.getByRole("navigation", { name: "Primary" });
    expect(within(nav).getByRole("link", { name: "Requests" })).toHaveAttribute("href", "/claims");
    const inbox = await within(nav).findByRole("link", { name: "Inbox, 2 unread" });
    expect(inbox).toHaveAttribute("href", "/inbox");
    expect(inbox).toHaveTextContent("2");
    // The words show at phone width too: none is screen-reader only.
    for (const word of ["Write", "Requests", "Inbox", "Account"]) {
      expect(within(nav).getByText(word)).not.toHaveClass("sr-only");
    }
  });

  it("counts requests waiting on the member on Requests, in the badge and in its name", async () => {
    const pending = { id: "cl", status: "CLAIM_STATUS_PENDING" };
    renderAt("/", {
      refresh: signedIn(),
      extra: { "LoreService/ListClaims": () => ({ body: { claims: [pending, { ...pending, id: "cl-2" }] } }) },
    });
    await screen.findByTestId("page");
    const nav = screen.getByRole("navigation", { name: "Primary" });
    const requests = await within(nav).findByRole("link", { name: "Requests, 2 waiting" });
    expect(requests).toHaveAttribute("href", "/claims");
    expect(requests).toHaveTextContent("2");
  });

  it("offers Reports to an admin", async () => {
    const user = userEvent.setup();
    const { router } = renderAt("/", { refresh: signedIn("ROLE_ADMIN") });
    await screen.findByTestId("page");
    await user.click(screen.getByRole("button", { name: "Account: wren" }));
    expect(await screen.findByRole("menuitem", { name: "Look up a user" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Audit log" })).toBeInTheDocument();
    await user.click(screen.getByRole("menuitem", { name: "Reports" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/reports"));
  });

  it("signs out through AuthService/Logout and returns to the signed-out header", async () => {
    const user = userEvent.setup();
    const { api } = renderAt("/arcs/a-1", { refresh: signedIn(), extra: { "AuthService/Logout": () => ({ body: {} }) } });
    await screen.findByTestId("page");
    await user.click(screen.getByRole("button", { name: "Account: wren" }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));
    expect(await screen.findByRole("link", { name: "Sign in" })).toBeInTheDocument();
    expect(api.calls.map((c) => c.procedure)).toContain("AuthService/Logout");
    expect(currentSession()).toBeNull();
  });
});
