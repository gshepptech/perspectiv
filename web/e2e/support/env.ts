/**
 * Settings for the E2E stack, shared by playwright.config.ts, the stack
 * launcher (stack.ts) and the specs.
 *
 * The suite runs against the dev loop's PostgreSQL and Mailpit containers
 * (make dev-up), but not against the dev loop's server: it builds and starts
 * its own perspectiv server and Vite on separate ports, on a database of its
 * own that is dropped and recreated for every run. Two reasons:
 *
 *  - The rank job is a River periodic job, and River runs periodic jobs only
 *    on the elected leader of each database. A second server on the dev
 *    database with a shorter interval might never be leader, so the suite's
 *    interval would not take effect. On its own database it is the only
 *    River client, and so the leader.
 *  - Sign-up rate limits and the admin report queue are database state. A
 *    fresh database makes every run start from the same empty state.
 *
 * Settings come from .env.dev, then .env.dev.local, then the process
 * environment, each overriding the one before. Under `make e2e`, scripts/dev.sh
 * has already exported both files, so the process environment holds the same
 * values; reading the files as well lets `pnpm exec playwright test` run
 * straight from web/.
 */
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const webDir = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
export const repoRoot = resolve(webDir, "..");
export const e2eDir = join(webDir, "e2e");
export const workDir = join(repoRoot, "tmp", "e2e");

/** Reads KEY=VALUE lines the way bash's `source` reads .env.dev: comments and blank lines skipped, one level of quotes removed. */
export function parseEnvFile(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const m = /^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=(.*)$/.exec(line);
    if (!m) continue;
    const key = m[1] as string;
    let value = (m[2] as string).trim();
    const quote = value[0];
    if ((quote === '"' || quote === "'") && value.endsWith(quote) && value.length >= 2) {
      value = value.slice(1, -1);
    } else {
      // An unquoted value ends at an inline comment, as in bash.
      value = value.replace(/\s+#.*$/, "");
    }
    out[key] = value;
  }
  return out;
}

function devEnv(): Record<string, string> {
  const merged: Record<string, string> = {};
  for (const name of [".env.dev", ".env.dev.local"]) {
    const path = join(repoRoot, name);
    if (existsSync(path)) Object.assign(merged, parseEnvFile(readFileSync(path, "utf8")));
  }
  for (const [k, v] of Object.entries(process.env)) {
    if (v !== undefined) merged[k] = v;
  }
  return merged;
}

function portFrom(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return Number.isInteger(n) && n > 0 && n < 65536 ? n : fallback;
}

/**
 * The rank interval the suite's server runs with, in seconds. Decision: the
 * suite shortens the production 60-second interval by starting its own server
 * with RANK_INTERVAL set to this value (default 2s, E2E_RANK_INTERVAL_SECONDS
 * to change it), rather than waiting out the dev loop's 60 seconds. The specs
 * wait for canon for at most canonWaitMs(), which is shorter than one
 * production interval, so a server left on the 60-second default fails the
 * journey instead of passing slowly.
 */
export function rankIntervalSeconds(env: Record<string, string | undefined> = process.env): number {
  const n = Number(env.E2E_RANK_INTERVAL_SECONDS ?? "2");
  if (!Number.isFinite(n) || n <= 0 || n > 10) {
    throw new Error(`E2E_RANK_INTERVAL_SECONDS must be a number of seconds above 0 and at most 10, got ${env.E2E_RANK_INTERVAL_SECONDS}`);
  }
  return n;
}

/** How long a spec waits for a rank pass to show: three intervals plus page loads, kept under one production interval. */
export function canonWaitMs(): number {
  return Math.min(rankIntervalSeconds() * 3_000 + 15_000, 45_000);
}

export type Stack = {
  /** The suite's Vite server, which proxies /api and /healthz to the suite's Go server. */
  baseURL: string;
  webPort: number;
  httpPort: number;
  /** DATABASE_URL of the suite's own database. */
  databaseURL: string;
  databaseName: string;
  /** The dev loop's Postgres container, used to drop and create the suite's database. */
  pgContainer: string;
  pgUser: string;
  mailpitURL: string;
  smtpAddr: string;
  binDir: string;
  logDir: string;
  authDir: string;
  /** Settings the suite's server is started with (on top of the inherited environment). */
  serverEnv: Record<string, string>;
};

export function stack(): Stack {
  const env = devEnv();
  const webPort = portFrom(env.E2E_WEB_PORT, 15173);
  const httpPort = portFrom(env.E2E_HTTP_PORT, 18080);
  const baseURL = `http://localhost:${webPort}`;
  const databaseName = env.E2E_DB_NAME ?? "perspectiv_e2e";
  if (!/^[a-z_][a-z0-9_]{0,62}$/.test(databaseName)) {
    throw new Error(`E2E_DB_NAME must be a lowercase Postgres identifier, got ${databaseName}`);
  }
  const devURL = env.DATABASE_URL;
  if (!devURL) throw new Error("DATABASE_URL is not set; is .env.dev present at the repository root?");
  const db = new URL(devURL);
  db.pathname = `/${databaseName}`;
  const mailpitURL = (env.MAILPIT_URL ?? "http://localhost:8025").replace(/\/+$/, "");
  const smtpAddr = env.SMTP_ADDR ?? "localhost:1025";
  const jwtSecret = env.JWT_SECRET ?? "";
  const binDir = join(workDir, "bin");
  return {
    baseURL,
    webPort,
    httpPort,
    databaseURL: db.toString(),
    databaseName,
    pgContainer: env.E2E_PG_CONTAINER ?? "perspectiv-postgres",
    pgUser: env.POSTGRES_USER ?? (decodeURIComponent(db.username) || "perspectiv"),
    mailpitURL,
    smtpAddr,
    binDir,
    logDir: join(workDir, "logs"),
    authDir: join(workDir, "auth"),
    serverEnv: {
      ROLE: "all",
      DATABASE_URL: db.toString(),
      HTTP_ADDR: `:${httpPort}`,
      // Emailed links must open the suite's own web server.
      PUBLIC_BASE_URL: baseURL,
      JWT_SECRET: jwtSecret,
      COOKIE_SECURE: "false",
      MAIL_PROVIDER: "smtp",
      SMTP_ADDR: smtpAddr,
      MAIL_FROM: env.MAIL_FROM ?? "Perspectiv <no-reply@perspectiv.local>",
      RESEND_API_KEY: "",
      RANK_INTERVAL: `${rankIntervalSeconds(env)}s`,
      // The journey builds exactly the 4-member lore the canon rule needs.
      CANON_MIN_MEMBERS: "4",
      EXPIRY_WINDOW: "168h",
      AVATAR_DIR: join(workDir, "avatars"),
      MIGRATE_ON_START: "false",
      LOG_LEVEL: env.E2E_LOG_LEVEL ?? "info",
      OTEL_EXPORTER_OTLP_ENDPOINT: "",
    },
  };
}

/** The suite's perspectiv binary, built by stack.ts. */
export function perspectivBin(s: Stack = stack()): string {
  return join(s.binDir, "perspectiv");
}

export function placectlBin(s: Stack = stack()): string {
  return join(s.binDir, "placectl");
}
