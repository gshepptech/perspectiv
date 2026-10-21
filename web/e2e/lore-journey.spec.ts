/**
 * NFR-004: the lore journey. Four strangers write about the same store on the
 * same day, link up by claiming each other's characters, become canon when
 * the rank job runs, and then vote one of themselves out of the lore.
 */
import { expect, test } from "@playwright/test";

import { loreAccounts } from "./support/accounts.ts";
import { canonWaitMs } from "./support/env.ts";
import { card, reader, reloadUntil, signedIn, visit, writeAt, writeInArc, type Writer } from "./support/ui.ts";

const STORE = "Lantern Street Books";

// Ana's characters; each of the other three recognises themselves in one.
const BARISTA = "the barista with the green scarf";
const READER = "the man reading a paperback by the window";
const UMBRELLA = "the woman shaking out a yellow umbrella";

/** Claims `label` in `author`'s perspective from the arc page. */
async function claim(w: Writer, arc: string, author: string, label: string) {
  // A fresh load: the arc page decides who may claim from who has written here.
  await visit(w.page, w.account, arc);
  // A story already in a contender lore is read, and claimed from, on its lore page.
  if ((await card(w.page, author).count()) === 0) {
    const contender = w.page.getByRole("region", { name: "Contenders" }).getByRole("article").filter({ hasText: author });
    await contender.getByRole("link", { name: /Read this lore/ }).click();
  }
  await card(w.page, author).getByRole("button", { name: "Claim a character" }).click();
  const dialog = w.page.getByRole("dialog", { name: "Claim a character" });
  await dialog.getByRole("listitem").filter({ hasText: label }).getByRole("button", { name: "That was me" }).click();
  await expect(dialog.getByRole("status")).toContainText("Claim sent.");
  await w.page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
}

/** Casts a remove vote on `target`'s perspective in the canon lore. */
async function voteToRemove(w: Writer, arc: string, target: string) {
  await visit(w.page, w.account, arc);
  await card(w.page.getByRole("region", { name: "Canon" }), target).getByRole("button", { name: "Vote to remove" }).click();
  const dialog = w.page.getByRole("dialog", { name: `Vote to remove ${target}'s perspective?` });
  await dialog.getByRole("button", { name: "Vote to remove" }).click();
  await expect(dialog).toBeHidden();
}

test("NFR-004 four accounts publish, claim, accept, reach canon within 60 seconds and vote one out", async ({ browser }) => {
  test.setTimeout(360_000);
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
      const unlinked = anon.getByRole("region", { name: "Not yet linked" });
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
  await anaCardForBen.getByRole("button", { name: "Upvote" }).click();
  await expect(anaCardForBen.getByRole("button", { name: "Remove upvote" })).toBeVisible();
  await expect(anaCardForBen).toContainText("1 upvote");

  // Ana accepts all three claims from her claims inbox.
  await ana.page.getByRole("link", { name: "Claims", exact: true }).click();
  const incoming = ana.page.getByRole("list", { name: "Claims on your characters" });
  for (const [who, label] of [
    [benAcct, BARISTA],
    [calAcct, READER],
    [deeAcct, UMBRELLA],
  ] as const) {
    const item = incoming.getByRole("listitem").filter({ hasText: `${who.username} says they are` });
    await expect(item).toContainText(label);
    await item.getByRole("button", { name: "Accept" }).click();
    await expect(item.getByText("Accepted")).toBeVisible();
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
  await expect(canon).toContainText("4 members");
  expect(Date.now() - acceptedAt).toBeLessThan(60_000);
  await expect(canon).toContainText("1 upvote");
  for (const u of [anaAcct, benAcct, calAcct, deeAcct]) {
    await expect(card(canon, u.username)).toContainText("In lore");
  }

  // Dee cannot vote on her own perspective; the other members can.
  await visit(dee.page, deeAcct, arc);
  await expect(card(dee.page, deeAcct.username)).toBeVisible();
  await expect(card(dee.page, deeAcct.username).getByRole("button", { name: "Vote to remove" })).toHaveCount(0);

  // Two of the other three members are a majority: Dee is voted out.
  await voteToRemove(ana, arc, deeAcct.username);
  const deeForAna = card(ana.page.getByRole("region", { name: "Canon" }), deeAcct.username);
  await expect(deeForAna.getByRole("status")).toHaveText("1 of 3 members have voted to remove it.");
  await expect(deeForAna).toContainText("In lore");
  await voteToRemove(ben, arc, deeAcct.username);
  // The vote that makes the majority removes her at once; the arc page then
  // lists her perspective under Tales rather than in the canon lore.
  await expect(card(ben.page.getByRole("region", { name: "Tales" }), deeAcct.username)).toContainText("Removed from lore");
  await expect(card(ben.page.getByRole("region", { name: "Canon" }), deeAcct.username)).toHaveCount(0);

  // The lore is down to 3 members, below the canon minimum, so the next pass
  // clears canon; Dee's perspective stays readable as a removed tale.
  await reloadUntil(
    anon,
    arc,
    async () => {
      await expect(canon).toContainText("No canon yet.", { timeout: 2_000 });
      await expect(anon.getByRole("region", { name: "Contenders" })).toContainText("3 members", { timeout: 2_000 });
    },
    canonWaitMs(),
  );
  const tales = anon.getByRole("region", { name: "Tales" });
  await expect(card(tales, deeAcct.username)).toContainText("Removed from lore");
  // The demoted lore is a contender: its people are listed on its card.
  const contenders = anon.getByRole("region", { name: "Contenders" });
  for (const u of [anaAcct, benAcct, calAcct]) {
    await expect(contenders.getByRole("link", { name: u.username })).toBeVisible();
  }
});
