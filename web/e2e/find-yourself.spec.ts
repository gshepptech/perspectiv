/**
 * Finding yourself in someone else's story without knowing where to look:
 * a reader searches a place's unclaimed descriptions, and a writer who was
 * there claims straight from the day's "Is this you?" list.
 */
import { expect, test } from "@playwright/test";

import { findAccounts } from "./support/accounts.ts";
import { reader, signedIn, visit, writeAt, writeInArc } from "./support/ui.ts";

const STORE = "Gulf Pier Bait and Tackle";
const HAT = "the man in the orange fishing hat";

test("a stranger is found by search, claimed from Is this you?, and leaves both lists", async ({ browser }) => {
  test.setTimeout(180_000);
  const { finn: finnAcct, gil: gilAcct } = findAccounts();
  const finn = await signedIn(browser, finnAcct);
  const gil = await signedIn(browser, gilAcct);
  const anon = await reader(browser);

  const { arc } = await writeAt(finn, STORE, {
    title: "Mullet run at dawn",
    genre: "Slice of life",
    parts: ["The pier was empty except for one man who caught nothing and laughed about it."],
    characters: [HAT],
  });

  // Anyone can search the place for how strangers were described.
  await anon.goto(arc);
  await anon.getByRole("navigation", { name: "Place" }).getByRole("link", { name: STORE }).click();
  const find = anon.getByRole("region", { name: "Were you in someone's story?" });
  await find.getByRole("searchbox", { name: "Search descriptions" }).fill("Orange hat");
  await expect(find.getByRole("status")).toContainText("Nobody unclaimed matches");
  await find.getByRole("searchbox", { name: "Search descriptions" }).fill("orange fishing");
  await expect(find.getByRole("list", { name: "Matching descriptions" })).toContainText(`“${HAT}”`);

  // Gil was there, writes his side, and finds himself in the day's list.
  await writeInArc(gil, arc, { genre: "Comedy", parts: ["I caught nothing. I had a great time."] });
  await visit(gil.page, gilAcct, arc);
  const isThisYou = gil.page.getByRole("region", { name: "Is this you?" });
  await isThisYou.getByRole("link", { name: `That was me: ${HAT}` }).click();
  const dialog = gil.page.getByRole("dialog", { name: "Claim a character" });
  await dialog.getByRole("listitem").filter({ hasText: HAT }).getByRole("button", { name: "That was me" }).click();
  await expect(dialog.getByRole("status")).toContainText("Claim sent.");
  await gil.page.keyboard.press("Escape");

  // A claimed description is nobody's to find any more.
  await visit(gil.page, gilAcct, arc);
  await expect(gil.page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(gil.page.getByRole("region", { name: "Is this you?" })).toHaveCount(0);
  await anon.reload();
  await find.getByRole("searchbox", { name: "Search descriptions" }).fill("orange fishing");
  await expect(find.getByRole("status")).toContainText("Nobody unclaimed matches");
});
