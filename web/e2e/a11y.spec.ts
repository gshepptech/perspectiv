/**
 * NFR-011: an axe scan of the pages a signed-in member reads and writes on,
 * the admin pages, and the signed-out sign-in and sign-up pages. Two writers
 * publish at a store no other spec writes at and link by a claim, so the
 * place, arc, story, claims and lore pages are scanned with real content.
 * The second writer is the find-yourself spec's Gil, signed in with his
 * password in a session of his own (see a11yAccounts for why).
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

function report(findings: Finding[]): string {
  return findings
    .map((f) => `${f.route}: [${f.impact}] ${f.id}: ${f.help}\n${f.targets.map((t) => `    ${t}`).join("\n")}`)
    .join("\n");
}

test("NFR-011 the main routes have no serious or critical WCAG 2.1 A/AA violations", async ({ browser }) => {
  test.setTimeout(240_000);
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
  await writeInArc(gil, arc, { genre: "Comedy", parts: ["I sorted that crate three times and nobody bought a thing."] });
  await visit(gil.page, gilAcct, arc);
  await gil.page.getByRole("link", { name: `Read ${ivyAcct.username}'s perspective` }).click();
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

  const member: [string, ((page: Page) => Locator)?][] = [
    ["/", (p) => p.getByRole("heading", { level: 1, name: `Welcome back, ${ivyAcct.username}` })],
    [place as string, (p) => p.getByRole("link", { name: "Record Store Day line around the block" })],
    ["/best/shopping", (p) => p.getByRole("link", { name: "Record Store Day line around the block" })],
    [arc, (p) => p.getByRole("article", { name: `${gilAcct.username}'s perspective` })],
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

  const anon = await reader(browser);
  for (const path of ["/login", "/signup"]) {
    await anon.goto(path);
    findings.push(...(await scan(anon, path)));
  }

  expect(findings, `axe found serious or critical WCAG 2.1 A/AA violations:\n${report(findings)}`).toEqual([]);
});
