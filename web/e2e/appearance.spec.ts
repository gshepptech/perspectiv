/**
 * The Appearance page in a real browser: a forced theme survives a reload
 * and is on the page before the app starts (public/appearance.js), so a
 * reader on a light device who chose Lamplight never sees Paper first.
 */
import { expect, test } from "@playwright/test";

test.use({ colorScheme: "light" });

test("a reader picks Lamplight and larger story text, and both hold from the first paint", async ({ page }) => {
  await page.goto("/appearance");
  await expect(page.getByRole("heading", { level: 1, name: "Appearance" })).toBeVisible();
  const background = () => page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  expect(await background()).toBe("rgb(247, 245, 240)");

  await page.getByRole("radio", { name: "Lamplight (dark)" }).check();
  await page.getByRole("radio", { name: "Larger" }).check();
  await expect(page.getByRole("status")).toHaveText("Saved on this device.");
  expect(await background()).toBe("rgb(22, 21, 20)");

  // Before any module script runs, the root already carries the choice.
  await page.addInitScript(() => {
    document.addEventListener("readystatechange", () => {
      if (document.readyState === "interactive") {
        (window as unknown as { early: string | undefined }).early = document.documentElement.dataset.theme;
      }
    });
  });
  await page.goto("/");
  expect(await page.evaluate(() => (window as unknown as { early: string | undefined }).early)).toBe("dark");
  expect(await background()).toBe("rgb(22, 21, 20)");
  await expect(page.locator('meta[name="theme-color"]').first()).toHaveAttribute("content", "#161514");

  await page.goto("/appearance");
  await page.getByRole("radio", { name: /Match this device/ }).check();
  expect(await background()).toBe("rgb(247, 245, 240)");
});
