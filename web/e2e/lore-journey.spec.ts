/**
 * The lore journey. Four strangers write about the same store on the
 * same day, link up by claiming each other's characters, become canon when
 * the rank job runs, and then vote one of themselves out of the lore. The
 * three left then talk it over in the lore's thread: a post, a reply that
 * reaches its author's inbox and opens on the reply, an upvote and both sort
 * orders. Last, a signed-out reader opens the lore as a book and turns into
 * chapter 2.
 */
import { expect, test, type Page } from "@playwright/test";

import { loreAccounts } from "./support/accounts.ts";
import { canonWaitMs } from "./support/env.ts";
import { card, reader, reloadUntil, signedIn, visit, writeAt, writeInArc, type Writer } from "./support/ui.ts";

const STORE = "Lantern Street Books";

// Ana's characters; each of the other three recognises themselves in one.
const BARISTA = "the barista with the green scarf";
const READER = "the man reading a paperback by the window";
const UMBRELLA = "the woman shaking out a yellow umbrella";

/**
 * Opens `author`'s story from the arc page and returns the actions under it,
 * where a reader claims, votes it out or reports it.
 */
async function openStory(w: Writer, arc: string, author: string) {
  // A fresh load: the arc decides who may act from who has written here.
  await visit(w.page, w.account, arc);
  await w.page.getByRole("link", { name: `Read ${author}'s story` }).click();
  await expect(w.page.getByRole("heading", { level: 1 })).toBeVisible();
  return w.page.getByRole("region", { name: "Respond to this story" });
}

/** Claims `label` in `author`'s story from the story's own page. */
async function claim(w: Writer, arc: string, author: string, label: string) {
  const actions = await openStory(w, arc, author);
  await actions.getByRole("button", { name: "Claim a character" }).click();
  const dialog = w.page.getByRole("dialog", { name: "Claim a character" });
  await dialog.getByRole("listitem").filter({ hasText: label }).getByRole("button", { name: "That was me" }).click();
  // A claim that fails shows an alert in the dialog; name it in the failure
  // rather than only timing out on the missing confirmation.
  const sent = dialog.getByRole("status").filter({ hasText: "Claim sent." });
  const refused = dialog.getByRole("alert");
  await expect(sent.or(refused)).toBeVisible();
  if (await refused.isVisible()) {
    throw new Error(`claim of "${label}" by ${w.account.username} was refused: ${(await refused.innerText()).trim()}`);
  }
  await w.page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
}

/** Casts a remove vote on `target`'s story from its own page; returns the actions under it. */
async function voteToRemove(w: Writer, arc: string, target: string) {
  const actions = await openStory(w, arc, target);
  await actions.getByRole("button", { name: "Vote to remove" }).click();
  const dialog = w.page.getByRole("dialog", { name: `Vote to remove ${target}'s story?` });
  await dialog.getByRole("button", { name: "Vote to remove" }).click();
  await expect(dialog).toBeHidden();
  return actions;
}

