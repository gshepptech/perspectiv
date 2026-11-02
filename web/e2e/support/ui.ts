/**
 * Steps the specs share, each driven through the UI the way a person would.
 */
import { expect, type Browser, type Locator, type Page } from "@playwright/test";

import type { Account } from "./accounts.ts";

export type Writer = { account: Account; page: Page };

/** The account menu in the header, present once the root loader has the session. */
function accountMenu(page: Page, account: Account) {
  return page.getByRole("button", { name: `Account: ${account.username}` });
}

/**
 * Opens `path` with a full page load and waits until the session is loaded.
 *
 * A full load spends the refresh cookie: the root loader calls Refresh, which
 * rotates the token. The page has the rotated cookie only once that response
 * arrived, and the header renders only after the root loader finished, so
 * waiting for the account menu keeps the next load from presenting the old
 * token (which the server treats as reuse and answers by revoking the
 * session).
 */
export async function visit(page: Page, account: Account, path: string): Promise<void> {
  await page.goto(path);
  await expect(accountMenu(page, account)).toBeVisible();
}

/** A browser context of its own, signed in from the state the setup project saved. */
export async function signedIn(browser: Browser, account: Account): Promise<Writer> {
  const context = await browser.newContext({ storageState: account.storageState });
  const page = await context.newPage();
  await visit(page, account, "/");
  return { account, page };
}

/**
 * A browser context of its own, signed in through the sign-in page with the
 * account's password. The session is new, so the state the setup project
 * saved for the account stays good for the spec that owns it.
 */
export async function signInWithPassword(browser: Browser, account: Account): Promise<Writer> {
  const page = await (await browser.newContext()).newPage();
  await page.goto("/login");
  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(account.password);
  await page.getByRole("main").getByRole("button", { name: "Sign in" }).click();
  await expect(accountMenu(page, account)).toBeVisible();
  return { account, page };
}

/** A signed-out reader; reading every page is public. */
export async function reader(browser: Browser): Promise<Page> {
  const context = await browser.newContext();
  return context.newPage();
}

export type Draft = {
  genre: string;
  parts: string[];
  characters?: string[];
  /** Needed only by the first writer of an arc. */
  title?: string;
};

/** Fills the composer and publishes; returns the new perspective's path and its arc's path. */
async function fillAndPublish(page: Page, draft: Draft): Promise<{ perspective: string; arc: string }> {
  await expect(page.getByRole("heading", { level: 1, name: "Write a story" })).toBeVisible();
  if (draft.title) await page.getByLabel("Title of the day").fill(draft.title);
  await page.getByLabel("Genre").click();
  await page.getByRole("option", { name: draft.genre, exact: true }).click();
  for (const [i, body] of draft.parts.entries()) {
    if (i > 0) await page.getByRole("button", { name: "Add part" }).click();
    await page.getByLabel(`Part ${i + 1}`, { exact: true }).fill(body);
  }
  for (const [i, label] of (draft.characters ?? []).entries()) {
    await page.getByRole("button", { name: "Add character" }).click();
    await page.getByLabel(`Character ${i + 1}`, { exact: true }).fill(label);
  }
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page.waitForURL(/\/perspectives\/[^/?]+/);
  const perspective = new URL(page.url()).pathname;
  await page.getByRole("link", { name: "Back to the day", exact: true }).click();
  await page.waitForURL(/\/arcs\/[^/?]+/);
  const arc = new URL(page.url()).pathname;
  // Every arc page has a Canon section; an error page has none.
  await expect(page.getByRole("region", { name: "Canon" })).toBeVisible();
  return { perspective, arc };
}

/** Starts from Write in the header, picks the place by search and publishes there for today. */
export async function writeAt(w: Writer, place: string, draft: Draft) {
  await w.page.getByRole("link", { name: "Write", exact: true }).click();
  await w.page.waitForURL(/\/compose$/);
  await w.page.getByRole("searchbox", { name: "Where were you?" }).fill(place);
  await w.page.getByRole("list", { name: "Matching places" }).getByRole("button", { name: new RegExp(place) }).click();
  return fillAndPublish(w.page, draft);
}

/** Opens an existing arc and publishes through its "Write your story" link, so the place and day match. */
export async function writeInArc(w: Writer, arcPath: string, draft: Draft) {
  await visit(w.page, w.account, arcPath);
  await w.page.getByRole("link", { name: "Write your story" }).click();
  return fillAndPublish(w.page, draft);
}

/** The card for `username`'s story on an arc page, or within one section of it. */
export function card(scope: Page | Locator, username: string): Locator {
  return scope.getByRole("article", { name: `${username}'s story` });
}

/**
 * Reloads a page until `check` passes, for state the rank job writes on its
 * own schedule. Only a signed-out page is reloaded this way: it has no
 * refresh cookie to spend.
 */
export async function reloadUntil(page: Page, path: string, check: () => Promise<void>, timeoutMs: number): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await check();
  }).toPass({ timeout: timeoutMs, intervals: [1_000] });
}
