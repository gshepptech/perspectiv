/**
 * NFR-011: an axe scan of the pages a signed-in member reads and writes on,
 * the admin pages, and the signed-out sign-in and sign-up pages. Two writers
 * publish at a store no other spec writes at and link by a claim, so the
 * place, arc, story, claims and lore pages are scanned with real content.
 * The second writer is the find-yourself spec's Gil, signed in with his
 * password in a session of his own (see a11yAccounts for why).
 * The lore's thread (a post, an upvote and a reply), Saved, Appearance, a
 * profile's Follow toggle and its Thread posts tab, the lore read as a book
 * (contents, a chapter, a chapter's last part, the title page with its "Continue"
 * offer, the closing page), the report queue with its disclosure open, and
 * the signed-out guide to how Perspectiv works are scanned in both themes,
 * forced through the root's data-theme as the Appearance page forces them.
 * The scan fails on any serious or critical WCAG 2.1 A or AA violation and
 * lists every one, route by route.
 */
import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Locator, type Page } from "@playwright/test";

import { a11yAccounts, findAccounts } from "./support/accounts.ts";
import { reader, signedIn, signInWithPassword, visit, writeAt, writeInArc, type Writer } from "./support/ui.ts";

const STORE = "Palafox Record Exchange";
const CLERK = "the clerk sorting a crate of jazz records";
const WCAG21_AA = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"];
// Thirty short lines, under the 1,000-character limit: taller than the
// report queue's 20-line box, so that box scrolls.
const LONG_POST = Array.from({ length: 30 }, (_, i) => `Bin ${i + 1}: Coltrane, Monk, Mingus.`).join("\n");

type Finding = { route: string; id: string; impact: string; help: string; targets: string[] };

/**
 * Scans the page as it stands, once `ready` (by default its level-one
 * heading) shows, and returns its serious and critical WCAG 2.1 A/AA
 * violations.
 */
async function scan(page: Page, route: string, ready: Locator = page.getByRole("heading", { level: 1 })): Promise<Finding[]> {
  await expect(ready.first()).toBeVisible();
  await page.waitForLoadState("networkidle");
  const result = await new AxeBuilder({ page }).withTags(WCAG21_AA).analyze();
  return result.violations
    .filter((v) => v.impact === "serious" || v.impact === "critical")
    .map((v) => ({
      route,
      id: v.id,
      impact: v.impact ?? "",
      help: v.help,
      targets: v.nodes.map((n) => n.target.join(" ")),
    }));
}

/** Opens `path` signed in, waits for `ready` when given, and scans it. */
async function scanAs(w: Writer, path: string, ready?: (page: Page) => Locator): Promise<Finding[]> {
  await visit(w.page, w.account, path);
  if (ready) await expect(ready(w.page).first()).toBeVisible();
  return scan(w.page, path);
}

/**
 * Scans the page as it stands in Paper and then in Lamplight, forced on the
 * root element, and puts the reader's own theme back.
 */
async function scanBothThemes(page: Page, route: string, ready?: Locator): Promise<Finding[]> {
  const found: Finding[] = [];
  for (const theme of ["light", "dark"]) {
    await page.evaluate((t) => (document.documentElement.dataset.theme = t), theme);
    found.push(...(await scan(page, `${route} (${theme})`, ready)));
  }
  await page.evaluate(() => delete document.documentElement.dataset.theme);
  return found;
}

function report(findings: Finding[]): string {
  return findings
    .map((f) => `${f.route}: [${f.impact}] ${f.id}: ${f.help}\n${f.targets.map((t) => `    ${t}`).join("\n")}`)
    .join("\n");
}

