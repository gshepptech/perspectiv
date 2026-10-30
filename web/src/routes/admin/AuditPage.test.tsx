import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";
import { apiError, installApi, ok, renderRoute } from "@/routes/compose/api.testutil";

beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const admin = { role: "ROLE_ADMIN", username: "warden" };

function entry(over: Record<string, unknown> = {}) {
  return {
    id: "7",
    actor: { userId: "u-a", username: "ana" },
    action: "AUDIT_ACTION_HIDE_PERSPECTIVE",
    targetKind: "AUDIT_TARGET_KIND_PERSPECTIVE",
    targetId: "p-7",
    targetLabel: "Rain at the deli",
    reason: "names the cashier",
    at: "2026-10-20T15:00:00Z",
    ...over,
  };
}

describe("audit log", () => {
  it("never asks the server for a non-admin", async () => {
    const api = installApi({ "ModerationService/ListAuditLog": () => ok({ entries: [entry()] }) });
    renderRoute("/admin/audit");
    expect(await screen.findByText("Only site admins can read the audit log.")).toBeInTheDocument();
    expect(api.callsTo("ModerationService/ListAuditLog")).toHaveLength(0);
  });

  it("reads each entry as a sentence with its target linked and its reason quoted", async () => {
    installApi(
      {
        "ModerationService/ListAuditLog": () =>
          ok({
            entries: [
              entry(),
              entry({ id: "6", actor: undefined, action: "AUDIT_ACTION_ADMIN_GRANT", targetKind: "AUDIT_TARGET_KIND_USER", targetId: "u-9", targetLabel: "marlo", reason: "" }),
              entry({ id: "5", action: "AUDIT_ACTION_ACCESS_CODE_CREATE", targetKind: "AUDIT_TARGET_KIND_ACCESS_CODE", targetId: "", targetLabel: "beta-x", reason: "" }),
            ],
          }),
      },
      admin,
    );
    renderRoute("/admin/audit");
    const list = await screen.findByRole("list", { name: "Audit entries" });
    const [hide, grant, code] = within(list).getAllByRole("listitem");
    expect(hide).toHaveTextContent("ana hid a perspective: Rain at the deli");
    expect(hide).toHaveTextContent("“names the cashier”");
    expect(within(hide!).getByRole("link", { name: "Rain at the deli" })).toHaveAttribute("href", "/perspectives/p-7");
    expect(within(hide!).getByRole("link", { name: "ana" })).toHaveAttribute("href", "/admin/users?q=u-a");
    expect(grant).toHaveTextContent("The command line made a user an admin: marlo");
    expect(within(grant!).getByRole("link", { name: "marlo" })).toHaveAttribute("href", "/admin/users?q=u-9");
    expect(code).toHaveTextContent("created an access code: beta-x");
    expect(within(code!).queryByRole("link", { name: "beta-x" })).not.toBeInTheDocument();
  });

  it("filters by action from the address and pages with Show more", async () => {
    const api = installApi(
      {
        "ModerationService/ListAuditLog": (body) =>
          ok(body.pageToken ? { entries: [entry({ id: "1" })] } : { entries: [entry()], nextPageToken: "1" }),
      },
      admin,
    );
    renderRoute("/admin/audit?action=DISMISS");
    await screen.findByRole("list", { name: "Audit entries" });
    expect(api.callsTo("ModerationService/ListAuditLog")[0]?.body).toMatchObject({ action: "AUDIT_ACTION_DISMISS" });
    await userEvent.setup().click(screen.getByRole("button", { name: "Show more" }));
    await waitFor(() => expect(within(screen.getByRole("list", { name: "Audit entries" })).getAllByRole("listitem")).toHaveLength(2));
  });

  it("shows the refusal when the server denies an admin whose role has changed", async () => {
    installApi({ "ModerationService/ListAuditLog": () => apiError("permission_denied", "admin only") }, admin);
    renderRoute("/admin/audit");
    expect(await screen.findByRole("alert")).toHaveTextContent("Only site admins can read the audit log.");
  });
});
