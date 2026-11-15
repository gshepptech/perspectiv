import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";
import { apiError, installApi, ok, renderRoute } from "@/routes/compose/api.testutil";

beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

const admin = { role: "ROLE_ADMIN", username: "warden" };

function report(over: Record<string, unknown> = {}) {
  return {
    id: "r-1",
    reporter: { userId: "u-r", username: "marlo" },
    targetKind: "REPORT_TARGET_KIND_PERSPECTIVE",
    targetId: "p-7",
    targetLabel: "The rainy Thursday",
    reason: "REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER",
    details: "Names my neighbour in part 2.",
    status: "REPORT_STATUS_OPEN",
    createdAt: "2026-10-15T12:00:00Z",
    ...over,
  };
}

describe("report queue access", () => {
  it("tells a non-admin the queue is for site admins and never asks the server for it", async () => {
    const api = installApi({ "ModerationService/ListReports": () => ok({ reports: [report()] }) });
    renderRoute("/admin/reports");
    expect(await screen.findByRole("heading", { name: "Report queue" })).toBeInTheDocument();
    expect(screen.getByText("Only site admins can open the report queue.")).toBeInTheDocument();
    expect(api.callsTo("ModerationService/ListReports")).toHaveLength(0);
  });

  it("shows the same refusal when the server denies an admin whose role has changed", async () => {
    installApi({ "ModerationService/ListReports": () => apiError("permission_denied", "admin only") }, admin);
    renderRoute("/admin/reports");
    expect(await screen.findByRole("alert")).toHaveTextContent("Only site admins can open the report queue.");
  });
});

