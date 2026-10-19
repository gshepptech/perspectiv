import { QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { beforeAll, expect, vi } from "vitest";

import { createQueryClient } from "@/app/queryClient";
import { buildRoutes, globPageLoader, pages, type PageLoader, type RouteModule } from "@/app/router";

/**
 * The account routes' test harness. It renders a path through the real route
 * table (lazy module loading, the signed-in guard, the shell) and answers the
 * Connect requests the real transport sends through globalThis.fetch, keeping
 * each request's decoded JSON body so a test can assert what the page sent.
 * Only tests import it.
 */
export type Call = { procedure: string; body: Record<string, unknown>; authorization: string | null };
export type Reply = { status?: number; body: unknown };
export type Handler = (body: Record<string, unknown>, call: Call) => Reply | Promise<Reply>;
export type Handlers = Record<string, Handler>;

const statusFor: Record<string, number> = {
  invalid_argument: 400,
  failed_precondition: 400,
  unauthenticated: 401,
  permission_denied: 403,
  not_found: 404,
  already_exists: 409,
  resource_exhausted: 429,
  unavailable: 503,
};

export function fail(code: keyof typeof statusFor | "internal", message: string = code): Reply {
  return { status: statusFor[code] ?? 500, body: { code, message } };
}

export const ok = (body: unknown = {}): Reply => ({ body });

export const USER_ID = "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10";

export function sessionBody(opts: { token?: string; username?: string; emailVerified?: boolean } = {}) {
  return {
    accessToken: opts.token ?? "tok",
    accessTokenExpiresAt: new Date(Date.now() + 600_000).toISOString(),
    user: {
      userId: USER_ID,
      username: opts.username ?? "wren",
      role: "ROLE_USER",
      emailVerified: opts.emailVerified ?? true,
    },
  };
}

function decode(body: BodyInit | null | undefined): Record<string, unknown> {
  if (body == null) return {};
  const text = typeof body === "string" ? body : new TextDecoder().decode(body as Uint8Array);
  return text ? (JSON.parse(text) as Record<string, unknown>) : {};
}

export function installApi(handlers: Handlers) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
      const prefix = "/api/perspectiv.v1.";
      const at = url.indexOf(prefix);
      if (at < 0) throw new Error(`unexpected fetch ${url}`);
      const call: Call = {
        procedure: url.slice(at + prefix.length),
        body: decode(init?.body),
        authorization: new Headers(init?.headers).get("Authorization"),
      };
      calls.push(call);
      const handler = handlers[call.procedure];
      const reply = handler ? await handler(call.body, call) : fail("internal", `no fake for ${call.procedure}`);
      return new Response(JSON.stringify(reply.body), {
        status: reply.status ?? 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  return {
    calls,
    /** Bodies sent to one procedure, in order. */
    sent: (procedure: string) => calls.filter((c) => c.procedure === procedure).map((c) => c.body),
  };
}

export type RenderOptions = { signedIn?: boolean; emailVerified?: boolean; username?: string };

// The account routes' page modules, loaded once per test file before any test
// runs. The first import of a page is a cold Vite transform of the page and
// everything it imports, which on a loaded host takes longer than a findBy
// query's 1000 ms; done inside a test, that cost raced the test's first
// assertion. Done in a beforeAll with its own timeout, every test renders a
// page whose module is already in memory.
const ACCOUNT_ROUTE = /^(auth|settings|inbox)\//;
const PRELOAD_TIMEOUT_MS = 120_000;
const loaded = new Map<string, RouteModule>();

export function preloadAccountRoutes() {
  beforeAll(async () => {
    const modules = pages.map((p) => p.module).filter((m) => ACCOUNT_ROUTE.test(m));
    await Promise.all(modules.map(async (m) => loaded.set(m, await globPageLoader(m))));
  }, PRELOAD_TIMEOUT_MS);
}

const harnessLoader: PageLoader = (module) => {
  const mod = loaded.get(module);
  return mod ? Promise.resolve(mod) : globPageLoader(module);
};

export function renderRoute(path: string, handlers: Handlers = {}, opts: RenderOptions = {}) {
  const refresh: Handler = opts.signedIn
    ? () => ok(sessionBody({ username: opts.username, emailVerified: opts.emailVerified }))
    : () => fail("unauthenticated");
  const api = installApi({ "AuthService/Refresh": refresh, ...handlers });
  const router = createMemoryRouter(buildRoutes(harnessLoader), { initialEntries: [path] });
  const client = createQueryClient();
  client.setDefaultOptions({ queries: { ...client.getDefaultOptions().queries, retry: false } });
  const user = userEvent.setup();
  const view = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { api, router, user, client, view };
}

// The phone-width check. jsdom does no layout, so it cannot measure
// scrollWidth; instead it walks the rendered page and rejects what makes a
// page wider than a 375px viewport less its two 16px gutters (343px): a fixed
// width or min-width over 343px, in a Tailwind class or an inline style, and
// text that is forbidden to wrap without being clipped. The real measurement
// at 375px runs in the Playwright suite against the dev loop.
const PHONE_COLUMN_PX = 375 - 32;

function fixedPx(cls: string): number | null {
  const arbitrary = /^(?:min-)?w-\[(\d+(?:\.\d+)?)(px|rem)\]$/.exec(cls);
  if (arbitrary) return Number(arbitrary[1]) * (arbitrary[2] === "rem" ? 16 : 1);
  const scale = /^(?:min-)?w-(\d+(?:\.\d+)?)$/.exec(cls);
  if (scale) return Number(scale[1]) * 4;
  return null;
}

export function expectFitsPhone(root: HTMLElement) {
  const offenders: string[] = [];
  for (const el of [root, ...root.querySelectorAll<HTMLElement>("*")]) {
    const classes = typeof el.className === "string" ? el.className.split(/\s+/) : [];
    for (const cls of classes) {
      const px = fixedPx(cls);
      if (px !== null && px > PHONE_COLUMN_PX) offenders.push(`<${el.tagName.toLowerCase()} class="${cls}">`);
    }
    if (classes.includes("whitespace-nowrap") && !classes.includes("truncate") && !classes.some((c) => c.startsWith("overflow-")))
      offenders.push(`<${el.tagName.toLowerCase()}> nowrap without overflow`);
    for (const prop of ["width", "minWidth"] as const) {
      const v = el.style[prop];
      const m = /^(\d+(?:\.\d+)?)px$/.exec(v);
      if (m && Number(m[1]) > PHONE_COLUMN_PX) offenders.push(`<${el.tagName.toLowerCase()} style="${prop}:${v}">`);
    }
    if (el.tagName === "TABLE" && !el.parentElement?.className.includes("overflow-x-auto"))
      offenders.push("<table> outside an overflow-x-auto wrapper");
  }
  expect(offenders).toEqual([]);
}
