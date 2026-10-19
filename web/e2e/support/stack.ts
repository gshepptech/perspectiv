/**
 * Starts the stack the E2E suite runs against; playwright.config.ts runs it as
 * the webServer command (node runs this TypeScript file directly).
 *
 *  1. checks the dev loop's Postgres and Mailpit containers answer;
 *  2. builds the perspectiv and placectl binaries into tmp/e2e/bin;
 *  3. drops and recreates the suite's database, migrates it with
 *     `perspectiv migrate` and imports the place fixtures with placectl;
 *  4. runs `perspectiv` (ROLE=all, the shortened rank interval) and Vite,
 *     proxying to it, until Playwright stops this process.
 *
 * Server and Vite output goes to tmp/e2e/logs. If either child exits early,
 * this process exits non-zero so Playwright reports the stack as down rather
 * than timing out.
 */
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { existsSync, mkdirSync, openSync } from "node:fs";
import { join } from "node:path";

import { e2eDir, perspectivBin, placectlBin, repoRoot, stack, webDir, workDir } from "./env.ts";

const s = stack();
const children: ChildProcess[] = [];

function fail(message: string): never {
  console.error(`e2e stack: ${message}`);
  stopChildren();
  process.exit(1);
}

function stopChildren(): void {
  for (const child of children) {
    if (child.exitCode === null && child.signalCode === null) child.kill("SIGTERM");
  }
}

/** Runs a command to completion with output on this process's stderr; fails the stack on a non-zero exit. */
function run(what: string, cmd: string, args: string[], opts: { cwd?: string; env?: Record<string, string> } = {}): void {
  console.error(`e2e stack: ${what}`);
  const res = spawnSync(cmd, args, {
    cwd: opts.cwd ?? repoRoot,
    env: { ...process.env, ...opts.env },
    stdio: ["ignore", "inherit", "inherit"],
  });
  if (res.error) fail(`${what}: ${res.error.message}`);
  if (res.status !== 0) fail(`${what} exited with ${res.status ?? res.signal}`);
}

function psql(sql: string): void {
  run(`psql: ${sql}`, "docker", [
    "exec",
    s.pgContainer,
    "psql",
    "-v",
    "ON_ERROR_STOP=1",
    "-q",
    "-U",
    s.pgUser,
    "-d",
    "postgres",
    "-c",
    sql,
  ]);
}

async function preflight(): Promise<void> {
  const ready = spawnSync("docker", ["exec", s.pgContainer, "pg_isready", "-q", "-U", s.pgUser], { stdio: "ignore" });
  if (ready.error || ready.status !== 0) {
    fail(`the Postgres container ${s.pgContainer} is not ready; run make dev-up (or set E2E_PG_CONTAINER)`);
  }
  try {
    const res = await fetch(`${s.mailpitURL}/api/v1/info`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  } catch (err) {
    fail(`Mailpit is not answering at ${s.mailpitURL} (${String(err)}); run make dev-up or set MAILPIT_URL`);
  }
}

function build(): void {
  // The server embeds web/dist; the dev loop needs the same bundle, so it is
  // only built here when it is missing.
  if (!existsSync(join(webDir, "dist", "index.html"))) {
    run("building web/dist (missing)", "pnpm", ["--dir", webDir, "run", "build"]);
  }
  mkdirSync(s.binDir, { recursive: true });
  run("building perspectiv", "go", ["build", "-o", perspectivBin(s), "./cmd/perspectiv"], { env: { CGO_ENABLED: "0" } });
  run("building placectl", "go", ["build", "-o", placectlBin(s), "./cmd/placectl"], { env: { CGO_ENABLED: "0" } });
}

function resetDatabase(): void {
  psql(`DROP DATABASE IF EXISTS "${s.databaseName}" WITH (FORCE)`);
  psql(`CREATE DATABASE "${s.databaseName}"`);
  run("migrating the suite's database", perspectivBin(s), ["migrate"], { env: { DATABASE_URL: s.databaseURL, LOG_LEVEL: "warn" } });
  run(
    "importing place fixtures",
    placectlBin(s),
    [
      "import",
      "--divisions",
      join(e2eDir, "fixtures", "divisions.ndjson"),
      "--places",
      join(e2eDir, "fixtures", "places.ndjson"),
    ],
    { env: { DATABASE_URL: s.databaseURL, LOG_LEVEL: "warn" } },
  );
}

function start(name: string, cmd: string, args: string[], env: Record<string, string>, cwd: string): void {
  mkdirSync(s.logDir, { recursive: true });
  const log = join(s.logDir, `${name}.log`);
  const fd = openSync(log, "w");
  const child = spawn(cmd, args, { cwd, env: { ...process.env, ...env }, stdio: ["ignore", fd, fd] });
  children.push(child);
  console.error(`e2e stack: ${name} pid ${child.pid}, log ${log}`);
  child.on("exit", (code, signal) => {
    if (stopping) return;
    fail(`${name} exited early (${code ?? signal}); see ${log}`);
  });
}

let stopping = false;
for (const sig of ["SIGTERM", "SIGINT"] as const) {
  process.on(sig, () => {
    stopping = true;
    stopChildren();
    process.exit(0);
  });
}

await preflight();
mkdirSync(workDir, { recursive: true });
build();
resetDatabase();
start("server", perspectivBin(s), [], s.serverEnv, repoRoot);
// vite.config.ts reads HTTP_ADDR for its proxy target and WEB_PORT for its port.
start("web", join(webDir, "node_modules", ".bin", "vite"), [], { HTTP_ADDR: `:${s.httpPort}`, WEB_PORT: String(s.webPort) }, webDir);
console.error(`e2e stack: web ${s.baseURL}, server :${s.httpPort}, database ${s.databaseName}, rank interval ${s.serverEnv.RANK_INTERVAL}`);
