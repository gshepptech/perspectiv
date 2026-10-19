import { randomBytes } from "node:crypto";
import { join } from "node:path";

import { defineConfig, devices } from "@playwright/test";

import { stack, workDir } from "./e2e/support/env.ts";

// One id per run names this run's accounts. It is set here, in the runner
// process, before the stack and the workers start, and they inherit it.
process.env.E2E_RUN_ID ??= randomBytes(4).readUInt32BE(0).toString(36).padStart(6, "0").slice(-6);

const s = stack();

/**
 * Browser E2E: the lore journey and the admin report flow (`make e2e`).
 *
 * The suite needs the dev loop's Postgres and Mailpit containers (make
 * dev-up). It starts its own server, on its own freshly created database with
 * a shortened rank interval, and its own Vite; e2e/support/env.ts explains why.
 * E2E_WEB_PORT (default 15173) sets the base URL and E2E_HTTP_PORT (default
 * 18080) the server's port.
 */
export default defineConfig({
  testDir: "./e2e",
  outputDir: join(workDir, "test-results"),
  // Not yet proven order-independent: one worker, files in order.
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  // Generous: a sign-in hashes a password with bcrypt and every step is a
  // real round trip to Postgres, which is slow on a busy machine.
  timeout: 90_000,
  expect: { timeout: 20_000 },
  reporter: [["list"], ["html", { outputFolder: join(workDir, "playwright-report"), open: "never" }]],
  use: {
    baseURL: s.baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  projects: [
    { name: "setup", testMatch: /.*\.setup\.ts$/ },
    {
      name: "chromium",
      testMatch: /.*\.spec\.ts$/,
      use: { ...devices["Desktop Chrome"] },
      dependencies: ["setup"],
    },
  ],
  webServer: {
    command: "node e2e/support/stack.ts",
    url: `${s.baseURL}/healthz`,
    // The stack always starts fresh: it recreates the suite's database.
    reuseExistingServer: false,
    // Two Go builds, a migration and an import before the server listens.
    timeout: 300_000,
    stdout: "pipe",
    stderr: "pipe",
    gracefulShutdown: { signal: "SIGTERM", timeout: 10_000 },
  },
});