test("NFR-011 the main routes have no serious or critical WCAG 2.1 A/AA violations", async ({ browser }) => {
  test.setTimeout(360_000);
  const { ivy: ivyAcct } = a11yAccounts();
  const { gil: gilAcct } = findAccounts();
  const ivy = await signedIn(browser, ivyAcct);
  const gil = await signInWithPassword(browser, gilAcct);
  const findings: Finding[] = [];

  // Content to read: Ivy's story with a character, Gil's beside it, and Gil's claim on it.
  const { arc, perspective } = await writeAt(ivy, STORE, {
    title: "Record Store Day line around the block",
    genre: "Slice of life",
    parts: ["The line started before dawn.", "By noon the jazz bins were empty."],
    characters: [CLERK],
  });
  const gilStory = await writeInArc(gil, arc, { genre: "Comedy", parts: ["I sorted that crate three times and nobody bought a thing."] });
  await visit(gil.page, gilAcct, arc);
  await gil.page.getByRole("link", { name: `Read ${ivyAcct.username}'s story` }).click();
  const actions = gil.page.getByRole("region", { name: "Respond to this story" });
  await actions.getByRole("button", { name: "Claim a character" }).click();
  const dialog = gil.page.getByRole("dialog", { name: "Claim a character" });
  await dialog.getByRole("listitem").filter({ hasText: CLERK }).getByRole("button", { name: "That was me" }).click();
  await expect(dialog.getByRole("status")).toContainText("Claim sent.");
  // The claim dialog, open, is part of the story page a reader sees.
  findings.push(...(await scan(gil.page, `${perspective} (claim dialog)`, dialog)));
  await gil.page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();

  // A pending claim on Ivy's requests page, then accepted, which links a lore.
  const claims = (p: Page) => p.getByRole("list", { name: "Claims on your characters" }).getByRole("listitem");
  findings.push(...(await scanAs(ivy, "/claims", claims)));
  await claims(ivy.page).filter({ hasText: CLERK }).getByRole("button", { name: "Accept" }).click();
  await expect(claims(ivy.page).getByText(/^Linked\./)).toBeVisible();

  await visit(ivy.page, ivyAcct, arc);
  const place = await ivy.page.getByRole("navigation", { name: "Place" }).getByRole("link", { name: STORE }).getAttribute("href");
  const lore = await ivy.page.getByRole("link", { name: /Open (this lore|the canon)/ }).first().getAttribute("href");
  expect(place).toMatch(/^\/places\//);
  expect(lore).toMatch(/^\/lores\//);

  // The thread: Ivy posts, and her post takes focus; Gil upvotes it, which
  // is announced, replies, and reports it for the queue below.
  await visit(ivy.page, ivyAcct, lore as string);
  const thread = (p: Page) => p.getByRole("region", { name: "Thread" });
  const ivysPost = (p: Page) => thread(p).getByRole("article", { name: `${ivyAcct.username}'s post` });
  await thread(ivy.page).getByRole("textbox", { name: "Add to the thread" }).fill(LONG_POST);
  await thread(ivy.page).getByRole("button", { name: "Post", exact: true }).click();
  await expect(ivysPost(ivy.page)).toBeFocused();
  await visit(gil.page, gilAcct, lore as string);
  await ivysPost(gil.page).getByRole("button", { name: `Upvote ${ivyAcct.username}'s post` }).click();
  await expect(ivysPost(gil.page).getByRole("status")).toHaveText("Upvoted. 1 upvote.");
  await ivysPost(gil.page).getByRole("button", { name: "Reply" }).click();
  await thread(gil.page).getByRole("textbox", { name: `Reply to ${ivyAcct.username}` }).fill("Me. Sorry about Monk.");
  await thread(gil.page).getByRole("button", { name: "Post reply" }).click();
  const gilsReply = thread(gil.page).getByRole("article", { name: `${gilAcct.username}'s post` });
  await expect(gilsReply).toBeFocused();
  await ivysPost(gil.page).getByRole("button", { name: /^Report/ }).click();
  const reportDialog = gil.page.getByRole("dialog", { name: `Report ${ivyAcct.username}'s post` });
  await reportDialog.getByRole("combobox", { name: "Reason" }).click();
  await gil.page.getByRole("option").first().click();
  await reportDialog.getByRole("button", { name: "Send report" }).click();
  await expect(reportDialog.getByRole("status")).toBeVisible();
  await gil.page.keyboard.press("Escape");
  findings.push(...(await scanBothThemes(gil.page, `${lore} (thread, as a member)`, gilsReply)));

  // The lore as a book. Reading a chapter first leaves a place to continue
  // from, so the title page is scanned with its "Continue" offer.
  const book: [string, (p: Page) => Locator][] = [
    [`${lore}/book?at=contents`, (p) => p.getByRole("list", { name: "Chapters" })],
    [`${lore}/book?chapter=1&part=1`, (p) => p.getByRole("heading", { level: 1, name: /^Chapter 1: / })],
    // Past the last part opens the last: the chapter's end, where Next reads
    // "Next chapter" (or "The end" on the last chapter).
    [`${lore}/book?chapter=2&part=9`, (p) => p.getByRole("navigation", { name: "Pages" }).getByRole("button", { name: /^(Next chapter|The end)$/ })],
    [`${lore}/book`, (p) => p.getByRole("link", { name: "Continue where you left off" })],
    [`${lore}/book?at=end`, (p) => p.getByRole("link", { name: "Read its thread" })],
  ];
  for (const [path, ready] of book) {
    await visit(ivy.page, ivyAcct, path);
    findings.push(...(await scanBothThemes(ivy.page, path, ready(ivy.page))));
  }

  // Ivy saves Gil's story and follows him, so Saved, his profile and
  // Settings' "Writers you follow" have something in them.
  await visit(ivy.page, ivyAcct, gilStory.perspective);
  await ivy.page.getByRole("button", { name: `Save ${gilAcct.username}'s story` }).click();
  await expect(ivy.page.getByRole("button", { name: `Saved ${gilAcct.username}'s story` })).toHaveAttribute("aria-pressed", "true");
  await visit(ivy.page, ivyAcct, "/saved");
  findings.push(...(await scanBothThemes(ivy.page, "/saved", ivy.page.getByRole("list", { name: "Saved stories" }))));
  await visit(ivy.page, ivyAcct, `/users/${gilAcct.username}`);
  await ivy.page.getByRole("button", { name: `Follow ${gilAcct.username}` }).click();
  const following = ivy.page.getByRole("button", { name: `Following ${gilAcct.username}` });
  await expect(following).toHaveAttribute("aria-pressed", "true");
  findings.push(...(await scanBothThemes(ivy.page, `/users/${gilAcct.username}`, following)));
  // Ivy's profile on its Thread posts tab, which lists her post above.
  await visit(ivy.page, ivyAcct, `/users/${ivyAcct.username}?tab=posts`);
  const posts = ivy.page.getByRole("list", { name: "Thread posts" });
  // Each row opens the lore at the post itself.
  await expect(posts.getByRole("link").first()).toHaveAttribute("href", new RegExp(`^${lore as string}#post-[0-9a-f-]+$`));
  await expect(ivy.page.getByRole("tab", { name: "Thread posts" })).toHaveAttribute("aria-selected", "true");
  findings.push(...(await scanBothThemes(ivy.page, `/users/${ivyAcct.username}?tab=posts`, posts)));
  await visit(ivy.page, ivyAcct, "/appearance");
  findings.push(...(await scanBothThemes(ivy.page, "/appearance", ivy.page.getByRole("radio", { name: "Larger" }))));

  const member: [string, ((page: Page) => Locator)?][] = [
    ["/", (p) => p.getByRole("heading", { level: 1, name: `Welcome back, ${ivyAcct.username}` })],
    [place as string, (p) => p.getByRole("link", { name: "Record Store Day line around the block" })],
    ["/best/shopping", (p) => p.getByRole("link", { name: "Record Store Day line around the block" })],
    [arc, (p) => p.getByRole("article", { name: `${gilAcct.username}'s story` })],
    [perspective, (p) => p.getByText("The line started before dawn.")],
    [lore as string, (p) => p.getByText("The line started before dawn.")],
    [`/users/${ivyAcct.username}`, (p) => p.getByRole("link", { name: /Record Store Day/ })],
    ["/compose", (p) => p.getByRole("searchbox", { name: "Where were you?" })],
    ["/inbox", (p) => p.getByRole("listitem")],
    ["/settings", (p) => p.getByRole("list", { name: "Signed-in devices" })],
    ["/terms"],
    ["/privacy"],
    ["/guidelines"],
  ];
  for (const [path, ready] of member) findings.push(...(await scanAs(ivy, path, ready)));

  const adminRoutes: [string, ((page: Page) => Locator)?][] = [
    ["/admin/reports"],
    ["/admin/audit"],
    [`/admin/users?q=${ivyAcct.username}`, (p) => p.getByRole("article")],
    ["/admin/access"],
  ];
  for (const [path, ready] of adminRoutes) findings.push(...(await scanAs(ivy, path, ready)));

  // The queue's copy of Ivy's post, opened: taller than its box, so it scrolls.
  await visit(ivy.page, ivyAcct, "/admin/reports");
  await ivy.page.getByText("What it said when reported").first().click();
  const copy = ivy.page.getByRole("region", { name: "What it said when reported" }).first();
  await expect(copy).toContainText("Bin 30");
  expect(await copy.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
  findings.push(...(await scanBothThemes(ivy.page, "/admin/reports (what it said, open)", copy)));

  const anon = await reader(browser);
  for (const path of ["/login", "/signup"]) {
    await anon.goto(path);
    findings.push(...(await scan(anon, path)));
  }
  // The guide, as a first-time visitor meets it from the footer.
  await anon.goto("/how-it-works");
  findings.push(...(await scanBothThemes(anon, "/how-it-works", anon.getByRole("heading", { level: 2, name: "Your privacy" }))));

  expect(findings, `axe found serious or critical WCAG 2.1 A/AA violations:\n${report(findings)}`).toEqual([]);
});
