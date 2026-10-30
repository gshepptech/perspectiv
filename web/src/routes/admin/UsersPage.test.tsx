import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";
import { apiError, installApi, ok, renderRoute } from "@/routes/compose/api.testutil";

beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const admin = { role: "ROLE_ADMIN", username: "warden" };

function view(over: Record<string, unknown> = {}) {
  return {
    userId: "u-9",
    username: "marlo",
    emailHint: "ma•••@example.test",
    emailVerified: true,
    role: "ROLE_USER",
    joinedAt: "2026-09-01T10:00:00Z",
    failedSignIns24h: 3,
    stories: 4,
    hiddenStories: 1,
    openReportsAgainst: 1,
    reportsAgainst: 2,
    reportsFiled: 0,
    ...over,
  };
}

const empty = {
  "ModerationService/ListReports": () => ok({ reports: [] }),
  "ModerationService/ListAuditLog": () => ok({ entries: [] }),
};

describe("user lookup", () => {
  it("never asks the server for a non-admin", async () => {
    const api = installApi({ "ModerationService/LookupUser": () => ok({ user: view() }) });
    renderRoute("/admin/users?q=marlo");
    expect(await screen.findByText("Only site admins can look up users.")).toBeInTheDocument();
    expect(api.callsTo("ModerationService/LookupUser")).toHaveLength(0);
  });

  it("looks up on submit only, puts the query in the address and shows the dossier", async () => {
    const user = userEvent.setup();
    const api = installApi({ "ModerationService/LookupUser": () => ok({ user: view() }), ...empty }, admin);
    const router = renderRoute("/admin/users");
    await user.type(await screen.findByLabelText("Username, email or account id"), "marlo");
    expect(api.callsTo("ModerationService/LookupUser")).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "Look up" }));
    const dossier = await screen.findByRole("article", { name: "marlo" });
    expect(router.state.location.search).toBe("?q=marlo");
    expect(api.callsTo("ModerationService/LookupUser")).toHaveLength(1);
    expect(dossier).toHaveTextContent("ma•••@example.test");
    expect(dossier).toHaveTextContent("4, 1 hidden");
    expect(dossier).toHaveTextContent("1 open of 2");
    expect(dossier).toHaveTextContent("Never");
    expect(within(dossier).getByRole("link", { name: "Public profile" })).toHaveAttribute("href", "/users/marlo");
    expect(within(dossier).getByRole("list", { name: "Standing" })).toHaveTextContent("Email verified");
    await waitFor(() => expect(api.callsTo("ModerationService/ListReports")[0]?.body).toMatchObject({ subjectUserId: "u-9" }));
    expect(api.callsTo("ModerationService/ListAuditLog")[0]?.body).toMatchObject({ subjectUserId: "u-9" });
  });

  it("says a suspended, deleted account is so, in words", async () => {
    installApi(
      { "ModerationService/LookupUser": () => ok({ user: view({ deleted: true, emailHint: "", suspendedAt: "2026-10-01T00:00:00Z" }) }), ...empty },
      admin,
    );
    renderRoute("/admin/users?q=u-9");
    const dossier = await screen.findByRole("article", { name: "[deleted]" });
    const standing = within(dossier).getByRole("list", { name: "Standing" });
    expect(standing).toHaveTextContent(/Suspended/);
    expect(standing).toHaveTextContent("Deleted");
    expect(within(dossier).queryByRole("link", { name: "Public profile" })).not.toBeInTheDocument();
  });

  it("says plainly when nothing matches", async () => {
    installApi({ "ModerationService/LookupUser": () => apiError("not_found", "no account matches") }, admin);
    renderRoute("/admin/users?q=nobody");
    expect(await screen.findByRole("alert")).toHaveTextContent("No account matches “nobody”.");
  });

  it("fits a phone", async () => {
    installApi({ "ModerationService/LookupUser": () => ok({ user: view({ username: "a_very_long_username_x" }) }), ...empty }, admin);
    renderRoute("/admin/users?q=marlo");
    await screen.findByRole("article");
    expect(document.body.scrollWidth).toBeLessThanOrEqual(window.innerWidth);
  });
});
