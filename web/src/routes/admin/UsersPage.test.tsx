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
    expect(dossier).toHaveTextContent("None in the last 30 days");
    expect(screen.getByRole("heading", { level: 1, name: "Look up a user" })).toBeInTheDocument();
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

  it("says Never for a new account with no sign-in, since older sessions are what get deleted", async () => {
    const joinedAt = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString();
    installApi({ "ModerationService/LookupUser": () => ok({ user: view({ joinedAt }) }), ...empty }, admin);
    renderRoute("/admin/users?q=marlo");
    const dossier = await screen.findByRole("article", { name: "marlo" });
    expect(dossier).toHaveTextContent(/Last sign-inNever/);
    expect(dossier).not.toHaveTextContent("None in the last 30 days");
  });

  it("asks for a query when Look up is pressed with an empty box", async () => {
    const user = userEvent.setup();
    const api = installApi({ "ModerationService/LookupUser": () => ok({ user: view() }) }, admin);
    const router = renderRoute("/admin/users");
    const field = await screen.findByLabelText("Username, email or account id");
    await user.click(screen.getByRole("button", { name: "Look up" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Enter a username, email or account id.");
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription("Enter a username, email or account id.");
    expect(field).toHaveFocus();
    expect(router.state.location.search).toBe("");
    expect(api.callsTo("ModerationService/LookupUser")).toHaveLength(0);
    await user.type(field, "m");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(field).toHaveAttribute("aria-invalid", "false");
  });

  it("keeps the box in step with the address when a link or Back changes the dossier", async () => {
    const user = userEvent.setup();
    installApi(
      {
        "ModerationService/LookupUser": (body) => ok({ user: view(body.query === "u-r" ? { userId: "u-r", username: "reed" } : {}) }),
        "ModerationService/ListReports": (body) =>
          ok({
            reports:
              body.subjectUserId === "u-9"
                ? [{ id: "r-1", reporter: { userId: "u-r", username: "reed" }, targetKind: "REPORT_TARGET_KIND_USER", targetId: "u-9", targetLabel: "marlo", reason: "REPORT_REASON_SPAM", status: "REPORT_STATUS_RESOLVED" }]
                : [],
          }),
        "ModerationService/ListAuditLog": () => ok({ entries: [] }),
      },
      admin,
    );
    const router = renderRoute("/admin/users?q=marlo");
    const field = await screen.findByLabelText("Username, email or account id");
    expect(field).toHaveValue("marlo");
    const reports = await screen.findByRole("list", { name: "Reports against marlo" });
    await user.click(within(reports).getByRole("link", { name: "reed" }));
    await screen.findByRole("article", { name: "reed" });
    expect(field).toHaveValue("u-r");
    await router.navigate(-1);
    await screen.findByRole("article", { name: "marlo" });
    expect(field).toHaveValue("marlo");
  });

  it("re-reads the dossier after a resolve there, says so and moves focus to that line", async () => {
    const user = userEvent.setup();
    let open = true;
    const api = installApi(
      {
        "ModerationService/LookupUser": () => ok({ user: view({ openReportsAgainst: open ? 1 : 0 }) }),
        "ModerationService/ListReports": () =>
          ok({
            reports: [
              {
                id: "r-1",
                reporter: { userId: "u-r", username: "reed" },
                targetKind: "REPORT_TARGET_KIND_USER",
                targetId: "u-9",
                targetLabel: "marlo",
                reason: "REPORT_REASON_SPAM",
                status: open ? "REPORT_STATUS_OPEN" : "REPORT_STATUS_RESOLVED",
              },
            ],
          }),
        "ModerationService/ListAuditLog": () =>
          ok({
            entries: open
              ? []
              : [{ id: "1", actor: { userId: "u-w", username: "warden" }, action: "AUDIT_ACTION_DISMISS", targetKind: "AUDIT_TARGET_KIND_USER", targetId: "u-9", targetLabel: "marlo", reason: "not spam" }],
          }),
        "ModerationService/Resolve": () => {
          open = false;
          return ok({});
        },
      },
      admin,
    );
    renderRoute("/admin/users?q=marlo");
    const dossier = await screen.findByRole("article", { name: "marlo" });
    expect(dossier).toHaveTextContent("1 open of 2");
    await user.click(await within(dossier).findByRole("button", { name: "Resolve" }));
    const dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    await user.click(within(dialog).getByRole("radio", { name: "Dismiss" }));
    await user.type(within(dialog).getByRole("textbox", { name: "Reason for the audit log" }), "not spam");
    await user.click(within(dialog).getByRole("button", { name: "Resolve report" }));

    const status = await screen.findByText("Report resolved: dismiss.");
    expect(status).toHaveAttribute("role", "status");
    await waitFor(() => expect(status).toHaveFocus());
    // A resolve is said above the reports it changed.
    const reports = within(dossier).getByRole("heading", { name: "Reports against them" });
    expect(reports.compareDocumentPosition(status) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    await waitFor(() => expect(dossier).toHaveTextContent("0 open of 2"));
    expect(api.callsTo("ModerationService/LookupUser")).toHaveLength(2);
    expect(await within(dossier).findByRole("list", { name: "Actions about marlo" })).toHaveTextContent("warden dismissed a report");
  });

  it("fits a phone", async () => {
    installApi({ "ModerationService/LookupUser": () => ok({ user: view({ username: "a_very_long_username_x" }) }), ...empty }, admin);
    renderRoute("/admin/users?q=marlo");
    await screen.findByRole("article");
    expect(document.body.scrollWidth).toBeLessThanOrEqual(window.innerWidth);
  });
});

describe("lifting a suspension", () => {
  it("asks for a reason, lifts it, says so and re-reads the dossier", async () => {
    const user = userEvent.setup();
    let suspended = true;
    const api = installApi(
      {
        "ModerationService/LookupUser": () => ok({ user: view(suspended ? { suspendedAt: "2026-10-01T00:00:00Z" } : {}) }),
        "ModerationService/LiftSuspension": () => {
          suspended = false;
          return ok({ user: view() });
        },
        ...empty,
      },
      admin,
    );
    renderRoute("/admin/users?q=marlo");
    await user.click(await screen.findByRole("button", { name: "Lift suspension…" }));
    const dialog = await screen.findByRole("dialog", { name: "Lift marlo's suspension?" });
    await user.click(within(dialog).getByRole("button", { name: "Lift suspension" }));
    expect(await within(dialog).findByText("Say why. The reason is written to the audit log.")).toBeInTheDocument();
    expect(api.callsTo("ModerationService/LiftSuspension")).toHaveLength(0);
    await user.type(within(dialog).getByLabelText("Reason for the audit log"), "appeal upheld");
    await user.click(within(dialog).getByRole("button", { name: "Lift suspension" }));
    const status = await screen.findByText("marlo's suspension is lifted.");
    expect(status).toHaveAttribute("role", "status");
    expect(status).not.toHaveAttribute("aria-label");
    expect(status).toHaveFocus();
    // Said under the standing, where the button was, not down by the reports.
    expect(status.parentElement).toContainElement(screen.getByRole("list", { name: "Standing" }));
    const reports = screen.getByRole("heading", { name: "Reports against them" });
    expect(reports.compareDocumentPosition(status) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    expect(api.callsTo("ModerationService/LiftSuspension")[0]?.body).toMatchObject({ userId: "u-9", reason: "appeal upheld" });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Lift suspension…" })).not.toBeInTheDocument());
  });

  it("is not offered for an account in good standing", async () => {
    installApi({ "ModerationService/LookupUser": () => ok({ user: view() }), ...empty }, admin);
    renderRoute("/admin/users?q=marlo");
    await screen.findByRole("article", { name: "marlo" });
    expect(screen.queryByRole("button", { name: "Lift suspension…" })).not.toBeInTheDocument();
  });
});
