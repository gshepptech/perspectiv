/**
 * The setup project: signs every account of this run up through the sign-up
 * page, follows the verification link the server emailed (read from Mailpit's
 * API), grants the admin its role with `perspectiv admin grant`, signs in and
 * saves the browser state the specs start from.
 */
import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";

import { expect, test as setup } from "@playwright/test";

import { allAccounts } from "./support/accounts.ts";
import { perspectivBin, stack } from "./support/env.ts";
import { verificationLink } from "./support/mailpit.ts";

for (const account of allAccounts()) {
  setup(`sign up ${account.key}, verify the email from Mailpit and sign in`, async ({ page }) => {
    await page.goto("/signup");
    await page.getByLabel("Email").fill(account.email);
    await page.getByLabel("Username").fill(account.username);
    await page.getByLabel("Password").fill(account.password);
    await page.getByLabel("I am 18 or over, and I agree to the rules above.").check();
    await page.getByRole("button", { name: "Sign up" }).click();
    await expect(page.getByRole("heading", { name: "Check your email" })).toBeVisible();

    // The link is built from PUBLIC_BASE_URL, which the suite's server has set
    // to the suite's own web server.
    const link = await verificationLink(account.email);
    expect(link.startsWith(`${stack().baseURL}/verify-email?token=`)).toBe(true);
    await page.goto(link);
    await expect(page.getByRole("heading", { name: "Your email is verified" })).toBeVisible();

    // A link is single use: following it again is refused.
    await page.goto(link);
    await expect(page.getByRole("heading", { name: "This link no longer works" })).toBeVisible();

    if (account.admin) {
      // The production binary path: the CLI an operator runs to make an admin.
      execFileSync(perspectivBin(), ["admin", "grant", account.username], {
        env: { ...process.env, DATABASE_URL: stack().databaseURL, LOG_LEVEL: "warn" },
        stdio: ["ignore", "inherit", "inherit"],
      });
    }

    await page.getByRole("link", { name: "Sign in" }).first().click();
    await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
    await page.getByLabel("Email").fill(account.email);
    await page.getByLabel("Password").fill(account.password);
    await page.getByRole("main").getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByRole("button", { name: `Account: ${account.username}` })).toBeVisible();

    mkdirSync(stack().authDir, { recursive: true });
    await page.context().storageState({ path: account.storageState });
  });
}
