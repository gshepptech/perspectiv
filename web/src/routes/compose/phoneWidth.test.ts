// @vitest-environment node
import { fileURLToPath } from "node:url";

import { chromium, type Browser, type Page } from "@playwright/test";
import { createServer, type ViteDevServer } from "vite";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

/**
 * Every page at phone width (OT-037, AC-034), measured in a real browser:
 * jsdom has no layout, so this serves the app through Vite and loads each
 * route in Chromium at 375 pixels with the Connect calls answered at the
 * network boundary. Content is long but valid: a real 45-letter place name,
 * 20-character usernames, 80-character titles, long free text and four-digit
 * react counts, so anything that cannot wrap shows up as overflow.
 */

const PHONE = { width: 375, height: 812 };
const ts = "2026-10-15T20:00:00Z";
const longWord = "Chargoggagoggmanchauggagoggchaubunagungamaugg";
const storeName = `${longWord} General Store`;
const username = "WWWWWWWWWWWWWWWWWWW_";
const title = `${longWord} at midnight in the rain`.slice(0, 80);
const longTitle = "The night the whole Northshore Mall food court sang along to the broken jukebox".slice(0, 80);
const label = "the tall woman in the long green raincoat carrying a very large striped umbrella";
const prose = `${longWord} `.repeat(20);
const city = "Llanfairpwllgwyngyllgogerychwyrndrobwllllantysiliogogogoch";
const ancestors = [
  { id: "p-state", kind: "PLACE_KIND_STATE", name: "Massachusetts" },
  { id: "p-city", kind: "PLACE_KIND_CITY", name: city },
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
  following: true,
  stories: 1234,
};
const storeRef = { id: store.id, kind: store.kind, name: storeName };
/** Distinct ids, all with the longest username the server allows. */
const user = (id: string) => ({ userId: id, username });
const EMOJI = ["HEART", "LAUGH", "WOW", "SAD", "FIRE", "CLAP"];
const reacts = EMOJI.map((e, i) => ({ emoji: `EMOJI_${e}`, count: 9999 - i * 1111, mine: i % 2 === 0 }));

function summary(id: string, loreStatus = "LORE_STATUS_IN_LORE", loreId = "lore-canon") {
  return {
    id,
    arcId: "a-day",
    author: user(`u-${id}`),
    genre: "GENRE_MYSTERY",
    loreStatus,
    loreId,
    excerpt: prose,
    partCount: 12,
    upvotes: 98765,
    publishedAt: ts,
  };
}

function loreOf(id: string, standing: string, n: number) {
  const members = Array.from({ length: n }, (_, i) => ({ perspectiveId: `${id}-p${i}`, user: user(`u-${id}-${i}`) }));
  return {
    lore: {
      id,
      arcId: "a-day",
      standing: `LORE_STANDING_${standing}`,
      memberCount: n,
      upvotes: 54321,
      rankScore: 9.5,
      computedAt: ts,
      members,
      encounters: [],
      name: longTitle,
      namedBy: user("u-namer"),
    },
    perspectives: members.map((m) => summary(m.perspectiveId, "LORE_STATUS_IN_LORE", id)),
  };
}

const dayArc = {
  id: "a-day",
  title: longTitle,
  place: storeRef,
  placeChain: [...ancestors, storeRef],
  localDate: "2026-10-15",
  parentArcId: "",
  canonLoreId: "lore-canon",
  canonScore: 12.5,
  memberCount: 6,
  createdBy: user("u-maker"),
  createdAt: ts,
};

const epicArcs = [0, 1, 2].map((i) => ({
  arcId: `a-${i}`,
  title: longTitle,
  place: { id: `p-s${i}`, kind: "PLACE_KIND_STORE", name: storeName },
  localDate: "2026-10-15",
  hour: i === 1 ? 23 : undefined,
  canonLoreId: i === 0 ? "lore-canon" : "",
  canonScore: 9.5,
  memberCount: 12345,
  perspectiveCount: 23456,
  createdAt: ts,
  city,
  excerpt: prose,
}));

