/**
 * Two-factor sign-in, end to end through the production server: an admin
 * without it sees the notice, turns it on in Settings with a code computed
 * here from the key the page shows (as an authenticator app would), keeps
 * the recovery codes, and the notice goes. Signing in then asks for a code
 * after the password, and a recovery code works in place of the app once.
 * Last, turning it off returns sign-in to the password alone, so the
 * account ends as it began.
 */
import { expect, test, type Browser, type Page } from "@playwright/test";

import { type Account, twoFactorAccounts } from "./support/accounts.ts";
import { totp } from "./support/totp.ts";
import { signedIn } from "./support/ui.ts";

/** Signs in with the password on a fresh browser and stops at the second step. */
async function passwordStep(browser: Browser, account: Account): Promise<Page> {
  const page = await (await browser.newContext()).newPage();
  await page.goto("/login");
  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(account.password);
  await page.getByRole("main").getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Enter your code" })).toBeFocused();
  return page;
}

test("an admin turns on two-factor sign-in, signs in with a code and with a recovery code, and turns it off", async ({ browser }) => {
  test.setTimeout(180_000);
  const { tia } = twoFactorAccounts();
  const notice = (page: Page) => page.getByRole("region", { name: "Account security notice" });
  let secret = "";
  let codes: string[] = [];

  await test.step("the admin is asked to turn it on, and does", async () => {
    const { page } = await signedIn(browser, tia);
    await expect(notice(page)).toContainText("Your admin account has no two-factor sign-in.");
    await notice(page).getByRole("link", { name: "Turn it on in Settings" }).click();
    await page.waitForURL(/\/settings#two-factor$/);

    const block = page.getByRole("group", { name: "Two-factor sign-in" });
    await block.getByRole("button", { name: "Turn on two-factor sign-in…" }).click();
    await block.getByLabel("Current password").fill(tia.password);
    await block.getByRole("button", { name: "Continue" }).click();

    // The QR code is drawn in the page; the key under it is what an app reads.
    await expect(block.getByRole("img", { name: "QR code for adding Perspectiv to your authenticator app" })).toBeVisible();
    const shown = block.getByText(/^[A-Z2-7]{4}( [A-Z2-7]{1,4})+$/);
    secret = (await shown.textContent())!.replace(/\s/g, "");
    expect(secret).toHaveLength(32);

    await block.getByLabel("Code from your app").fill(totp(secret));
    await block.getByRole("button", { name: "Turn on" }).click();
    await expect(block.getByRole("heading", { name: "Save your recovery codes" })).toBeFocused();
    codes = await block.getByRole("list", { name: "Recovery codes" }).getByRole("listitem").allTextContents();
    expect(codes).toHaveLength(10);

    const download = page.waitForEvent("download");
    await block.getByRole("button", { name: "Download as a text file" }).click();
    expect((await download).suggestedFilename()).toBe("perspectiv-recovery-codes.txt");

    await block.getByRole("button", { name: "I've saved my codes" }).click();
    await expect(block.getByText(/^On since /)).toBeVisible();
    await expect(block.getByText("10 recovery codes left.")).toBeVisible();
    await expect(notice(page)).toHaveCount(0);
  });

  await test.step("the password alone no longer signs in; the app's code does", async () => {
    const page = await passwordStep(browser, tia);
    await page.getByLabel("Code from your app").fill("000000");
    await page.getByRole("main").getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByText("That code is not right.")).toBeVisible();
    // The step that turned it on is spent, so the app's next code is the one
    // that signs in; a phone a step ahead is accepted.
    await page.getByLabel("Code from your app").fill(totp(secret, Date.now() + 30_000));
    await page.getByRole("main").getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByRole("button", { name: `Account: ${tia.username}` })).toBeVisible();
    await expect(notice(page)).toHaveCount(0);
  });

  await test.step("a recovery code signs in once", async () => {
    const page = await passwordStep(browser, tia);
    await page.getByRole("button", { name: "Use a recovery code" }).click();
    await page.getByLabel("Recovery code").fill(codes[0]!.toUpperCase());
    await page.getByRole("main").getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByRole("button", { name: `Account: ${tia.username}` })).toBeVisible();
    await page.goto("/settings#two-factor");
    await expect(page.getByRole("group", { name: "Two-factor sign-in" }).getByText("9 recovery codes left.")).toBeVisible();

    const again = await passwordStep(browser, tia);
    await again.getByRole("button", { name: "Use a recovery code" }).click();
    await again.getByLabel("Recovery code").fill(codes[0]!);
    await again.getByRole("main").getByRole("button", { name: "Sign in" }).click();
    await expect(again.getByText("That code is not right.")).toBeVisible();
  });

  await test.step("turning it off needs the password and a code, and brings the notice back", async () => {
    const page = await passwordStep(browser, tia);
    await page.getByRole("button", { name: "Use a recovery code" }).click();
    await page.getByLabel("Recovery code").fill(codes[1]!);
    await page.getByRole("main").getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByRole("button", { name: `Account: ${tia.username}` })).toBeVisible();
    await page.goto("/settings#two-factor");
    const block = page.getByRole("group", { name: "Two-factor sign-in" });
    await block.getByRole("button", { name: "Turn off…" }).click();
    const dialog = page.getByRole("dialog", { name: "Turn off two-factor sign-in?" });
    await dialog.getByLabel("Current password").fill(tia.password);
    await dialog.getByLabel("Code from your app or a recovery code").fill(codes[2]!);
    await dialog.getByRole("button", { name: "Turn off" }).click();
    await expect(block.getByRole("status")).toHaveText("Two-factor sign-in is off.");
    await expect(notice(page)).toBeVisible();

    const fresh = await (await browser.newContext()).newPage();
    await fresh.goto("/login");
    await fresh.getByLabel("Email").fill(tia.email);
    await fresh.getByLabel("Password").fill(tia.password);
    await fresh.getByRole("main").getByRole("button", { name: "Sign in" }).click();
    await expect(fresh.getByRole("button", { name: `Account: ${tia.username}` })).toBeVisible();
  });
});
