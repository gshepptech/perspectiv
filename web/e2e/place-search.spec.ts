/**
 * A first-time visitor looks for a place from any page: the header's search
 * opens with "/", reads a name with the town it is in, in any of the forms
 * people type, and a result leads to the place's page, by click or by the
 * combobox's keys. A place it does not know is offered for adding with the
 * name and the town apart.
 */
import AxeBuilder from "@axe-core/playwright";
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
      const field = dialog.getByRole("combobox", { name: "Search places" });
      await expect(field).toBeFocused();
      await field.fill(query);
      const result = dialog.getByRole("listbox", { name: "Matching places" }).getByRole("option", { name: new RegExp(STORE) });
      await expect(result).toContainText("in Pensacola, Florida");
      await anon.keyboard.press("Escape");
      await expect(dialog).toBeHidden();
    });
  }

  await test.step("a result opens the place page and closes the dialog", async () => {
    await header.getByRole("button", { name: "Search" }).click();
    const dialog = anon.getByRole("dialog", { name: "Search places" });
    await dialog.getByRole("combobox", { name: "Search places" }).fill("laundromat pensacola");
    await dialog.getByRole("option", { name: new RegExp(STORE) }).click();
    await anon.waitForURL(/\/places\/[0-9a-f-]+$/);
    await expect(anon.getByRole("heading", { level: 1, name: STORE })).toBeVisible();
    await expect(dialog).toBeHidden();
  });
});

test("the front page's search moves with the arrow keys, opens with Enter and offers a missing place apart from its town", async ({
  browser,
}) => {
  const anon = await reader(browser);
  await anon.goto("/");
  const field = anon.getByRole("combobox", { name: "Search places" });
  const list = anon.getByRole("listbox", { name: "Matching places" });

  await test.step("↓ makes a result active while focus stays in the field; Escape closes the list, then clears", async () => {
    await field.fill("laundromat pensacola");
    const result = list.getByRole("option", { name: new RegExp(STORE) });
    await expect(result).toBeVisible();
    await expect(field).toHaveAttribute("aria-expanded", "true");
    await field.press("ArrowDown");
    await expect(result).toHaveAttribute("aria-selected", "true");
    await expect(field).toHaveAttribute("aria-activedescendant", (await result.getAttribute("id")) ?? "");
    await expect(field).toBeFocused();
    // The open combobox, a result active, passes axe's WCAG 2.1 A/AA rules.
    const scan = await new AxeBuilder({ page: anon }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
    expect(scan.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([]);
    await field.press("Escape");
    await expect(field).toHaveAttribute("aria-expanded", "false");
    await expect(list).toBeHidden();
    await expect(field).toHaveValue("laundromat pensacola");
    await field.press("Escape");
    await expect(field).toHaveValue("");
  });

  await test.step("Enter opens the first result", async () => {
    await field.fill("laundromat pensacola");
    await expect(list.getByRole("option", { name: new RegExp(STORE) })).toBeVisible();
    await field.press("Enter");
    await anon.waitForURL(/\/places\/[0-9a-f-]+$/);
    await expect(anon.getByRole("heading", { level: 1, name: STORE })).toBeVisible();
  });

  await test.step("a place it does not know is offered with its name and town apart", async () => {
    await anon.goto("/");
    await field.fill("joes crab shack pensacola");
    const add = anon.getByRole("link", { name: "Not here? Add “Joes Crab Shack” in Pensacola, Florida" });
    await expect(add).toBeVisible();
    await expect(add).toHaveAttribute("href", /^\/compose\?add=Joes\+Crab\+Shack&in=[0-9a-f-]+$/);
  });
});