describe("report queue", () => {
  it("lists open reports with their target, reason, details and reporter", async () => {
    const api = installApi({ "ModerationService/ListReports": () => ok({ reports: [report()] }) }, admin);
    renderRoute("/admin/reports");
    const item = await screen.findByRole("listitem", { name: /The rainy Thursday/ });
    expect(item).toHaveTextContent("Story");
    expect(item).toHaveTextContent("Real name or handle of a non-user");
    expect(item).toHaveTextContent("Names my neighbour in part 2.");
    expect(item).toHaveTextContent("Reported by marlo");
    expect(within(item).getByRole("link", { name: "Open the story" })).toHaveAttribute("href", "/perspectives/p-7");
    expect(api.callsTo("ModerationService/ListReports")[0]?.body).toMatchObject({ status: "REPORT_STATUS_OPEN" });
  });

  it("keeps what the target said when reported behind a disclosure, so a later edit cannot hide it", async () => {
    const user = userEvent.setup();
    installApi(
      { "ModerationService/ListReports": () => ok({ reports: [report({ reportedText: "Part one.\n\nCharacters: Jo Smith" })] }) },
      admin,
    );
    renderRoute("/admin/reports");
    const item = await screen.findByRole("listitem", { name: /The rainy Thursday/ });
    const copy = within(item).getByText(/Characters: Jo Smith/);
    expect(copy).not.toBeVisible();
    await user.click(within(item).getByText("What it said when reported"));
    expect(copy).toBeVisible();
    expect(copy).toHaveClass("font-serif", "whitespace-pre-wrap");
    // The text scrolls on its own past 20 lines, so the keyboard reaches it next.
    await user.tab();
    expect(within(item).getByRole("region", { name: "What it said when reported" })).toHaveFocus();
  });

  it("resolves a report with an action and an audit reason, then drops it from the open queue", async () => {
    const user = userEvent.setup();
    let open = true;
    const api = installApi(
      {
        "ModerationService/ListReports": () => ok({ reports: open ? [report()] : [] }),
        "ModerationService/Resolve": () => {
          open = false;
          return ok({ report: report({ status: "REPORT_STATUS_RESOLVED" }) });
        },
      },
      admin,
    );
    renderRoute("/admin/reports");
    const item = await screen.findByRole("listitem", { name: /The rainy Thursday/ });
    await user.click(within(item).getByRole("button", { name: "Resolve" }));
    const dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    const actions = within(dialog).getByRole("group", { name: "Action" });
    expect(within(actions).getAllByRole("radio").map((r) => r.getAttribute("aria-label") ?? r.closest("label")?.textContent)).toEqual([
      "Hide story",
      "Suspend the author",
      "Dismiss",
    ]);

    // Nothing is sent without a reason for the audit row.
    await user.click(within(dialog).getByRole("button", { name: "Resolve report" }));
    expect(await within(dialog).findByText("Say why. The reason is written to the audit log.")).toBeInTheDocument();
    expect(api.callsTo("ModerationService/Resolve")).toHaveLength(0);

    await user.click(within(actions).getByRole("radio", { name: "Hide story" }));
    await user.type(within(dialog).getByRole("textbox", { name: "Reason for the audit log" }), "Names a private person");
    await user.click(within(dialog).getByRole("button", { name: "Resolve report" }));

    await waitFor(() => expect(api.callsTo("ModerationService/Resolve")).toHaveLength(1));
    expect(api.callsTo("ModerationService/Resolve")[0]?.body).toEqual({
      reportId: "r-1",
      action: "MODERATION_ACTION_HIDE_PERSPECTIVE",
      reason: "Names a private person",
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(await screen.findByText("No open reports.")).toBeInTheDocument();
    expect(screen.getByRole("status", { name: "Queue status" })).toHaveTextContent("Report resolved: hide story.");
    // The Resolve button went with the report; focus is on what happened.
    await waitFor(() => expect(screen.getByRole("status", { name: "Queue status" })).toHaveFocus());
  });

  it("offers the action that fits each target kind", async () => {
    const user = userEvent.setup();
    installApi(
      {
        "ModerationService/ListReports": () =>
          ok({
            reports: [
              report({ id: "r-u", targetKind: "REPORT_TARGET_KIND_USER", targetId: "u-9", targetLabel: "loudmouth" }),
              report({ id: "r-p", targetKind: "REPORT_TARGET_KIND_PLACE", targetId: "pl-1", targetLabel: "Fake Mall" }),
            ],
          }),
      },
      admin,
    );
    renderRoute("/admin/reports");
    const userItem = await screen.findByRole("listitem", { name: /loudmouth/ });
    await user.click(within(userItem).getByRole("button", { name: "Resolve" }));
    let dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    expect(within(dialog).getByRole("radio", { name: "Suspend user" })).toBeInTheDocument();
    expect(within(dialog).queryByRole("radio", { name: "Hide story" })).toBeNull();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    const placeItem = screen.getByRole("listitem", { name: /Fake Mall/ });
    expect(within(placeItem).getByRole("link", { name: "Open the place" })).toHaveAttribute("href", "/places/pl-1");
    await user.click(within(placeItem).getByRole("button", { name: "Resolve" }));
    dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    expect(within(dialog).getByRole("radio", { name: "Hide place" })).toBeInTheDocument();
  });

  it("hides a reported thread post or clears a reported lore name", async () => {
    const user = userEvent.setup();
    installApi(
      {
        "ModerationService/ListReports": () =>
          ok({
            reports: [
              report({ id: "r-post", targetKind: "REPORT_TARGET_KIND_LORE_POST", targetId: "lp-1", targetLabel: "That cashier is Jo Smith" }),
              report({ id: "r-name", targetKind: "REPORT_TARGET_KIND_LORE_NAME", targetId: "lore-1", targetLabel: "Bad name" }),
            ],
          }),
      },
      admin,
    );
    renderRoute("/admin/reports");
    const postItem = await screen.findByRole("listitem", { name: /Jo Smith/ });
    expect(postItem).toHaveTextContent("Thread post");
    await user.click(within(postItem).getByRole("button", { name: "Resolve" }));
    let dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    expect(within(dialog).getByRole("radio", { name: "Hide post" })).toBeInTheDocument();
    expect(within(dialog).queryByRole("radio", { name: "Hide story" })).toBeNull();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    const nameItem = screen.getByRole("listitem", { name: /Bad name/ });
    expect(within(nameItem).getByRole("link", { name: "Open the lore" })).toHaveAttribute("href", "/lores/lore-1");
    await user.click(within(nameItem).getByRole("button", { name: "Resolve" }));
    dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    expect(within(dialog).getByRole("radio", { name: "Clear the name" })).toBeInTheDocument();
  });

  it("can be worked entirely from the keyboard", async () => {
    const user = userEvent.setup();
    const api = installApi(
      {
        "ModerationService/ListReports": () => ok({ reports: [report()] }),
        "ModerationService/Resolve": () => ok({ report: report({ status: "REPORT_STATUS_RESOLVED" }) }),
      },
      admin,
    );
    renderRoute("/admin/reports");
    const item = await screen.findByRole("listitem", { name: /The rainy Thursday/ });
    within(item).getByRole("button", { name: "Resolve" }).focus();
    await user.keyboard("{Enter}");
    const dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    // Focus starts on the first action; arrows move between actions.
    expect(within(dialog).getByRole("radio", { name: "Hide story" })).toHaveFocus();
    await user.keyboard("{ArrowDown}");
    expect(within(dialog).getByRole("radio", { name: "Suspend the author" })).toBeChecked();
    await user.keyboard("{ArrowDown}");
    expect(within(dialog).getByRole("radio", { name: "Dismiss" })).toBeChecked();
    await user.tab();
    await user.keyboard("Not a real name");
    await user.tab();
    expect(within(dialog).getByRole("button", { name: "Resolve report" })).toHaveFocus();
    await user.keyboard("{Enter}");
    await waitFor(() => expect(api.callsTo("ModerationService/Resolve")).toHaveLength(1));
    expect(api.callsTo("ModerationService/Resolve")[0]?.body).toMatchObject({
      action: "MODERATION_ACTION_DISMISS",
      reason: "Not a real name",
    });
  });

  it("suspends the author of a reported perspective through a report on that user", async () => {
    const user = userEvent.setup();
    const author = { userId: "u-author", username: "loudmouth" };
    const api = installApi(
      {
        "ModerationService/ListReports": () => ok({ reports: [report()] }),
        "PerspectiveService/Get": () => ok({ perspective: { id: "p-7", author, status: "PERSPECTIVE_STATUS_PUBLISHED" } }),
        "ModerationService/Report": () => ok({ reportId: "r-user" }),
        "ModerationService/Resolve": (body) =>
          ok({
            report: report({
              id: body.reportId,
              targetKind: "REPORT_TARGET_KIND_USER",
              targetId: author.userId,
              targetLabel: author.username,
              status: "REPORT_STATUS_RESOLVED",
            }),
          }),
      },
      admin,
    );
    renderRoute("/admin/reports");
    const item = await screen.findByRole("listitem", { name: /The rainy Thursday/ });
    await user.click(within(item).getByRole("button", { name: "Resolve" }));
    const dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    await user.click(within(dialog).getByRole("radio", { name: "Suspend the author" }));
    await user.type(within(dialog).getByRole("textbox", { name: "Reason for the audit log" }), "Repeatedly names a neighbour");
    await user.click(within(dialog).getByRole("button", { name: "Resolve report" }));

    await waitFor(() => expect(api.callsTo("ModerationService/Resolve")).toHaveLength(1));
    expect(api.callsTo("PerspectiveService/Get")[0]?.body).toEqual({ perspectiveId: "p-7" });
    // The suspension is filed and resolved as a report on the author, so the
    // audit row names the user.
    expect(api.callsTo("ModerationService/Report")[0]?.body).toEqual({
      targetKind: "REPORT_TARGET_KIND_USER",
      targetId: "u-author",
      reason: "REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER",
      details: "Filed from the report on story p-7: Repeatedly names a neighbour",
    });
    expect(api.callsTo("ModerationService/Resolve")[0]?.body).toEqual({
      reportId: "r-user",
      action: "MODERATION_ACTION_SUSPEND_USER",
      reason: "Repeatedly names a neighbour",
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByRole("status", { name: "Queue status" })).toHaveTextContent("loudmouth is suspended.");
  });

  it("explains when a reported perspective no longer shows its author", async () => {
    const user = userEvent.setup();
    const api = installApi(
      {
        "ModerationService/ListReports": () => ok({ reports: [report()] }),
        "PerspectiveService/Get": () => ok({ perspective: { id: "p-7", hidden: true } }),
      },
      admin,
    );
    renderRoute("/admin/reports");
    const item = await screen.findByRole("listitem", { name: /The rainy Thursday/ });
    await user.click(within(item).getByRole("button", { name: "Resolve" }));
    const dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    await user.click(within(dialog).getByRole("radio", { name: "Suspend the author" }));
    await user.type(within(dialog).getByRole("textbox", { name: "Reason for the audit log" }), "spam");
    await user.click(within(dialog).getByRole("button", { name: "Resolve report" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "This story no longer shows its author, so they cannot be suspended from here.",
    );
    expect(api.callsTo("ModerationService/Report")).toHaveLength(0);
    expect(api.callsTo("ModerationService/Resolve")).toHaveLength(0);
  });

  it("shows resolved reports with who resolved them and how", async () => {
    const user = userEvent.setup();
    const api = installApi(
      {
        "ModerationService/ListReports": (body) =>
          ok({
            reports:
              body.status === "REPORT_STATUS_RESOLVED"
                ? [
                    report({
                      status: "REPORT_STATUS_RESOLVED",
                      resolvedBy: { userId: "u-a", username: "warden" },
                      resolution: "MODERATION_ACTION_HIDE_PERSPECTIVE",
                      resolutionReason: "Names a private person",
                      resolvedAt: "2026-10-16T09:00:00Z",
                    }),
                  ]
                : [],
          }),
      },
      admin,
    );
    renderRoute("/admin/reports");
    expect(await screen.findByText("No open reports.")).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Resolved" }));
    const item = await screen.findByRole("listitem", { name: /The rainy Thursday/ });
    expect(item).toHaveTextContent("Hide story by warden");
    expect(item).toHaveTextContent("Names a private person");
    expect(within(item).queryByRole("button", { name: "Resolve" })).toBeNull();
    expect(api.callsTo("ModerationService/ListReports").at(-1)?.body).toMatchObject({
      status: "REPORT_STATUS_RESOLVED",
    });
  });

  it("keeps the dialog open and shows the server's reason when resolving fails", async () => {
    const user = userEvent.setup();
    installApi(
      {
        "ModerationService/ListReports": () => ok({ reports: [report()] }),
        "ModerationService/Resolve": () => apiError("failed_precondition", "report already resolved"),
      },
      admin,
    );
    renderRoute("/admin/reports");
    const item = await screen.findByRole("listitem", { name: /The rainy Thursday/ });
    await user.click(within(item).getByRole("button", { name: "Resolve" }));
    const dialog = await screen.findByRole("dialog", { name: "Resolve report" });
    await user.type(within(dialog).getByRole("textbox", { name: "Reason for the audit log" }), "dup");
    await user.click(within(dialog).getByRole("button", { name: "Resolve report" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("report already resolved");
  });
});