test("four accounts publish, claim, accept, reach canon within 60 seconds and vote one out", async ({ browser }) => {
  test.setTimeout(480_000);
  const { ana: anaAcct, ben: benAcct, cal: calAcct, dee: deeAcct } = loreAccounts();
  const ana = await signedIn(browser, anaAcct);
  const ben = await signedIn(browser, benAcct);
  const cal = await signedIn(browser, calAcct);
  const dee = await signedIn(browser, deeAcct);
  const anon = await reader(browser);

  // Ana is first at the store today, so she names the arc.
  const { arc } = await writeAt(ana, STORE, {
    title: "Rain on the bookshop windows",
    genre: "Slice of life",
    parts: [
      "The rain came in sideways and the bookshop filled up with strangers.",
      "By closing time we had all read the same page of the same paperback.",
    ],
    characters: [BARISTA, READER, UMBRELLA],
  });
  await expect(ana.page.getByRole("heading", { level: 1, name: "Rain on the bookshop windows" })).toBeVisible();
  await expect(card(ana.page, anaAcct.username)).toContainText("Waiting for a link");

  // The other three find the arc and write their own side of the afternoon.
  await writeInArc(ben, arc, { genre: "Comedy", parts: ["I wore the green scarf. I also spilled the oat milk."] });
  await writeInArc(cal, arc, { genre: "Drama", parts: ["I read by the window and pretended not to watch the door."] });
  await writeInArc(dee, arc, { genre: "Mystery", parts: ["My umbrella was yellow and it was not mine."] });

  // Everyone is in the same arc, waiting for a link.
  await reloadUntil(
    anon,
    arc,
    async () => {
      const unlinked = anon.getByRole("region", { name: "Standing alone" });
      for (const u of [anaAcct, benAcct, calAcct, deeAcct]) await expect(card(unlinked, u.username)).toBeVisible({ timeout: 2_000 });
    },
    15_000,
  );

  // Each of them claims one of Ana's characters.
  await claim(ben, arc, anaAcct.username, BARISTA);
  await claim(cal, arc, anaAcct.username, READER);
  await claim(dee, arc, anaAcct.username, UMBRELLA);

  // Ben also upvotes Ana: participants vote, and votes feed the lore's score.
  await visit(ben.page, benAcct, arc);
  const anaCardForBen = card(ben.page, anaAcct.username);
  await anaCardForBen.getByRole("button", { name: /^Upvote/, pressed: false }).click();
  await expect(anaCardForBen.getByRole("button", { name: /^Upvote/, pressed: true })).toBeVisible();
  await expect(anaCardForBen).toContainText("1 upvote");

  // Ana accepts all three claims from her claims inbox.
  await ana.page.getByRole("link", { name: "Requests", exact: true }).click();
  const incoming = ana.page.getByRole("list", { name: "Claims on your characters" });
  for (const [who, label] of [
    [benAcct, BARISTA],
    [calAcct, READER],
    [deeAcct, UMBRELLA],
  ] as const) {
    const item = incoming.getByRole("listitem").filter({ hasText: `${who.username} says they are` });
    await expect(item).toContainText(label);
    await item.getByRole("button", { name: "Accept" }).click();
    await expect(item.getByText(/^Linked\./)).toBeVisible();
  }
  const acceptedAt = Date.now();

  // The rank job promotes the 4-member lore to canon on its next pass.
  const canon = anon.getByRole("region", { name: "Canon" });
  // Before promotion the canon region already says "4 of 4 members"; the
  // link to the canon's lore page appears only once there is a canon.
  await reloadUntil(
    anon,
    arc,
    async () => expect(canon.getByRole("link", { name: /Open the canon/ })).toBeVisible({ timeout: 2_000 }),
    canonWaitMs(),
  );
  await expect(canon).toContainText("4 stories");
  expect(Date.now() - acceptedAt).toBeLessThan(60_000);
  await expect(canon).toContainText("1 upvote");
  for (const u of [anaAcct, benAcct, calAcct, deeAcct]) {
    await expect(card(canon, u.username)).toBeVisible();
  }

  // Dee cannot vote on her own perspective; its page offers its author no
  // reader actions at all. The other members can.
  await expect(await openStory(dee, arc, deeAcct.username)).toHaveCount(0);

  // Two of the other three members are a majority: Dee is voted out.
  const anaVote = await voteToRemove(ana, arc, deeAcct.username);
  await expect(anaVote.getByRole("status")).toHaveText("1 of 3 members have voted to remove it.");
  await visit(ana.page, anaAcct, arc);
  await expect(card(ana.page.getByRole("region", { name: "Canon" }), deeAcct.username)).toBeVisible();
  await voteToRemove(ben, arc, deeAcct.username);
  // The vote that makes the majority removes her at once; the arc page then
  // lists her perspective as standing alone rather than in the canon lore.
  await visit(ben.page, benAcct, arc);
  await expect(card(ben.page.getByRole("region", { name: "Standing alone" }), deeAcct.username)).toContainText("Removed from lore");
  await expect(card(ben.page.getByRole("region", { name: "Canon" }), deeAcct.username)).toHaveCount(0);

  // The lore is down to 3 members, below the canon minimum, so the next pass
  // clears canon; Dee's story stays readable as a removed tale.
  await reloadUntil(
    anon,
    arc,
    async () => {
      await expect(canon).toContainText("No canon yet.", { timeout: 2_000 });
      await expect(anon.getByRole("region", { name: "Contenders" })).toContainText("3 stories", { timeout: 2_000 });
    },
    canonWaitMs(),
  );
  const tales = anon.getByRole("region", { name: "Standing alone" });
  await expect(card(tales, deeAcct.username)).toContainText("Removed from lore");
  // The demoted lore is a contender: its people's stories are listed in its block.
  const contenders = anon.getByRole("region", { name: "Contenders" });
  for (const u of [anaAcct, benAcct, calAcct]) {
    await expect(contenders.getByRole("link", { name: u.username, exact: true })).toBeVisible();
  }

  const lorePath = await talkInTheThread(arc, ana, ben, cal);
  await readAsABook(anon, lorePath, anaAcct.username, benAcct.username);
});

