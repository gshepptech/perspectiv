/**
 * A first-time visitor looks for a place from any page: the header's search
 * opens with "/", reads a name with the town it is in, in any of the forms
 * people type, and a result leads to the place's page.
 */
import { expect, test } from "@playwright/test";

import { reader } from "./support/ui.ts";

const STORE = "Harbor Light Laundromat";

test("the header search finds a place by name and town, from any page", async ({ browser }) => {
  const anon = await reader(browser);
  await anon.goto("/guidelines");
  const header = anon.getByRole("navigation", { name: "Primary" });
  await expect(header.getByRole("button", { name: "Search" })).toBeVisible();

  for (const query of ["laundromat pensacola", "Pensacola laundromat", "laundromat, Pensacola, FL"]) {
    await test.step(`"${query}"`, async () => {
      await anon.locator("body").press("/");
      const dialog = anon.getByRole("dialog", { name: "Search places" });
      const field = dialog.getByRole("searchbox", { name: "Search places" });
      await expect(field).toBeFocused();
      await field.fill(query);
      const result = dialog.getByRole("list", { name: "Matching places" }).getByRole("link", { name: new RegExp(STORE) });
      await expect(result).toContainText("in Pensacola, Florida");
      await anon.keyboard.press("Escape");
      await expect(dialog).toBeHidden();
    });
  }

  await test.step("a result opens the place page and closes the dialog", async () => {
    await header.getByRole("button", { name: "Search" }).click();
    const dialog = anon.getByRole("dialog", { name: "Search places" });
    await dialog.getByRole("searchbox", { name: "Search places" }).fill("laundromat pensacola");
    await dialog.getByRole("link", { name: new RegExp(STORE) }).click();
    await anon.waitForURL(/\/places\/[0-9a-f-]+$/);
    await expect(anon.getByRole("heading", { level: 1, name: STORE })).toBeVisible();
    await expect(dialog).toBeHidden();
  });
});
