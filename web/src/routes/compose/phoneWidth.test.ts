// @vitest-environment node
import { fileURLToPath } from "node:url";

import { chromium, type Browser, type Page } from "@playwright/test";
import { createServer, type ViteDevServer } from "vite";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

/**
 * The writing routes at phone width, measured in a real browser: jsdom has no
 * layout, so this serves the app through Vite and loads each route in
 * Chromium at 375 pixels with the Connect calls answered at the network
 * boundary. Content is long but valid: a real 58-letter place name, a
 * 20-character username and long free text.
 */

const PHONE = { width: 375, height: 812 };
const ts = "2026-10-15T20:00:00Z";
const longWord = "Chargoggagoggmanchauggagoggchaubunagungamaugg";
const storeName = `${longWord} General Store`;
const username = "WWWWWWWWWWWWWWWWWWW_";
const title = `${longWord} at midnight in the rain`.slice(0, 80);
const label = "the tall woman in the long green raincoat carrying a very large striped umbrella";
const ancestors = [
  { id: "p-state", kind: "PLACE_KIND_STATE", name: "Massachusetts" },
  { id: "p-mall", kind: "PLACE_KIND_MALL", name: "Northshore Mall and Lifestyle Center Outlet Promenade" },
];
const store = {
  id: "p-store",
  kind: "PLACE_KIND_STORE",
  name: storeName,
  parentId: "p-mall",
  timezone: "America/New_York",
  userAdded: true,
  ancestors,
};
const user = (id: string) => ({ userId: id, username });

function reply(procedure: string): unknown {
  switch (procedure) {
    case "AuthService/Refresh":
      return {
        accessToken: "tok",
        accessTokenExpiresAt: "2030-01-01T00:00:00Z",
        user: { userId: "u-me", username, role: "ROLE_ADMIN", emailVerified: true },
      };
    case "PlaceService/GetPlace":
      return { place: store };
    case "PlaceService/SearchPlaces":
      return { places: [store] };
    case "PerspectiveService/Get":
      return {
        perspective: {
          id: "pub-1",
          arcId: "a-day",
          arcTitle: title,
          author: user("u-me"),
          genre: "GENRE_MYSTERY",
          status: "PERSPECTIVE_STATUS_PUBLISHED",
          loreStatus: "LORE_STATUS_PENDING",
          place: { id: store.id, kind: store.kind, name: storeName },
          localDate: "2026-10-15",
          hour: 14,
          parts: [{ position: 1, body: `${longWord} `.repeat(20) }],
          characters: [{ id: "c-1", position: 1, label, hasAcceptedClaim: true }],
        },
      };
    case "PerspectiveService/ListMine":
      return { perspectives: [] };
    case "LoreService/ListClaims":
      return {
        claims: [0, 1, 2].map((i) => ({
          id: `cl${i}`,
          arcId: "a-day",
          characterId: `c${i}`,
          characterLabel: label,
          perspectiveId: "v1",
          author: user("u-a"),
          claimant: user("u-b"),
          claimantPerspectiveId: "v2",
          status: "CLAIM_STATUS_PENDING",
          createdAt: ts,
        })),
      };
    case "ModerationService/ListReports":
      return {
        reports: ["PERSPECTIVE", "PLACE", "USER"].map((k, i) => ({
          id: `r${i}`,
          reporter: user("u-r"),
          targetKind: `REPORT_TARGET_KIND_${k}`,
          targetId: `x${i}`,
          targetLabel: k === "PLACE" ? storeName : title,
          reason: "REPORT_REASON_REAL_NAME_OR_HANDLE_OF_NON_USER",
          details: `${longWord} `.repeat(12),
          status: "REPORT_STATUS_OPEN",
          createdAt: ts,
        })),
      };
    default:
      return null;
  }
}

let server: ViteDevServer;
let browser: Browser;
let base: string;

beforeAll(async () => {
  server = await createServer({
    configFile: fileURLToPath(new URL("../../../vite.config.ts", import.meta.url)),
    logLevel: "silent",
    server: { host: "127.0.0.1", port: 0, strictPort: false, hmr: false },
  });
  await server.listen();
  base = server.resolvedUrls?.local[0]?.replace(/\/$/, "") ?? "";
  browser = await chromium.launch();
}, 120_000);

afterAll(async () => {
  await browser?.close();
  await server?.close();
});

async function phonePage(): Promise<Page> {
  const ctx = await browser.newContext({ viewport: PHONE });
  await ctx.route("**/api/perspectiv.v1.*/*", async (route) => {
    const procedure = new URL(route.request().url()).pathname.replace("/api/perspectiv.v1.", "");
    const body = reply(procedure);
    await route.fulfill(
      body === null
        ? { status: 404, contentType: "application/json", body: JSON.stringify({ code: "unimplemented" }) }
        : { status: 200, contentType: "application/json", body: JSON.stringify(body) },
    );
  });
  return ctx.newPage();
}

/** The document's scroll width and the elements that stick out past the viewport. */
async function overflow(page: Page) {
  return page.evaluate(() => {
    const vw = window.innerWidth;
    const offenders = [...document.querySelectorAll("body *")]
      .filter((el) => el.getBoundingClientRect().right > vw + 1)
      .map((el) => `${el.tagName.toLowerCase()} "${(el.textContent ?? "").trim().slice(0, 40)}"`)
      .slice(0, 5);
    return { scrollWidth: document.documentElement.scrollWidth, offenders };
  });
}

describe("writing routes at 375 pixels (OT-037, AC-034)", () => {
  it.each([
    ["/compose?placeId=p-store", "Day of your visit"],
    ["/compose?perspectiveId=pub-1", "Part 1"],
    ["/compose", "Where were you?"],
    ["/claims", null],
    ["/admin/reports", null],
  ])(
    "%s does not scroll sideways with long content",
    async (path, waitForLabel) => {
      const page = await phonePage();
      await page.goto(base + path);
      await page.locator("h1").first().waitFor();
      if (waitForLabel) await page.getByLabel(waitForLabel).first().waitFor();
      await page.waitForLoadState("networkidle");
      const got = await overflow(page);
      expect(got.offenders).toEqual([]);
      expect(got.scrollWidth).toBe(PHONE.width);
      await page.context().close();
    },
    120_000,
  );
});
