import { QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { vi } from "vitest";

import { createQueryClient } from "@/app/queryClient";
import { buildRoutes } from "@/app/router";
import { preloadedPageLoader, preloadPages } from "@/app/routes.testutil";

// Every test file that renders routes through this harness loads the pages
// first, so no test races a cold module transform.
preloadPages();

/**
 * A stand-in for the Go server at the HTTP boundary for the writing routes'
 * tests. It answers the Connect protocol JSON the real transport sends through
 * globalThis.fetch and records each request body, so a test can assert what
 * the page actually sent. Only tests import it.
 */
export type ApiCall = { procedure: string; body: Record<string, unknown> };
export type ApiReply = { status?: number; body: unknown };
export type ApiHandler = (body: Record<string, unknown>, call: ApiCall) => ApiReply | Promise<ApiReply>;

const httpStatus: Record<string, number> = {
  invalid_argument: 400,
  failed_precondition: 400,
  unauthenticated: 401,
  permission_denied: 403,
  not_found: 404,
  already_exists: 409,
  resource_exhausted: 429,
  unavailable: 503,
};

export function apiError(code: string, message = code): ApiReply {
  return { status: httpStatus[code] ?? 500, body: { code, message } };
}

export const ok = (body: unknown = {}): ApiReply => ({ body });

export const userId = "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10";

function sessionBody(role: string, username: string) {
  return {
    accessToken: "tok",
    accessTokenExpiresAt: new Date(Date.now() + 3_600_000).toISOString(),
    user: { userId, username, role, emailVerified: true },
  };
}

function decode(body: BodyInit | null | undefined): Record<string, unknown> {
  if (body == null) return {};
  const text = typeof body === "string" ? body : new TextDecoder().decode(body as Uint8Array);
  return text ? (JSON.parse(text) as Record<string, unknown>) : {};
}

export function installApi(routes: Record<string, ApiHandler>, opts: { role?: string; username?: string } = {}) {
  const calls: ApiCall[] = [];
  const all: Record<string, ApiHandler> = {
    "AuthService/Refresh": () => ok(sessionBody(opts.role ?? "ROLE_USER", opts.username ?? "wren")),
    ...routes,
  };
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const prefix = "/api/perspectiv.v1.";
    const at = url.indexOf(prefix);
    if (at < 0) throw new Error(`unexpected fetch ${url}`);
    const call = { procedure: url.slice(at + prefix.length), body: decode(init?.body) };
    calls.push(call);
    const handler = all[call.procedure];
    const reply = handler ? await handler(call.body, call) : apiError("unimplemented", `no fake for ${call.procedure}`);
    return new Response(JSON.stringify(reply.body), {
      status: reply.status ?? 200,
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
  const callsTo = (procedure: string) => calls.filter((c) => c.procedure === procedure);
  return { calls, callsTo };
}

/** Renders the real route table (the same lazy modules the app loads) at a path. */
export function renderRoute(path: string | { pathname: string; state?: unknown }) {
  const router = createMemoryRouter(buildRoutes(preloadedPageLoader), { initialEntries: [path] });
  render(
    <QueryClientProvider client={createQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

/**
 * jsdom lacks the pointer-capture and scrolling methods Radix Select calls
 * while it opens; these no-ops let a test drive the real component.
 */
export function polyfillRadix(): void {
  const proto = Element.prototype as unknown as Record<string, unknown>;
  proto.hasPointerCapture ??= () => false;
  proto.setPointerCapture ??= () => undefined;
  proto.releasePointerCapture ??= () => undefined;
  proto.scrollIntoView ??= () => undefined;
}