/**
 * A signed-out reader opens the lore as a book from its page, turns from the
 * title page to the contents and into chapter 1, pages through chapter 1's
 * parts with the arrow key, and turns into chapter 2 with its Next button,
 * back with Previous and on again with the arrow key. Each chapter is titled
 * by who its writer was: Ana, whom no one placed, by her opening words; Ben
 * by the character he claimed in Ana's story.
 */
async function readAsABook(page: Page, lorePath: string, ana: string, ben: string) {
  const first = "The rain came in sideways and the…";
  const second = "The barista with the green scarf";
  await page.goto(lorePath);
  await page.getByRole("link", { name: "Read as a book" }).click();
  await expect(page.getByRole("list", { name: "Writers" })).toBeVisible();
  await page.getByRole("link", { name: "Next page" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Contents" })).toBeFocused();
  const rows = page.getByRole("list", { name: "Chapters" }).getByRole("link");
  await expect(rows.nth(0)).toContainText(`Chapter 1${first}by ${ana} · 2 parts`);
  await expect(rows.nth(1)).toContainText(`Chapter 2${second}by ${ben} · 1 part`);
  await rows.first().click();
  const chapter1 = page.getByRole("heading", { level: 1, name: `Chapter 1: ${first}` });
  await expect(chapter1).toBeFocused();
  // The writer is the byline, never the title.
  await expect(page.locator("article header").getByRole("link", { name: ana, exact: true })).toBeVisible();
  const pages = page.getByRole("navigation", { name: "Pages" });
  const label = await pages.getByText(/^Chapter 1, part 1 of \d+$/).textContent();
  const parts = Number(/of (\d+)$/.exec(label ?? "")?.[1] ?? 0);
  expect(parts).toBeGreaterThan(0);
  for (let part = 1; part < parts; part++) {
    await page.keyboard.press("ArrowRight");
    await expect(page).toHaveURL(new RegExp(`[?]chapter=1&part=${part + 1}$`));
  }
  // On the last part Next is "Next chapter", with no second row of chapter links.
  await expect(page.getByRole("navigation", { name: "Chapters" })).toHaveCount(0);
  await pages.getByRole("button", { name: "Next chapter" }).click();
  await expect(page).toHaveURL(/[?]chapter=2&part=1$/);
  await expect(page.getByRole("heading", { level: 1, name: `Chapter 2: ${second}` })).toBeFocused();
  await expect(page.getByRole("status").filter({ hasText: /^Chapter 2, part 1 of \d+$/ })).toBeAttached();
  // Previous on a first part goes back to the previous chapter's last part, as ArrowLeft does.
  await pages.getByRole("button", { name: "Previous chapter" }).click();
  await expect(page).toHaveURL(new RegExp(`[?]chapter=1&part=${parts}$`));
  await expect(chapter1).toBeFocused();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("heading", { level: 1, name: `Chapter 2: ${second}` })).toBeFocused();
}

/**
 * Ben posts in the lore's thread, Cal replies, Ana upvotes, and the thread is
 * read Top and New. Returns the lore page's path.
 */
async function talkInTheThread(arc: string, ana: Writer, ben: Writer, cal: Writer): Promise<string> {
  await visit(ben.page, ben.account, arc);
  await ben.page
    .getByRole("region", { name: "Contenders" })
    .getByRole("link", { name: /^Open this lore/ })
    .click();
  const thread = ben.page.getByRole("region", { name: "Thread" });
  await thread.getByRole("textbox", { name: "Add to the thread" }).fill("Whose yellow umbrella was it in the end?");
  await thread.getByRole("button", { name: "Post", exact: true }).click();
  const bens = (w: Writer) => w.page.getByRole("article", { name: `${ben.account.username}'s post` });
  await expect(bens(ben)).toContainText("Whose yellow umbrella was it in the end?");
  const lorePath = new URL(ben.page.url()).pathname;

  // Cal replies under Ben's post.
  await visit(cal.page, cal.account, lorePath);
  await bens(cal).getByRole("button", { name: "Reply" }).click();
  await cal.page.getByRole("textbox", { name: `Reply to ${ben.account.username}` }).fill("Not mine. I was by the window.");
  await cal.page.getByRole("button", { name: "Post reply" }).click();
  const replies = cal.page.getByRole("list", { name: `Replies to ${ben.account.username}` });
  await expect(replies.getByRole("article", { name: `${cal.account.username}'s post` })).toContainText("Not mine. I was by the window.");

  // Ana upvotes Cal's reply and Ben's post, then posts something newer.
  await visit(ana.page, ana.account, lorePath);
  for (const who of [cal.account.username, ben.account.username]) {
    const post = ana.page.getByRole("article", { name: `${who}'s post` });
    await post.getByRole("button", { name: `Upvote ${who}'s post`, pressed: false }).click();
    await expect(post.getByRole("button", { name: `Upvote ${who}'s post`, pressed: true })).toBeVisible();
    await expect(post).toContainText("1 upvote");
  }
  const anaThread = ana.page.getByRole("region", { name: "Thread" });
  await anaThread.getByRole("textbox", { name: "Add to the thread" }).fill("The rain stopped at six.");
  await anaThread.getByRole("button", { name: "Post", exact: true }).click();

  // Top puts Ben's upvoted post first; New puts Ana's newer one first.
  const firstPost = () => anaThread.getByRole("tabpanel").getByRole("list", { name: "Posts" }).locator(":scope > li").first();
  await expect(anaThread.getByRole("tab", { name: "Top", selected: true })).toBeVisible();
  await expect(firstPost()).toContainText("Whose yellow umbrella was it in the end?");
  await anaThread.getByRole("tab", { name: "New" }).click();
  await expect(firstPost()).toContainText("The rain stopped at six.");

  // Ben hears of Cal's reply in his inbox once the notify job has run. A
  // signed-in page reloads through visit, which waits for each refresh.
  await expect(async () => {
    await visit(ben.page, ben.account, "/inbox");
    await expect(ben.page.getByText(`${cal.account.username} replied to your post`)).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 30_000, intervals: [1_000] });

  // Opening that notification lands on Cal's reply itself, focused.
  await ben.page.getByRole("link", { name: new RegExp(`${cal.account.username} replied to your post`) }).click();
  const calsReply = ben.page.getByRole("article", { name: `${cal.account.username}'s post` });
  await expect(calsReply).toBeFocused();
  await expect(calsReply).toContainText("Not mine. I was by the window.");
  expect(new URL(ben.page.url()).hash).toBe(`#${await calsReply.getAttribute("id")}`);
  return lorePath;
}
