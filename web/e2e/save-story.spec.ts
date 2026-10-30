/**
 * Saving a story to read later: a signed-out reader who presses Save is sent
 * to sign in and back; a member saves another writer's story, finds it on
 * Saved through the account menu, opens it from there and unsaves it.
 * Finn writes at a store no other spec writes at. Finn and Gil are the
 * find-yourself spec's accounts, signed in with their passwords in sessions
 * of their own (see a11yAccounts for why).
 */
import { expect, test } from "@playwright/test";

import { findAccounts } from "./support/accounts.ts";
import { reader, signInWithPassword, visit, writeAt } from "./support/ui.ts";

const STORE = "Seville Square Kite Shop";
const TITLE = "The kite that would not come down";

test("a member saves a story, finds it on Saved, and unsaves it", async ({ browser }) => {
  test.setTimeout(180_000);
  const { finn: finnAcct, gil: gilAcct } = findAccounts();
  const finn = await signInWithPassword(browser, finnAcct);
  const { perspective } = await writeAt(finn, STORE, {
    title: TITLE,
    genre: "Comedy",
    parts: ["A red box kite caught in the oak over the shop door and stayed there all afternoon."],
  });
  const saveName = `Save ${finnAcct.username}'s perspective`;
  const savedName = `Saved ${finnAcct.username}'s perspective`;

  // Reading is public; saving needs an account, and sign-in comes back here.
  const anon = await reader(browser);
  await anon.goto(perspective);
  await anon.getByRole("region", { name: "Respond to this story" }).getByRole("button", { name: saveName }).click();
  await anon.waitForURL(/\/login\?/);
  expect(new URL(anon.url()).searchParams.get("next")).toBe(perspective);

  const gil = await signInWithPassword(browser, gilAcct);
  await visit(gil.page, gilAcct, perspective);
  const actions = gil.page.getByRole("region", { name: "Respond to this story" });
  await actions.getByRole("button", { name: saveName, pressed: false }).click();
  await expect(actions.getByRole("button", { name: savedName })).toHaveAttribute("aria-pressed", "true");

  // Saved is in the account menu, and the row opens the story.
  await gil.page.getByRole("button", { name: `Account: ${gilAcct.username}` }).click();
  await gil.page.getByRole("menuitem", { name: "Saved" }).click();
  await gil.page.waitForURL(/\/saved$/);
  await expect(gil.page.getByRole("heading", { level: 1, name: "Saved stories" })).toBeVisible();
  await expect(gil.page).toHaveTitle("Saved stories · Perspectiv");
  const row = gil.page.getByRole("list", { name: "Saved stories" }).getByRole("link", { name: new RegExp(TITLE) });
  await expect(row).toContainText(`by ${finnAcct.username}`);
  await row.click();
  await gil.page.waitForURL((url) => url.pathname === perspective);

  // Unsaving takes it off the list.
  await actions.getByRole("button", { name: savedName, pressed: true }).click();
  await expect(actions.getByRole("button", { name: saveName })).toHaveAttribute("aria-pressed", "false");
  await visit(gil.page, gilAcct, "/saved");
  await expect(gil.page.getByText("You have not saved any stories yet. Press Save under a story to keep it here.")).toBeVisible();
});
