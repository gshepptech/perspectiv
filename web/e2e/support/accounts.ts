/**
 * The accounts the suite signs up, one set per run. The setup project signs
 * each one up, verifies its email through Mailpit and saves its signed-in
 * browser state; the specs open a context from that state.
 *
 * Refresh tokens rotate on every use and a reused one revokes the session, so
 * a saved state is good for exactly one browser context. Each account is
 * therefore used by one spec only, and the lore journey and the report flow
 * have their own accounts.
 */
import { join } from "node:path";

import { stack } from "./env.ts";

export type Account = {
  key: string;
  username: string;
  email: string;
  password: string;
  /** Granted the ADMIN role with `perspectiv admin grant` before signing in. */
  admin: boolean;
  storageState: string;
};

/**
 * Six base-36 characters set once per run by playwright.config.ts and
 * inherited by the setup and test workers, so every process names the same
 * accounts. Usernames stay within the 20-character limit.
 */
export function runId(): string {
  const id = process.env.E2E_RUN_ID;
  if (!id || !/^[a-z0-9]{1,8}$/.test(id)) {
    throw new Error("E2E_RUN_ID is not set; run the suite through playwright.config.ts");
  }
  return id;
}

function account(key: string, admin = false): Account {
  const id = runId();
  return {
    key,
    username: `${key}_${id}`,
    email: `${key}.${id}@e2e.perspectiv.test`,
    password: `Lantern ${id} harbor walk`,
    admin,
    storageState: join(stack().authDir, `${key}.json`),
  };
}

/** The four strangers of the lore journey. */
export const loreAccounts = () => ({
  ana: account("ana"),
  ben: account("ben"),
  cal: account("cal"),
  dee: account("dee"),
});

/** The writer, the reader who reports and the admin of the report flow. */
export const reportAccounts = () => ({
  writer: account("wren"),
  reporter: account("remy"),
  admin: account("adm", true),
});

export function allAccounts(): Account[] {
  return [...Object.values(loreAccounts()), ...Object.values(reportAccounts())];
}