function perspective(id: string) {
  return {
    id,
    arcId: "a-day",
    arcTitle: longTitle,
    author: user("u-author"),
    genre: "GENRE_MYSTERY",
    status: "PERSPECTIVE_STATUS_PUBLISHED",
    loreStatus: "LORE_STATUS_IN_LORE",
    loreId: "lore-canon",
    place: storeRef,
    localDate: "2026-10-15",
    hour: 14,
    parts: [1, 2, 3].map((position) => ({ position, body: prose })),
    characters: [
      { id: "c-1", position: 1, label, hasAcceptedClaim: true, claimedBy: user("u-claimer") },
      { id: "c-2", position: 2, label: `${label} and a second hat` },
    ],
    upvotes: 98765,
    voted: true,
    reacts,
    publishedAt: ts,
  };
}

function reply(procedure: string, body: Record<string, unknown>): unknown {
  switch (procedure) {
    case "PlaceService/GetPlace":
      return { place: store };
    case "PlaceService/SearchPlaces":
      return { places: [store] };
    case "PlaceService/ListChildren":
      if (!body.parentId) {
        // The top level is the states; the longest real names.
        return {
          places: ["District of Columbia", "Massachusetts", "North Carolina", "South Carolina", "West Virginia"].map((name, i) => ({
            id: `st-${i}`,
            kind: "PLACE_KIND_STATE",
            name,
            stories: 4321,
          })),
        };
      }
      return {
        places: [0, 1, 2, 3].map((i) => ({
          id: `p-c${i}`,
          kind: "PLACE_KIND_STORE",
          name: `${storeName} number ${i}`,
          parentId: body.parentId,
          stories: 4321,
        })),
      };
    case "PlaceService/ListFollowing":
      return { places: [store, { ...store, id: "p-store2", name: `${longWord} Hardware and Feed Supply` }] };
    case "PlaceService/GetEpic":
      if (body.tab === "EPIC_TAB_TALES") {
        return {
          place: store,
          tales: [0, 1].map((i) => ({
            perspectiveId: `t-${i}`,
            arcId: "a-day",
            arcTitle: longTitle,
            place: storeRef,
            localDate: "2026-10-15",
            author: user(`u-t${i}`),
            genre: "GENRE_DRAMA",
            loreStatus: "LORE_STATUS_SOLO",
            publishedAt: ts,
            excerpt: prose,
          })),
        };
      }
      return { place: store, arcs: epicArcs };
    case "PlaceService/SearchCharacters":
      return { characters: [] };
    case "PlaceService/ListStoriedCategories":
      // Every kind, so the front page's row wraps at its longest.
      return {
        categories: [
          "PLACE_CATEGORY_FOOD_AND_DRINK", "PLACE_CATEGORY_NIGHTLIFE", "PLACE_CATEGORY_SHOPPING", "PLACE_CATEGORY_OUTDOORS",
          "PLACE_CATEGORY_ARTS_AND_ENTERTAINMENT", "PLACE_CATEGORY_SPORTS_AND_RECREATION", "PLACE_CATEGORY_CULTURE_AND_HISTORY",
          "PLACE_CATEGORY_EDUCATION", "PLACE_CATEGORY_COMMUNITY", "PLACE_CATEGORY_TRAVEL", "PLACE_CATEGORY_LODGING",
          "PLACE_CATEGORY_SERVICES", "PLACE_CATEGORY_HEALTH",
        ],
      };
    case "ArcService/GetArc":
      return {
        arc: dayArc,
        viewer: { perspectiveId: "lore-canon-p0", loreId: "lore-canon" },
        canon: loreOf("lore-canon", "CANON", 6),
        sublores: [loreOf("lore-sub", "SUBLORE", 3)],
        tales: [summary("p-solo", "LORE_STATUS_SOLO", ""), summary("p-gone", "LORE_STATUS_REMOVED", "")],
        unlinked: [summary("p-new", "LORE_STATUS_PENDING", "")],
        hours: [
          {
            arc: { ...dayArc, id: "a-h23", hour: 23, parentArcId: "a-day", canonLoreId: "lore-h23" },
            canon: loreOf("lore-h23", "CANON", 4),
            perspectiveCount: 23456,
          },
          { arc: { ...dayArc, id: "a-h9", hour: 9, parentArcId: "a-day", canonLoreId: "" }, perspectiveCount: 1 },
        ],
      };
    case "ArcService/ListOpenCharacters":
      return {
        characters: [0, 1, 2].map((i) => ({ characterId: `oc${i}`, label, perspectiveId: `p-o${i}`, author: user(`u-o${i}`) })),
      };
    case "ArcService/ListArcSection":
      return {};
    case "PerspectiveService/Get":
      return { perspective: perspective(String(body.perspectiveId ?? "pub-1")) };
    case "PerspectiveService/ListMine":
      return {
        perspectives: [
          {
            id: "d-1",
            status: "PERSPECTIVE_STATUS_DRAFT",
            titleIfNewArc: longTitle,
            place: storeRef,
            localDate: "2026-10-15",
            parts: [{ position: 1, body: prose }],
          },
          { ...perspective("pub-2"), loreStatus: "LORE_STATUS_SOLO" },
        ],
      };
    case "PerspectiveService/ListSaved":
      return { perspectives: [perspective("pub-5"), { ...perspective("pub-6"), loreStatus: "LORE_STATUS_SOLO" }], nextPageToken: "more" };
    case "PerspectiveService/ListByAuthor":
      return { perspectives: [perspective("pub-3"), { ...perspective("pub-4"), loreStatus: "LORE_STATUS_SOLO" }] };
    case "LoreService/GetLore":
      return { lore: loreOf("lore-canon", "CANON", 6).lore };
    case "LoreService/ListLorePosts": {
      // Replies one step in, one answering another by a long name, an
      // edited post with a four-digit score, and a deleted post kept for
      // its replies.
      const post = (id: string, more: Record<string, unknown> = {}) => ({
        id,
        author: user(`u-${id}`),
        body: prose,
        createdAt: ts,
        editedAt: ts,
        upvotes: 1234,
        byMember: true,
        ...more,
      });
      const replies = [0, 1, 2].map((i) => post(`lr${i}`, { replyToId: "lp0", replyingTo: i > 0 ? user("u-lr0") : undefined }));
      return {
        posts: [
          post("lp0", { replyCount: 7, replies, moreRepliesToken: "more" }),
          post("lp1", { byMember: false }),
          { id: "lp2", deleted: true, createdAt: ts, replyCount: 1, replies: [post("lr3", { replyToId: "lp2" })] },
        ],
      };
    }
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
    case "UserService/GetProfile":
      return {
        // Another writer's profile, so the Follow toggle shows.
        following: body.username === "other",
        followers: 12345,
        profile: {
          userId: body.username === "other" ? "u-other" : "u-me",
          username,
          bio: prose.slice(0, 280),
          joinedAt: ts,
          canonCount: 4321,
          loreCount: 8765,
          storyCount: 123456,
          upvotesReceived: 9876543,
          peopleFound: 54321,
          placesWritten: 12345,
        },
        account: {
          email: `${longWord.toLowerCase()}.${longWord.toLowerCase()}@example.com`,
          emailVerified: true,
          role: "ROLE_ADMIN",
          usernameChangedAt: ts,
          nextUsernameChangeAt: "2030-01-01T00:00:00Z",
          blocked: [user("u-bl1"), user("u-bl2")],
        },
      };
    case "UserService/ListUserPosts":
      // A long lore name, an hour arc's long title at the long store, a
      // full-length excerpt with an unbreakable word, and a reply.
      return {
        posts: [0, 1].map((i) => ({
          id: `up-${i}`,
          loreId: "lore-canon",
          loreName: i === 0 ? longTitle : "",
          arcId: "a-day",
          arcTitle: `${title.slice(0, 72)} · 23:00`,
          localDate: "2026-10-15",
          hour: 23,
          placeName: storeName,
          excerpt: `${prose.slice(0, 239)}…`,
          reply: i === 1,
          createdAt: ts,
        })),
        nextPageToken: "more",
      };
    case "UserService/ListFollowedWriters":
      return { writers: [user("u-fw1"), user("u-fw2")].map((w) => ({ ...w, avatarUrl: "/avatars/fw.png", followedAt: ts })) };
    case "AuthService/GetSignUpInfo":
      return { inviteOnly: false };
    case "AuthService/ListSessions":
      return {
        sessions: [0, 1].map((i) => ({
          sessionId: `s${i}`,
          device: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
          signedInAt: ts,
          lastUsedAt: ts,
          current: i === 0,
        })),
      };
    case "NotifyService/GetPreferences":
      return {
        preferences: [
          "CLAIM_RECEIVED",
          "CLAIM_ACCEPTED",
          "NEW_PERSPECTIVE_IN_ARC",
          "VOTE_OUT_OPENED",
          "CANON_CHANGE",
          "REPORT_RESOLVED",
          "INVITE_ANSWERED",
          "NEW_STORY_AT_FOLLOWED_PLACE",
          "NEW_STORY_BY_FOLLOWED_WRITER",
          "THREAD_REPLY",
        ].map((k, i) => ({ kind: `NOTIFICATION_KIND_${k}`, inApp: true, email: i % 2 === 0 })),
        emailDigest: true,
      };
    case "NotifyService/ListInbox":
      return {
        unreadCount: 4,
        notifications: [
          "CLAIM_RECEIVED",
          "CLAIM_ACCEPTED",
          "NEW_PERSPECTIVE_IN_ARC",
          "VOTE_OUT_OPENED",
          "CANON_CHANGE",
          "REPORT_RESOLVED",
          "INVITE_ANSWERED",
          "NEW_STORY_AT_FOLLOWED_PLACE",
          "NEW_STORY_BY_FOLLOWED_WRITER",
          "THREAD_REPLY",
        ].map((k, i) => ({
          id: `n${i}`,
          kind: `NOTIFICATION_KIND_${k}`,
          actor: user(`u-n${i}`),
          arcId: "a-day",
          arcTitle: longTitle,
          perspectiveId: "pub-1",
          claimId: "cl0",
          loreId: "lore-canon",
          reportId: "r0",
          createdAt: ts,
          readAt: i % 2 ? ts : undefined,
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
          reportedText: `${prose}\n`.repeat(6),
          status: "REPORT_STATUS_OPEN",
          createdAt: ts,
        })),
      };
    case "ModerationService/ListAuditLog":
      return {
        entries: [
          ["HIDE_PERSPECTIVE", "PERSPECTIVE"],
          ["HIDE_PLACE", "PLACE"],
          ["SUSPEND_USER", "USER"],
          ["ACCESS_CODE_CREATE", "ACCESS_CODE"],
          ["USER_LOOKUP", "USER"],
        ].map(([action, kind], i) => ({
          id: `au${i}`,
          actor: user(`u-adm${i}`),
          action: `AUDIT_ACTION_${action}`,
          targetKind: `AUDIT_TARGET_KIND_${kind}`,
          targetId: `0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a1${i}`,
          targetRef: "QWERTYUIOPASDFGHJKLZXCVBNM234567",
          targetLabel: kind === "PLACE" ? storeName : longTitle,
          reportId: i === 0 ? "r0" : "",
          reason: prose,
          at: ts,
        })),
      };
    case "ModerationService/LookupUser":
      return {
        user: {
          userId: "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10",
          username,
          emailHint: `c***@${longWord.toLowerCase()}.example.com`,
          emailVerified: true,
          role: "ROLE_USER",
          joinedAt: ts,
          suspendedAt: ts,
          lastSignInAt: ts,
          failedSignIns24h: 12345,
          stories: 23456,
          hiddenStories: 3456,
          openReportsAgainst: 4567,
          reportsAgainst: 5678,
          reportsFiled: 6789,
        },
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

const session = {
  accessToken: "tok",
  accessTokenExpiresAt: "2030-01-01T00:00:00Z",
  user: { userId: "u-me", username, role: "ROLE_ADMIN", emailVerified: true },
};

/** A 375-pixel page whose Connect calls are answered by reply(); signed out, Refresh is refused. */
async function phonePage(signedIn: boolean): Promise<Page> {
  const ctx = await browser.newContext({ viewport: PHONE });
  await ctx.route("**/api/perspectiv.v1.*/*", async (route) => {
    const procedure = new URL(route.request().url()).pathname.replace("/api/perspectiv.v1.", "");
    const sent = route.request().postData();
    const body =
      procedure === "AuthService/Refresh"
        ? signedIn
          ? session
          : null
        : reply(procedure, sent ? (JSON.parse(sent) as Record<string, unknown>) : {});
    await route.fulfill(
      body === null
        ? procedure === "AuthService/Refresh"
          ? { status: 401, contentType: "application/json", body: JSON.stringify({ code: "unauthenticated" }) }
          : { status: 404, contentType: "application/json", body: JSON.stringify({ code: "unimplemented" }) }
        : { status: 200, contentType: "application/json", body: JSON.stringify(body) },
    );
  });
  return ctx.newPage();
}

/**
 * The document's scroll width and the innermost elements that stick out past
 * the viewport: by their own box, or by content (an unbreakable word) that
 * spills out of a box that does not clip it.
 */
async function overflow(page: Page) {
  return page.evaluate(() => {
    const vw = window.innerWidth;
    const sticksOut = (el: Element) => {
      const r = el.getBoundingClientRect();
      if (r.right > vw + 1) return true;
      return getComputedStyle(el).overflowX === "visible" && r.left + el.scrollWidth > vw + 1;
    };
    const all = [...document.querySelectorAll("body *")].filter(sticksOut);
    const offenders = all
      .filter((el) => !all.some((other) => other !== el && el.contains(other)))
      .map((el) => `${el.tagName.toLowerCase()}.${String(el.getAttribute("class") ?? "").trim().replace(/\s+/g, ".")} "${(el.textContent ?? "").trim().slice(0, 40)}"`)
      .slice(0, 8);
    return { scrollWidth: document.documentElement.scrollWidth, offenders };
  });
}

type Case = {
  path: string;
  /** Signed in as an admin unless false. */
  signedIn?: boolean;
  /** A form field the page must have rendered before it is measured. */
  label?: string;
  /** Text the page must have rendered, proving the long stub content arrived. */
  text?: string;
};

const cases: Case[] = [
  // Writing.
  { path: "/compose?placeId=p-store", label: "Day of your visit" },
  { path: "/compose?perspectiveId=pub-1", label: "Part 1" },
  { path: "/compose", label: "Where were you?" },
  { path: "/claims", text: label },
  { path: "/admin/reports", text: storeName },
  // Reading.
  { path: "/", signedIn: false, text: city },
  { path: "/", text: `Welcome back, ${username}` },
  { path: "/places/p-store", text: longTitle },
  { path: "/best/arts-and-entertainment", signedIn: false, text: longTitle },
  { path: "/arcs/a-day", text: "23:00" },
  { path: "/perspectives/pub-1", label: "Heart, 9999" },
  { path: "/lores/lore-canon", text: label },
  // The lore as a book: title page, contents, a chapter's last part with
  // its "Next chapter" control, the last chapter's end, and the closing page.
  { path: "/lores/lore-canon/book", text: "Written by" },
  { path: "/lores/lore-canon/book?at=contents", text: "Chapter 6" },
  { path: "/lores/lore-canon/book?chapter=2&part=3", text: "Next chapter" },
  { path: "/lores/lore-canon/book?chapter=6&part=3", text: "The end" },
  { path: "/lores/lore-canon/book?at=end", text: "Read its thread" },
  { path: `/users/${username}`, text: longTitle },
  { path: "/terms", signedIn: false },
  { path: "/privacy", signedIn: false },
  { path: "/guidelines", signedIn: false },
  // Account.
  { path: "/login", signedIn: false, label: "Email" },
  { path: "/signup", signedIn: false, label: "Username" },
  { path: "/settings", text: "Mobile/15E148" },
  { path: "/saved", text: longTitle },
  { path: "/appearance", label: "Larger" },
  { path: "/users/other", text: "12345 followers" },
  { path: "/users/other?tab=posts", text: "Unnamed lore" },
  { path: "/inbox", text: longTitle },
  { path: "/admin/audit", text: "hid a place" },
  { path: `/admin/users?q=${username}`, text: "23456, 3456 hidden" },
];

describe("every route at 375 pixels (OT-037, AC-034)", () => {
  it.each(cases)(
    "$path (signed in: $signedIn) does not scroll sideways with long content",
    async ({ path, signedIn = true, label: field, text }) => {
      const page = await phonePage(signedIn);
      await page.goto(base + path);
      await page.locator("h1").first().waitFor();
      if (field) await page.getByLabel(field).first().waitFor();
      if (text) await page.getByText(text, { exact: false }).first().waitFor();
      await page.waitForLoadState("networkidle");
      const got = await overflow(page);
      expect(got.offenders, got.offenders.join("\n")).toEqual([]);
      expect(got.scrollWidth).toBe(PHONE.width);
      await page.context().close();
    },
    120_000,
  );
});

/**
 * Every control on the newest screens is a 44-pixel target (Folio,
 * Interaction), measured in the browser at 375 pixels with Larger story
 * text: the thread, Writers you follow, Saved, Appearance, a profile's
 * Follow, tabs and Thread posts, the best days at a kind of place, the
 * report queue with its disclosure open and the pages of a lore's book. A
 * link set inline in a sentence is exempt, as WCAG's target size is.
 */
describe("the newest screens' targets at 375 pixels", () => {
  const screens: (Case & { scope: string; open?: string })[] = [
    { path: "/lores/lore-canon", text: "Show 4 more replies", scope: "section:has(> h2:text-is('Thread'))" },
    { path: "/lores/lore-canon/book", text: "Written by", scope: "main" },
    { path: "/lores/lore-canon/book?at=contents", text: "Chapter 6", scope: "main" },
    { path: "/lores/lore-canon/book?chapter=2&part=3", text: "Next chapter", scope: "main" },
    { path: "/lores/lore-canon/book?at=end", text: "Read its thread", scope: "main" },
    { path: "/settings", text: "Mobile/15E148", scope: "section:has(> h2:text-is('Writers you follow'))" },
    { path: "/saved", text: longTitle, scope: "main" },
    { path: "/appearance", label: "Larger", scope: "main" },
    { path: "/users/other", text: "12345 followers", scope: "main" },
    { path: "/users/other?tab=posts", text: "Unnamed lore", scope: "main" },
    { path: "/best/arts-and-entertainment", signedIn: false, text: longTitle, scope: "main" },
    { path: "/best/bowling", signedIn: false, text: "Kind of place not found", scope: "main" },
    { path: "/admin/reports", text: storeName, scope: "main", open: "What it said when reported" },
  ];
  it.each(screens)(
    "$path: no target in $scope is under 44 pixels tall, and nothing scrolls sideways",
    async ({ path, signedIn = true, label: field, text, scope, open }) => {
      const page = await phonePage(signedIn);
      await page.addInitScript(() => localStorage.setItem("perspectiv.text", "larger"));
      await page.goto(base + path);
      await page.locator("h1").first().waitFor();
      if (field) await page.getByLabel(field).first().waitFor();
      if (text) await page.getByText(text, { exact: false }).first().waitFor();
      await page.waitForLoadState("networkidle");
      if (open) for (const summary of await page.getByText(open, { exact: true }).all()) await summary.click();
      const small = await page.locator(scope).first().evaluate((root) => {
        const targets = root.querySelectorAll<HTMLElement>("a[href], button, summary, [role='tab'], label:has(input)");
        return [...targets]
          .filter((el) => el.getClientRects().length > 0 && getComputedStyle(el).display !== "inline")
          .filter((el) => !(el.tagName === "BUTTON" && el.closest("label")))
          .map((el) => ({ el, h: el.getBoundingClientRect().height }))
          .filter(({ h }) => h < 43.5)
          .map(({ el, h }) => `${el.tagName.toLowerCase()} "${(el.getAttribute("aria-label") ?? el.textContent ?? "").trim().slice(0, 40)}" ${h.toFixed(1)}px`);
      });
      expect(small, small.join("\n")).toEqual([]);
      const got = await overflow(page);
      expect(got.offenders, got.offenders.join("\n")).toEqual([]);
      await page.context().close();
    },
    120_000,
  );
});
