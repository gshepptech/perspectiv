/**
 * NFR-004: the admin report flow. A reader reports a perspective that names a
 * real person, the queue is closed to anyone but an admin, an admin hides the
 * perspective, and from then on every reader sees only the placeholder.
 */
import { expect, test } from "@playwright/test";

import { reportAccounts } from "./support/accounts.ts";
import { reader, signedIn, visit, writeAt } from "./support/ui.ts";

const STORE = "Harbor Light Laundromat";
const HIDDEN = "This perspective was hidden by a moderator.";

test("NFR-004 a report is resolved by an admin and the perspective is hidden from readers", async ({ browser }) => {
  test.setTimeout(180_000);
  const { writer: writerAcct, reporter: reporterAcct, admin: adminAcct } = reportAccounts();
  const writer = await signedIn(browser, writerAcct);
  const reporter = await signedIn(browser, reporterAcct);
  const admin = await signedIn(browser, adminAcct);
  const anon = await reader(browser);

  const story = "Jordan Avery from the bakery folded my towels while the dryers hummed.";
  const { perspective, arc } = await writeAt(writer, STORE, {
    title: "Spin cycle at midnight",
    genre: "Drama",
    parts: [story],
  });

  // Any signed-in, verified reader can report; the reporter has no
  // perspective in this arc.
  await visit(reporter.page, reporterAcct, arc);
  await reporter.page.getByRole("link", { name: `Read ${writerAcct.username}'s perspective` }).click();
  await reporter.page
    .getByRole("region", { name: "Respond to this story" })
    .getByRole("button", { name: /^Report:/ })
    .click();
  const dialog = reporter.page.getByRole("dialog", { name: `Report ${writerAcct.username}'s perspective` });
  await dialog.getByRole("combobox", { name: "Reason" }).click();
  await reporter.page.getByRole("option", { name: "Real name or handle of a non-user" }).click();
  await dialog.getByLabel("Details (optional)").fill("Names a real person who does not use the site.");
  await dialog.getByRole("button", { name: "Send report" }).click();
  await expect(dialog.getByRole("status")).toHaveText("Report sent. A moderator will review it.");
  await reporter.page.keyboard.press("Escape");

  // The queue is not open to the reporter, who is not an admin.
  await visit(reporter.page, reporterAcct, "/admin/reports");
  await expect(reporter.page.getByText("Only site admins can open the report queue.")).toBeVisible();
  await expect(reporter.page.getByRole("list", { name: "Open reports" })).toHaveCount(0);

  // The admin finds the report in the open queue and hides the perspective.
  await admin.page.getByRole("button", { name: `Account: ${adminAcct.username}` }).click();
  await admin.page.getByRole("menuitem", { name: "Reports" }).click();
  await expect(admin.page.getByRole("heading", { level: 1, name: "Report queue" })).toBeVisible();
  const open = admin.page.getByRole("list", { name: "Open reports" });
  const item = open.getByRole("listitem").filter({ hasText: `Reported by ${reporterAcct.username}` });
  await expect(item).toContainText("Perspective:");
  await expect(item).toContainText("Real name or handle of a non-user");
  await item.getByRole("button", { name: "Resolve" }).click();
  const resolve = admin.page.getByRole("dialog", { name: "Resolve report" });
  await resolve.getByRole("radio", { name: "Hide perspective" }).check();
  await resolve.getByLabel("Reason for the audit log").fill("Names a non-user by their real name.");
  await resolve.getByRole("button", { name: "Resolve report" }).click();
  await expect(resolve).toBeHidden();
  await expect(admin.page.getByRole("status", { name: "Queue status" })).toHaveText("Report resolved: hide perspective.");
  await expect(item).toHaveCount(0);

  await admin.page.getByRole("tab", { name: "Resolved" }).click();
  const done = admin.page
    .getByRole("list", { name: "Resolved reports" })
    .getByRole("listitem")
    .filter({ hasText: `Reported by ${reporterAcct.username}` });
  await expect(done).toContainText(`Hide perspective by ${adminAcct.username}`);
  await expect(done).toContainText("Names a non-user by their real name.");

  // Hidden content: readers get the placeholder, never the text.
  await anon.goto(perspective);
  await expect(anon.getByText(HIDDEN)).toBeVisible();
  await expect(anon.getByText(story)).toHaveCount(0);
  await anon.goto(arc);
  await expect(anon.getByRole("article", { name: "Unavailable perspective" })).toContainText(HIDDEN);
  await expect(anon.getByText(story)).toHaveCount(0);

  // The author sees the same placeholder on their own perspective.
  await visit(writer.page, writerAcct, perspective);
  await expect(writer.page.getByText(HIDDEN)).toBeVisible();
});
