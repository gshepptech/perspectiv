/**
 * What a writer new to a day meets: a draft that is picked up again rather
 * than started twice (on load, and again when the day is changed to one it
 * was started for) and is named by its words, a notification that says
 * who might be in a new story, and a request that quotes the claimant's own
 * story so it can be judged on the Requests page. Finn and Gil are the
 * find-yourself spec's accounts, signed in with their passwords in sessions
 * of their own (see a11yAccounts for why), at a store no other spec writes at.
 */
import { expect, test, type Page } from "@playwright/test";

import { findAccounts } from "./support/accounts.ts";
import { waitForEmail } from "./support/mailpit.ts";
import { signInWithPassword, visit, writeInArc } from "./support/ui.ts";

const STORE = "Muscogee Wharf Oyster Bar";
const TITLE = "The night the oyster boats came in late";
const OPENING = "The oyster boats came in after midnight and the whole bar went out onto the wharf to watch.";
const CAST = ["the woman with the foam parrot", "the clerk", "the twins", "the drummer"];
const EARLIER = "The fog sat on the wharf all morning and the shuckers worked by lamplight at noon.";

/** The day n days back, as the composer's date field takes it. */
function daysAgo(n: number): string {
  return new Date(Date.now() - n * 24 * 3600_000).toISOString().slice(0, 10);
}

/** Opens the composer from Write in the header, at STORE for today. */
async function composeAtStore(page: Page) {
  await page.getByRole("link", { name: "Write", exact: true }).click();
  await page.waitForURL(/\/compose$/);
  await page.getByRole("searchbox", { name: "Where were you?" }).fill(STORE);
  await page.getByRole("list", { name: "Matching places" }).getByRole("button", { name: new RegExp(STORE) }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Write a story" })).toBeVisible();
}

test("a draft is resumed, a new story names who might be in it, and a request quotes the claimant", async ({ browser }) => {
  test.setTimeout(180_000);
  const { finn: finnAcct, gil: gilAcct } = findAccounts();
  const gil = await signInWithPassword(browser, gilAcct);

  await test.step("a draft for a past day reopens when the writer comes back through Write, with no second draft", async () => {
    const past = daysAgo(10);
    await composeAtStore(gil.page);
    await gil.page.getByLabel("Day of your visit").fill(past);
    await gil.page.getByLabel("Part 1", { exact: true }).fill(EARLIER);
    await expect(gil.page.getByRole("status", { name: "Save status" })).toHaveText("Draft saved");

    // Leave, then find the draft on the front page with a way back in.
    await visit(gil.page, gilAcct, "/");
    const yours = gil.page.getByRole("region", { name: "Your stories" });
    await expect(yours.getByRole("listitem").filter({ hasText: "Draft ·" })).toHaveCount(1);
    await expect(yours.getByRole("link", { name: /^Continue The fog sat on the wharf/ })).toBeVisible();

    // Come back the way the walkthrough did: Write, the place, then the day.
    // The composer opens on today, so the draft is found only once the day is set.
    await composeAtStore(gil.page);
    await expect(gil.page.getByLabel("Part 1", { exact: true })).toHaveValue("");
    await gil.page.getByLabel("Day of your visit").fill(past);
    await expect(gil.page.getByLabel("Part 1", { exact: true })).toHaveValue(EARLIER);
    await expect(gil.page.getByText("You started a draft for this day, so it is open again.")).toBeVisible();
    await expect(gil.page).toHaveURL(/perspectiveId=/);

    // No empty draft was made beside it.
    await visit(gil.page, gilAcct, "/");
    await expect(yours.getByRole("listitem").filter({ hasText: "Draft ·" })).toHaveCount(1);
    await expect(yours).not.toContainText(/Untitled/);
    await yours.getByRole("link", { name: /^Continue The fog sat on the wharf/ }).click();
    await expect(gil.page.getByLabel("Part 1", { exact: true })).toHaveValue(EARLIER);
  });

  await test.step("a draft is named by its first words and picked up again", async () => {
    await composeAtStore(gil.page);
    await gil.page.getByLabel("Part 1", { exact: true }).fill(OPENING);
    await expect(gil.page.getByRole("status", { name: "Save status" })).toHaveText("Draft saved");

    await visit(gil.page, gilAcct, "/");
    const yours = gil.page.getByRole("region", { name: "Your stories" });
    const row = yours.getByRole("link", { name: /^The oyster boats came in after…/ });
    await expect(row).toContainText("Draft");
    await expect(yours).not.toContainText(/Untitled/);

    // Coming back to the same place and day opens the same draft.
    await composeAtStore(gil.page);
    await expect(gil.page.getByLabel("Part 1", { exact: true })).toHaveValue(OPENING);
    await expect(gil.page.getByText("You started a draft for this day, so it is open again.")).toBeVisible();
    await expect(gil.page).toHaveURL(/perspectiveId=/);
  });

  const { gilStory, arc } = await test.step("the draft is published, starting the day", async () => {
    await gil.page.getByLabel("Title of the day").fill(TITLE);
    await gil.page.getByLabel("Genre").click();
    await gil.page.getByRole("option", { name: "Slice of life", exact: true }).click();
    await gil.page.getByRole("button", { name: "Publish", exact: true }).click();
    await gil.page.waitForURL(/\/perspectives\/[^/?]+/);
    const story = new URL(gil.page.url()).pathname;
    await gil.page.getByRole("link", { name: "Back to the day", exact: true }).click();
    await gil.page.waitForURL(/\/arcs\/[^/?]+/);
    return { gilStory: story, arc: new URL(gil.page.url()).pathname };
  });

  const finn = await signInWithPassword(browser, finnAcct);
  await writeInArc(finn, arc, {
    genre: "Comedy",
    parts: ["Somebody brought a foam parrot to the wharf and it outlasted the boats."],
    characters: CAST,
  });

  await test.step("the notification and its email say who might be in the new story", async () => {
    const who = "Who might be in it: “the woman with the foam parrot”, “the clerk”, “the twins” and 1 more.";
    const mail = await waitForEmail(gilAcct.email, `A new story in ${TITLE}`);
    expect(mail.Text.replace(/\s+/g, " ")).toContain(`${who} If one of them was you, open the story and claim them.`);
    await visit(gil.page, gilAcct, "/inbox");
    const row = gil.page.getByRole("list", { name: "Notifications" }).getByRole("listitem").filter({ hasText: `published a new story in ${TITLE}` });
    await expect(row.getByRole("link")).toContainText(who);
  });

  await test.step("a request quotes the claimant's story where it is judged", async () => {
    await visit(gil.page, gilAcct, arc);
    await gil.page.getByRole("region", { name: "Is this you?" }).getByRole("link", { name: "That was me: the clerk" }).click();
    const dialog = gil.page.getByRole("dialog", { name: "Claim a character" });
    await dialog.getByRole("listitem").filter({ hasText: "the clerk" }).getByRole("button", { name: "That was me" }).click();
    await expect(dialog.getByRole("status")).toContainText("Claim sent.");
    await gil.page.keyboard.press("Escape");

    await visit(finn.page, finnAcct, "/claims");
    // Gil claimed a character of Finn's in the find-yourself spec too.
    const request = finn.page.getByRole("listitem", { name: `${gilAcct.username} says they are “the clerk”` });
    await expect(request.locator("blockquote")).toHaveText(OPENING);
    await expect(request.getByRole("link", { name: "Read their story" })).toHaveAttribute("href", gilStory);
  });
});
