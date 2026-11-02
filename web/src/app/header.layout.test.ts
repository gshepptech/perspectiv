// @vitest-environment node
import { fileURLToPath } from "node:url";

import { chromium, type Browser, type Page } from "@playwright/test";
import { createServer, type ViteDevServer } from "vite";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

/**
 * The header measured in a real browser (jsdom has no layout): the count
 * badges on Inbox and Requests never cover a letter of their words, and the
 * header search dialog fits the viewport, at phone and desktop widths. The
 * Connect calls are answered at the network boundary.
 */

const widths = [
  { name: "phone", viewport: { width: 375, height: 812 } },
  { name: "desktop", viewport: { width: 1280, height: 800 } },
];

const place = {
  id: "p-store",
  kind: "PLACE_KIND_STORE",
  name: "Starbucks",
  parentId: "p-city",
  timezone: "America/New_York",
  category: "PLACE_CATEGORY_FOOD_AND_DRINK",
  stories: 3,
  ancestors: [
    { id: "p-state", kind: "PLACE_KIND_STATE", name: "Florida" },
    { id: "p-city", kind: "PLACE_KIND_CITY", name: "Tampa" },
  ],
};

function reply(procedure: string, unread: number): unknown {
  switch (procedure) {
    case "AuthService/Refresh":
      return {
        accessToken: "tok",
        accessTokenExpiresAt: "2030-01-01T00:00:00Z",
        user: { userId: "u-me", username: "wren", role: "ROLE_USER", emailVerified: true },
      };
    case "NotifyService/ListInbox":
      return { unreadCount: unread };
    case "LoreService/ListClaims":
      // A full page and a next one: the badge reads "99+".
      return { claims: Array.from({ length: 100 }, (_, i) => ({ id: `cl-${i}`, status: "CLAIM_STATUS_PENDING" })), nextPageToken: "n" };
    case "PlaceService/SearchPlaces":
      return { places: Array.from({ length: 20 }, (_, i) => ({ ...place, id: `p-${i}` })), more: true };
    default:
      return null;
  }
}

let server: ViteDevServer;
let browser: Browser;
let base: string;

beforeAll(async () => {
  server = await createServer({
    configFile: fileURLToPath(new URL("../../vite.config.ts", import.meta.url)),
    logLevel: "silent",
    server: { host: "127.0.0.1", port: 0, strictPort: false, hmr: false },
  });
  await server.listen();
  base = server.resolvedUrls?.local[0]?.replace(/\/$/, "") ?? "";
  browser = await chromium.launch();
}, 120_000);

// Closing a real browser and a Vite server outlasts the default 10 s hook
// timeout on a busy machine, as launching them does.
afterAll(async () => {
  await browser?.close();
  await server?.close();
}, 60_000);

async function memberPage(viewport: { width: number; height: number }, unread: number): Promise<Page> {
  const ctx = await browser.newContext({ viewport });
  await ctx.route("**/api/perspectiv.v1.*/*", async (route) => {
    const procedure = new URL(route.request().url()).pathname.replace("/api/perspectiv.v1.", "");
    const body = reply(procedure, unread);
    await route.fulfill(
      body === null
        ? { status: 404, contentType: "application/json", body: JSON.stringify({ code: "unimplemented" }) }
        : { status: 200, contentType: "application/json", body: JSON.stringify(body) },
    );
  });
  return ctx.newPage();
}

/** How many pixels a link's badge overlaps the box of its word, in each axis. */
async function badgeOverlap(page: Page, name: string) {
  const link = page.getByRole("link", { name });
  await link.waitFor();
  return link.evaluate((el) => {
    const word = [...el.children].find((c) => c.tagName === "SPAN" && !c.querySelector("svg"));
    const badge = el.querySelector(".bg-accent");
    if (!word || !badge) throw new Error("word or badge missing");
    // The word's ink: a range over its text, not the span's box.
    const range = document.createRange();
    range.selectNodeContents(word);
    const w = range.getBoundingClientRect();
    const b = badge.getBoundingClientRect();
    return {
      x: Math.min(w.right, b.right) - Math.max(w.left, b.left),
      y: Math.min(w.bottom, b.bottom) - Math.max(w.top, b.top),
    };
  });
}

describe.each(widths)("header at $name width", ({ viewport }) => {
  it.each([
    { unread: 7, badge: "7" },
    { unread: 250, badge: "99+" },
  ])("keeps the $badge badge off the Inbox and Requests words", async ({ unread, badge }) => {
    const page = await memberPage(viewport, unread);
    await page.goto(base + "/terms");
    for (const name of [`Inbox, ${badge} unread`, "Requests, 99+ waiting"]) {
      const o = await badgeOverlap(page, name);
      // Overlapping boxes overlap in both axes.
      expect(o.x <= 0 || o.y <= 0, `${name}: badge overlaps its word by ${o.x}x${o.y}px`).toBe(true);
    }
    const scroll = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(scroll).toBeLessThanOrEqual(0);
    await page.context().close();
  }, 60_000);

  it("opens the search dialog with / and fits it to the viewport", async () => {
    const page = await memberPage(viewport, 2);
    await page.goto(base + "/terms");
    await page.getByRole("button", { name: "Search" }).waitFor();
    await page.keyboard.press("/");
    const dialog = page.getByRole("dialog", { name: "Search places" });
    await dialog.waitFor();
    await page.keyboard.type("starbucks");
    await dialog.getByText("More places match: add a town to narrow it", { exact: true }).waitFor();
    const box = await dialog.boundingBox();
    if (!box) throw new Error("dialog has no box");
    expect(box.x).toBeGreaterThanOrEqual(16 - 1);
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width - 16 + 1);
    expect(box.y + box.height).toBeLessThanOrEqual(viewport.height);
    const scroll = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(scroll).toBeLessThanOrEqual(0);
    await page.context().close();
  }, 60_000);
});
